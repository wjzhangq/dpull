package store

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// TaskState represents the persistent state of a download task
type TaskState struct {
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

// Save atomically writes the task state to disk
// Uses tmp → fsync → rename pattern to ensure durability
func (t *TaskState) Save(path string) error {
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

// Progress returns download progress as a fraction [0, 1]
func (t *TaskState) Progress() float64 {
	if t.TotalSize == 0 {
		return 0
	}

	var downloaded int64
	for _, blob := range t.Blobs {
		if blob.State == "complete" && blob.Verified {
			downloaded += blob.Size
		} else if blob.State == "downloading" && blob.Bitfield != "" {
			// Estimate progress from bitfield
			bf := NewBitfield(blob.Pieces)
			if err := bf.FromHex(blob.Bitfield); err == nil {
				downloaded += int64(bf.Count()) * blob.PieceSize
			}
		}
	}

	return float64(downloaded) / float64(t.TotalSize)
}
