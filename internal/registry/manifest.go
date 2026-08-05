package registry

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"

	"github.com/wjzhangq/dpull/internal/ref"
)

// GetManifest fetches the manifest for an image reference
func (c *Client) GetManifest(r *ref.Reference, platform string) (*Manifest, error) {
	// Authenticate first
	authedClient, err := c.authenticate(r.Registry, r.Repo())
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
	req, err := http.NewRequest("GET", url, nil)
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
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fetch manifest: %d %s", resp.StatusCode, string(body))
	}

	// Read body and compute digest
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	manifestDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	mediaType := resp.Header.Get("Content-Type")

	// Check if it's a manifest list/index
	if mediaType == MediaTypeOCIIndex || mediaType == MediaTypeDockerManifestList {
		return authedClient.selectPlatformManifest(r, body, platform)
	}

	// Single manifest
	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	manifest.Digest = manifestDigest
	return &manifest, nil
}

// selectPlatformManifest selects a platform-specific manifest from a list
func (c *Client) selectPlatformManifest(r *ref.Reference, listBody []byte, platform string) (*Manifest, error) {
	var list ManifestList
	if err := json.Unmarshal(listBody, &list); err != nil {
		return nil, fmt.Errorf("parse manifest list: %w", err)
	}

	// Parse platform (e.g., "linux/arm64")
	targetOS, targetArch := parsePlatform(platform)

	// Find matching platform
	var selectedDigest string
	for _, m := range list.Manifests {
		if m.Platform != nil {
			if m.Platform.OS == targetOS && m.Platform.Architecture == targetArch {
				selectedDigest = m.Digest
				break
			}
		}
	}

	if selectedDigest == "" {
		return nil, fmt.Errorf("no manifest found for platform %s/%s", targetOS, targetArch)
	}

	// Fetch the platform-specific manifest
	url := c.buildURL(r.Registry, fmt.Sprintf("/v2/%s/manifests/%s", r.Repo(), selectedDigest))
	req, err := http.NewRequest("GET", url, nil)
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
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fetch platform manifest: %d %s", resp.StatusCode, string(body))
	}

	var manifest Manifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parse platform manifest: %w", err)
	}

	manifest.Digest = selectedDigest
	return &manifest, nil
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
	authedClient, err := c.authenticate(r.Registry, r.Repo())
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
