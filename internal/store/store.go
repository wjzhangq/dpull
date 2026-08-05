package store

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Store manages blob cache and task state
type Store struct {
	cacheDir string
	tasksDir string
}

// NewStore creates a new store
func NewStore(cacheDir string) (*Store, error) {
	tasksDir := filepath.Join(cacheDir, "tasks")

	// Create directories
	if err := os.MkdirAll(filepath.Join(cacheDir, "blobs", "sha256"), 0755); err != nil {
		return nil, fmt.Errorf("create blobs dir: %w", err)
	}
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		return nil, fmt.Errorf("create tasks dir: %w", err)
	}

	return &Store{
		cacheDir: cacheDir,
		tasksDir: tasksDir,
	}, nil
}

// BlobPath returns the path where a blob should be stored
// digest format: "sha256:abcdef..."
func (s *Store) BlobPath(digest string) string {
	// Strip "sha256:" prefix if present
	if len(digest) > 7 && digest[:7] == "sha256:" {
		digest = digest[7:]
	}
	return filepath.Join(s.cacheDir, "blobs", "sha256", digest)
}

// BlobExists checks if a blob is already downloaded and complete
func (s *Store) BlobExists(digest string) bool {
	path := s.BlobPath(digest)
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// BlobSize returns the size of a cached blob, or -1 if not found
func (s *Store) BlobSize(digest string) int64 {
	info, err := os.Stat(s.BlobPath(digest))
	if err != nil {
		return -1
	}
	return info.Size()
}

// TaskPath returns the path to a task state file
func (s *Store) TaskPath(taskID string) string {
	return filepath.Join(s.tasksDir, taskID+".json")
}

// TaskExists checks if a task state file exists
func (s *Store) TaskExists(taskID string) bool {
	_, err := os.Stat(s.TaskPath(taskID))
	return err == nil
}

// ListTasks returns all task state files
func (s *Store) ListTasks() ([]*TaskState, error) {
	entries, err := os.ReadDir(s.tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read tasks dir: %w", err)
	}

	var tasks []*TaskState
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(s.tasksDir, entry.Name())
		task, err := LoadTask(path)
		if err != nil {
			// Skip corrupted files
			continue
		}

		tasks = append(tasks, task)
	}

	return tasks, nil
}

// VerifyBlob checks that a blob's sha256 matches its digest
func (s *Store) VerifyBlob(digest string) (bool, error) {
	path := s.BlobPath(digest)
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open blob: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, fmt.Errorf("hash blob: %w", err)
	}

	computed := fmt.Sprintf("sha256:%x", h.Sum(nil))
	expected := digest
	if len(digest) > 7 && digest[:7] != "sha256:" {
		expected = "sha256:" + digest
	}

	return computed == expected, nil
}

// RemoveBlob removes a blob from cache
func (s *Store) RemoveBlob(digest string) error {
	path := s.BlobPath(digest)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove blob: %w", err)
	}
	// Also remove .part file if exists
	if err := os.Remove(path + ".part"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove blob part: %w", err)
	}
	return nil
}

// RemoveTask removes a task state file
func (s *Store) RemoveTask(taskID string) error {
	path := s.TaskPath(taskID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove task: %w", err)
	}
	return nil
}
