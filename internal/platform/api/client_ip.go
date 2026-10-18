package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/wisalgroup/labena/pkg/apperror"
)

type clientIPKey struct{}

// ClientIP returns the address accepted at the HTTP boundary. With no trusted
// proxies it is the immediate peer. It is a rate-limit signal, not a principal.
func ClientIP(ctx context.Context) (string, error) {
	if address, ok := ctx.Value(clientIPKey{}).(string); ok && address != "" {
		return address, nil
	}
	return PeerIP(ctx)
}

// ParseTrustedProxies validates explicit IPv4/IPv6 CIDRs. Trusting all addresses
// would let arbitrary clients supply their own security rate-limit identity.
func ParseTrustedProxies(cidrs []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(cidrs))
	for _, cidr := range cidrs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
		if err != nil || !validProxyPrefix(prefix) {
			return nil, errors.New("api: trusted proxies must be explicit IPv4 or IPv6 CIDRs, excluding /0 and IPv4-mapped IPv6")
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func validProxyPrefix(prefix netip.Prefix) bool {
	return prefix.IsValid() && prefix.Bits() > 0 && !prefix.Addr().Is4In6()
}

func resolveClientIP(r *http.Request, proxies []netip.Prefix) (string, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "", apperror.InvalidInput("Invalid client address.")
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || peer.Zone() != "" {
		return "", apperror.InvalidInput("Invalid client address.")
	}
	peer = peer.Unmap()
	trusted := func(address netip.Addr) bool {
		for _, prefix := range proxies {
			if prefix.Contains(address) {
				return true
			}
		}
		return false
	}
	// Ignore all forwarding claims from untrusted peers, including malformed ones.
	if !trusted(peer) {
		return peer.String(), nil
	}
	values := r.Header.Values("X-Forwarded-For")
	if len(values) == 0 {
		return peer.String(), nil
	}
	if len(values) != 1 || len(values[0]) > 4096 {
		return "", apperror.InvalidInput("Invalid forwarded client address.")
	}
	parts := strings.Split(values[0], ",")
	if len(parts) > 32 {
		return "", apperror.InvalidInput("Invalid forwarded client address.")
	}
	addresses := make([]netip.Addr, len(parts))
	for i, part := range parts {
		address, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil || address.Zone() != "" {
			return "", apperror.InvalidInput("Invalid forwarded client address.")
		}
		addresses[i] = address.Unmap()
	}
	// Walk only the trusted suffix. Earlier, client-supplied entries cannot
	// override the first untrusted address observed by the trusted proxy chain.
	client := peer
	for i := len(addresses) - 1; i >= 0 && trusted(client); i-- {
		client = addresses[i]
	}
	return client.String(), nil
}
