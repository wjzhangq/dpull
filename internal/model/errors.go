package model

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
)

// StatusError is a non-2xx hub response.
type StatusError struct {
	Op   string
	Code int
	Body string
}

func (e *StatusError) Error() string {
	msg := fmt.Sprintf("%s: %d", e.Op, e.Code)
	if e.Body != "" {
		msg += " " + e.Body
	}
	if e.Code == 401 || e.Code == 403 {
		msg += " (set HF_TOKEN or MODELSCOPE_API_TOKEN, and accept the model license)"
	}
	return msg
}

// Permanent tells the downloader not to retry an authorization or missing-repo failure.
func (e *StatusError) Permanent() bool {
	switch e.Code {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect,
		400, 401, 403, 404:
		return true
	default:
		return false
	}
}

func isNotFound(err error) bool {
	var status *StatusError
	return errors.As(err, &status) && status.Code == 404
}

// ShouldFailover reports that the other Hugging Face host is worth trying.
func ShouldFailover(err error) bool {
	if err == nil || isNotFound(err) {
		return isNotFound(err)
	}
	var status *StatusError
	if errors.As(err, &status) {
		switch status.Code {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
			http.StatusTemporaryRedirect, http.StatusPermanentRedirect,
			408, 429, 500, 502, 503, 504:
			return true
		default:
			return false
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var op *net.OpError
	return errors.As(err, &op)
}
