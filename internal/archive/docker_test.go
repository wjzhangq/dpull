package archive

import (
	"testing"
)

func TestNormalizeRepoTag(t *testing.T) {
	tests := []struct {
		name      string
		canonical string
		want      string
	}{
		{
			name:      "docker hub official image",
			canonical: "docker.io/library/nginx:1.27",
			want:      "nginx:1.27",
		},
		{
			name:      "docker hub user image",
			canonical: "docker.io/lmsysorg/sglang:v1",
			want:      "lmsysorg/sglang:v1",
		},
		{
			name:      "ghcr unchanged",
			canonical: "ghcr.io/foo/bar:v1",
			want:      "ghcr.io/foo/bar:v1",
		},
		{
			name:      "digest only returns empty",
			canonical: "docker.io/library/nginx@sha256:abcdef",
			want:      "",
		},
		{
			name:      "tag and digest strips digest",
			canonical: "docker.io/library/nginx:1.27@sha256:abcdef",
			want:      "nginx:1.27",
		},
		{
			name:      "no registry assumes docker.io",
			canonical: "nginx:1.27",
			want:      "nginx:1.27",
		},
		{
			name:      "custom registry with port",
			canonical: "registry.example.com:5000/myapp:v2",
			want:      "registry.example.com:5000/myapp:v2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeRepoTag(tt.canonical)
			if got != tt.want {
				t.Errorf("normalizeRepoTag(%q) = %q, want %q", tt.canonical, got, tt.want)
			}
		})
	}
}

func TestStripAlgo(t *testing.T) {
	tests := []struct {
		digest string
		want   string
	}{
		{"sha256:abcdef123456", "abcdef123456"},
		{"sha512:xyz", "xyz"},
		{"nocolon", "nocolon"},
	}

	for _, tt := range tests {
		got := stripAlgo(tt.digest)
		if got != tt.want {
			t.Errorf("stripAlgo(%q) = %q, want %q", tt.digest, got, tt.want)
		}
	}
}

func TestGenerateOutputPath(t *testing.T) {
	tests := []struct {
		name      string
		canonical string
		platform  string
		want      string
	}{
		{
			name:      "docker hub official",
			canonical: "docker.io/library/nginx:1.27",
			platform:  "linux/amd64",
			want:      "nginx_1.27.tar",
		},
		{
			name:      "arm64 platform suffix",
			canonical: "docker.io/library/nginx:1.27",
			platform:  "linux/arm64",
			want:      "nginx_1.27_arm64.tar",
		},
		{
			name:      "digest only",
			canonical: "nginx@sha256:abcdef",
			platform:  "",
			want:      "nginx.tar",
		},
		{
			name:      "nested namespace",
			canonical: "ghcr.io/org/team/app:v2.1",
			platform:  "linux/amd64",
			want:      "app_v2.1.tar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateOutputPath(tt.canonical, tt.platform)
			if got != tt.want {
				t.Errorf("GenerateOutputPath(%q, %q) = %q, want %q",
					tt.canonical, tt.platform, got, tt.want)
			}
		})
	}
}
