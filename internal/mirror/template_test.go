package mirror

import (
	"testing"

	"github.com/wjzhangq/dpull/internal/ref"
)

func TestTemplate_Render(t *testing.T) {
	tests := []struct {
		name     string
		template string
		ref      ref.Reference
		want     string
	}{
		{
			name:     "simple repo template (1ms.run style)",
			template: "{repo}",
			ref: ref.Reference{
				Registry:  "docker.io",
				Namespace: "lmsysorg",
				Name:      "sglang",
				Tag:       "v1",
			},
			want: "lmsysorg/sglang",
		},
		{
			name:     "with registry prefix (Huawei SWR style)",
			template: "ddn-k8s/{registry}/{repo}",
			ref: ref.Reference{
				Registry:  "docker.io",
				Namespace: "lmsysorg",
				Name:      "sglang",
				Tag:       "v1",
			},
			want: "ddn-k8s/docker.io/lmsysorg/sglang",
		},
		{
			name:     "namespace only (Aliyun personal)",
			template: "myns/{name}",
			ref: ref.Reference{
				Registry:  "docker.io",
				Namespace: "lmsysorg",
				Name:      "sglang",
				Tag:       "v1",
			},
			want: "myns/sglang",
		},
		{
			name:     "harbor proxy cache style",
			template: "dockerhub-proxy/{repo}",
			ref: ref.Reference{
				Registry:  "docker.io",
				Namespace: "lmsysorg",
				Name:      "sglang",
				Tag:       "v1",
			},
			want: "dockerhub-proxy/lmsysorg/sglang",
		},
		{
			name:     "official image (library namespace)",
			template: "{repo}",
			ref: ref.Reference{
				Registry:  "docker.io",
				Namespace: "library",
				Name:      "nginx",
				Tag:       "1.27",
			},
			want: "library/nginx",
		},
		{
			name:     "with tag variable",
			template: "{namespace}/{name}/{tag}",
			ref: ref.Reference{
				Registry:  "docker.io",
				Namespace: "lmsysorg",
				Name:      "sglang",
				Tag:       "v1",
			},
			want: "lmsysorg/sglang/v1",
		},
		{
			name:     "digest reference",
			template: "{repo}@{digest}",
			ref: ref.Reference{
				Registry:  "docker.io",
				Namespace: "library",
				Name:      "nginx",
				Digest:    "sha256:abc123",
			},
			want: "library/nginx@sha256:abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl := NewTemplate(tt.template)
			got := tmpl.Render(&tt.ref)
			if got != tt.want {
				t.Errorf("Render() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMirror_RewriteRef(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		template string
		ref      ref.Reference
		want     string
	}{
		{
			name:     "1ms.run mirror",
			host:     "7c5a5eb84ed0a6a64e548ee8d2f90cb1.d.1ms.run",
			template: "{repo}",
			ref: ref.Reference{
				Namespace: "lmsysorg",
				Name:      "sglang",
				Tag:       "v1",
			},
			want: "7c5a5eb84ed0a6a64e548ee8d2f90cb1.d.1ms.run/lmsysorg/sglang",
		},
		{
			name:     "default template (empty string)",
			host:     "mirror.example.com",
			template: "",
			ref: ref.Reference{
				Namespace: "library",
				Name:      "nginx",
				Tag:       "latest",
			},
			want: "mirror.example.com/library/nginx",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMirror(tt.host, tt.template)
			got := m.RewriteRef(&tt.ref)
			if got != tt.want {
				t.Errorf("RewriteRef() = %v, want %v", got, tt.want)
			}
		})
	}
}
