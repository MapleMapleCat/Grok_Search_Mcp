package ratelimit

import (
	"net"
	"net/http"
	"strings"
)

// ClientIPResolver resolves the client IP supplied by the deployment's
// trusted reverse proxy.
type ClientIPResolver struct{}

// NewClientIPResolver creates a forwarded-header-aware IP resolver.
func NewClientIPResolver() *ClientIPResolver {
	return &ClientIPResolver{}
}

// Resolve returns a valid forwarded client IP. X-Real-IP takes precedence;
// otherwise the first valid X-Forwarded-For entry is used. An empty result
// means that IP-based protection should be skipped for the request.
func (resolver *ClientIPResolver) Resolve(request *http.Request) string {
	if request == nil {
		return ""
	}

	if realIP := strings.TrimSpace(request.Header.Get("X-Real-IP")); realIP != "" {
		if host := normalizeIPHost(realIP); host != "" {
			return host
		}
	}
	if forwardedFor := strings.TrimSpace(request.Header.Get("X-Forwarded-For")); forwardedFor != "" {
		for _, forwardedHop := range strings.Split(forwardedFor, ",") {
			if host := normalizeIPHost(forwardedHop); host != "" {
				return host
			}
		}
	}
	return ""
}

func normalizeIPHost(rawHost string) string {
	host := strings.TrimSpace(rawHost)
	if host == "" {
		return ""
	}
	// X-Forwarded-For entries are usually bare IPs; tolerate host:port.
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip == nil {
		return ""
	}
	return host
}
