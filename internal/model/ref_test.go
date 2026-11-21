package model

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in       string
		kind     Kind
		host     string
		id       string
		rev      string
		file     string
		explicit bool
	}{
		{"Qwen/Qwen2.5-7B", KindHF, "", "Qwen/Qwen2.5-7B", "", "", false},
		{"Qwen/Qwen2.5-7B@main", KindHF, "", "Qwen/Qwen2.5-7B", "main", "", false},
		{"hf:Qwen/Qwen2.5-7B", KindHF, "", "Qwen/Qwen2.5-7B", "", "", true},
		{"ms:Qwen/Qwen2.5-7B", KindModelScope, "", "Qwen/Qwen2.5-7B", "", "", true},
		{"https://huggingface.co/Qwen/Qwen2.5-7B/tree/main", KindHF, HostHuggingFace, "Qwen/Qwen2.5-7B", "main", "", true},
		{"https://hf-mirror.com/Qwen/Qwen2.5-7B/blob/main/config.json", KindHF, HostHFMirror, "Qwen/Qwen2.5-7B", "main", "config.json", true},
		{"https://huggingface.co/Qwen/Qwen2.5-7B/resolve/main/sub/a.json?download=true", KindHF, HostHuggingFace, "Qwen/Qwen2.5-7B", "main", "sub/a.json", true},
		{"https://hf.co/Qwen/Qwen2.5-7B", KindHF, HostHuggingFace, "Qwen/Qwen2.5-7B", "", "", true},
		{"https://www.modelscope.cn/models/Qwen/Qwen2.5-7B/summary", KindModelScope, HostModelScope, "Qwen/Qwen2.5-7B", "", "", true},
		{"https://modelscope.cn/models/Qwen/Qwen2.5-7B", KindModelScope, HostModelScope, "Qwen/Qwen2.5-7B", "", "", true},
	}
	for _, tc := range cases {
		got, err := Parse(tc.in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.in, err)
		}
		if got.Kind != tc.kind || got.Host != tc.host || got.ID != tc.id || got.Revision != tc.rev || got.File != tc.file || got.ExplicitKind != tc.explicit {
			t.Fatalf("Parse(%q) = %+v", tc.in, got)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, raw := range []string{"", "nginx", "https://huggingface.co/datasets/org/name", "org/name@../x"} {
		if _, err := Parse(raw); err == nil {
			t.Fatalf("Parse(%q) succeeded", raw)
		}
	}
}
