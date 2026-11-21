package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RedirectResolver turns a hub file URL into a fresh object-storage URL.
// Signed URLs expire in minutes, so each piece asks Get again.
type RedirectResolver struct {
	source  string
	token   string
	resolve *http.Client
	blob    *http.Client

	mu       sync.Mutex
	refresh  sync.Mutex
	cached   string
	expireAt time.Time
	direct   bool
}

// Get returns a URL that is still inside its signature window.
func (r *RedirectResolver) Get(ctx context.Context) (string, error) {
	if cached, ok := r.fresh(); ok {
		return cached, nil
	}
	r.refresh.Lock()
	defer r.refresh.Unlock()
	if cached, ok := r.fresh(); ok {
		return cached, nil
	}
	location, expiry, direct, err := r.resolveOnce(ctx)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	r.cached = location
	r.expireAt = expiry
	r.direct = direct
	r.mu.Unlock()
	return location, nil
}

func (r *RedirectResolver) fresh() (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cached == "" || !time.Now().Before(r.expireAt.Add(-60*time.Second)) {
		return "", false
	}
	return r.cached, true
}

// Decorate adds the hub token only when the file is served by the hub itself.
// A signed CDN URL is left alone.
func (r *RedirectResolver) Decorate(req *http.Request) {
	req.Header.Set("User-Agent", userAgent())
	r.mu.Lock()
	direct := r.direct
	r.mu.Unlock()
	if direct && r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
}

// SupportsRange probes the storage URL with a one-byte range request.
func (r *RedirectResolver) SupportsRange(ctx context.Context) (bool, error) {
	location, err := r.Get(ctx)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Range", "bytes=0-0")
	r.Decorate(req)
	resp, err := r.blob.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1))
	switch resp.StatusCode {
	case http.StatusPartialContent:
		return true, nil
	case http.StatusOK:
		return false, nil
	default:
		return false, fmt.Errorf("range probe: %d", resp.StatusCode)
	}
}

func (r *RedirectResolver) resolveOnce(ctx context.Context) (string, time.Time, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.source, nil)
	if err != nil {
		return "", time.Time{}, false, err
	}
	req.Header.Set("Range", "bytes=0-0")
	req.Header.Set("Accept", "*/*")
	setAuth(req, r.token)
	resp, err := r.resolve.Do(req)
	if err != nil {
		return "", time.Time{}, false, fmt.Errorf("resolve file URL: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		location, err := redirectLocation(resp)
		if err != nil {
			return "", time.Time{}, false, err
		}
		if hubHost(location) {
			return "", time.Time{}, false, &StatusError{
				Op:   "resolve file",
				Code: resp.StatusCode,
				Body: "redirect to " + location,
			}
		}
		expiry := parseExpiry(location, time.Now())
		if expiry.IsZero() {
			expiry = time.Now().Add(5 * time.Minute)
		}
		return location, expiry, false, nil
	case http.StatusOK, http.StatusPartialContent:
		if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "json") {
			return r.readJSONURL(ctx)
		}
		return r.source, time.Now().Add(time.Minute), true, nil
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusBadRequest:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", time.Time{}, false, &StatusError{Op: "resolve file", Code: resp.StatusCode, Body: trimBody(body)}
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", time.Time{}, false, fmt.Errorf("resolve file: %d %s", resp.StatusCode, trimBody(body))
	}
}

func (r *RedirectResolver) readJSONURL(ctx context.Context) (string, time.Time, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.source, nil)
	if err != nil {
		return "", time.Time{}, false, err
	}
	setHubHeaders(req, r.token)
	resp, err := r.resolve.Do(req)
	if err != nil {
		return "", time.Time{}, false, err
	}
	body, err := readBody(resp, 1<<20)
	if err != nil {
		return "", time.Time{}, false, err
	}
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, false, &StatusError{Op: "resolve file", Code: resp.StatusCode, Body: trimBody(body)}
	}
	location, err := urlFromJSON(body)
	if err != nil {
		return "", time.Time{}, false, err
	}
	expiry := parseExpiry(location, time.Now())
	if expiry.IsZero() {
		expiry = time.Now().Add(5 * time.Minute)
	}
	return location, expiry, false, nil
}

func redirectLocation(resp *http.Response) (string, error) {
	raw := resp.Header.Get("Location")
	if raw == "" {
		return "", fmt.Errorf("redirect without Location")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("redirect location: %w", err)
	}
	if resp.Request != nil && resp.Request.URL != nil {
		parsed = resp.Request.URL.ResolveReference(parsed)
	}
	return parsed.String(), nil
}

func urlFromJSON(body []byte) (string, error) {
	var payload struct {
		URL  string `json:"url"`
		Data struct {
			URL string `json:"Url"`
		} `json:"Data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode download url: %w", err)
	}
	if payload.Data.URL != "" {
		return payload.Data.URL, nil
	}
	if payload.URL != "" {
		return payload.URL, nil
	}
	var loose struct {
		Data json.RawMessage `json:"Data"`
	}
	if json.Unmarshal(body, &loose) == nil && len(loose.Data) > 0 {
		var asString string
		if json.Unmarshal(loose.Data, &asString) == nil && strings.HasPrefix(asString, "http") {
			return asString, nil
		}
	}
	return "", fmt.Errorf("download response did not include a URL")
}

func hubHost(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Hostname()) {
	case HostHuggingFace, "hf.co", HostHFMirror, HostModelScope, "modelscope.cn":
		return true
	default:
		return false
	}
}

func parseExpiry(raw string, now time.Time) time.Time {
	parsed, err := url.Parse(raw)
	if err != nil {
		return time.Time{}
	}
	query := parsed.Query()
	if seconds := query.Get("X-Amz-Expires"); seconds != "" {
		value, err := strconv.ParseInt(seconds, 10, 64)
		if err != nil || value <= 0 {
			return time.Time{}
		}
		start := now
		if date := query.Get("X-Amz-Date"); date != "" {
			if parsedDate, err := time.Parse("20060102T150405Z", date); err == nil {
				start = parsedDate
			}
		}
		return start.Add(time.Duration(value) * time.Second)
	}
	for _, key := range []string{"Expires", "expires"} {
		if stamp := query.Get(key); stamp != "" {
			value, err := strconv.ParseInt(stamp, 10, 64)
			if err != nil || value <= 0 {
				return time.Time{}
			}
			return time.Unix(value, 0)
		}
	}
	return time.Time{}
}
