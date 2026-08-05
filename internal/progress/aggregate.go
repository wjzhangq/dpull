package progress

import (
	"sync"
	"time"
)

// Aggregate tracks aggregated progress across multiple blobs
type Aggregate struct {
	mu            sync.Mutex
	totalBytes    int64
	downloadedBytes int64
	startTime     time.Time
	lastUpdate    time.Time
	bytesAtLast   int64
}

// NewAggregate creates a new aggregate progress tracker
func NewAggregate(totalBytes int64) *Aggregate {
	now := time.Now()
	return &Aggregate{
		totalBytes:  totalBytes,
		startTime:   now,
		lastUpdate:  now,
	}
}

// Update records progress for a single blob
func (a *Aggregate) Update(downloaded int64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.downloadedBytes = downloaded
	a.lastUpdate = time.Now()
}

// Add adds bytes to the total downloaded
func (a *Aggregate) Add(bytes int64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.downloadedBytes += bytes
	a.lastUpdate = time.Now()
}

// Stats returns current progress statistics
func (a *Aggregate) Stats() (downloaded, total int64, percentage float64, speed float64, eta time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()

	downloaded = a.downloadedBytes
	total = a.totalBytes

	if total > 0 {
		percentage = float64(downloaded) / float64(total) * 100
	}

	elapsed := time.Since(a.startTime).Seconds()
	if elapsed > 0 {
		speed = float64(downloaded) / elapsed
	}

	if speed > 0 && downloaded < total {
		remaining := total - downloaded
		eta = time.Duration(float64(remaining)/speed) * time.Second
	}

	return
}

// InstantSpeed returns speed since last update
func (a *Aggregate) InstantSpeed() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()

	elapsed := time.Since(a.lastUpdate).Seconds()
	if elapsed == 0 {
		return 0
	}

	bytesSinceLast := a.downloadedBytes - a.bytesAtLast
	a.bytesAtLast = a.downloadedBytes

	return float64(bytesSinceLast) / elapsed
}
