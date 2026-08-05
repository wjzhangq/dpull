package registry

import (
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// Manifest represents an OCI/Docker manifest
type Manifest struct {
	SchemaVersion int                `json:"schemaVersion"`
	MediaType     string             `json:"mediaType"`
	Config        Descriptor         `json:"config"`
	Layers        []Descriptor       `json:"layers"`
	Digest        string             `json:"-"` // Set after fetch
}

// Descriptor represents a content descriptor
type Descriptor struct {
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

// ManifestList represents an OCI image index or Docker manifest list
type ManifestList struct {
	SchemaVersion int                  `json:"schemaVersion"`
	MediaType     string               `json:"mediaType"`
	Manifests     []ManifestDescriptor `json:"manifests"`
}

// ManifestDescriptor describes a platform-specific manifest
type ManifestDescriptor struct {
	MediaType string     `json:"mediaType"`
	Size      int64      `json:"size"`
	Digest    string     `json:"digest"`
	Platform  *Platform  `json:"platform,omitempty"`
}

// Platform represents a platform (OS/architecture)
type Platform struct {
	Architecture string   `json:"architecture"`
	OS           string   `json:"os"`
	OSVersion    string   `json:"os.version,omitempty"`
	OSFeatures   []string `json:"os.features,omitempty"`
	Variant      string   `json:"variant,omitempty"`
}

// ImageConfig represents an OCI/Docker image configuration
type ImageConfig struct {
	Architecture string              `json:"architecture"`
	OS           string              `json:"os"`
	Config       v1.Config           `json:"config"`
	RootFS       RootFS              `json:"rootfs"`
	History      []v1.History        `json:"history"`
	Created      time.Time           `json:"created"`
}

// RootFS describes the root filesystem
type RootFS struct {
	Type    string   `json:"type"`
	DiffIDs []string `json:"diff_ids"`
}

// Media types
const (
	// OCI
	MediaTypeOCIManifest      = "application/vnd.oci.image.manifest.v1+json"
	MediaTypeOCIIndex         = "application/vnd.oci.image.index.v1+json"
	MediaTypeOCIConfig        = "application/vnd.oci.image.config.v1+json"
	MediaTypeOCILayer         = "application/vnd.oci.image.layer.v1.tar+gzip"

	// Docker
	MediaTypeDockerManifest      = "application/vnd.docker.distribution.manifest.v2+json"
	MediaTypeDockerManifestList  = "application/vnd.docker.distribution.manifest.list.v2+json"
	MediaTypeDockerConfig        = "application/vnd.docker.container.image.v1+json"
	MediaTypeDockerLayer         = "application/vnd.docker.image.rootfs.diff.tar.gzip"
)

// TotalSize returns the total size of all layers
func (m *Manifest) TotalSize() int64 {
	total := m.Config.Size
	for _, layer := range m.Layers {
		total += layer.Size
	}
	return total
}
