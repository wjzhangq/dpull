package progress

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// JSONTracker outputs progress as JSON events
type JSONTracker struct {
	blobs     map[string]*jsonBlobProgress
	aggregate *Aggregate
}

type jsonBlobProgress struct {
	digest string
	size   int64
	index  int
	total  int
}

// JSONEvent represents a progress event
type JSONEvent struct {
	Timestamp string  `json:"ts"`
	Event     string  `json:"event"`
	Digest    string  `json:"digest,omitempty"`
	Index     int     `json:"index,omitempty"`
	Total     int     `json:"total,omitempty"`
	Size      int64   `json:"size,omitempty"`
	Done      int64   `json:"done,omitempty"`
	Percent   float64 `json:"percent,omitempty"`
	Verified  bool    `json:"verified,omitempty"`
}

// NewJSONTracker creates a new JSON progress tracker
func NewJSONTracker(totalSize int64) *JSONTracker {
	return &JSONTracker{
		blobs:     make(map[string]*jsonBlobProgress),
		aggregate: NewAggregate(totalSize),
	}
}

// AddBlob emits a layer_start event
func (t *JSONTracker) AddBlob(digest string, size int64, index, total int) {
	t.blobs[digest] = &jsonBlobProgress{
		digest: digest,
		size:   size,
		index:  index,
		total:  total,
	}

	t.emit(JSONEvent{
		Timestamp: time.Now().Format(time.RFC3339),
		Event:     "layer_start",
		Digest:    digest,
		Index:     index,
		Total:     total,
		Size:      size,
	})
}

// UpdateBlob emits a layer_progress event
func (t *JSONTracker) UpdateBlob(digest string, downloaded int64) {
	blob, ok := t.blobs[digest]
	if !ok {
		return
	}

	pct := float64(0)
	if blob.size > 0 {
		pct = float64(downloaded) / float64(blob.size) * 100
	}

	t.emit(JSONEvent{
		Timestamp: time.Now().Format(time.RFC3339),
		Event:     "layer_progress",
		Digest:    digest,
		Done:      downloaded,
		Size:      blob.size,
		Percent:   pct,
	})
}

// CompleteBlob emits a layer_complete event
func (t *JSONTracker) CompleteBlob(digest string) {
	t.emit(JSONEvent{
		Timestamp: time.Now().Format(time.RFC3339),
		Event:     "layer_complete",
		Digest:    digest,
		Verified:  false,
	})
}

// SetTotal updates blob size (no event emitted)
func (t *JSONTracker) SetTotal(digest string, total int64) {
	if blob, ok := t.blobs[digest]; ok {
		blob.size = total
	}
}

// Wait is a no-op for JSON tracker
func (t *JSONTracker) Wait() {
	// No-op
}

// Summary emits an overall progress event
func (t *JSONTracker) Summary() string {
	downloaded, total, pct, _, _ := t.aggregate.Stats()

	t.emit(JSONEvent{
		Timestamp: time.Now().Format(time.RFC3339),
		Event:     "overall_progress",
		Done:      downloaded,
		Size:      total,
		Percent:   pct,
	})

	return fmt.Sprintf("%.1f%% complete", pct)
}

func (t *JSONTracker) emit(event JSONEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	fmt.Fprintln(os.Stdout, string(data))
}
