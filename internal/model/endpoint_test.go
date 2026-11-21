package model

import "testing"

func TestResolveDefaults(t *testing.T) {
	hf, err := Parse("Qwen/Qwen2.5-7B")
	if err != nil {
		t.Fatal(err)
	}
	ep, err := Resolve(hf, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if ep.Host != HostHFMirror || ep.Revision != "main" || ep.Pinned {
		t.Fatalf("default endpoint = %+v", ep)
	}
	next, ok := ep.Fallback()
	if !ok || next.Host != HostHuggingFace || !next.Pinned {
		t.Fatalf("fallback = %+v ok=%v", next, ok)
	}
	if _, ok := next.Fallback(); ok {
		t.Fatal("pinned endpoint fell back")
	}

	withProxy, err := Resolve(hf, "", "", "http://127.0.0.1:7890")
	if err != nil {
		t.Fatal(err)
	}
	if withProxy.Host != HostHuggingFace || withProxy.Pinned {
		t.Fatalf("proxy endpoint = %+v", withProxy)
	}

	pinned, err := Resolve(hf, "huggingface", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if pinned.Host != HostHuggingFace || !pinned.Pinned {
		t.Fatalf("mirror endpoint = %+v", pinned)
	}
	if _, err := Resolve(hf, "modelscope", "", ""); err == nil {
		t.Fatal("modelscope mirror was accepted for a Hugging Face id")
	}

	ms, err := Parse("ms:Qwen/Qwen2.5-7B")
	if err != nil {
		t.Fatal(err)
	}
	msEp, err := Resolve(ms, "", "", "http://127.0.0.1:7890")
	if err != nil {
		t.Fatal(err)
	}
	if msEp.Host != HostModelScope || msEp.Revision != "master" {
		t.Fatalf("modelscope endpoint = %+v", msEp)
	}
	if _, ok := msEp.Fallback(); ok {
		t.Fatal("modelscope endpoint fell back to Hugging Face")
	}
}
