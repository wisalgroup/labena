package api

import (
	"context"
	"mime"
	"net"
	"net/netip"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wisalgroup/labena/pkg/apperror"
)

type peerKey struct{}

// PeerIP returns the immediate peer captured by the HTTP boundary. Forwarding
// headers cannot change this value; trusted ingress needs an explicit policy.
func PeerIP(ctx context.Context) (string, error) {
	remote, _ := ctx.Value(peerKey{}).(string)
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return "", apperror.InvalidInput("Invalid client address.")
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return "", apperror.InvalidInput("Invalid client address.")
	}
	return addr.Unmap().String(), nil
}

// Reject unsupported encodings before decoding. Decompression is deliberately
// not implicit: supporting it requires a separate bound on expanded bytes.
func requestEncoding(a huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if ctx.Operation().RequestBody == nil {
			next(ctx)
			return
		}
		var types, encodings []string
		ctx.EachHeader(func(name, value string) {
			switch {
			case strings.EqualFold(name, "Content-Type"):
				types = append(types, value)
			case strings.EqualFold(name, "Content-Encoding"):
				encodings = append(encodings, value)
			}
		})
		valid := len(types) <= 1 && len(encodings) <= 1
		if len(encodings) == 1 && !strings.EqualFold(strings.TrimSpace(encodings[0]), "identity") {
			valid = false
		}
		if len(types) == 1 && types[0] != "" {
			_, params, err := mime.ParseMediaType(types[0])
			charset := strings.ToLower(params["charset"])
			valid = valid && err == nil && (charset == "" || charset == "utf-8" || charset == "utf8")
		}
		if !valid {
			_ = huma.WriteErr(a, ctx, 415, "Unsupported request encoding.")
			return
		}
		next(ctx)
	}
}
