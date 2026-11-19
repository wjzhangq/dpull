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
	direct   bool
	authed   *Client
}

// NewBlobURLResolver creates a new blob URL resolver
func NewBlobURLResolver(client *Client, r *ref.Reference, digest string) *BlobURLResolver {
	return &BlobURLResolver{
		client: client,
		ref:    r,
		digest: digest,
	}
}

// Get returns a valid blob download URL, refreshing if expired.
// The registry is queried with HEAD so a direct 200 cannot pull the blob body.
func (r *BlobURLResolver) Get(ctx context.Context) (string, error) {
	r.mu.Lock()
	if r.cached != "" && time.Now().Before(r.expireAt.Add(-60*time.Second)) {
		cached := r.cached
		r.mu.Unlock()
		return cached, nil
	}
	r.mu.Unlock()

	authedClient, err := r.client.authenticate(ctx, r.ref.Registry, r.ref.Repo())
	if err != nil {
		return "", fmt.Errorf("authenticate: %w", err)
	}

	blobURL := authedClient.buildURL(r.ref.Registry, fmt.Sprintf("/v2/%s/blobs/%s", r.ref.Repo(), r.digest))
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, blobURL, nil)
	if err != nil {
		return "", fmt.Errorf("create blob request: %w", err)
	}

	resp, err := authedClient.do(req)
	if err != nil {
		return "", fmt.Errorf("fetch blob URL: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusTemporaryRedirect, http.StatusFound, http.StatusMovedPermanently:
		location := resp.Header.Get("Location")
		if location == "" {
			return "", fmt.Errorf("redirect without Location header")
		}
		expiry := parsePresignExpiry(location, time.Now())
		if expiry.IsZero() {
			expiry = time.Now().Add(5 * time.Minute)
		}
		r.store(location, expiry, false, authedClient)
		return location, nil
	case http.StatusOK, http.StatusPartialContent:
		// Blob is served by the registry itself. Re-resolve often: the bearer
		// token is short-lived, and the piece request must carry it.
		r.store(blobURL, time.Now().Add(time.Minute), true, authedClient)
		return blobURL, nil
	default:
		return "", fmt.Errorf("unexpected blob response: %d", resp.StatusCode)
	}
}

func (r *BlobURLResolver) store(location string, expiry time.Time, direct bool, authed *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cached = location
	r.expireAt = expiry
	r.direct = direct
	r.authed = authed
}

// Decorate adds registry auth for a direct blob URL. Presigned URLs are left alone.
func (r *BlobURLResolver) Decorate(req *http.Request) {
	r.mu.Lock()
	direct := r.direct
	authed := r.authed
	r.mu.Unlock()
	if !direct || authed == nil {
		return
	}
	_ = authed.setAuth(req)
}

// SupportsRange reports whether the resolved blob URL honors Range requests.
// A 200 to a ranged GET is treated as "no": the body is cancelled immediately
// so a full layer is not downloaded just to probe.
func (r *BlobURLResolver) SupportsRange(ctx context.Context) (bool, error) {
	loc, err := r.Get(ctx)
	if err != nil {
		return false, err
	}

	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, loc, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Range", "bytes=0-0")
	r.Decorate(req)

	resp, err := r.client.BlobHTTPClient().Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusPartialContent {
		return true, nil
	}
	cancel()
	return false, nil
}

// parsePresignExpiry reads the lifetime of a redirected blob URL.
// OSS-style Expires is a unix timestamp. AWS X-Amz-Expires is a duration
// in seconds from X-Amz-Date, not a timestamp.
func parsePresignExpiry(urlStr string, now time.Time) time.Time {
	u, err := url.Parse(urlStr)
	if err != nil {
		return time.Time{}
	}
	q := u.Query()

	if raw := q.Get("X-Amz-Expires"); raw != "" {
		secs, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || secs <= 0 {
			return time.Time{}
		}
		start := now
		if date := q.Get("X-Amz-Date"); date != "" {
			if parsed, err := time.Parse("20060102T150405Z", date); err == nil {
				start = parsed
			}
		}
		return start.Add(time.Duration(secs) * time.Second)
	}

	if raw := q.Get("Expires"); raw != "" {
		ts, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || ts <= 0 {
			return time.Time{}
		}
		return time.Unix(ts, 0)
	}
	return time.Time{}
}

// TestBlobAccess tests if blob download is accessible
func (c *Client) TestBlobAccess(r *ref.Reference, digest string) (bool, bool) {
	authedClient, err := c.authenticate(context.Background(), r.Registry, r.Repo())
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
