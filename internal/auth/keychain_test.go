package auth

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeRegistryKey(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://index.docker.io/v1/", "index.docker.io"},
		{"index.docker.io", "index.docker.io"},
		{"http://localhost:5000", "localhost:5000"},
		{"registry.example.com:5000/path", "registry.example.com:5000"},
		{"GHCR.IO", "ghcr.io"},
		{"  ghcr.io  ", "ghcr.io"},
	}

	for _, tt := range tests {
		if got := normalizeRegistryKey(tt.in); got != tt.want {
			t.Errorf("normalizeRegistryKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDecodeAuthEntry(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("alice:s3cret"))

	tests := []struct {
		name     string
		entry    dockerAuthEntry
		wantUser string
		wantPass string
		wantErr  bool
	}{
		{
			name:     "base64 auth",
			entry:    dockerAuthEntry{Auth: b64},
			wantUser: "alice",
			wantPass: "s3cret",
		},
		{
			name:     "explicit fields win",
			entry:    dockerAuthEntry{Auth: b64, Username: "bob", Password: "other"},
			wantUser: "bob",
			wantPass: "other",
		},
		{
			name:     "identity token",
			entry:    dockerAuthEntry{IdentityToken: "tok123"},
			wantUser: "<token>",
			wantPass: "tok123",
		},
		{
			name:  "empty entry yields empty credential",
			entry: dockerAuthEntry{},
		},
		{
			name:    "invalid base64",
			entry:   dockerAuthEntry{Auth: "!!!not-base64!!!"},
			wantErr: true,
		},
		{
			name:    "missing colon separator",
			entry:   dockerAuthEntry{Auth: base64.StdEncoding.EncodeToString([]byte("nocolon"))},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cred, err := decodeAuthEntry(tt.entry)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cred.Username != tt.wantUser || cred.Password != tt.wantPass {
				t.Errorf("got (%q, %q), want (%q, %q)",
					cred.Username, cred.Password, tt.wantUser, tt.wantPass)
			}
		})
	}
}

func TestLoadKeychain(t *testing.T) {
	dir := t.TempDir()
	b64 := base64.StdEncoding.EncodeToString([]byte("alice:s3cret"))

	cfg := `{
  "auths": {
    "https://index.docker.io/v1/": {"auth": "` + b64 + `"},
    "ghcr.io": {"username": "bob", "password": "ghp_x"},
    "helper.example.com": {}
  },
  "credsStore": "osxkeychain",
  "credHelpers": {
    "gcr.io": "gcloud"
  }
}`

	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", dir)

	kc, err := LoadKeychain()
	if err != nil {
		t.Fatalf("LoadKeychain: %v", err)
	}

	// Docker Hub resolves through any alias.
	for _, alias := range []string{"docker.io", "index.docker.io", "registry-1.docker.io"} {
		cred, ok := kc.Resolve(alias)
		if !ok {
			t.Errorf("Resolve(%q): not found", alias)
			continue
		}
		if cred.Username != "alice" || cred.Password != "s3cret" {
			t.Errorf("Resolve(%q) = (%q, %q), want (alice, s3cret)", alias, cred.Username, cred.Password)
		}
	}

	cred, ok := kc.Resolve("ghcr.io")
	if !ok || cred.Username != "bob" || cred.Password != "ghp_x" {
		t.Errorf("Resolve(ghcr.io) = (%q, %q, %v), want (bob, ghp_x, true)", cred.Username, cred.Password, ok)
	}

	if _, ok := kc.Resolve("unknown.example.com"); ok {
		t.Error("Resolve(unknown.example.com) should not be found")
	}

	// Empty auths entry falls back to credsStore.
	if helper, ok := kc.UsesExternalHelper("helper.example.com"); !ok || helper != "osxkeychain" {
		t.Errorf("UsesExternalHelper(helper.example.com) = (%q, %v), want (osxkeychain, true)", helper, ok)
	}

	// credHelpers-only registry has no auths entry at all.
	if helper, ok := kc.UsesExternalHelper("gcr.io"); !ok || helper != "gcloud" {
		t.Errorf("UsesExternalHelper(gcr.io) = (%q, %v), want (gcloud, true)", helper, ok)
	}
}

func TestLoadKeychainMissingFile(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", t.TempDir())

	kc, err := LoadKeychain()
	if err != nil {
		t.Fatalf("missing config should not error: %v", err)
	}
	if _, ok := kc.Resolve("docker.io"); ok {
		t.Error("empty keychain should resolve nothing")
	}
}
