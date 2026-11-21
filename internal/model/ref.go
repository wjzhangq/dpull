package model

import (
	"fmt"
	"net/url"
	"strings"
)

// Kind identifies which hub a repository lives on.
type Kind string

const (
	KindHF         Kind = "hf"
	KindModelScope Kind = "modelscope"
)

const (
	HostHuggingFace = "huggingface.co"
	HostHFMirror    = "hf-mirror.com"
	HostModelScope  = "www.modelscope.cn"
)

// Ref is a model repository, optionally pinned to a host, revision, or one file.
type Ref struct {
	Kind         Kind
	ExplicitKind bool
	Host         string
	ID           string
	Revision     string
	File         string
}

// Parse accepts org/name, hf: and ms: prefixes, and hub page URLs.
func Parse(raw string) (Ref, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Ref{}, fmt.Errorf("model reference is empty")
	}
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimSuffix(raw, "/")
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimPrefix(raw, "//")

	ref := Ref{Kind: KindHF}
	switch {
	case strings.HasPrefix(raw, "hf:"):
		raw = strings.TrimPrefix(raw, "hf:")
		ref.ExplicitKind = true
	case strings.HasPrefix(raw, "huggingface:"):
		raw = strings.TrimPrefix(raw, "huggingface:")
		ref.ExplicitKind = true
	case strings.HasPrefix(raw, "ms:"):
		raw = strings.TrimPrefix(raw, "ms:")
		ref.Kind = KindModelScope
		ref.ExplicitKind = true
	case strings.HasPrefix(raw, "modelscope:"):
		raw = strings.TrimPrefix(raw, "modelscope:")
		ref.Kind = KindModelScope
		ref.ExplicitKind = true
	}
	raw = strings.TrimPrefix(raw, "//")
	raw = strings.Trim(raw, "/")

	if host, rest, ok := splitKnownHost(raw); ok {
		ref.Host = host
		ref.Kind = kindOfHost(host)
		ref.ExplicitKind = true
		raw = rest
	}

	id, rev, file, err := splitRepoPath(ref.Kind, raw)
	if err != nil {
		return Ref{}, err
	}
	if err := validateID(id); err != nil {
		return Ref{}, err
	}
	if rev != "" {
		if err := validateRevision(rev); err != nil {
			return Ref{}, err
		}
	}
	ref.ID = id
	ref.Revision = rev
	ref.File = file
	return ref, nil
}

func splitKnownHost(raw string) (host, rest string, ok bool) {
	name, rest, found := strings.Cut(raw, "/")
	if !found {
		return "", "", false
	}
	switch strings.ToLower(name) {
	case HostHuggingFace, "hf.co":
		return HostHuggingFace, rest, true
	case HostHFMirror:
		return HostHFMirror, rest, true
	case HostModelScope, "modelscope.cn":
		return HostModelScope, rest, true
	default:
		return "", "", false
	}
}

func kindOfHost(host string) Kind {
	if host == HostModelScope {
		return KindModelScope
	}
	return KindHF
}

func splitRepoPath(kind Kind, raw string) (id, rev, file string, err error) {
	raw = strings.Trim(raw, "/")
	if raw == "" {
		return "", "", "", fmt.Errorf("model id must look like org/name")
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "datasets/") || strings.HasPrefix(lower, "dataset/") || strings.HasPrefix(lower, "spaces/"):
		return "", "", "", fmt.Errorf("only model repositories are supported")
	}
	if kind == KindModelScope {
		raw = strings.TrimPrefix(raw, "models/")
		raw = strings.TrimPrefix(raw, "Models/")
	}

	if id, rev, found := cutMark(raw, "/tree/"); found {
		return id, rev, "", nil
	}
	if id, rev, found := cutMark(raw, "/commit/"); found {
		return id, rev, "", nil
	}
	for _, mark := range []string{"/blob/", "/resolve/", "/raw/"} {
		if head, tail, found := cutMark(raw, mark); found {
			rev, file, err = splitRevFile(tail)
			if err != nil {
				return "", "", "", err
			}
			return head, rev, file, nil
		}
	}

	if kind == KindModelScope {
		parts := strings.Split(raw, "/")
		if len(parts) >= 3 && isModelScopePage(parts[2]) {
			raw = strings.Join(parts[:2], "/")
		}
	}

	id, rev = splitAt(raw)
	if unescaped, err := url.PathUnescape(id); err == nil {
		id = unescaped
	}
	return id, rev, "", nil
}

func cutMark(raw, mark string) (head, tail string, ok bool) {
	idx := strings.Index(raw, mark)
	if idx <= 0 {
		return "", "", false
	}
	return raw[:idx], strings.Trim(raw[idx+len(mark):], "/"), true
}

func splitRevFile(tail string) (rev, file string, err error) {
	rev, file, found := strings.Cut(tail, "/")
	if !found || rev == "" || file == "" {
		return "", "", fmt.Errorf("file URL must include a revision and a path")
	}
	file, err = url.PathUnescape(file)
	if err != nil {
		return "", "", fmt.Errorf("decode file path: %w", err)
	}
	return rev, file, nil
}

func splitAt(raw string) (id, rev string) {
	id, rev, _ = strings.Cut(raw, "@")
	return id, rev
}

func isModelScopePage(seg string) bool {
	switch strings.ToLower(seg) {
	case "summary", "files", "feedback", "quickstart":
		return true
	default:
		return false
	}
}

func validateID(id string) error {
	parts := strings.Split(id, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("model id must look like org/name")
	}
	if parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
		return fmt.Errorf("model id must look like org/name")
	}
	return nil
}

func validateRevision(rev string) error {
	if rev == "" || strings.Contains(rev, "..") || strings.ContainsAny(rev, " \t?#\\") {
		return fmt.Errorf("invalid revision %q", rev)
	}
	return nil
}

// Canonical is the identity stored in the task file. The mirror host is not part of it.
func (r Ref) Canonical(revision string) string {
	scheme := "hf"
	if r.Kind == KindModelScope {
		scheme = "ms"
	}
	return scheme + "://" + r.ID + "@" + revision
}
