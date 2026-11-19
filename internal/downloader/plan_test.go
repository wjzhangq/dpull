package downloader

import "testing"

func TestPlanPieceSizeCapsLargeBlobs(t *testing.T) {
	const sixGiB = 6 << 30
	got := planPieceSize(sixGiB, 20<<20, 8)
	if got != maxPieceSize {
		t.Fatalf("piece size = %d, want cap %d", got, maxPieceSize)
	}
	_, pieces := resolvePiecePlan(sixGiB, 20<<20, 8, 0, 0)
	if pieces < 90 {
		t.Fatalf("pieces = %d, want a checkpoint well under one connection per piece", pieces)
	}
}

func TestPlanPieceSizeRespectsMinimum(t *testing.T) {
	got := planPieceSize(100<<20, 20<<20, 8)
	if got != 20<<20 {
		t.Fatalf("piece size = %d, want 20MiB", got)
	}
}

func TestResolvePiecePlanKeepsSavedGeometry(t *testing.T) {
	size, pieces := resolvePiecePlan(6<<30, 20<<20, 16, 32<<20, 192)
	if size != 32<<20 || pieces != 192 {
		t.Fatalf("plan = %d x %d, want saved 32MiB x 192", size, pieces)
	}
}
