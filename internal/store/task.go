package store

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// TaskState represents the persistent state of a download task.
// Save and Mutate serialize writers so concurrent piece updates cannot
// persist a stale bitfield.
type TaskState struct {
	mu             sync.Mutex  `json:"-"`
	TaskID         string      `json:"task_id"`
	Version        int         `json:"version"`
	Canonical      string      `json:"canonical"`
	Platform       string      `json:"platform"`
	ManifestDigest string      `json:"manifest_digest"`
	ConfigDigest   string      `json:"config_digest"`
	Mirror         MirrorInfo  `json:"mirror"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	TotalSize      int64       `json:"total_size"`
	Blobs          []BlobState `json:"blobs"`
}

// MirrorInfo holds mirror configuration used for this task
type MirrorInfo struct {
	Endpoint string `json:"endpoint"`
	Path     string `json:"path"`
}

// BlobState tracks download state for a single blob
type BlobState struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	PieceSize int64  `json:"piece_size,omitempty"`
	Pieces    int    `json:"pieces,omitempty"`
	Bitfield  string `json:"bitfield,omitempty"` // Hex string
	State     string `json:"state"`              // "pending", "downloading", "complete"
	Verified  bool   `json:"verified"`
}

// NewTaskState creates a new task state
func NewTaskState(taskID, canonical, platform string) *TaskState {
	now := time.Now()
	return &TaskState{
		TaskID:    taskID,
		Version:   1,
		Canonical: canonical,
		Platform:  platform,
		CreatedAt: now,
		UpdatedAt: now,
		Blobs:     []BlobState{},
	}
}

// Save atomically writes the task state to disk.
// Uses tmp → fsync → rename to stay durable across a crash.
func (t *TaskState) Save(path string) error {
	return t.Mutate(path, nil)
}

// Mutate applies fn and then persists the task. fn runs under the task lock;
// it must not call Save or Mutate.
func (t *TaskState) Mutate(path string, fn func()) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if fn != nil {
		fn()
	}
	if path == "" {
		return nil
	}
	return t.saveLocked(path)
}

func (t *TaskState) saveLocked(path string) error {
	t.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}

	// Write to temporary file
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write temp file: %w", err)
	}

	// Flush to disk before rename
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("sync temp file: %w", err)
	}

	f.Close()

	// Atomic rename
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// LoadTask loads a task state from disk
func LoadTask(path string) (*TaskState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read task file: %w", err)
	}

	var task TaskState
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, fmt.Errorf("parse task file: %w", err)
	}

	return &task, nil
}

// BytesDownloaded reports durable progress for one blob.
// A short final piece is counted at its real length, so the total cannot
// exceed the blob size.
func (b BlobState) BytesDownloaded() int64 {
	if b.State == "complete" && b.Verified {
		return b.Size
	}
	if b.Pieces <= 0 || b.PieceSize <= 0 || b.Bitfield == "" {
		return 0
	}
	bf := NewBitfield(b.Pieces)
	if err := bf.FromHex(b.Bitfield); err != nil {
		return 0
	}
	return CoveredBytes(bf, b.PieceSize, b.Size)
}

// Progress returns download progress as a fraction [0, 1]
func (t *TaskState) Progress() float64 {
	if t.TotalSize == 0 {
		return 0
	}

	var downloaded int64
	for _, blob := range t.Blobs {
		downloaded += blob.BytesDownloaded()
	}
	if downloaded > t.TotalSize {
		downloaded = t.TotalSize
	}
	return float64(downloaded) / float64(t.TotalSize)
}
