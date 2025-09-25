package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSanitizeResponseCookiesRewritesDomain(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://upstream.localhost/api", nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set("X-Original-Host", "api.first.localhost")

	resp := &http.Response{
		Header:  http.Header{"Set-Cookie": {"session=abc; Domain=localhost; Path=/"}},
		Request: req,
	}

	if err := sanitizeResponseCookies(resp); err != nil {
		t.Fatalf("sanitizeResponseCookies returned error: %v", err)
	}

	cookies := resp.Header.Values("Set-Cookie")
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	expected := "session=abc; Domain=first.localhost; Path=/"
	if cookies[0] != expected {
		t.Fatalf("expected %q, got %q", expected, cookies[0])
	}
}

func TestSanitizeResponseCookiesKeepsNonLocalhost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://upstream.localhost/api", nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set("X-Original-Host", "service.dev.internal")

	raw := "session=abc; Domain=localhost; Path=/"
	resp := &http.Response{
		Header:  http.Header{"Set-Cookie": {raw}},
		Request: req,
	}

	if err := sanitizeResponseCookies(resp); err != nil {
		t.Fatalf("sanitizeResponseCookies returned error: %v", err)
	}

	cookies := resp.Header.Values("Set-Cookie")
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if cookies[0] != raw {
		t.Fatalf("expected cookie to remain %q, got %q", raw, cookies[0])
	}
}

func TestSanitizeResponseCookiesNoDomainAttribute(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://upstream.localhost/api", nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set("X-Original-Host", "api.first.localhost")

	raw := "session=abc; Path=/"
	resp := &http.Response{
		Header:  http.Header{"Set-Cookie": {raw}},
		Request: req,
	}

	if err := sanitizeResponseCookies(resp); err != nil {
		t.Fatalf("sanitizeResponseCookies returned error: %v", err)
	}

	cookies := resp.Header.Values("Set-Cookie")
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if cookies[0] != raw {
		t.Fatalf("expected cookie to remain %q, got %q", raw, cookies[0])
	}
}
