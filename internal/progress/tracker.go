package progress

// Tracker is the interface for progress tracking
type Tracker interface {
	AddBlob(digest string, size int64, index, total int)
	UpdateBlob(digest string, downloaded int64)
	CompleteBlob(digest string)
	SetTotal(digest string, total int64)
	Wait()
	Summary() string
}

// NewTracker creates a progress tracker based on mode
func NewTracker(mode string, totalSize int64) Tracker {
	switch mode {
	case "bar":
		return NewBarTracker(totalSize)
	case "plain":
		return NewPlainTracker(totalSize)
	case "json":
		return NewJSONTracker(totalSize)
	case "none":
		return &noopTracker{}
	default:
		return NewBarTracker(totalSize)
	}
}

type noopTracker struct{}

func (t *noopTracker) AddBlob(digest string, size int64, index, total int) {}
func (t *noopTracker) UpdateBlob(digest string, downloaded int64)          {}
func (t *noopTracker) CompleteBlob(digest string)                          {}
func (t *noopTracker) SetTotal(digest string, total int64)                 {}
func (t *noopTracker) Wait()                                              {}
func (t *noopTracker) Summary() string                                    { return "" }
