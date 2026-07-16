// Package provider owns tenant-scoped Jimu provider client lifecycles.
package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

var (
	// ErrRedirect is returned instead of forwarding bearer credentials to a redirect target.
	ErrRedirect = errors.New("Studio provider redirect rejected")
	safeToken   = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]+$`)
)

// Client is bound to exactly one opaque bearer token and one provider origin.
// Closing it cancels every in-flight request created through the client.
type Client struct {
	baseURL *url.URL
	token   string
	http    *http.Client
	ctx     context.Context
	cancel  context.CancelFunc
	once    sync.Once
}

// NewClient constructs a tenant-scoped provider client. The optional transport
// exists for controlled provider adapters and tests; nil uses the default transport.
func NewClient(parent context.Context, baseURL, token string, transport http.RoundTripper) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse Studio provider URL: %w", err)
	}
	if (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("Studio provider URL must be an HTTP(S) origin without credentials, query, or fragment")
	}
	if token == "" || len(token) > 16*1024 || strings.ContainsAny(token, "\r\n") || !safeToken.MatchString(token) {
		return nil, errors.New("safe opaque Studio bearer token is required")
	}
	if parent == nil {
		parent = context.Background()
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	ctx, cancel := context.WithCancel(parent)
	return &Client{
		baseURL: parsed,
		token:   token,
		http: &http.Client{
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return ErrRedirect
			},
		},
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

// Close irreversibly disposes the token-bound client and cancels its requests.
func (client *Client) Close() {
	client.once.Do(client.cancel)
}

// Do executes one request against the fixed provider origin.
func (client *Client) Do(caller context.Context, method, path string, body io.Reader) (*http.Response, error) {
	reference, err := url.Parse(path)
	if err != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || reference.IsAbs() || reference.Host != "" {
		return nil, errors.New("Studio provider path must be an absolute-path reference")
	}
	requestContext, cancel := context.WithCancel(client.ctx)
	defer cancel()
	if caller != nil {
		stop := context.AfterFunc(caller, cancel)
		defer stop()
	}
	request, err := http.NewRequestWithContext(requestContext, method, client.baseURL.ResolveReference(reference).String(), body)
	if err != nil {
		return nil, fmt.Errorf("create Studio provider request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.http.Do(request)
	if err != nil {
		return response, err
	}
	return response, nil
}

func (client *Client) do(caller context.Context, method, path string, body io.Reader) (*http.Response, error) {
	return client.Do(caller, method, path, body)
}
