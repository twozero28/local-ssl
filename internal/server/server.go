package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"local-ssl/internal/certs"
	"local-ssl/internal/config"
)

// Options configure the behaviour of the reverse proxy server.
type Options struct {
	ConfigPath string
	StateDir   string
	HTTPPort   int
	HTTPSPort  int
}

// Server orchestrates the TLS proxy for .localhost domains.
type Server struct {
	opts      Options
	watcher   *fsnotify.Watcher
	mu        sync.RWMutex
	routers   map[string]*domainRouter
	tlsConfig *tls.Config
}

// New creates a new server and loads initial configuration.
func New(opts Options) (*Server, error) {
	if opts.ConfigPath == "" {
		return nil, errors.New("config path is required")
	}
	if opts.HTTPPort == 0 {
		opts.HTTPPort = 80
	}
	if opts.HTTPSPort == 0 {
		opts.HTTPSPort = 443
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}

	mgr, err := certs.NewManager(opts.StateDir)
	if err != nil {
		watcher.Close()
		return nil, err
	}

	tlsCert, err := mgr.EnsureCertificate()
	if err != nil {
		watcher.Close()
		return nil, err
	}

	s := &Server{
		opts:    opts,
		watcher: watcher,
		routers: map[string]*domainRouter{},
		tlsConfig: &tls.Config{
			Certificates: []tls.Certificate{*tlsCert},
			MinVersion:   tls.VersionTLS12,
		},
	}

	if err := s.reload(); err != nil {
		watcher.Close()
		return nil, err
	}

	if err := watcher.Add(opts.ConfigPath); err != nil {
		log.Printf("watch: %v", err)
	}

	return s, nil
}

// Run starts the HTTP and HTTPS servers until the context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.opts.HTTPPort),
		Handler:      http.HandlerFunc(s.redirectToHTTPS),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  2 * time.Minute,
	}

	httpsServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.opts.HTTPSPort),
		Handler:      http.HandlerFunc(s.handleHTTPS),
		TLSConfig:    s.tlsConfig,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  2 * time.Minute,
	}

	errCh := make(chan error, 2)

	go func() {
		log.Printf("HTTP redirect server listening on %s", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()

	go func() {
		log.Printf("HTTPS proxy server listening on %s", httpsServer.Addr)
		if err := httpsServer.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("https server: %w", err)
		}
	}()

	go s.watchLoop(ctx)

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	_ = httpsServer.Shutdown(shutdownCtx)

	return nil
}

func (s *Server) watchLoop(ctx context.Context) {
	defer s.watcher.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-s.watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				if err := s.reload(); err != nil {
					log.Printf("reload error: %v", err)
				}
			}
		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("watch error: %v", err)
		}
	}
}

func (s *Server) redirectToHTTPS(w http.ResponseWriter, r *http.Request) {
	target := fmt.Sprintf("https://%s%s", hostWithoutPort(r.Host, s.opts.HTTPPort, s.opts.HTTPSPort), r.URL.RequestURI())
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

func (s *Server) handleHTTPS(w http.ResponseWriter, r *http.Request) {
	host := strings.ToLower(hostOnly(r.Host))
	if host == "" {
		http.Error(w, "missing host", http.StatusBadRequest)
		return
	}

	router := s.lookupRouter(host)
	if router == nil {
		http.Error(w, "unknown domain", http.StatusBadGateway)
		return
	}

	router.ServeHTTP(w, r)
}

func (s *Server) lookupRouter(host string) *domainRouter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if router, ok := s.routers[host]; ok {
		return router
	}
	return nil
}

func (s *Server) reload() error {
	cfg, err := config.Load(s.opts.ConfigPath)
	if err != nil {
		return err
	}
	routers, err := buildRouters(cfg)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.routers = routers
	s.mu.Unlock()
	log.Printf("configuration reloaded: %d project(s)", len(cfg.Projects))
	return nil
}

