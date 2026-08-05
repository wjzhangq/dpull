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

// NewMirror creates a new mirror
func NewMirror(host, template string) *Mirror {
	if template == "" {
		template = "{repo}"
	}
	return &Mirror{
		Host:     host,
		Template: NewTemplate(template),
	}
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
