package registry

import (
	"strings"
	"testing"
)

func TestChoosePlatformListsOptions(t *testing.T) {
	list := ManifestList{Manifests: []ManifestDescriptor{
		{Digest: "sha256:amd", Platform: &Platform{OS: "linux", Architecture: "amd64"}},
		{Digest: "sha256:arm", Platform: &Platform{OS: "linux", Architecture: "arm64", Variant: "v8"}},
		{Digest: "sha256:att", Platform: &Platform{OS: "unknown", Architecture: "unknown"}},
	}}

	digest, selected, available, err := choosePlatform(list, "linux/arm64")
	if err != nil {
		t.Fatal(err)
	}
	if digest != "sha256:arm" || selected != "linux/arm64/v8" {
		t.Fatalf("selected %s %s", digest, selected)
	}
	if len(available) != 2 || available[0] != "linux/amd64" || available[1] != "linux/arm64/v8" {
		t.Fatalf("available = %v", available)
	}
}

func TestChoosePlatformMissingIncludesChoices(t *testing.T) {
	list := ManifestList{Manifests: []ManifestDescriptor{
		{Digest: "sha256:amd", Platform: &Platform{OS: "linux", Architecture: "amd64"}},
	}}
	_, _, _, err := choosePlatform(list, "darwin/arm64")
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, part := range []string{"darwin/arm64", "linux/amd64", "--platform"} {
		if !strings.Contains(msg, part) {
			t.Fatalf("error %q does not mention %s", msg, part)
		}
	}
}
