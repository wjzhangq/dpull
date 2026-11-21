package model

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wjzhangq/dpull/pkg/version"
)

func userAgent() string {
	return "mpull/" + version.Version
}

func newTransport(proxyURL string) (*http.Transport, error) {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
	}
	if proxyURL == "" {
		return transport, nil
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("invalid proxy %q", proxyURL)
	}
	transport.Proxy = http.ProxyURL(parsed)
	return transport, nil
}

func newAPIClient(transport *http.Transport) *http.Client {
	return &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
		// A mirror that answers 308 to huggingface.co must not be followed.
		// Following it hides the failure and dials the official hub with the
		// mirror's timeout. Same-host redirects, such as http to https, still proceed.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 && req.URL.Host != via[0].URL.Host {
				return http.ErrUseLastResponse
			}
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

func newResolveClient(transport *http.Transport) *http.Client {
	return &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// newBlobClient has no overall timeout. A multi-gigabyte piece can take longer
// than any fixed deadline; cancellation comes from the request context.
func newBlobClient(transport *http.Transport) *http.Client {
	return &http.Client{Transport: transport}
}

func setAuth(req *http.Request, token string) {
	req.Header.Set("User-Agent", userAgent())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func setHubHeaders(req *http.Request, token string) {
	setAuth(req, token)
	req.Header.Set("Accept", "application/json")
}

func readBody(resp *http.Response, limit int64) ([]byte, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, err
	}
	return body, nil
}

func trimBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}

func escapeSegments(value string) string {
	parts := strings.Split(value, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}
