package registry

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// HTTPStatusError is a non-2xx response from a registry.
type HTTPStatusError struct {
	Op   string
	Code int
	Body string
}

func (e *HTTPStatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("%s: %d", e.Op, e.Code)
	}
	return fmt.Sprintf("%s: %d %s", e.Op, e.Code, e.Body)
}

// IsReachabilityError reports a failure that means this host is a bad path,
// not that the image or the credentials are wrong. 401 and 404 are excluded.
func IsReachabilityError(err error) bool {
	if err == nil {
		return false
	}
	var status *HTTPStatusError
	if errors.As(err, &status) {
		switch status.Code {
		case 403, 408, 429, 500, 502, 503, 504:
			return true
		default:
			return false
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return IsDialError(err)
}

// IsDialError reports that no HTTP response was received: DNS, refused,
// reset, or a timeout while connecting.
func IsDialError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var status *HTTPStatusError
	if errors.As(err, &status) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return true
	}
	var op *net.OpError
	return errors.As(err, &op)
}
