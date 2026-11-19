package store

import "testing"

func TestCoveredBytesShortLastPiece(t *testing.T) {
	bf := NewBitfield(8)
	bf.Set(0)
	bf.Set(7)

	// 8 pieces of 10 bytes would be 80, but the blob is 75 so the last piece is 5.
	got := CoveredBytes(bf, 10, 75)
	if got != 15 {
		t.Fatalf("CoveredBytes = %d, want 15", got)
	}
}

func TestProgressDoesNotExceedTotal(t *testing.T) {
	bf := NewBitfield(8)
	for i := 0; i < 8; i++ {
		bf.Set(i)
	}
	task := &TaskState{
		TotalSize: 75,
		Blobs: []BlobState{{
			Size:      75,
			PieceSize: 10,
			Pieces:    8,
			Bitfield:  bf.ToHex(),
			State:     "downloading",
		}},
	}
	if task.Progress() > 1 {
		t.Fatalf("progress = %v, want <= 1", task.Progress())
	}
	if got := task.Blobs[0].BytesDownloaded(); got != 75 {
		t.Fatalf("BytesDownloaded = %d, want 75", got)
	}
}

func TestBytesDownloadedVerified(t *testing.T) {
	b := BlobState{Size: 100, State: "complete", Verified: true}
	if got := b.BytesDownloaded(); got != 100 {
		t.Fatalf("BytesDownloaded = %d, want 100", got)
	}
}