func buildRouters(cfg *config.Config) (map[string]*domainRouter, error) {
	routers := map[string]*domainRouter{}
	for name, project := range cfg.Projects {
		if len(project.Domains) == 0 {
			return nil, fmt.Errorf("project %s has no domains", name)
		}
		dr, err := newDomainRouter(project)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", name, err)
		}
		for _, domain := range project.Domains {
			normalized := strings.ToLower(hostOnly(domain))
			if !strings.HasSuffix(normalized, ".localhost") {
				return nil, fmt.Errorf("project %s: domain %s is not a .localhost domain", name, domain)
			}
			routers[normalized] = dr
		}
	}
	return routers, nil
}

// domainRouter handles routing for a single domain.
type domainRouter struct {
	routes   []*runtimeRoute
	fallback *runtimeRoute
}

func newDomainRouter(project *config.Project) (*domainRouter, error) {
	if len(project.Routes) == 0 {
		return nil, errors.New("project has no routes")
	}
	dr := &domainRouter{}
	for _, r := range project.Routes {
		runtime, err := buildRuntimeRoute(r)
		if err != nil {
			return nil, err
		}
		if r.Path == "/" && r.SpaFallback {
			dr.fallback = runtime
		}
		dr.routes = append(dr.routes, runtime)
	}
	sort.Slice(dr.routes, func(i, j int) bool {
		li := len(dr.routes[i].path)
		lj := len(dr.routes[j].path)
		if li == lj {
			return dr.routes[i].path < dr.routes[j].path
		}
		return li > lj
	})
	return dr, nil
}

func (dr *domainRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	originalPath := r.URL.Path
	route := dr.match(originalPath)
	fallbackTriggered := false
	if route == nil && dr.fallback != nil && acceptsHTML(r) {
		fallbackTriggered = true
		route = dr.fallback
		r.Header.Set("X-Devlink-Original-Path", originalPath)
		r.URL.Path = "/"
		r.URL.RawPath = ""
	}
	if route == nil {
		http.Error(w, "no matching route", http.StatusBadGateway)
		return
	}
	route.serveHTTP(w, r, fallbackTriggered)
}

func (dr *domainRouter) match(path string) *runtimeRoute {
	for _, route := range dr.routes {
		if route.matches(path) {
			return route
		}
	}
	return nil
}

// runtimeRoute is an executable route entry.
type runtimeRoute struct {
	path        string
	stripPrefix bool
	spaFallback bool
	proxy       *httputil.ReverseProxy
}

func buildRuntimeRoute(r *config.Route) (*runtimeRoute, error) {
	if r.Path == "" || !strings.HasPrefix(r.Path, "/") {
		return nil, fmt.Errorf("invalid path %q", r.Path)
	}
	upstreamURL, err := url.Parse(r.Upstream)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream for path %s: %w", r.Path, err)
	}

	switch upstreamURL.Scheme {
	case "http", "https":
	case "ws":
		upstreamURL.Scheme = "http"
	case "wss":
		upstreamURL.Scheme = "https"
	default:
		return nil, fmt.Errorf("unsupported scheme %s", upstreamURL.Scheme)
	}

	strip := true
	if r.StripPathPrefix != nil {
		strip = *r.StripPathPrefix
	}

	proxy := httputil.NewSingleHostReverseProxy(upstreamURL)
	originalDirector := proxy.Director
	pathPrefix := r.Path
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Header.Set("X-Forwarded-Proto", "https")
		originalHost := req.Header.Get("X-Original-Host")
		if originalHost == "" {
			originalHost = req.Host
		}
		req.Header.Set("X-Forwarded-Host", originalHost)
		req.Host = upstreamURL.Host
		rewritePath(req, pathPrefix, strip)
	}
	proxy.ModifyResponse = sanitizeResponseCookies
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("proxy error for %s via %s: %v", r.URL.Path, upstreamURL, err)
		http.Error(w, "upstream error", http.StatusBadGateway)
	}

	return &runtimeRoute{
		path:        pathPrefix,
		stripPrefix: strip,
		spaFallback: r.SpaFallback,
		proxy:       proxy,
	}, nil
}

