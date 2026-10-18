// Package catalog demonstrates a small REST integration built on httpclient.
// Copy the pattern into internal/modules/<module>/integrations/<provider> as needed.
// This is an example API, not a client for a real catalog provider.
package catalog

import (
	"errors"
	"net/http"

	"github.com/wisalgroup/labena/pkg/httpclient"
)

// Client embeds the framework client so Do, Build and Configure need no wrappers.
// Construct it with New and reuse it across requests.
type Client struct{ *httpclient.Client }

// New configures the API prefix and optional bearer token without sending requests.
func New(baseURL, token string) (*Client, error) {
	if baseURL == "" {
		return nil, errors.New("catalog: base URL is required")
	}
	header := http.Header{"Accept": {"application/json"}}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}
	client, err := httpclient.New(httpclient.Config{BaseURL: baseURL, Header: header})
	if err != nil {
		return nil, err
	}
	return &Client{Client: client}, nil
}
