package archive

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wjzhangq/dpull/internal/registry"
)

// DockerArchiveManifest is the top-level manifest.json in docker-archive format
type DockerArchiveManifest struct {
	Config   string   `json:"Config"`
	RepoTags []string `json:"RepoTags"`
	Layers   []string `json:"Layers"`
}

// AssembleTar creates a docker-archive tar from manifest, config, and layer blobs.
// The canonical name is used in RepoTags, so `docker load` reports the image the
// user asked for rather than the mirror it was fetched from.
func AssembleTar(
	manifest *registry.Manifest,
	configPath string,
	layerPaths []string,
	canonical string,
	destPath string,
) error {
	if len(layerPaths) != len(manifest.Layers) {
		return fmt.Errorf("layer count mismatch: manifest has %d, got %d paths",
			len(manifest.Layers), len(layerPaths))
	}

	// Layer entry names inside the tar. docker load runs each layer through
	// DecompressStream, so the registry's gzipped blob can be stored as-is.
	layerNames := make([]string, len(manifest.Layers))
	for i, layer := range manifest.Layers {
		layerNames[i] = layerFileName(layer, i)
	}

	configName := stripAlgo(manifest.Config.Digest) + ".json"

	entry := DockerArchiveManifest{
		Config: configName,
		Layers: layerNames,
	}
	// A digest-only pull has no tag to record; docker load rejects a RepoTag
	// carrying a digest, so leave it null in that case.
	if repoTag := normalizeRepoTag(canonical); repoTag != "" {
		entry.RepoTags = []string{repoTag}
	}

	manifestEntry := []DockerArchiveManifest{entry}

	manifestJSON, err := json.Marshal(manifestEntry)
	if err != nil {
		return fmt.Errorf("marshal manifest.json: %w", err)
	}

	// Create output tar file
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create tar: %w", err)
	}
	defer f.Close()

	tw := tar.NewWriter(f)
	defer tw.Close()

	// Write manifest.json
	if err := writeTarEntry(tw, "manifest.json", manifestJSON); err != nil {
		return fmt.Errorf("write manifest.json: %w", err)
	}

	// Write config blob
	if err := writeTarFile(tw, configName, configPath); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	// Write layer blobs
	for i, layerPath := range layerPaths {
		if err := writeTarFile(tw, layerNames[i], layerPath); err != nil {
			return fmt.Errorf("write layer %d: %w", i, err)
		}
	}

	return nil
}

// layerFileName generates a tar entry name for a layer.
// Must not contain ':' since that breaks tar path parsing.
func layerFileName(desc registry.Descriptor, index int) string {
	stripped := stripAlgo(desc.Digest)
	return fmt.Sprintf("%s.tar.gz", stripped)
}

// stripAlgo removes the "sha256:" or other hash algorithm prefix
func stripAlgo(digest string) string {
	if idx := strings.Index(digest, ":"); idx != -1 {
		return digest[idx+1:]
	}
	return digest
}

// normalizeRepoTag converts canonical name to docker-archive RepoTag format.
// docker.io/lmsysorg/sglang:v1 → lmsysorg/sglang:v1
// docker.io/library/nginx:1.27 → nginx:1.27
// ghcr.io/foo/bar:v1 → ghcr.io/foo/bar:v1 (unchanged)
// Digest-only refs (no tag) return "" so the caller can omit RepoTags.
func normalizeRepoTag(canonical string) string {
	// Strip digest if present
	if idx := strings.Index(canonical, "@"); idx != -1 {
		canonical = canonical[:idx]
	}

	// No tag remaining after stripping digest → digest-only ref
	if !strings.Contains(canonical, ":") {
		return ""
	}

	// Handle docker.io special cases
	if strings.HasPrefix(canonical, "docker.io/") {
		rest := canonical[len("docker.io/"):]

		// Remove library/ prefix for official images
		if strings.HasPrefix(rest, "library/") {
			return rest[len("library/"):]
		}

		// Remove docker.io/ for other images
		return rest
	}

	// Other registries: keep as-is
	return canonical
}

// writeTarEntry writes a byte slice as a tar entry
func writeTarEntry(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name: name,
		Mode: 0644,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// writeTarFile writes a file from disk into the tar
func writeTarFile(tw *tar.Writer, tarPath, sourcePath string) error {
	f, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	hdr := &tar.Header{
		Name: tarPath,
		Mode: 0644,
		Size: info.Size(),
	}

	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("copy content: %w", err)
	}

	return nil
}

// GenerateOutputPath generates a default output path from canonical name and platform
// docker.io/library/nginx:1.27 + linux/amd64 → nginx_1.27_amd64.tar
func GenerateOutputPath(canonical, platform string) string {
	// Extract base name (strip registry and namespace)
	name := canonical
	if idx := strings.LastIndex(name, "/"); idx != -1 {
		name = name[idx+1:]
	}

	// Strip digest
	if idx := strings.Index(name, "@"); idx != -1 {
		name = name[:idx]
	}

	// Replace : with _
	name = strings.ReplaceAll(name, ":", "_")

	// Add platform suffix if not default
	if platform != "" && platform != "linux/amd64" {
		parts := strings.Split(platform, "/")
		if len(parts) == 2 {
			name += "_" + parts[1]
		}
	}

	return name + ".tar"
}

// OutputPathFromDir generates path in a specific directory
func OutputPathFromDir(dir, canonical, platform string) string {
	base := GenerateOutputPath(canonical, platform)
	return filepath.Join(dir, base)
}
