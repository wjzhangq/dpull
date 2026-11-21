package model

import (
	"path"
	"sort"
	"strings"
)

// Filter keeps files matching any include, then drops excludes.
// An empty include list keeps every file. Patterns without a slash match the base name.
func Filter(files []File, includes, excludes []string) []File {
	var out []File
	for _, file := range files {
		if file.Path == "" {
			continue
		}
		if len(includes) > 0 && !anyMatch(includes, file.Path) {
			continue
		}
		if anyMatch(excludes, file.Path) {
			continue
		}
		out = append(out, file)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func anyMatch(patterns []string, filePath string) bool {
	for _, pattern := range patterns {
		if matchPattern(pattern, filePath) {
			return true
		}
	}
	return false
}

func matchPattern(pattern, filePath string) bool {
	pattern = strings.ReplaceAll(pattern, "\\", "/")
	filePath = strings.ReplaceAll(filePath, "\\", "/")
	if pattern == filePath {
		return true
	}
	if ok, err := path.Match(pattern, filePath); err == nil && ok {
		return true
	}
	base := path.Base(filePath)
	if !strings.Contains(pattern, "/") {
		if ok, err := path.Match(pattern, base); err == nil && ok {
			return true
		}
	}
	if rest, ok := strings.CutPrefix(pattern, "**/"); ok {
		if ok, err := path.Match(rest, base); err == nil && ok {
			return true
		}
	}
	return false
}
