package model

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSafeJoin(t *testing.T) {
	root := t.TempDir()
	got, err := SafeJoin(root, "a/b.json")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "a", "b.json")
	if got != want {
		t.Fatalf("join = %s, want %s", got, want)
	}
	for _, rel := range []string{"../x", "/etc/passwd", `..\x`, ""} {
		if _, err := SafeJoin(root, rel); err == nil {
			t.Fatalf("SafeJoin(%q) succeeded", rel)
		}
	}
}

func TestPlaceLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks is restricted on Windows")
	}
	root := t.TempDir()
	if err := placeLink(root, "alias.bin", "weights.bin"); err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(filepath.Join(root, "alias.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "weights.bin" {
		t.Fatalf("link = %s", got)
	}
	if err := placeLink(root, "bad.bin", "../weights.bin"); err == nil {
		t.Fatal("escaping symlink was accepted")
	}
}
