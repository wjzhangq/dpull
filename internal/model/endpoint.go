package model

import (
	"fmt"
	"strings"
)

// Endpoint is one concrete hub request: host, repository, and revision.
type Endpoint struct {
	Kind            Kind
	Host            string
	Base            string
	ID              string
	Revision        string
	DefaultRevision bool
	Pinned          bool
	File            string
}

// Resolve picks the host. An explicit mirror or a host in the reference is pinned.
// Otherwise a Hugging Face id uses hf-mirror.com, or huggingface.co when a proxy is set.
func Resolve(ref Ref, mirror, revision, proxy string) (Endpoint, error) {
	kind := ref.Kind
	host := ref.Host
	pinned := ref.Host != ""

	if mirror != "" {
		mk, mh, err := parseMirror(mirror)
		if err != nil {
			return Endpoint{}, err
		}
		if mk != kind {
			if mk == KindModelScope {
				return Endpoint{}, fmt.Errorf("-m modelscope does not rewrite a Hugging Face id; use ms:org/name")
			}
			return Endpoint{}, fmt.Errorf("-m %s only applies to Hugging Face repositories", mirror)
		}
		host = mh
		kind = mk
		pinned = true
	}

	if host == "" {
		switch {
		case kind == KindModelScope:
			host = HostModelScope
		case proxy != "":
			host = HostHuggingFace
		default:
			host = HostHFMirror
		}
	}

	rev := revision
	if rev == "" {
		rev = ref.Revision
	}
	defaultRev := false
	if rev == "" {
		defaultRev = true
		if kind == KindModelScope {
			rev = "master"
		} else {
			rev = "main"
		}
	}
	if err := validateRevision(rev); err != nil {
		return Endpoint{}, err
	}

	return Endpoint{
		Kind:            kind,
		Host:            host,
		Base:            "https://" + host,
		ID:              ref.ID,
		Revision:        rev,
		DefaultRevision: defaultRev,
		Pinned:          pinned,
		File:            ref.File,
	}, nil
}

func parseMirror(spec string) (Kind, string, error) {
	switch strings.ToLower(strings.TrimSpace(spec)) {
	case "hf-mirror", "mirror", HostHFMirror:
		return KindHF, HostHFMirror, nil
	case "huggingface", "hf", "official", HostHuggingFace, "hf.co":
		return KindHF, HostHuggingFace, nil
	case "modelscope", "ms", HostModelScope, "modelscope.cn":
		return KindModelScope, HostModelScope, nil
	default:
		return "", "", fmt.Errorf("unknown mirror %q (hf-mirror, huggingface, modelscope)", spec)
	}
}

// Fallback is the other Hugging Face host. A pinned endpoint does not move.
func (e Endpoint) Fallback() (Endpoint, bool) {
	if e.Pinned || e.Kind != KindHF {
		return Endpoint{}, false
	}
	next := e
	next.Pinned = true
	switch e.Host {
	case HostHFMirror:
		next.Host = HostHuggingFace
	case HostHuggingFace:
		next.Host = HostHFMirror
	default:
		return Endpoint{}, false
	}
	next.Base = "https://" + next.Host
	return next, true
}

// Canonical is the repository identity stored with a task. The host is not part of it.
func (e Endpoint) Canonical() string {
	return Ref{Kind: e.Kind, ID: e.ID}.Canonical(e.Revision)
}

func alternateRevision(rev string) (string, bool) {
	switch rev {
	case "main":
		return "master", true
	case "master":
		return "main", true
	default:
		return "", false
	}
}
