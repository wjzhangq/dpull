package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
)

// BearerToken represents a registry bearer token
type BearerToken struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// AuthChallenge represents a WWW-Authenticate challenge
type AuthChallenge struct {
	Scheme string
	Realm  string
	Service string
	Scope  string
}

// parseAuthChallenge parses a WWW-Authenticate header
// Example: Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"
func parseAuthChallenge(header string) (*AuthChallenge, error) {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid auth challenge: %s", header)
	}

	challenge := &AuthChallenge{Scheme: parts[0]}

	// Parse parameters
	params := strings.Split(parts[1], ",")
	for _, param := range params {
		kv := strings.SplitN(param, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		value := strings.Trim(strings.TrimSpace(kv[1]), "\"")

		switch key {
		case "realm":
			challenge.Realm = value
		case "service":
			challenge.Service = value
		case "scope":
			challenge.Scope = value
		}
	}

	return challenge, nil
}

// fetchBearerToken exchanges credentials for a bearer token
func (c *Client) fetchBearerToken(challenge *AuthChallenge) (string, error) {
	req, err := http.NewRequest("GET", challenge.Realm, nil)
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}

	// Add query parameters
	q := req.URL.Query()
	if challenge.Service != "" {
		q.Set("service", challenge.Service)
	}
	if challenge.Scope != "" {
		q.Set("scope", challenge.Scope)
	}
	req.URL.RawQuery = q.Encode()

	// Add basic auth if available
	authConfig, err := c.auth.Authorization()
	if err != nil {
		return "", fmt.Errorf("get auth: %w", err)
	}

	if authConfig != nil && authConfig.Username != "" {
		req.SetBasicAuth(authConfig.Username, authConfig.Password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token request failed: %d %s", resp.StatusCode, string(body))
	}

	var token BearerToken
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}

	if token.Token != "" {
		return token.Token, nil
	}
	if token.AccessToken != "" {
		return token.AccessToken, nil
	}

	return "", fmt.Errorf("no token in response")
}

// authenticate handles registry authentication challenges
// Returns a new Client with bearer token authentication
func (c *Client) authenticate(host, repo string) (*Client, error) {
	// Ping registry to get auth challenge
	pingURL := c.buildURL(host, "/v2/")
	req, err := http.NewRequest("GET", pingURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create ping request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ping registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		// No auth required
		return c, nil
	}

	if resp.StatusCode != http.StatusUnauthorized {
		return nil, fmt.Errorf("unexpected ping status: %d", resp.StatusCode)
	}

	// Parse auth challenge
	authHeader := resp.Header.Get("WWW-Authenticate")
	if authHeader == "" {
		return nil, fmt.Errorf("no WWW-Authenticate header")
	}

	challenge, err := parseAuthChallenge(authHeader)
	if err != nil {
		return nil, fmt.Errorf("parse auth challenge: %w", err)
	}

	if challenge.Scheme != "Bearer" {
		return nil, fmt.Errorf("unsupported auth scheme: %s", challenge.Scheme)
	}

	// Add scope if not present
	if challenge.Scope == "" && repo != "" {
		challenge.Scope = fmt.Sprintf("repository:%s:pull", repo)
	}

	// Fetch token
	token, err := c.fetchBearerToken(challenge)
	if err != nil {
		return nil, fmt.Errorf("fetch bearer token: %w", err)
	}

	// Create new client with token auth
	tokenAuth := &authn.Bearer{Token: token}
	newClient := NewClient(
		WithProxy(c.getProxyURL()),
		WithAuth(tokenAuth),
		WithUserAgent(c.userAgent),
		WithInsecure(c.insecure),
		WithPlainHTTP(c.plainHTTP),
	)

	return newClient, nil
}

// getProxyURL returns the configured proxy URL (if any)
func (c *Client) getProxyURL() string {
	transport := c.httpClient.Transport.(*http.Transport)
	if transport.Proxy == nil {
		return ""
	}

	// Try to extract proxy from a dummy request
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	proxyURL, err := transport.Proxy(req)
	if err != nil || proxyURL == nil {
		return ""
	}

	return proxyURL.String()
}
