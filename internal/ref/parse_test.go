package ref

import (
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      Reference
		wantErr   bool
	}{
		{
			name:  "official image with tag",
			input: "nginx:1.27",
			want: Reference{
				Registry:  "index.docker.io",
				Namespace: "library",
				Name:      "nginx",
				Tag:       "1.27",
				Original:  "nginx:1.27",
			},
		},
		{
			name:  "official image without tag",
			input: "nginx",
			want: Reference{
				Registry:  "index.docker.io",
				Namespace: "library",
				Name:      "nginx",
				Tag:       "latest",
				Original:  "nginx",
			},
		},
		{
			name:  "docker hub with namespace",
			input: "lmsysorg/sglang:v1",
			want: Reference{
				Registry:  "index.docker.io",
				Namespace: "lmsysorg",
				Name:      "sglang",
				Tag:       "v1",
				Original:  "lmsysorg/sglang:v1",
			},
		},
		{
			name:  "explicit docker.io",
			input: "docker.io/lmsysorg/sglang:v1",
			want: Reference{
				Registry:  "index.docker.io",
				Namespace: "lmsysorg",
				Name:      "sglang",
				Tag:       "v1",
				Original:  "docker.io/lmsysorg/sglang:v1",
			},
		},
		{
			name:  "ghcr.io registry",
			input: "ghcr.io/foo/bar:v1",
			want: Reference{
				Registry:  "ghcr.io",
				Namespace: "foo",
				Name:      "bar",
				Tag:       "v1",
				Original:  "ghcr.io/foo/bar:v1",
			},
		},
		{
			name:  "digest reference",
			input: "nginx@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			want: Reference{
				Registry:  "index.docker.io",
				Namespace: "library",
				Name:      "nginx",
				Digest:    "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				Original:  "nginx@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			},
		},
		{
			name:  "registry with port",
			input: "localhost:5000/alpine:test",
			want: Reference{
				Registry:  "localhost:5000",
				Namespace: "",
				Name:      "alpine",
				Tag:       "test",
				Original:  "localhost:5000/alpine:test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			if got.Registry != tt.want.Registry {
				t.Errorf("Registry = %v, want %v", got.Registry, tt.want.Registry)
			}
			if got.Namespace != tt.want.Namespace {
				t.Errorf("Namespace = %v, want %v", got.Namespace, tt.want.Namespace)
			}
			if got.Name != tt.want.Name {
				t.Errorf("Name = %v, want %v", got.Name, tt.want.Name)
			}
			if got.Tag != tt.want.Tag {
				t.Errorf("Tag = %v, want %v", got.Tag, tt.want.Tag)
			}
			if got.Digest != tt.want.Digest {
				t.Errorf("Digest = %v, want %v", got.Digest, tt.want.Digest)
			}
		})
	}
}

func TestReference_Repo(t *testing.T) {
	tests := []struct {
		name string
		ref  Reference
		want string
	}{
		{
			name: "with namespace",
			ref:  Reference{Namespace: "lmsysorg", Name: "sglang"},
			want: "lmsysorg/sglang",
		},
		{
			name: "without namespace",
			ref:  Reference{Namespace: "", Name: "alpine"},
			want: "alpine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ref.Repo(); got != tt.want {
				t.Errorf("Repo() = %v, want %v", got, tt.want)
			}
		})
	}
}
