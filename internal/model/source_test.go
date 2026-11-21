package model

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHuggingFaceListPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer hf-token" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("cursor") == "" {
			w.Header().Set("Link", "<"+r.Host+"/unused>; rel=\"next\"")
			w.Header().Set("Link", "<"+serverURL(r)+"/api/models/acme/tiny/tree/main?cursor=2>; rel=\"next\"")
			_, _ = w.Write([]byte(`[{"type":"directory","path":"sub"},{"type":"file","path":"config.json","size":2},{"type":"file","path":"alias.bin","size":12,"target":"weights.bin"}]`))
			return
		}
		_, _ = w.Write([]byte(`[{"type":"file","path":"weights.bin","size":4,"lfs":{"oid":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":32}}]`))
	}))
	defer srv.Close()

	hub := &Hub{Kind: KindHF, Base: srv.URL, Token: "hf-token", API: srv.Client()}
	files, err := hub.List(context.Background(), "acme/tiny", "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("files = %+v", files)
	}
	if files[1].Link != "weights.bin" || files[1].Size != 0 {
		t.Fatalf("symlink = %+v", files[1])
	}
	if files[2].Size != 32 || files[2].SHA256 == "" {
		t.Fatalf("weights = %+v", files[2])
	}
}

func TestModelScopeList(t *testing.T) {
	const sum = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RequestURI()
		if r.Header.Get("Authorization") != "Bearer ms-token" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"Code":200,"Data":{"Files":[` +
			`{"Path":"config.json","Size":2,"Sha256":"` + sum + `","Type":"blob"},` +
			`{"Path":"subdir","Size":0,"Type":"tree"}]}}`))
	}))
	defer srv.Close()

	hub := &Hub{Kind: KindModelScope, Base: srv.URL, Token: "ms-token", API: srv.Client()}
	files, err := hub.List(context.Background(), "acme/tiny", "master")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].SHA256 != "sha256:"+sum {
		t.Fatalf("files = %+v", files)
	}
	if !strings.Contains(got, "/api/v1/models/acme/tiny/repo/files?") || !strings.Contains(got, "Revision=master") || !strings.Contains(got, "Recursive=True") {
		t.Fatalf("request = %s", got)
	}
}

func TestFileURL(t *testing.T) {
	hf := &Hub{Kind: KindHF, Base: "https://hf-mirror.com"}
	got := hf.fileURL("acme/tiny", "main", "sub/a b.bin")
	want := "https://hf-mirror.com/acme/tiny/resolve/main/sub/a%20b.bin"
	if got != want {
		t.Fatalf("hf url = %s", got)
	}
	ms := &Hub{Kind: KindModelScope, Base: "https://www.modelscope.cn"}
	got = ms.fileURL("acme/tiny", "master", "sub/a b.bin")
	if !strings.Contains(got, "/api/v1/models/acme/tiny/repo?") || !strings.Contains(got, "FilePath=sub%2Fa+b.bin") && !strings.Contains(got, "FilePath=sub%2Fa%20b.bin") {
		t.Fatalf("modelscope url = %s", got)
	}
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}

func TestListStopsOnHubRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://huggingface.co"+r.URL.Path, http.StatusPermanentRedirect)
	}))
	defer srv.Close()

	hub, err := newHub(Endpoint{Kind: KindHF, Base: srv.URL}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = hub.List(context.Background(), "acme/tiny", "main")
	var status *StatusError
	if !errors.As(err, &status) || status.Code != http.StatusPermanentRedirect || !strings.Contains(status.Body, "huggingface.co") {
		t.Fatalf("err = %v", err)
	}
}
