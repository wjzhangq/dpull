package registry

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/wjzhangq/dpull/internal/ref"
)

// BlobURLResolver resolves blob download URLs and handles presigned URL refresh
// Critical: Presigned URLs expire in 5-15 minutes, must refresh on expiry
type BlobURLResolver struct {
	client *Client
	ref    *ref.Reference
	digest string

	mu       sync.Mutex
	cached   string
	expireAt time.Time
}

// NewBlobURLResolver creates a new blob URL resolver
func NewBlobURLResolver(client *Client, r *ref.Reference, digest string) *BlobURLResolver {
	return &BlobURLResolver{
		client: client,
		ref:    r,
		digest: digest,
	}
}

// Get returns a valid blob download URL, refreshing if expired
func (r *BlobURLResolver) Get(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if cached URL is still valid (with 60s safety margin)
	if r.cached != "" && time.Now().Before(r.expireAt.Add(-60*time.Second)) {
		return r.cached, nil
	}

	// Need to refresh URL via registry API
	authedClient, err := r.client.authenticate(r.ref.Registry, r.ref.Repo())
	if err != nil {
		return "", fmt.Errorf("authenticate: %w", err)
	}

	// Request blob URL (registry returns 307 redirect to presigned URL)
	blobURL := authedClient.buildURL(r.ref.Registry, fmt.Sprintf("/v2/%s/blobs/%s", r.ref.Repo(), r.digest))
	req, err := http.NewRequestWithContext(ctx, "GET", blobURL, nil)
	if err != nil {
		return "", fmt.Errorf("create blob request: %w", err)
	}

	resp, err := authedClient.do(req)
	if err != nil {
		return "", fmt.Errorf("fetch blob URL: %w", err)
	}
	defer resp.Body.Close()

	// Handle redirect
	if resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusFound {
		location := resp.Header.Get("Location")
		if location == "" {
			return "", fmt.Errorf("redirect without Location header")
		}

		// Parse expiry from URL if available
		expiry := r.parseExpiry(location)
		if expiry.IsZero() {
			// Default to 5 minutes if no expiry in URL
			expiry = time.Now().Add(5 * time.Minute)
		}

		r.cached = location
		r.expireAt = expiry
		return location, nil
	}

	// Direct download (no redirect)
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
		// No presigned URL, use the blob URL directly
		r.cached = blobURL
		r.expireAt = time.Now().Add(24 * time.Hour) // Long TTL for non-presigned
		return blobURL, nil
	}

	return "", fmt.Errorf("unexpected blob response: %d", resp.StatusCode)
}

// parseExpiry extracts expiry timestamp from presigned URL
// Example: https://xxx.oss.com/blob?Expires=1754387234&Signature=...
func (r *BlobURLResolver) parseExpiry(urlStr string) time.Time {
	u, err := url.Parse(urlStr)
	if err != nil {
		return time.Time{}
	}

	expiresStr := u.Query().Get("Expires")
	if expiresStr == "" {
		expiresStr = u.Query().Get("X-Amz-Expires")
	}

	if expiresStr != "" {
		if timestamp, err := strconv.ParseInt(expiresStr, 10, 64); err == nil {
			return time.Unix(timestamp, 0)
		}
	}

	return time.Time{}
}

// TestBlobAccess tests if blob download is accessible
func (c *Client) TestBlobAccess(r *ref.Reference, digest string) (bool, bool) {
	authedClient, err := c.authenticate(r.Registry, r.Repo())
	if err != nil {
		return false, false
	}

	url := authedClient.buildURL(r.Registry, fmt.Sprintf("/v2/%s/blobs/%s", r.Repo(), digest))
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return false, false
	}

	resp, err := authedClient.do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()

	// Follow redirect if present
	if resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusFound {
		location := resp.Header.Get("Location")
		if location == "" {
			return false, false
		}

		req, err = http.NewRequest("HEAD", location, nil)
		if err != nil {
			return false, false
		}

		resp, err = authedClient.httpClient.Do(req)
		if err != nil {
			return false, false
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode != http.StatusOK {
		return false, false
	}

	// Check Range support
	acceptRanges := resp.Header.Get("Accept-Ranges")
	supportsRange := acceptRanges == "bytes"

	return true, supportsRange
}
