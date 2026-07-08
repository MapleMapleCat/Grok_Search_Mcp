package grok

import (
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewHTTPClientWithProxyUsesExplicitProxy(t *testing.T) {
	client, err := newHTTPClientWithProxy(time.Second, " http://127.0.0.1:7890 ", true)
	if err != nil {
		t.Fatalf("newHTTPClientWithProxy failed: %v", err)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}

	request := &http.Request{URL: mustParseURL(t, "https://api.example.test/v1/responses")}
	actualProxyURL, err := transport.Proxy(request)
	if err != nil {
		t.Fatalf("resolve proxy: %v", err)
	}
	if actualProxyURL == nil || actualProxyURL.String() != "http://127.0.0.1:7890" {
		t.Fatalf("expected explicit proxy URL, got %v", actualProxyURL)
	}
}

func TestNewHTTPClientWithProxyFallsBackToEnvironment(t *testing.T) {
	client, err := newHTTPClientWithProxy(time.Second, "", false)
	if err != nil {
		t.Fatalf("newHTTPClientWithProxy failed: %v", err)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if transport.Proxy == nil {
		t.Fatalf("expected environment proxy resolver, got nil")
	}

	actualProxyFunctionPointer := reflect.ValueOf(transport.Proxy).Pointer()
	expectedProxyFunctionPointer := reflect.ValueOf(http.ProxyFromEnvironment).Pointer()
	if actualProxyFunctionPointer != expectedProxyFunctionPointer {
		t.Fatalf("expected http.ProxyFromEnvironment fallback")
	}
}

func TestNewHTTPClientWithProxyRejectsEnabledProxyWithoutURL(t *testing.T) {
	_, err := newHTTPClientWithProxy(time.Second, " ", true)
	if err == nil || !strings.Contains(err.Error(), "proxy URL is required when proxy is enabled") {
		t.Fatalf("expected missing proxy URL error, got %v", err)
	}
}

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL %q: %v", rawURL, err)
	}
	return parsedURL
}
