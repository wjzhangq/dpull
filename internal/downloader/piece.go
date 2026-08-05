package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
)

// URLResolver provides fresh download URLs (handles presigned URL expiry)
type URLResolver interface {
	Get(ctx context.Context) (string, error)
}

// PieceDownloader downloads a single piece with Range requests
type PieceDownloader struct {
	client     *http.Client
	maxRetries int
}

// NewPieceDownloader creates a piece downloader
func NewPieceDownloader(client *http.Client, maxRetries int) *PieceDownloader {
	return &PieceDownloader{
		client:     client,
		maxRetries: maxRetries,
	}
}

// DownloadPiece downloads a single piece to a .part file at the given offset
// Returns ErrURLExpired if the URL returns 403/401 (caller should refresh and retry)
func (d *PieceDownloader) DownloadPiece(ctx context.Context, urlResolver URLResolver, dest string, offset, size int64) error {
	return RetryWithBackoff(func() error {
		// Get fresh URL (handles expiry)
		url, err := urlResolver.Get(ctx)
		if err != nil {
			return fmt.Errorf("resolve URL: %w", err)
		}

		// Range request
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}

		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+size-1))

		resp, err := d.client.Do(req)
		if err != nil {
			return fmt.Errorf("request: %w", err)
		}
		defer resp.Body.Close()

		// Handle presigned URL expiry
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			return ErrURLExpired
		}

		// Handle rate limiting
		if resp.StatusCode == http.StatusTooManyRequests {
			return ErrRateLimited
		}

		// Accept both 206 (partial content) and 200 (full content)
		if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, body)
		}

		// Open .part file for writing at offset
		f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			return fmt.Errorf("open dest: %w", err)
		}
		defer f.Close()

		if _, err := f.Seek(offset, 0); err != nil {
			return fmt.Errorf("seek: %w", err)
		}

		// Write response body
		written, err := io.Copy(f, resp.Body)
		if err != nil {
			return fmt.Errorf("write piece: %w", err)
		}

		if written != size {
			return fmt.Errorf("incomplete write: %d/%d bytes", written, size)
		}

		return nil
	}, d.maxRetries)
}

// DownloadSingleConnection downloads without splitting (for small blobs or when Range not supported)
func (d *PieceDownloader) DownloadSingleConnection(ctx context.Context, urlResolver URLResolver, dest string, expectedSize int64) error {
	return RetryWithBackoff(func() error {
		url, err := urlResolver.Get(ctx)
		if err != nil {
			return fmt.Errorf("resolve URL: %w", err)
		}

		// Check if resuming
		var resumeOffset int64
		if info, err := os.Stat(dest + ".part"); err == nil {
			resumeOffset = info.Size()
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}

		if resumeOffset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeOffset))
		}

		resp, err := d.client.Do(req)
		if err != nil {
			return fmt.Errorf("request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			return ErrURLExpired
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			return ErrRateLimited
		}

		// If server doesn't support range, start over
		if resumeOffset > 0 && resp.StatusCode == http.StatusOK {
			resumeOffset = 0
		}

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, body)
		}

		// Open file
		flags := os.O_WRONLY | os.O_CREATE
		f, err := os.OpenFile(dest+".part", flags, 0644)
		if err != nil {
			return fmt.Errorf("open dest: %w", err)
		}
		defer f.Close()

		if resumeOffset > 0 {
			if _, err := f.Seek(resumeOffset, 0); err != nil {
				return fmt.Errorf("seek: %w", err)
			}
		} else {
			// Truncate if starting fresh
			if err := f.Truncate(0); err != nil {
				return fmt.Errorf("truncate: %w", err)
			}
		}

		// Download
		written, err := io.Copy(f, resp.Body)
		if err != nil {
			return fmt.Errorf("write: %w", err)
		}

		totalWritten := resumeOffset + written
		if expectedSize > 0 && totalWritten != expectedSize {
			return fmt.Errorf("incomplete download: %d/%d bytes", totalWritten, expectedSize)
		}

		// Rename to final
		if err := os.Rename(dest+".part", dest); err != nil {
			return fmt.Errorf("rename: %w", err)
		}

		return nil
	}, d.maxRetries)
}
