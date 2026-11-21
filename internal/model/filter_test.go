package model

import "testing"

func TestFilter(t *testing.T) {
	files := []File{
		{Path: "config.json", Size: 2},
		{Path: "sub/model.safetensors", Size: 10},
		{Path: "README.md", Size: 4},
		{Path: "model.safetensors.index.json", Size: 3},
	}
	got := Filter(files, []string{"*.safetensors", "*.json"}, []string{"*index.json"})
	if len(got) != 2 || got[0].Path != "config.json" || got[1].Path != "sub/model.safetensors" {
		t.Fatalf("filtered = %+v", got)
	}
}
