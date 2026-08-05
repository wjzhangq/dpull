package progress

import (
	"fmt"

	"github.com/docker/go-units"
)

// PlainTracker displays simple text progress
type PlainTracker struct {
	blobs     map[string]*blobProgress
	aggregate *Aggregate
}

type blobProgress struct {
	digest     string
	size       int64
	downloaded int64
	index      int
	total      int
}

// NewPlainTracker creates a new plain text progress tracker
func NewPlainTracker(totalSize int64) *PlainTracker {
	return &PlainTracker{
		blobs:     make(map[string]*blobProgress),
		aggregate: NewAggregate(totalSize),
	}
}

// AddBlob registers a blob for tracking
func (t *PlainTracker) AddBlob(digest string, size int64, index, total int) {
	t.blobs[digest] = &blobProgress{
		digest: digest,
		size:   size,
		index:  index,
		total:  total,
	}
}

// UpdateBlob updates and prints progress for a blob
func (t *PlainTracker) UpdateBlob(digest string, downloaded int64) {
	blob, ok := t.blobs[digest]
	if !ok {
		return
	}

	blob.downloaded = downloaded
	t.aggregate.Update(downloaded)

	// Print progress line
	shortDigest := digest
	if len(digest) > 15 {
		shortDigest = digest[7:15]
	}

	pct := float64(0)
	if blob.size > 0 {
		pct = float64(downloaded) / float64(blob.size) * 100
	}

	_, _, _, speed, _ := t.aggregate.Stats()
	speedStr := units.HumanSize(speed) + "/s"

	fmt.Printf("[%2d/%2d] %s %s/%s %.1f%% %s\n",
		blob.index,
		blob.total,
		shortDigest,
		units.HumanSize(float64(downloaded)),
		units.HumanSize(float64(blob.size)),
		pct,
		speedStr,
	)
}

// CompleteBlob prints completion message for a blob
func (t *PlainTracker) CompleteBlob(digest string) {
	blob, ok := t.blobs[digest]
	if !ok {
		return
	}

	blob.downloaded = blob.size

	shortDigest := digest
	if len(digest) > 15 {
		shortDigest = digest[7:15]
	}

	fmt.Printf("[%2d/%2d] %s complete (%s)\n",
		blob.index,
		blob.total,
		shortDigest,
		units.HumanSize(float64(blob.size)),
	)
}

// SetTotal updates the total size for a blob
func (t *PlainTracker) SetTotal(digest string, total int64) {
	if blob, ok := t.blobs[digest]; ok {
		blob.size = total
	}
}

// Wait is a no-op for plain tracker
func (t *PlainTracker) Wait() {
	// No-op for plain output
}

// Summary returns overall progress summary
func (t *PlainTracker) Summary() string {
	downloaded, total, pct, speed, eta := t.aggregate.Stats()

	speedStr := units.HumanSize(speed) + "/s"
	etaStr := "calculating..."
	if eta > 0 {
		etaStr = formatDuration(eta)
	}

	return fmt.Sprintf("Overall: %.1f%% (%s / %s) at %s, ETA %s",
		pct,
		units.HumanSize(float64(downloaded)),
		units.HumanSize(float64(total)),
		speedStr,
		etaStr,
	)
}

