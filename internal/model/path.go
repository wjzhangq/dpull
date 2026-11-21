package model

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// SafeJoin joins a repository-relative path onto root and rejects paths that escape it.
func SafeJoin(root, rel string) (string, error) {
	if rel == "" || strings.Contains(rel, "\x00") {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	rel = strings.ReplaceAll(rel, "\\", "/")
	if strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	full := filepath.Join(root, filepath.FromSlash(clean))
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	relCheck, err := filepath.Rel(rootAbs, fullAbs)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	return full, nil
}

func validLink(target string) error {
	if target == "" || strings.HasPrefix(target, "/") || strings.Contains(target, "\\") || strings.Contains(target, "..") {
		return fmt.Errorf("invalid symlink %q", target)
	}
	return nil
}

func placeFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if same, err := sameFile(src, dst); err != nil {
		return err
	} else if same {
		return nil
	}
	_ = os.Remove(dst)
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}

func placeLink(root, rel, target string) error {
	if err := validLink(target); err != nil {
		return err
	}
	dst, err := SafeJoin(root, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if existing, err := os.Readlink(dst); err == nil && existing == target {
		return nil
	}
	_ = os.Remove(dst)
	return os.Symlink(target, dst)
}

func sameFile(src, dst string) (bool, error) {
	a, err := os.Stat(src)
	if err != nil {
		return false, err
	}
	b, err := os.Stat(dst)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if os.SameFile(a, b) {
		return true, nil
	}
	return a.Size() == b.Size() && a.Size() > 0, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".mpull-tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	_ = os.Remove(dst)
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
