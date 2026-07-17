package ratelimit

import (
	"errors"
	"net/http/httptest"
	"testing"
)

func TestClientIPResolverReturnsCanonicalAddressWithoutForwardedHeaderStorage(t *testing.T) {
	resolver := NewClientIPResolver()
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("X-Forwarded-For", "::ffff:198.51.100.10, 203.0.113.20")

	address, err := resolver.ResolveAddress(request)
	if err != nil {
		t.Fatalf("ResolveAddress returned error: %v", err)
	}
	if got, want := address.String(), "198.51.100.10"; got != want {
		t.Fatalf("resolved address = %q, want %q", got, want)
	}

	request.Header.Set("X-Forwarded-For", "192.0.2.99")
	if got, want := address.String(), "198.51.100.10"; got != want {
		t.Fatalf("previously resolved address changed after header mutation: got %q, want %q", got, want)
	}
}

func TestClientIPResolverAllowsMatchingRealIPAndForwardedFor(t *testing.T) {
	resolver := NewClientIPResolver()
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("X-Real-IP", "2001:db8::1")
	request.Header.Set("X-Forwarded-For", "2001:0db8:0:0:0:0:0:1, 203.0.113.20")

	address, err := resolver.ResolveAddress(request)
	if err != nil {
		t.Fatalf("ResolveAddress returned error: %v", err)
	}
	if got, want := address.String(), "2001:db8::1"; got != want {
		t.Fatalf("resolved address = %q, want %q", got, want)
	}
}

func TestClientIPResolverRejectsConflictingHeaders(t *testing.T) {
	resolver := NewClientIPResolver()
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("X-Real-IP", "198.51.100.10")
	request.Header.Set("X-Forwarded-For", "198.51.100.11")

	_, err := resolver.ResolveAddress(request)
	if !errors.Is(err, ErrInvalidForwardedClientIPHeaders) {
		t.Fatalf("ResolveAddress error = %v, want %v", err, ErrInvalidForwardedClientIPHeaders)
	}
}

func TestClientIPResolverRejectsDuplicateHeaderValuesAndNames(t *testing.T) {
	resolver := NewClientIPResolver()

	duplicateValuesRequest := httptest.NewRequest("GET", "/", nil)
	duplicateValuesRequest.Header["X-Forwarded-For"] = []string{"198.51.100.10", "198.51.100.11"}
	if _, err := resolver.ResolveAddress(duplicateValuesRequest); !errors.Is(err, ErrInvalidForwardedClientIPHeaders) {
		t.Fatalf("duplicate values error = %v, want %v", err, ErrInvalidForwardedClientIPHeaders)
	}

	duplicateNamesRequest := httptest.NewRequest("GET", "/", nil)
	duplicateNamesRequest.Header["X-Real-IP"] = []string{"198.51.100.10"}
	duplicateNamesRequest.Header["x-real-ip"] = []string{"198.51.100.10"}
	if _, err := resolver.ResolveAddress(duplicateNamesRequest); !errors.Is(err, ErrInvalidForwardedClientIPHeaders) {
		t.Fatalf("duplicate names error = %v, want %v", err, ErrInvalidForwardedClientIPHeaders)
	}
}

func TestClientIPResolverEnforcesHeaderLengthAndHopLimits(t *testing.T) {
	resolver := NewClientIPResolverWithConfig(ClientIPResolverConfig{
		MaximumRealIPHeaderBytes:       12,
		MaximumForwardedForHeaderBytes: 128,
		MaximumForwardedHops:           2,
	})

	oversizedRealIPRequest := httptest.NewRequest("GET", "/", nil)
	oversizedRealIPRequest.Header.Set("X-Real-IP", "198.51.100.100")
	if _, err := resolver.ResolveAddress(oversizedRealIPRequest); !errors.Is(err, ErrInvalidForwardedClientIPHeaders) {
		t.Fatalf("oversized X-Real-IP error = %v, want %v", err, ErrInvalidForwardedClientIPHeaders)
	}

	tooManyHopsRequest := httptest.NewRequest("GET", "/", nil)
	tooManyHopsRequest.Header.Set("X-Forwarded-For", "198.51.100.10,198.51.100.11,198.51.100.12")
	if _, err := resolver.ResolveAddress(tooManyHopsRequest); !errors.Is(err, ErrInvalidForwardedClientIPHeaders) {
		t.Fatalf("too many hops error = %v, want %v", err, ErrInvalidForwardedClientIPHeaders)
	}

	headerLimitedResolver := NewClientIPResolverWithConfig(ClientIPResolverConfig{
		MaximumForwardedForHeaderBytes: 20,
		MaximumForwardedHops:           16,
	})
	oversizedForwardedForRequest := httptest.NewRequest("GET", "/", nil)
	oversizedForwardedForRequest.Header.Set("X-Forwarded-For", "198.51.100.10, 198.51.100.11")
	if _, err := headerLimitedResolver.ResolveAddress(oversizedForwardedForRequest); !errors.Is(err, ErrInvalidForwardedClientIPHeaders) {
		t.Fatalf("oversized X-Forwarded-For error = %v, want %v", err, ErrInvalidForwardedClientIPHeaders)
	}
}

func TestClientIPResolverRejectsMalformedOrEmptyHops(t *testing.T) {
	resolver := NewClientIPResolver()
	testValues := []string{
		"",
		"unknown",
		"198.51.100.10,,203.0.113.20",
		"198.51.100.10, invalid",
		"fe80::1%eth0",
	}

	for _, testValue := range testValues {
		request := httptest.NewRequest("GET", "/", nil)
		request.Header.Set("X-Forwarded-For", testValue)
		if _, err := resolver.ResolveAddress(request); !errors.Is(err, ErrInvalidForwardedClientIPHeaders) {
			t.Fatalf("X-Forwarded-For %q error = %v, want %v", testValue, err, ErrInvalidForwardedClientIPHeaders)
		}
	}
}

func TestClientIPResolverReturnsNoAddressWhenHeadersAreAbsent(t *testing.T) {
	resolver := NewClientIPResolver()
	request := httptest.NewRequest("GET", "/", nil)

	address, err := resolver.ResolveAddress(request)
	if err != nil {
		t.Fatalf("ResolveAddress returned error: %v", err)
	}
	if address.IsValid() {
		t.Fatalf("resolved address = %v, want invalid address", address)
	}
}
