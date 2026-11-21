package progress

import "testing"

func TestShortLabel(t *testing.T) {
	if got := ShortLabel("sha256:0123456789abcdef"); got != "01234567" {
		t.Fatalf("digest label = %q", got)
	}
	if got := ShortLabel("config.json"); got != "config.json" {
		t.Fatalf("file label = %q", got)
	}
	if got := ShortLabel("weights/model.safetensors"); got != "model.safetensors" {
		t.Fatalf("base label = %q", got)
	}
	long := "model-00001-of-00004-extra-long-name.safetensors"
	got := ShortLabel("subdir/" + long)
	if len(got) != 40 || got[len(got)-3:] != "..." {
		t.Fatalf("truncated label = %q", got)
	}
}
