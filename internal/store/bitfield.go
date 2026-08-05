package store

import (
	"encoding/hex"
	"fmt"
	"sync"
)

// Bitfield tracks which pieces of a blob have been downloaded.
// Each bit corresponds to one piece; bit 0 is the most significant bit of
// byte 0, matching aria2's control-file convention.
//
// A Bitfield is safe for concurrent use.
type Bitfield struct {
	mu     sync.RWMutex
	pieces int
	bits   []byte
}

// NewBitfield creates a bitfield tracking the given number of pieces.
func NewBitfield(pieces int) *Bitfield {
	if pieces < 0 {
		pieces = 0
	}
	return &Bitfield{
		pieces: pieces,
		bits:   make([]byte, (pieces+7)/8),
	}
}

// Pieces returns the number of pieces tracked.
func (b *Bitfield) Pieces() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.pieces
}

// Set marks a piece as complete. Out-of-range indices are ignored.
func (b *Bitfield) Set(piece int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if piece < 0 || piece >= b.pieces {
		return
	}
	b.bits[piece/8] |= 1 << (7 - piece%8)
}

// Clear marks a piece as incomplete.
func (b *Bitfield) Clear(piece int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if piece < 0 || piece >= b.pieces {
		return
	}
	b.bits[piece/8] &^= 1 << (7 - piece%8)
}

// IsSet reports whether a piece is complete.
func (b *Bitfield) IsSet(piece int) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if piece < 0 || piece >= b.pieces {
		return false
	}
	return b.bits[piece/8]&(1<<(7-piece%8)) != 0
}

// Count returns the number of completed pieces.
func (b *Bitfield) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	n := 0
	for i := 0; i < b.pieces; i++ {
		if b.bits[i/8]&(1<<(7-i%8)) != 0 {
			n++
		}
	}
	return n
}

// Complete reports whether every piece is done.
func (b *Bitfield) Complete() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for i := 0; i < b.pieces; i++ {
		if b.bits[i/8]&(1<<(7-i%8)) == 0 {
			return false
		}
	}
	return true
}

// Missing returns the indices of pieces still to download, in order.
func (b *Bitfield) Missing() []int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var out []int
	for i := 0; i < b.pieces; i++ {
		if b.bits[i/8]&(1<<(7-i%8)) == 0 {
			out = append(out, i)
		}
	}
	return out
}

// ToHex encodes the bitfield as a lowercase hex string.
func (b *Bitfield) ToHex() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return hex.EncodeToString(b.bits)
}

// FromHex loads state from a hex string produced by ToHex. The piece count is
// preserved; a string of the wrong length is rejected so a stale state file
// cannot silently mark the wrong pieces complete.
func (b *Bitfield) FromHex(s string) error {
	if s == "" {
		return nil
	}
	bits, err := hex.DecodeString(s)
	if err != nil {
		return fmt.Errorf("decode bitfield: %w", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	want := (b.pieces + 7) / 8
	if len(bits) != want {
		return fmt.Errorf("bitfield length mismatch: got %d bytes, want %d for %d pieces",
			len(bits), want, b.pieces)
	}

	// Clear any padding bits beyond the piece count so Count/Complete stay honest.
	if rem := b.pieces % 8; rem != 0 && len(bits) > 0 {
		bits[len(bits)-1] &= ^byte(0) << (8 - rem)
	}

	b.bits = bits
	return nil
}
