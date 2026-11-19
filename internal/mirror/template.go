package mirror

import (
	"fmt"
	"strings"

	"github.com/wjzhangq/dpull/internal/ref"
)

// Template represents a mirror path template
type Template struct {
	raw string
}

// NewTemplate creates a new template from a string
// Variables: {registry}, {repo}, {namespace}, {name}, {tag}, {digest}
func NewTemplate(s string) *Template {
	return &Template{raw: s}
}

// Render renders the template with values from the reference
func (t *Template) Render(r *ref.Reference) string {
	result := t.raw

	// Replace variables
	result = strings.ReplaceAll(result, "{registry}", r.Registry)
	result = strings.ReplaceAll(result, "{repo}", r.Repo())
	result = strings.ReplaceAll(result, "{namespace}", r.Namespace)
	result = strings.ReplaceAll(result, "{name}", r.Name)
	result = strings.ReplaceAll(result, "{tag}", r.Tag)
	result = strings.ReplaceAll(result, "{digest}", r.Digest)

	return result
}

// Mirror represents a mirror configuration
type Mirror struct {
	Host     string
	Template *Template
}

// NewMirror creates a new mirror. An empty template means "use the image
// repository path as-is" (docker.1ms.run/library/nginx), not a template variable.
func NewMirror(host, template string) *Mirror {
	if template == "" {
		template = "{repo}"
	}
	return &Mirror{
		Host:     host,
		Template: NewTemplate(template),
	}
}

// Parse splits a mirror address into a registry host and an optional path prefix.
//
//	docker.1ms.run                              → docker.1ms.run, ""
//	https://docker.1ms.run/                     → docker.1ms.run, ""
//	swr.example.com/ddn-k8s/docker.io           → swr.example.com, ddn-k8s/docker.io
//	localhost:5000/mirror                       → localhost:5000, mirror
func Parse(raw string) (host, prefix string, err error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.Trim(raw, "/")
	if raw == "" {
		return "", "", fmt.Errorf("empty mirror address")
	}
	host, prefix, _ = strings.Cut(raw, "/")
	if host == "" || strings.Contains(host, " ") {
		return "", "", fmt.Errorf("invalid mirror address %q", raw)
	}
	return host, prefix, nil
}

// Resolve builds the registry host and repository path used to fetch an image.
//
// With no template, the image repository is appended to the mirror address:
//
//	-m docker.1ms.run  nginx:1.27
//	→ docker.1ms.run / library/nginx
//
// A prefix written in the address is kept in front of the repository:
//
//	-m swr.example.com/ddn-k8s/docker.io  nginx:1.27
//	→ swr.example.com / ddn-k8s/docker.io/library/nginx
//
// template is the advanced --mirror-path form. It is applied only when set.
func Resolve(mirrorSpec, template string, image *ref.Reference) (host, repoPath string, err error) {
	mirrorSpec = Expand(mirrorSpec)
	host, prefix, err := Parse(mirrorSpec)
	if err != nil {
		return "", "", err
	}
	if template != "" {
		return host, NewTemplate(template).Render(image), nil
	}
	repoPath = image.Repo()
	if prefix != "" {
		repoPath = prefix + "/" + repoPath
	}
	return host, repoPath, nil
}

// RewriteRef rewrites a reference to use this mirror
// Returns the full mirror URL path
func (m *Mirror) RewriteRef(r *ref.Reference) string {
	path := m.Template.Render(r)
	return fmt.Sprintf("%s/%s", m.Host, path)
}

// FetchRef builds the reference used to fetch from the mirror.
//
// host is the mirror endpoint and path is the already-rendered repository path
// (e.g. "library/nginx" or "index.docker.io/library/nginx"). The tag or digest
// is carried over from orig, since the rendered path usually omits it and
// re-parsing would default the tag to "latest".
func FetchRef(host, path string, orig *ref.Reference) *ref.Reference {
	namespace, name := splitRepo(path)
	return &ref.Reference{
		Registry:  host,
		Namespace: namespace,
		Name:      name,
		Tag:       orig.Tag,
		Digest:    orig.Digest,
		Original:  orig.Original,
	}
}

// splitRepo splits a repository path into its namespace and final name
// component. A path with no "/" has an empty namespace.
func splitRepo(path string) (namespace, name string) {
	path = strings.Trim(path, "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i], path[i+1:]
	}
	return "", path
}
