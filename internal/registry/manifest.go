package registry

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"

	"github.com/wjzhangq/dpull/internal/ref"
)

// GetManifest fetches the manifest for an image reference.
// ctx bounds the registry calls so a dead host can be abandoned quickly.
func (c *Client) GetManifest(ctx context.Context, r *ref.Reference, platform string) (*Manifest, error) {
	// Authenticate first
	authedClient, err := c.authenticate(ctx, r.Registry, r.Repo())
	if err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	// Determine reference (tag or digest)
	reference := r.Tag
	if r.IsDigest() {
		reference = r.Digest
	}

	// Fetch manifest
	url := authedClient.buildURL(r.Registry, fmt.Sprintf("/v2/%s/manifests/%s", r.Repo(), reference))
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create manifest request: %w", err)
	}

	// Accept multiple media types
	req.Header.Set("Accept", strings.Join([]string{
		MediaTypeOCIIndex,
		MediaTypeDockerManifestList,
		MediaTypeOCIManifest,
		MediaTypeDockerManifest,
	}, ", "))

	resp, err := authedClient.do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, &HTTPStatusError{Op: "fetch manifest", Code: resp.StatusCode, Body: string(body)}
	}

	// Read body and compute digest
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	manifestDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	// Check if it's a manifest list/index. Some registries append "; charset=utf-8".
	if isIndex(resp.Header.Get("Content-Type"), body) {
		return authedClient.selectPlatformManifest(ctx, r, body, platform)
	}

	// Single manifest
	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	manifest.Digest = manifestDigest
	return &manifest, nil
}

func isIndex(mediaType string, body []byte) bool {
	if i := strings.Index(mediaType, ";"); i >= 0 {
		mediaType = strings.TrimSpace(mediaType[:i])
	}
	if mediaType == MediaTypeOCIIndex || mediaType == MediaTypeDockerManifestList {
		return true
	}
	return looksLikeIndex(body)
}

// looksLikeIndex reports an index whose Content-Type was missing or generic.
func looksLikeIndex(body []byte) bool {
	var list ManifestList
	if err := json.Unmarshal(body, &list); err != nil {
		return false
	}
	return len(list.Manifests) > 0 && len(list.Manifests[0].Digest) > 0 && list.Manifests[0].Platform != nil
}

// selectPlatformManifest selects a platform-specific manifest from a list
func (c *Client) selectPlatformManifest(ctx context.Context, r *ref.Reference, listBody []byte, platform string) (*Manifest, error) {
	var list ManifestList
	if err := json.Unmarshal(listBody, &list); err != nil {
		return nil, fmt.Errorf("parse manifest list: %w", err)
	}

	selectedDigest, selectedName, available, err := choosePlatform(list, platform)
	if err != nil {
		return nil, err
	}

	// Fetch the platform-specific manifest
	url := c.buildURL(r.Registry, fmt.Sprintf("/v2/%s/manifests/%s", r.Repo(), selectedDigest))
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create platform manifest request: %w", err)
	}

	req.Header.Set("Accept", strings.Join([]string{
		MediaTypeOCIManifest,
		MediaTypeDockerManifest,
	}, ", "))

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch platform manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, &HTTPStatusError{Op: "fetch platform manifest", Code: resp.StatusCode, Body: string(body)}
	}

	var manifest Manifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parse platform manifest: %w", err)
	}

	manifest.Digest = selectedDigest
	manifest.Platform = selectedName
	manifest.Available = available
	return &manifest, nil
}

// choosePlatform picks the manifest whose os/arch matches platform and lists
// the other runnable platforms in the index. Attestation entries (unknown/unknown)
// are omitted.
func choosePlatform(list ManifestList, platform string) (digest, selected string, available []string, err error) {
	targetOS, targetArch := parsePlatform(platform)
	for _, m := range list.Manifests {
		name := formatPlatform(m.Platform)
		if name == "" {
			continue
		}
		available = append(available, name)
		if digest == "" && m.Platform.OS == targetOS && m.Platform.Architecture == targetArch {
			digest = m.Digest
			selected = name
		}
	}
	if digest == "" {
		choices := "(none)"
		if len(available) > 0 {
			choices = strings.Join(available, ", ")
		}
		return "", "", available, fmt.Errorf("platform %s/%s is not in this image\navailable: %s\nselect one with --platform", targetOS, targetArch, choices)
	}
	return digest, selected, available, nil
}

// formatPlatform renders a platform as os/arch[/variant]. Empty when the
// entry is an attestation manifest rather than a runnable image.
func formatPlatform(p *Platform) string {
	if p == nil || p.OS == "" || p.Architecture == "" {
		return ""
	}
	if p.OS == "unknown" || p.Architecture == "unknown" {
		return ""
	}
	name := p.OS + "/" + p.Architecture
	if p.Variant != "" {
		name += "/" + p.Variant
	}
	return name
}

// parsePlatform parses a platform string like "linux/arm64"
func parsePlatform(platform string) (os, arch string) {
	if platform == "" {
		return runtime.GOOS, runtime.GOARCH
	}

	parts := strings.Split(platform, "/")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}

	// Default to current platform
	return runtime.GOOS, runtime.GOARCH
}

// GetConfig fetches the image configuration blob
func (c *Client) GetConfig(r *ref.Reference, configDigest string) (*ImageConfig, error) {
	authedClient, err := c.authenticate(context.Background(), r.Registry, r.Repo())
	if err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	url := authedClient.buildURL(r.Registry, fmt.Sprintf("/v2/%s/blobs/%s", r.Repo(), configDigest))
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create config request: %w", err)
	}

	resp, err := authedClient.do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fetch config: %d %s", resp.StatusCode, string(body))
	}

	var config ImageConfig
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &config, nil
}
