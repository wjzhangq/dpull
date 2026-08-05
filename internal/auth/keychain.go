package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Credential holds a username/password pair for a registry
type Credential struct {
	Username string
	Password string
}

// IsEmpty reports whether the credential carries no usable secret
func (c Credential) IsEmpty() bool {
	return c.Username == "" && c.Password == ""
}

// dockerConfig mirrors the parts of ~/.docker/config.json we need
type dockerConfig struct {
	Auths       map[string]dockerAuthEntry `json:"auths"`
	CredsStore  string                     `json:"credsStore"`
	CredHelpers map[string]string          `json:"credHelpers"`
}

type dockerAuthEntry struct {
	Auth     string `json:"auth"`
	Username string `json:"username"`
	Password string `json:"password"`
	// IdentityToken is used by some registries in place of a password.
	IdentityToken string `json:"identitytoken"`
}

// Keychain resolves registry credentials from Docker's config.json.
// External credential helpers (credsStore / credHelpers) are recognized but
// not invoked; those registries fall back to anonymous access.
type Keychain struct {
	entries map[string]Credential
	// helperOnly records registries whose secret lives in an external helper.
	helperOnly map[string]string
}

// LoadKeychain reads credentials from the Docker config file.
// Lookup order: $DOCKER_CONFIG/config.json, then ~/.docker/config.json.
// A missing file is not an error — it yields an empty keychain.
func LoadKeychain() (*Keychain, error) {
	kc := &Keychain{
		entries:    make(map[string]Credential),
		helperOnly: make(map[string]string),
	}

	path, err := dockerConfigPath()
	if err != nil {
		return kc, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return kc, nil
		}
		return kc, fmt.Errorf("read docker config: %w", err)
	}

	var cfg dockerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return kc, fmt.Errorf("parse docker config %s: %w", path, err)
	}

	for registry, entry := range cfg.Auths {
		key := normalizeRegistryKey(registry)

		cred, err := decodeAuthEntry(entry)
		if err != nil {
			// A single malformed entry shouldn't sink the whole keychain.
			continue
		}

		if cred.IsEmpty() {
			// Entry exists but secret is delegated to a helper.
			if helper := helperFor(&cfg, registry); helper != "" {
				kc.helperOnly[key] = helper
			}
			continue
		}

		kc.entries[key] = cred
	}

	// Registries configured purely via credHelpers have no auths entry.
	for registry, helper := range cfg.CredHelpers {
		key := normalizeRegistryKey(registry)
		if _, ok := kc.entries[key]; !ok {
			kc.helperOnly[key] = helper
		}
	}

	return kc, nil
}

// Resolve returns the credential for a registry host, if any.
func (k *Keychain) Resolve(registry string) (Credential, bool) {
	key := normalizeRegistryKey(registry)

	if cred, ok := k.entries[key]; ok {
		return cred, true
	}

	// Docker Hub is spelled many ways in config.json; try its aliases.
	if isDockerHub(key) {
		for _, alias := range dockerHubAliases {
			if cred, ok := k.entries[alias]; ok {
				return cred, true
			}
		}
	}

	return Credential{}, false
}

// UsesExternalHelper reports whether a registry's credential is stored in an
// external helper binary that we do not invoke. Callers can use this to warn
// the user why authentication is unavailable.
func (k *Keychain) UsesExternalHelper(registry string) (string, bool) {
	key := normalizeRegistryKey(registry)

	if helper, ok := k.helperOnly[key]; ok {
		return helper, true
	}

	if isDockerHub(key) {
		for _, alias := range dockerHubAliases {
			if helper, ok := k.helperOnly[alias]; ok {
				return helper, true
			}
		}
	}

	return "", false
}

// decodeAuthEntry extracts a credential from an auths entry.
func decodeAuthEntry(entry dockerAuthEntry) (Credential, error) {
	// Explicit username/password wins when present.
	if entry.Username != "" && entry.Password != "" {
		return Credential{Username: entry.Username, Password: entry.Password}, nil
	}

	if entry.Auth != "" {
		decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
		if err != nil {
			return Credential{}, fmt.Errorf("decode auth: %w", err)
		}

		user, pass, found := strings.Cut(string(decoded), ":")
		if !found {
			return Credential{}, fmt.Errorf("malformed auth: missing separator")
		}
		return Credential{Username: user, Password: pass}, nil
	}

	// Some registries issue an identity token used as the password with a
	// sentinel username.
	if entry.IdentityToken != "" {
		user := entry.Username
		if user == "" {
			user = "<token>"
		}
		return Credential{Username: user, Password: entry.IdentityToken}, nil
	}

	return Credential{}, nil
}

// helperFor returns the credential helper responsible for a registry.
func helperFor(cfg *dockerConfig, registry string) string {
	if h, ok := cfg.CredHelpers[registry]; ok {
		return h
	}
	return cfg.CredsStore
}

// dockerConfigPath locates the Docker config file.
func dockerConfigPath() (string, error) {
	if dir := os.Getenv("DOCKER_CONFIG"); dir != "" {
		return filepath.Join(dir, "config.json"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".docker", "config.json"), nil
}

// dockerHubAliases are the spellings Docker Hub appears under in config.json.
var dockerHubAliases = []string{
	"index.docker.io",
	"registry-1.docker.io",
	"docker.io",
}

func isDockerHub(key string) bool {
	for _, alias := range dockerHubAliases {
		if key == alias {
			return true
		}
	}
	return false
}

// normalizeRegistryKey reduces a config.json key to a bare host[:port].
// Entries are commonly written as full URLs with a path suffix, e.g.
// "https://index.docker.io/v1/".
func normalizeRegistryKey(registry string) string {
	key := registry

	if idx := strings.Index(key, "://"); idx != -1 {
		key = key[idx+3:]
	}

	// Drop any path component ("index.docker.io/v1/" → "index.docker.io").
	if idx := strings.Index(key, "/"); idx != -1 {
		key = key[:idx]
	}

	return strings.ToLower(strings.TrimSpace(key))
}
