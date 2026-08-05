package registry

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
)

// Client is a registry client with proxy support
type Client struct {
	httpClient *http.Client
	auth       authn.Authenticator
	userAgent  string
	insecure   bool
	plainHTTP  bool
}

// Option is a functional option for Client
type Option func(*Client)

// NewClient creates a new registry client
func NewClient(opts ...Option) *Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment, // Default: respect HTTP_PROXY/HTTPS_PROXY
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
		TLSHandshakeTimeout: 15 * time.Second,
		// Bound the wait for response headers, NOT the body transfer.
		// Blob bodies are large and stream for minutes on slow links.
		ResponseHeaderTimeout: 60 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    false,
	}

	c := &Client{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   5 * time.Minute,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Don't follow redirects automatically for blob URLs
				// We need to refresh expired presigned URLs
				if len(via) >= 1 {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
		auth:      authn.Anonymous,
		userAgent: "dpull/1.0",
	}

	// Apply options
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// WithProxy sets a custom proxy URL (overrides environment variables)
func WithProxy(proxyURL string) Option {
	return func(c *Client) {
		if proxyURL == "" {
			return
		}
		u, err := url.Parse(proxyURL)
		if err != nil {
			// Log error but don't fail
			return
		}
		transport := c.httpClient.Transport.(*http.Transport)
		transport.Proxy = http.ProxyURL(u)
	}
}

// WithAuth sets the authenticator
func WithAuth(auth authn.Authenticator) Option {
	return func(c *Client) {
		if auth != nil {
			c.auth = auth
		}
	}
}

// WithUserAgent sets a custom user agent
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		c.userAgent = ua
	}
}

// WithInsecure skips TLS certificate verification
func WithInsecure(insecure bool) Option {
	return func(c *Client) {
		c.insecure = insecure
		transport := c.httpClient.Transport.(*http.Transport)
		transport.TLSClientConfig.InsecureSkipVerify = insecure
	}
}

// WithPlainHTTP uses HTTP instead of HTTPS
func WithPlainHTTP(plainHTTP bool) Option {
	return func(c *Client) {
		c.plainHTTP = plainHTTP
	}
}

// WithTimeout sets request timeout
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.httpClient.Timeout = timeout
	}
}

// HTTPClient returns the underlying HTTP client
// This allows sharing the proxy-enabled client with downloader
func (c *Client) HTTPClient() *http.Client {
	return c.httpClient
}

// BlobHTTPClient returns a client for streaming blob bodies.
//
// It shares the transport (and thus proxy/TLS config) with the API client but
// carries no overall Client.Timeout: that deadline covers reading the response
// body, so a large piece on a slow link would be cut off mid-transfer and
// exhaust its retries. Stalls are instead bounded by the transport's
// ResponseHeaderTimeout, and cancellation flows through the request context.
func (c *Client) BlobHTTPClient() *http.Client {
	return &http.Client{
		Transport:     c.httpClient.Transport,
		CheckRedirect: c.httpClient.CheckRedirect,
	}
}

// scheme returns the URL scheme based on plainHTTP setting
func (c *Client) scheme() string {
	if c.plainHTTP {
		return "http"
	}
	return "https"
}

// buildURL constructs a registry API URL
func (c *Client) buildURL(host, path string) string {
	return fmt.Sprintf("%s://%s%s", c.scheme(), host, path)
}

// do performs an HTTP request with authentication
func (c *Client) do(req *http.Request) (*http.Response, error) {
	// Set user agent
	req.Header.Set("User-Agent", c.userAgent)

	// Add authentication if available
	authConfig, err := c.auth.Authorization()
	if err != nil {
		return nil, fmt.Errorf("get auth: %w", err)
	}

	if authConfig != nil && authConfig.Username != "" {
		req.SetBasicAuth(authConfig.Username, authConfig.Password)
	}

	if authConfig != nil && authConfig.RegistryToken != "" {
		req.Header.Set("Authorization", "Bearer "+authConfig.RegistryToken)
	}

	return c.httpClient.Do(req)
}
