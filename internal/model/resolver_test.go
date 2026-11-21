package model

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolverCachesSignedURL(t *testing.T) {
	var hits atomic.Int32
	expires := time.Now().Add(2 * time.Hour).Unix()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Location", "https://cdn.example/weights.bin?Expires="+strconv.FormatInt(expires, 10))
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	resolver := &RedirectResolver{
		source:  srv.URL + "/acme/tiny/resolve/main/weights.bin",
		token:   "secret",
		resolve: srv.Client(),
	}
	resolver.resolve.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	first, err := resolver.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolver.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first != second || hits.Load() != 1 {
		t.Fatalf("hits=%d first=%s second=%s", hits.Load(), first, second)
	}
	if got := parseExpiry(first, time.Now()); got.Unix() != expires {
		t.Fatalf("expiry = %s", got)
	}
}

func TestResolverRefreshesSoonToExpireURL(t *testing.T) {
	var hits atomic.Int32
	expires := time.Now().Add(30 * time.Second).Unix()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Location", "https://cdn.example/weights.bin?Expires="+strconv.FormatInt(expires, 10))
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	resolver := &RedirectResolver{
		source:  srv.URL + "/file",
		resolve: srv.Client(),
	}
	resolver.resolve.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	if _, err := resolver.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatalf("hits = %d", hits.Load())
	}
}

func TestResolverRejectsHubRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://huggingface.co/gpt2/resolve/main/config.json")
		w.WriteHeader(http.StatusPermanentRedirect)
	}))
	defer srv.Close()

	resolver := &RedirectResolver{source: srv.URL + "/gpt2/resolve/main/config.json", resolve: srv.Client()}
	resolver.resolve.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	_, err := resolver.Get(context.Background())
	var status *StatusError
	if !errors.As(err, &status) || status.Code != http.StatusPermanentRedirect {
		t.Fatalf("err = %v", err)
	}
}

func TestParseExpiry(t *testing.T) {
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	got := parseExpiry("https://s3.example/blob?X-Amz-Date=20261118T100000Z&X-Amz-Expires=900", now)
	if !got.Equal(now.Add(900 * time.Second)) {
		t.Fatalf("aws expiry = %s", got)
	}
	got = parseExpiry("https://oss.example/blob?expires=1754387234", now)
	if got.Unix() != 1754387234 {
		t.Fatalf("unix expiry = %s", got)
	}
}