func (rt *runtimeRoute) matches(path string) bool {
	if rt.path == "/" {
		return true
	}
	if !strings.HasPrefix(path, rt.path) {
		return false
	}
	if len(path) == len(rt.path) {
		return true
	}
	if rt.path[len(rt.path)-1] == '/' {
		return true
	}
	return path[len(rt.path)] == '/'
}

func (rt *runtimeRoute) serveHTTP(w http.ResponseWriter, r *http.Request, fallback bool) {
	if fallback && rt.spaFallback {
		r.URL.Path = "/"
		r.URL.RawPath = ""
	}
	r.Header.Set("X-Original-Host", hostOnly(r.Host))
	rt.proxy.ServeHTTP(w, r)
}

func rewritePath(req *http.Request, prefix string, strip bool) {
	path := req.URL.Path
	if strip {
		if prefix == "/" {
			return
		}
		if strings.HasPrefix(path, prefix) {
			newPath := strings.TrimPrefix(path, prefix)
			if !strings.HasPrefix(newPath, "/") {
				newPath = "/" + newPath
			}
			if newPath == "" {
				newPath = "/"
			}
			req.URL.Path = newPath
			req.URL.RawPath = ""
		}
	}
}

func acceptsHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html") || accept == ""
}

func sanitizeResponseCookies(resp *http.Response) error {
	cookies := resp.Header.Values("Set-Cookie")
	if len(cookies) == 0 {
		return nil
	}
	host := hostOnly(resp.Request.Host)
	registrable := registrableLocalhost(host)
	resp.Header.Del("Set-Cookie")
	for _, raw := range cookies {
		resp.Header.Add("Set-Cookie", rewriteCookieHeader(raw, registrable))
	}
	return nil
}

func rewriteCookieHeader(raw, registrable string) string {
	if registrable == "" {
		return raw
	}
	segments := strings.Split(raw, ";")
	domainIdx := -1
	domainValue := ""
	for i, seg := range segments {
		trimmed := strings.TrimSpace(seg)
		if trimmed == "" {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "domain") {
			domainIdx = i
			domainValue = strings.TrimSpace(parts[1])
			break
		}
	}
	if domainIdx == -1 {
		return raw
	}
	normalized := strings.TrimPrefix(strings.ToLower(domainValue), ".")
	if normalized != "localhost" {
		return raw
	}

	replacement := fmt.Sprintf("Domain=%s", registrable)
	var builder strings.Builder
	for i, seg := range segments {
		trimmed := strings.TrimSpace(seg)
		if trimmed == "" {
			continue
		}
		if i == 0 {
			builder.WriteString(trimmed)
			continue
		}
		if i == domainIdx {
			builder.WriteString("; ")
			builder.WriteString(replacement)
			continue
		}
		builder.WriteString("; ")
		builder.WriteString(trimmed)
	}
	return builder.String()
}

func hostOnly(hostport string) string {
	if idx := strings.Index(hostport, ":"); idx != -1 {
		return hostport[:idx]
	}
	return hostport
}

func hostWithoutPort(host string, httpPort, httpsPort int) string {
	h := hostOnly(host)
	// Remove explicit HTTP port and replace with HTTPS port when necessary.
	if strings.Contains(host, ":") {
		parts := strings.Split(host, ":")
		if len(parts) == 2 {
			if port := parts[1]; port == fmt.Sprint(httpPort) {
				if httpsPort == 443 {
					return parts[0]
				}
				return fmt.Sprintf("%s:%d", parts[0], httpsPort)
			}
		}
	}
	if httpsPort != 443 {
		return fmt.Sprintf("%s:%d", h, httpsPort)
	}
	return h
}

func registrableLocalhost(host string) string {
	host = strings.ToLower(hostOnly(host))
	if !strings.HasSuffix(host, ".localhost") {
		return ""
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return ""
	}
	if len(labels) == 2 {
		return host
	}
	return strings.Join(labels[len(labels)-2:], ".")
}
