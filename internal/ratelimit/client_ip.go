package ratelimit

import (
	"errors"
	"net/http"
	"net/netip"
	"strings"
)

const (
	defaultMaximumRealIPHeaderBytes       = 64
	defaultMaximumForwardedForHeaderBytes = 512
	defaultMaximumForwardedHops           = 16
)

// ErrInvalidForwardedClientIPHeaders indicates that forwarded client address
// headers were present but malformed, duplicated, conflicting, or oversized.
var ErrInvalidForwardedClientIPHeaders = errors.New("invalid forwarded client IP headers")

// ClientIPResolverConfig bounds the amount of forwarded-header input parsed
// for one request. Zero values use conservative defaults.
type ClientIPResolverConfig struct {
	MaximumRealIPHeaderBytes       int
	MaximumForwardedForHeaderBytes int
	MaximumForwardedHops           int
}

// ClientIPResolver resolves the client IP supplied by the deployment's
// trusted reverse proxy.
type ClientIPResolver struct {
	maximumRealIPHeaderBytes       int
	maximumForwardedForHeaderBytes int
	maximumForwardedHops           int
}

// NewClientIPResolver creates a forwarded-header-aware IP resolver.
func NewClientIPResolver() *ClientIPResolver {
	return NewClientIPResolverWithConfig(ClientIPResolverConfig{})
}

// NewClientIPResolverWithConfig creates a resolver with explicit parsing
// limits. The reverse proxy must still clear and replace forwarded headers.
func NewClientIPResolverWithConfig(config ClientIPResolverConfig) *ClientIPResolver {
	if config.MaximumRealIPHeaderBytes <= 0 {
		config.MaximumRealIPHeaderBytes = defaultMaximumRealIPHeaderBytes
	}
	if config.MaximumForwardedForHeaderBytes <= 0 {
		config.MaximumForwardedForHeaderBytes = defaultMaximumForwardedForHeaderBytes
	}
	if config.MaximumForwardedHops <= 0 {
		config.MaximumForwardedHops = defaultMaximumForwardedHops
	}
	return &ClientIPResolver{
		maximumRealIPHeaderBytes:       config.MaximumRealIPHeaderBytes,
		maximumForwardedForHeaderBytes: config.MaximumForwardedForHeaderBytes,
		maximumForwardedHops:           config.MaximumForwardedHops,
	}
}

// Resolve returns a canonical forwarded client IP. When both supported
// headers are present, their client addresses must agree. An empty result
// means that neither header was present or that parsing failed; callers that
// need to distinguish those cases should use ResolveAddress.
func (resolver *ClientIPResolver) Resolve(request *http.Request) string {
	address, err := resolver.ResolveAddress(request)
	if err != nil || !address.IsValid() {
		return ""
	}
	return address.String()
}

// ResolveAddress returns a canonical, comparable address that never retains
// the original header string. An invalid address with a nil error means that
// neither forwarded client address header was present.
func (resolver *ClientIPResolver) ResolveAddress(request *http.Request) (netip.Addr, error) {
	if request == nil {
		return netip.Addr{}, nil
	}

	realIPValue, hasRealIP, err := readSingleHeaderValue(request.Header, "X-Real-IP")
	if err != nil {
		return netip.Addr{}, err
	}
	forwardedForValue, hasForwardedFor, err := readSingleHeaderValue(request.Header, "X-Forwarded-For")
	if err != nil {
		return netip.Addr{}, err
	}
	if !hasRealIP && !hasForwardedFor {
		return netip.Addr{}, nil
	}

	var realIPAddress netip.Addr
	if hasRealIP {
		if len(realIPValue) > resolver.maximumRealIPHeaderBytes {
			return netip.Addr{}, ErrInvalidForwardedClientIPHeaders
		}
		realIPAddress, err = parseForwardedAddress(realIPValue)
		if err != nil {
			return netip.Addr{}, err
		}
	}

	var firstForwardedAddress netip.Addr
	if hasForwardedFor {
		if len(forwardedForValue) > resolver.maximumForwardedForHeaderBytes {
			return netip.Addr{}, ErrInvalidForwardedClientIPHeaders
		}
		firstForwardedAddress, err = parseForwardedForAddresses(
			forwardedForValue,
			resolver.maximumForwardedHops,
		)
		if err != nil {
			return netip.Addr{}, err
		}
	}

	if hasRealIP && hasForwardedFor && realIPAddress != firstForwardedAddress {
		return netip.Addr{}, ErrInvalidForwardedClientIPHeaders
	}
	if hasRealIP {
		return realIPAddress, nil
	}
	return firstForwardedAddress, nil
}

func readSingleHeaderValue(header http.Header, targetName string) (string, bool, error) {
	found := false
	value := ""
	for headerName, headerValues := range header {
		if !strings.EqualFold(headerName, targetName) {
			continue
		}
		if found || len(headerValues) != 1 {
			return "", true, ErrInvalidForwardedClientIPHeaders
		}
		found = true
		value = headerValues[0]
	}
	return value, found, nil
}

func parseForwardedForAddresses(value string, maximumHops int) (netip.Addr, error) {
	if maximumHops <= 0 {
		return netip.Addr{}, ErrInvalidForwardedClientIPHeaders
	}

	var firstAddress netip.Addr
	hopStart := 0
	hopCount := 0
	for {
		hopCount++
		if hopCount > maximumHops {
			return netip.Addr{}, ErrInvalidForwardedClientIPHeaders
		}

		remainingValue := value[hopStart:]
		separatorOffset := strings.IndexByte(remainingValue, ',')
		hopEnd := len(value)
		if separatorOffset >= 0 {
			hopEnd = hopStart + separatorOffset
		}

		address, err := parseForwardedAddress(value[hopStart:hopEnd])
		if err != nil {
			return netip.Addr{}, err
		}
		if hopCount == 1 {
			firstAddress = address
		}

		if separatorOffset < 0 {
			return firstAddress, nil
		}
		hopStart = hopEnd + 1
	}
}

func parseForwardedAddress(rawAddress string) (netip.Addr, error) {
	addressText := strings.TrimSpace(rawAddress)
	if addressText == "" {
		return netip.Addr{}, ErrInvalidForwardedClientIPHeaders
	}

	address, err := netip.ParseAddr(addressText)
	if err != nil {
		if addressPort, addressPortError := netip.ParseAddrPort(addressText); addressPortError == nil {
			address = addressPort.Addr()
			err = nil
		} else if strings.HasPrefix(addressText, "[") && strings.HasSuffix(addressText, "]") {
			address, err = netip.ParseAddr(addressText[1 : len(addressText)-1])
		}
	}
	if err != nil || !address.IsValid() || address.Zone() != "" {
		return netip.Addr{}, ErrInvalidForwardedClientIPHeaders
	}
	return address.Unmap(), nil
}
