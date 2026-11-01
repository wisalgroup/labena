package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/netip"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wisalgroup/labena/pkg/apperror"
)

// PeerIPKey always uses the direct connection. Use ClientIPKey for trusted ingress.
// Arbitrary forwarding headers cannot change the budget. NAT clients share a limit.
func PeerIPKey(c huma.Context) (string, error) {
	host, _, err := net.SplitHostPort(c.RemoteAddr())
	if err != nil {
		return "", apperror.Validation("Invalid client address.")
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return "", apperror.Validation("Invalid client address.")
	}
	hash := sha256.Sum256([]byte(addr.Unmap().String()))
	return "api:peer:" + hex.EncodeToString(hash[:]), nil
}

// ClientIPKey uses the address accepted by the server's trusted-proxy policy.
// Keep the key namespace stable for direct deployments upgrading this policy.
func ClientIPKey(c huma.Context) (string, error) {
	address, err := ClientIP(c.Context())
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(address))
	return "api:peer:" + hex.EncodeToString(hash[:]), nil
}
