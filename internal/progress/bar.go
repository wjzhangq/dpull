package progress

import (
	"fmt"
	"sync"

	"github.com/docker/go-units"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

// BarTracker displays progress bars using mpb
type BarTracker struct {
	mu        sync.Mutex
	container *mpb.Progress
	bars      map[string]*mpb.Bar
	sizes     map[string]int64
	lastDone  map[string]int64
	aggregate *Aggregate
}

// NewBarTracker creates a new progress bar tracker
func NewBarTracker(totalSize int64) *BarTracker {
	return &BarTracker{
		container: mpb.New(mpb.WithWidth(80)),
		bars:      make(map[string]*mpb.Bar),
		sizes:     make(map[string]int64),
		lastDone:  make(map[string]int64),
		aggregate: NewAggregate(totalSize),
	}
}

// AddBlob adds a progress bar for a blob
func (t *BarTracker) AddBlob(digest string, size int64, index, total int) {
	if t.container == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Short digest for display (first 8 hex chars after sha256:)
	shortDigest := digest
	if len(digest) > 15 {
		shortDigest = digest[7:15]
	}

	bar := t.container.AddBar(size,
		mpb.PrependDecorators(
			decor.Name(fmt.Sprintf("[%2d/%2d] %s", index, total, shortDigest), decor.WCSyncSpace),
			decor.Counters(decor.SizeB1024(0), "% 6.1f / % 6.1f", decor.WCSyncSpace),
		),
		mpb.AppendDecorators(
			decor.Percentage(decor.WCSyncSpace),
			decor.AverageSpeed(decor.SizeB1024(0), "% .1f", decor.WCSyncSpace),
		),
	)

	t.bars[digest] = bar
	t.sizes[digest] = size
	t.lastDone[digest] = 0
}

// UpdateBlob updates progress for a specific blob
func (t *BarTracker) UpdateBlob(digest string, downloaded int64) {
	if bar, ok := t.bars[digest]; ok {
		bar.SetCurrent(downloaded)
	}
	t.aggregate.Add(0) // Update timestamp
}

// CompleteBlob marks a blob as complete
func (t *BarTracker) CompleteBlob(digest string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if bar, ok := t.bars[digest]; ok {
		if size, exists := t.sizes[digest]; exists {
			bar.SetCurrent(size)
		}
	}
}

// SetTotal sets the total size for a blob (for late initialization)
func (t *BarTracker) SetTotal(digest string, total int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.sizes[digest] = total
	if bar, ok := t.bars[digest]; ok {
		bar.SetTotal(total, false)
	}
}

// Wait waits for all progress bars to complete
func (t *BarTracker) Wait() {
	if t.container != nil {
		t.container.Wait()
	}
}

// Summary returns a progress summary
func (t *BarTracker) Summary() string {
	downloaded, total, pct, speed, eta := t.aggregate.Stats()

	speedStr := units.HumanSize(speed) + "/s"
	etaStr := "calculating..."
	if eta > 0 {
		etaStr = formatDuration(eta)
	}

	return fmt.Sprintf("%.1f%% (%s / %s) at %s, ETA %s",
		pct,
		units.HumanSize(float64(downloaded)),
		units.HumanSize(float64(total)),
		speedStr,
		etaStr,
	)
}

