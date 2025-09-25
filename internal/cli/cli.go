package cli

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"local-ssl/internal/config"
	"local-ssl/internal/server"
	"local-ssl/internal/util"
)

// Execute runs the CLI.
func Execute() error {
	root := &cobra.Command{
		Use:   "devlink",
		Short: "Devlink .localhost development proxy",
	}

	var configPath string
	root.PersistentFlags().StringVar(&configPath, "config", "", "path to configuration file")

	root.AddCommand(newServeCommand(&configPath))
	root.AddCommand(newAddCommand(&configPath))
	root.AddCommand(newListCommand(&configPath))
	root.AddCommand(newRemoveCommand(&configPath))

	return root.Execute()
}

func resolveConfigPath(flag *string) string {
	if flag != nil && *flag != "" {
		return *flag
	}
	path := util.ConfigPath()
	if flag != nil {
		*flag = path
	}
	return path
}

func newServeCommand(configPath *string) *cobra.Command {
	var httpPort int
	var httpsPort int
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTPS reverse proxy",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := resolveConfigPath(configPath)
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
				if err := config.Save(path, config.New()); err != nil {
					return fmt.Errorf("initialize config: %w", err)
				}
			}
			opts := server.Options{
				ConfigPath: path,
				StateDir:   util.StateDir(),
				HTTPPort:   httpPort,
				HTTPSPort:  httpsPort,
			}
			srv, err := server.New(opts)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			defer signal.Stop(sigCh)

			go func() {
				<-sigCh
				cancel()
			}()

			log.Println("devlink proxy starting")
			return srv.Run(ctx)
		},
	}
	cmd.Flags().IntVar(&httpPort, "http-port", 80, "port for HTTP->HTTPS redirect")
	cmd.Flags().IntVar(&httpsPort, "https-port", 443, "port for HTTPS proxy")
	return cmd
}

type addOptions struct {
	domains       []string
	front         string
	backend       string
	backendPrefix string
	routes        []string
}

func newAddCommand(configPath *string) *cobra.Command {
	opts := &addOptions{}
	cmd := &cobra.Command{
		Use:   "add <project>",
		Short: "Add or update a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if name == "" {
				return errors.New("project name required")
			}
			path := resolveConfigPath(configPath)
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			proj := &config.Project{}
			if existing, ok := cfg.Projects[name]; ok {
				proj = existing
			}
			if len(opts.domains) > 0 {
				proj.Domains = opts.domains
			}
			if len(proj.Domains) == 0 {
				return errors.New("at least one --domain is required")
			}
			routes := []*config.Route{}
			if opts.front != "" {
				routes = append(routes, &config.Route{
					Path:        "/",
					Upstream:    opts.front,
					SpaFallback: true,
				})
			}
			if opts.backend != "" {
				prefix := opts.backendPrefix
				if prefix == "" {
					prefix = "/api"
				}
				strip := true
				routes = append(routes, &config.Route{
					Path:            prefix,
					Upstream:        opts.backend,
					StripPathPrefix: &strip,
				})
			}
			for _, raw := range opts.routes {
				route, err := parseRouteFlag(raw)
				if err != nil {
					return err
				}
				routes = append(routes, route)
			}
			if len(routes) > 0 {
				proj.Routes = routes
			}
			if err := validateProject(proj); err != nil {
				return err
			}
			cfg.Projects[name] = proj
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "project %s saved\n", name)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&opts.domains, "domain", nil, "domain(s) for the project (must end with .localhost)")
	cmd.Flags().StringVar(&opts.front, "front", "", "frontend upstream URL")
	cmd.Flags().StringVar(&opts.backend, "backend", "", "backend upstream URL")
	cmd.Flags().StringVar(&opts.backendPrefix, "backend-prefix", "/api", "default backend route prefix")
	cmd.Flags().StringArrayVar(&opts.routes, "route", nil, "additional route in form <path>=<upstream>")
	return cmd
}

func parseRouteFlag(value string) (*config.Route, error) {
	segments := strings.Split(value, ";")
	if len(segments) == 0 {
		return nil, errors.New("empty route value")
	}
	parts := strings.SplitN(strings.TrimSpace(segments[0]), "=", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid route %q", value)
	}
	path := strings.TrimSpace(parts[0])
	upstream := strings.TrimSpace(parts[1])
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("route path must start with '/' (got %s)", path)
	}
	if _, err := url.Parse(upstream); err != nil {
		return nil, fmt.Errorf("invalid upstream %s: %w", upstream, err)
	}
	strip := true
	route := &config.Route{Path: path, Upstream: upstream, StripPathPrefix: &strip}
	for _, opt := range segments[1:] {
		opt = strings.TrimSpace(opt)
		if opt == "" {
			continue
		}
		switch strings.ToLower(opt) {
		case "strip":
			strip = true
			route.StripPathPrefix = &strip
		case "keep":
			strip = false
			route.StripPathPrefix = &strip
		case "spa", "spafallback":
			route.SpaFallback = true
		case "ws", "websocket":
			route.Websocket = true
		default:
			return nil, fmt.Errorf("unknown route option %s", opt)
		}
	}
	return route, nil
}

func validateProject(proj *config.Project) error {
	if len(proj.Domains) == 0 {
		return errors.New("project requires at least one domain")
	}
	for _, domain := range proj.Domains {
		if !strings.HasSuffix(strings.ToLower(domain), ".localhost") {
			return fmt.Errorf("domain %s must end with .localhost", domain)
		}
	}
	if len(proj.Routes) == 0 {
		return errors.New("project requires at least one route")
	}
	return nil
}

func newListCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := resolveConfigPath(configPath)
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			if len(cfg.Projects) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no projects configured")
				return nil
			}
			for name, proj := range cfg.Projects {
				fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", name)
				fmt.Fprintf(cmd.OutOrStdout(), "  domains: %s\n", strings.Join(proj.Domains, ", "))
				for _, route := range proj.Routes {
					strip := true
					if route.StripPathPrefix != nil {
						strip = *route.StripPathPrefix
					}
					fmt.Fprintf(cmd.OutOrStdout(), "  route %s -> %s (strip=%t)\n", route.Path, route.Upstream, strip)
				}
			}
			return nil
		},
	}
}

func newRemoveCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <project>",
		Short: "Remove a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			path := resolveConfigPath(configPath)
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			if _, ok := cfg.Projects[name]; !ok {
				return fmt.Errorf("project %s not found", name)
			}
			delete(cfg.Projects, name)
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "project %s removed\n", name)
			return nil
		},
	}
}
