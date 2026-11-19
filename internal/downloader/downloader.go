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

// DownloadBlob downloads a blob with multi-connection splitting and bitfield resume.
// commit persists blobState mutations. It may run from multiple piece goroutines
// and must be safe for concurrent use. onProgress is display-only.
func (d *Downloader) DownloadBlob(
	ctx context.Context,
	urlResolver URLResolver,
	dest string,
	size int64,
	blobState *store.BlobState,
	commit func(func()) error,
	onProgress func(downloaded int64),
) error {
	// Already complete
	if blobState.State == "complete" && blobState.Verified {
		return nil
	}

	if d.useSingleConnection(ctx, urlResolver, size, blobState) {
		return d.downloadSingle(ctx, urlResolver, dest, size, blobState, commit)
	}

	// Multi-connection download with bitfield
	return d.downloadMultiConnection(ctx, urlResolver, dest, size, blobState, commit, onProgress)
}

// useSingleConnection is true for small blobs and for servers that ignore Range.
// A blob that already has a saved piece plan stays on that plan.
func (d *Downloader) useSingleConnection(ctx context.Context, urlResolver URLResolver, size int64, blobState *store.BlobState) bool {
	if size < d.minSplitSize {
		return true
	}
	if blobState.PieceSize > 0 && blobState.Pieces > 0 {
		return false
	}
	prober, ok := urlResolver.(interface {
		SupportsRange(context.Context) (bool, error)
	})
	if !ok {
		return false
	}
	supported, err := prober.SupportsRange(ctx)
	if err != nil {
		// A failed probe is not proof that Range is missing.
		return false
	}
	return !supported
}

func (d *Downloader) downloadSingle(
	ctx context.Context,
	urlResolver URLResolver,
	dest string,
	size int64,
	blobState *store.BlobState,
	commit func(func()) error,
) error {
	if err := applyCommit(commit, func() {
		blobState.State = "downloading"
	}); err != nil {
		return err
	}
	if err := d.pieceDownloader.DownloadSingleConnection(ctx, urlResolver, dest, size); err != nil {
		return err
	}
	return applyCommit(commit, func() {
		blobState.State = "complete"
	})
}

func applyCommit(commit func(func()) error, fn func()) error {
	if commit == nil {
		fn()
		return nil
	}
	return commit(fn)
}

func (d *Downloader) downloadMultiConnection(
	ctx context.Context,
	urlResolver URLResolver,
	dest string,
	size int64,
	blobState *store.BlobState,
	commit func(func()) error,
	onProgress func(downloaded int64),
) error {
	pieceSize, pieces := resolvePiecePlan(size, d.minSplitSize, d.connections, blobState.PieceSize, blobState.Pieces)

	bitfield := store.NewBitfield(pieces)
	resetBits := false
	if blobState.Bitfield != "" {
		if err := bitfield.FromHex(blobState.Bitfield); err != nil {
			// Corrupted bitfield, start fresh. Piece geometry stays the same.
			bitfield = store.NewBitfield(pieces)
			resetBits = true
		}
	}

	partPath := dest + ".part"
	partExists, destExists, err := existingFiles(partPath, dest)
	if err != nil {
		return err
	}
	if bitfield.Count() > 0 && !partExists && !destExists {
		for i := 0; i < bitfield.Pieces(); i++ {
			bitfield.Clear(i)
		}
		resetBits = true
	}

	if err := applyCommit(commit, func() {
		blobState.PieceSize = pieceSize
		blobState.Pieces = pieces
		blobState.State = "downloading"
		if resetBits {
			blobState.Bitfield = ""
		}
	}); err != nil {
		return err
	}

	missing := bitfield.Missing()
	if len(missing) == 0 && (partExists || destExists) {
		if partExists {
			return finishBlob(partPath, dest, blobState, commit)
		}
		return applyCommit(commit, func() {
			blobState.State = "complete"
		})
	}

	if err := preparePartFile(partPath, size); err != nil {
		return err
	}

	connections := d.connections
	if connections < 1 {
		connections = 1
	}

	var (
		wg       sync.WaitGroup
		errMu    sync.Mutex
		firstErr error
		sem      = make(chan struct{}, connections)
	)

	for _, pieceIdx := range missing {
		if err := ctx.Err(); err != nil {
			break
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				errMu.Lock()
				if firstErr == nil {
					firstErr = ctx.Err()
				}
				errMu.Unlock()
				return
			case sem <- struct{}{}:
			}
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

			var downloaded int64
			err = applyCommit(commit, func() {
				bitfield.Set(idx)
				blobState.Bitfield = bitfield.ToHex()
				downloaded = store.CoveredBytes(bitfield, pieceSize, size)
			})
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("save piece %d: %w", idx, err)
				}
				errMu.Unlock()
				return
			}
			if onProgress != nil {
				onProgress(downloaded)
			}
		}(pieceIdx)
	}

	wg.Wait()

	if firstErr != nil {
		return firstErr
	}

	return finishBlob(partPath, dest, blobState, commit)
}

func existingFiles(partPath, dest string) (partExists, destExists bool, err error) {
	switch _, err := os.Stat(partPath); {
	case err == nil:
		partExists = true
	case !os.IsNotExist(err):
		return false, false, fmt.Errorf("stat part file: %w", err)
	}
	switch _, err := os.Stat(dest); {
	case err == nil:
		destExists = true
	case !os.IsNotExist(err):
		return false, false, fmt.Errorf("stat blob: %w", err)
	}
	return partExists, destExists, nil
}

func preparePartFile(partPath string, size int64) error {
	f, err := os.OpenFile(partPath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("create part file: %w", err)
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		return fmt.Errorf("truncate part file: %w", err)
	}
	return f.Close()
}

func finishBlob(partPath, dest string, blobState *store.BlobState, commit func(func()) error) error {
	if err := os.Rename(partPath, dest); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return applyCommit(commit, func() {
		blobState.State = "complete"
	})
}

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
