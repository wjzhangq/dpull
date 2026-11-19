package registry

import (
	"context"
	"fmt"
	"net"
	"testing"
)

func TestIsReachabilityError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"not found", &HTTPStatusError{Code: 404}, false},
		{"unauthorized", &HTTPStatusError{Code: 401}, false},
		{"forbidden", &HTTPStatusError{Code: 403}, true},
		{"bad gateway", &HTTPStatusError{Code: 502}, true},
		{"dns", &net.DNSError{Err: "no such host", Name: "docker.io", IsNotFound: true}, true},
		{"deadline", context.DeadlineExceeded, true},
		{"wrapped dial", fmt.Errorf("ping registry: %w", &net.DNSError{Err: "no such host", Name: "docker.io"}), true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReachabilityError(tt.err); got != tt.want {
				t.Fatalf("IsReachabilityError = %v, want %v", got, tt.want)
			}
		})
	}
}
