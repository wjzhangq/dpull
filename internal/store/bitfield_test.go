package store

import (
	"testing"
)

func TestBitfield_SetIsSet(t *testing.T) {
	b := NewBitfield(16)

	// Initially all clear
	for i := 0; i < 16; i++ {
		if b.IsSet(i) {
			t.Errorf("piece %d set before Set called", i)
		}
	}

	// Set some pieces
	b.Set(0)
	b.Set(5)
	b.Set(15)

	// Check set pieces
	for i, want := range []bool{
		true, false, false, false, false, true, false, false,
		false, false, false, false, false, false, false, true,
	} {
		if got := b.IsSet(i); got != want {
			t.Errorf("piece %d: IsSet=%v, want %v", i, got, want)
		}
	}

	// Count
	if got := b.Count(); got != 3 {
		t.Errorf("Count() = %d, want 3", got)
	}

	// Not complete
	if b.Complete() {
		t.Errorf("Complete() = true, want false")
	}

	// Clear one
	b.Clear(5)
	if b.IsSet(5) {
		t.Error("piece 5 still set after Clear")
	}
	if got := b.Count(); got != 2 {
		t.Errorf("Count() = %d, want 2 after Clear(5)", got)
	}
}

func TestBitfield_Complete(t *testing.T) {
	b := NewBitfield(8)

	// Set all
	for i := 0; i < 8; i++ {
		b.Set(i)
	}

	if !b.Complete() {
		t.Error("Complete() = false after setting all pieces")
	}

	if got := b.Count(); got != 8 {
		t.Errorf("Count() = %d, want 8", got)
	}
}

func TestBitfield_Missing(t *testing.T) {
	b := NewBitfield(10)
	b.Set(0)
	b.Set(3)
	b.Set(9)

	want := []int{1, 2, 4, 5, 6, 7, 8}
	got := b.Missing()

	if len(got) != len(want) {
		t.Fatalf("Missing() len = %d, want %d", len(got), len(want))
	}

	for i, v := range want {
		if got[i] != v {
			t.Errorf("Missing()[%d] = %d, want %d", i, got[i], v)
		}
	}
}

func TestBitfield_HexRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		pieces int
		set    []int
	}{
		{
			name:   "8 pieces, 3 set",
			pieces: 8,
			set:    []int{0, 3, 7},
		},
		{
			name:   "16 pieces, various",
			pieces: 16,
			set:    []int{1, 5, 9, 15},
		},
		{
			name:   "5 pieces (non-byte-aligned)",
			pieces: 5,
			set:    []int{0, 2, 4},
		},
		{
			name:   "all clear",
			pieces: 12,
			set:    []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b1 := NewBitfield(tt.pieces)
			for _, i := range tt.set {
				b1.Set(i)
			}

			hex := b1.ToHex()

			b2 := NewBitfield(tt.pieces)
			if err := b2.FromHex(hex); err != nil {
				t.Fatalf("FromHex(%q): %v", hex, err)
			}

			// Check all pieces match
			for i := 0; i < tt.pieces; i++ {
				if b1.IsSet(i) != b2.IsSet(i) {
					t.Errorf("piece %d mismatch after round-trip", i)
				}
			}

			if b1.Count() != b2.Count() {
				t.Errorf("Count mismatch: %d vs %d", b1.Count(), b2.Count())
			}
		})
	}
}

func TestBitfield_FromHex_Error(t *testing.T) {
	tests := []struct {
		name    string
		pieces  int
		hex     string
		wantErr bool
	}{
		{
			name:    "length mismatch",
			pieces:  8,
			hex:     "0000", // 2 bytes, but 8 pieces needs 1
			wantErr: true,
		},
		{
			name:    "invalid hex",
			pieces:  8,
			hex:     "zz",
			wantErr: true,
		},
		{
			name:    "empty string ok",
			pieces:  8,
			hex:     "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBitfield(tt.pieces)
			err := b.FromHex(tt.hex)
			if (err != nil) != tt.wantErr {
				t.Errorf("FromHex() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBitfield_ByteBoundary(t *testing.T) {
	// Test across byte boundaries to catch bit-shift errors
	b := NewBitfield(17)

	// Set pieces in first byte, at boundary, and in second byte
	b.Set(0)   // byte 0, bit 7
	b.Set(7)   // byte 0, bit 0
	b.Set(8)   // byte 1, bit 7
	b.Set(15)  // byte 1, bit 0
	b.Set(16)  // byte 2, bit 7

	for i, want := range []bool{
		true, false, false, false, false, false, false, true,
		true, false, false, false, false, false, false, true,
		true,
	} {
		if got := b.IsSet(i); got != want {
			t.Errorf("piece %d: IsSet=%v, want %v", i, got, want)
		}
	}
}
