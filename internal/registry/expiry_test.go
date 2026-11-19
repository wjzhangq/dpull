package registry

import (
	"testing"
	"time"
)

func TestParsePresignExpiryOSS(t *testing.T) {
	got := parsePresignExpiry("https://oss.example/blob?Expires=1754387234&Signature=x", time.Now())
	want := time.Unix(1754387234, 0)
	if !got.Equal(want) {
		t.Fatalf("expiry = %s, want %s", got, want)
	}
}

func TestParsePresignExpiryAWSDuration(t *testing.T) {
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	raw := "https://s3.example/blob?X-Amz-Date=20261118T100000Z&X-Amz-Expires=900"
	got := parsePresignExpiry(raw, now)
	want := now.Add(900 * time.Second)
	if !got.Equal(want) {
		t.Fatalf("expiry = %s, want %s", got, want)
	}
}
