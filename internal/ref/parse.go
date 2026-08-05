package ref

import (
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
)

// Reference represents a parsed image reference
type Reference struct {
	Registry  string
	Namespace string
	Name      string
	Tag       string
	Digest    string
	Original  string
}

// Parse parses an image reference and normalizes it
// Examples:
//   nginx → docker.io/library/nginx:latest
//   ubuntu:20.04 → docker.io/library/ubuntu:20.04
//   lmsysorg/sglang:v1 → docker.io/lmsysorg/sglang:v1
//   ghcr.io/foo/bar:v1 → ghcr.io/foo/bar:v1
//   image@sha256:abc... → digest reference
func Parse(s string) (*Reference, error) {
	ref := &Reference{Original: s}

	// Use go-containerregistry for parsing
	nameRef, err := name.ParseReference(s)
	if err != nil {
		return nil, fmt.Errorf("parse reference: %w", err)
	}

	// Extract registry
	ref.Registry = nameRef.Context().RegistryStr()

	// Extract repo (includes namespace)
	repo := nameRef.Context().RepositoryStr()

	// Split namespace and name
	parts := strings.Split(repo, "/")
	if len(parts) == 1 {
		// No namespace (e.g., localhost:5000/alpine or official images already normalized to library/alpine by go-containerregistry)
		ref.Namespace = ""
		ref.Name = parts[0]
	} else if len(parts) == 2 && parts[0] == "library" {
		// Official image normalized by go-containerregistry: library/nginx
		ref.Namespace = "library"
		ref.Name = parts[1]
	} else {
		// Multi-part namespace or user namespace
		ref.Namespace = strings.Join(parts[:len(parts)-1], "/")
		ref.Name = parts[len(parts)-1]
	}

	// Extract tag or digest
	if digested, ok := nameRef.(name.Digest); ok {
		ref.Digest = digested.DigestStr()
	} else if tagged, ok := nameRef.(name.Tag); ok {
		ref.Tag = tagged.TagStr()
	} else {
		ref.Tag = "latest"
	}

	return ref, nil
}

// String returns the canonical string representation
func (r *Reference) String() string {
	var sb strings.Builder
	sb.WriteString(r.Registry)
	sb.WriteString("/")
	if r.Namespace != "" {
		sb.WriteString(r.Namespace)
		sb.WriteString("/")
	}
	sb.WriteString(r.Name)

	if r.Digest != "" {
		sb.WriteString("@")
		sb.WriteString(r.Digest)
	} else if r.Tag != "" {
		sb.WriteString(":")
		sb.WriteString(r.Tag)
	}

	return sb.String()
}

// Repo returns the full repository path (namespace/name)
func (r *Reference) Repo() string {
	if r.Namespace != "" {
		return r.Namespace + "/" + r.Name
	}
	return r.Name
}

// IsDigest returns true if this is a digest reference
func (r *Reference) IsDigest() bool {
	return r.Digest != ""
}
