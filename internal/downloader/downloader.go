package downloader

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/wjzhangq/dpull/internal/store"
)

// Downloader orchestrates multi-connection downloads with bitfield resume
type Downloader struct {
	client          *http.Client
	jobs            int   // Max concurrent blobs
	connections     int   // Connections per blob
	minSplitSize    int64 // Min size to split (20MB default)
	maxRetries      int
	pieceDownloader *PieceDownloader
}

// Option configures a Downloader
type Option func(*Downloader)

// New creates a new downloader
func New(opts ...Option) *Downloader {
	d := &Downloader{
		client:       http.DefaultClient,
		jobs:         3,
		connections:  8,
		minSplitSize: 20 * 1024 * 1024, // 20MB
		maxRetries:   10,
	}

	for _, opt := range opts {
		opt(d)
	}

	d.pieceDownloader = NewPieceDownloader(d.client, d.maxRetries)
	return d
}

// WithHTTPClient sets the HTTP client (shares proxy config with registry)
func WithHTTPClient(client *http.Client) Option {
	return func(d *Downloader) {
		d.client = client
	}
}

// WithJobs sets max concurrent blob downloads
func WithJobs(jobs int) Option {
	return func(d *Downloader) {
		d.jobs = jobs
	}
}

// WithConnections sets connections per blob
func WithConnections(conn int) Option {
	return func(d *Downloader) {
		d.connections = conn
	}
}

// WithMinSplitSize sets minimum size for splitting
func WithMinSplitSize(size int64) Option {
	return func(d *Downloader) {
		d.minSplitSize = size
	}
}

// WithMaxRetries sets max retries per piece
func WithMaxRetries(retries int) Option {
	return func(d *Downloader) {
		d.maxRetries = retries
	}
}

// DownloadBlob downloads a blob with multi-connection splitting and bitfield resume
func (d *Downloader) DownloadBlob(
	ctx context.Context,
	urlResolver URLResolver,
	dest string,
	size int64,
	blobState *store.BlobState,
	onProgress func(downloaded int64),
) error {
	// Already complete
	if blobState.State == "complete" && blobState.Verified {
		return nil
	}

	// Small blob or initial download: use single connection
	if size < d.minSplitSize {
		blobState.State = "downloading"
		err := d.pieceDownloader.DownloadSingleConnection(ctx, urlResolver, dest, size)
		if err != nil {
			return err
		}
		blobState.State = "complete"
		return nil
	}

	// Multi-connection download with bitfield
	return d.downloadMultiConnection(ctx, urlResolver, dest, size, blobState, onProgress)
}

func (d *Downloader) downloadMultiConnection(
	ctx context.Context,
	urlResolver URLResolver,
	dest string,
	size int64,
	blobState *store.BlobState,
	onProgress func(downloaded int64),
) error {
	// Calculate piece size
	pieceSize := max(d.minSplitSize, size/int64(d.connections))
	pieces := int((size + pieceSize - 1) / pieceSize)

	// Initialize or load bitfield
	bitfield := store.NewBitfield(pieces)
	if blobState.Bitfield != "" {
		if err := bitfield.FromHex(blobState.Bitfield); err != nil {
			// Corrupted bitfield, start fresh
			bitfield = store.NewBitfield(pieces)
		}
	}

	// Update blob state
	blobState.PieceSize = pieceSize
	blobState.Pieces = pieces
	blobState.State = "downloading"

	// Prepare .part file
	partPath := dest + ".part"
	f, err := os.OpenFile(partPath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("create part file: %w", err)
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		return fmt.Errorf("truncate part file: %w", err)
	}
	f.Close()

	// Find missing pieces
	missing := bitfield.Missing()
	if len(missing) == 0 {
		// All pieces done, just rename
		if err := os.Rename(partPath, dest); err != nil {
			return fmt.Errorf("rename: %w", err)
		}
		blobState.State = "complete"
		return nil
	}

	// Download missing pieces with concurrency limit
	var (
		wg     sync.WaitGroup
		errMu  sync.Mutex
		firstErr error
		sem    = make(chan struct{}, d.connections)
	)

	for _, pieceIdx := range missing {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()

			offset := int64(idx) * pieceSize
			currentSize := min(pieceSize, size-offset)

			err := d.pieceDownloader.DownloadPiece(ctx, urlResolver, partPath, offset, currentSize)
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("piece %d: %w", idx, err)
				}
				errMu.Unlock()
				return
			}

			// Mark piece complete
			bitfield.Set(idx)
			blobState.Bitfield = bitfield.ToHex()

			// Report progress
			if onProgress != nil {
				onProgress(int64(bitfield.Count()) * pieceSize)
			}
		}(pieceIdx)
	}

	wg.Wait()

	if firstErr != nil {
		return firstErr
	}

	// All pieces done
	if err := os.Rename(partPath, dest); err != nil {
		return fmt.Errorf("rename: %w", err)
	}

	blobState.State = "complete"
	return nil
}

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
