package downloader

// maxPieceSize caps a piece so a crash loses at most this much of a layer.
// The connection count still limits how many pieces run at once; it no longer
// decides the piece size for large blobs.
const maxPieceSize int64 = 64 << 20

// planPieceSize picks a piece size for a blob that will be split.
func planPieceSize(size, minSplit int64, connections int) int64 {
	if size <= 0 {
		return 0
	}
	if connections < 1 {
		connections = 1
	}
	if minSplit <= 0 {
		minSplit = 20 << 20
	}

	pieceSize := size / int64(connections)
	if pieceSize < minSplit {
		pieceSize = minSplit
	}
	if pieceSize > maxPieceSize {
		pieceSize = maxPieceSize
	}
	if pieceSize > size {
		pieceSize = size
	}
	return pieceSize
}

// resolvePiecePlan keeps a saved plan so a later -x change cannot invalidate
// the bitfield. A new blob gets a fresh plan.
func resolvePiecePlan(size, minSplit int64, connections int, savedSize int64, savedPieces int) (int64, int) {
	if savedSize > 0 && savedPieces > 0 {
		return savedSize, savedPieces
	}
	pieceSize := planPieceSize(size, minSplit, connections)
	if pieceSize <= 0 {
		return size, 1
	}
	pieces := int((size + pieceSize - 1) / pieceSize)
	if pieces < 1 {
		pieces = 1
	}
	return pieceSize, pieces
}
