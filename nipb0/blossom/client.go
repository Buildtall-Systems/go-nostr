package blossom

import (
	"strings"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/valyala/fasthttp"
)

// Client represents a Blossom client for interacting with a media server
type Client struct {
	mediaserver string
	httpClient  *fasthttp.Client
	signer      nostr.Signer
}

// ClientOption configures a Client at construction time.
type ClientOption func(*Client)

// WithReadTimeout sets the fasthttp per-operation read timeout on the client.
// Defaults to zero (no per-operation read timeout) when not set.
func WithReadTimeout(d time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.ReadTimeout = d
	}
}

// WithWriteTimeout sets the fasthttp per-operation write timeout on the client.
// Defaults to zero (no per-operation write timeout) when not set.
func WithWriteTimeout(d time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.WriteTimeout = d
	}
}

// NewClient creates a new Blossom client
func NewClient(mediaserver string, signer nostr.Signer, opts ...ClientOption) *Client {
	if !strings.HasPrefix(mediaserver, "http") {
		mediaserver = "https://" + mediaserver
	}

	c := &Client{
		mediaserver: strings.TrimSuffix(mediaserver, "/") + "/",
		httpClient:  createHTTPClient(),
		signer:      signer,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// createHTTPClient creates a properly configured HTTP client
func createHTTPClient() *fasthttp.Client {
	maxIdleConnDuration, _ := time.ParseDuration("1h")
	return &fasthttp.Client{
		MaxIdleConnDuration:           maxIdleConnDuration,
		NoDefaultUserAgentHeader:      true, // Don't send: User-Agent: fasthttp
		DisableHeaderNamesNormalizing: true, // If you set the case on your headers correctly you can enable this
		DisablePathNormalizing:        true,
		// increase DNS cache time to an hour instead of default minute
		Dial: (&fasthttp.TCPDialer{
			Concurrency:      4096,
			DNSCacheDuration: time.Hour,
		}).Dial,
	}
}

// GetSigner returns the client's signer
func (c *Client) GetSigner() nostr.Signer {
	return c.signer
}

// GetMediaServer returns the client's media server URL
func (c *Client) GetMediaServer() string {
	return c.mediaserver
}
