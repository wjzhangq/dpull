package model

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPullHuggingFaceAndResume(t *testing.T) {
	body := bytes.Repeat([]byte("w"), 32)
	sum := sha256.Sum256(body)
	oid := hex.EncodeToString(sum[:])
	var ranged atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/tree/"):
			if r.Header.Get("Authorization") != "Bearer secret" {
				t.Errorf("list auth = %q", r.Header.Get("Authorization"))
			}
			fmt.Fprintf(w, `[{"type":"file","path":"config.json","size":2},{"type":"file","path":"weights.bin","size":32,"lfs":{"oid":"sha256:%s","size":32}}]`, oid)
		case strings.Contains(r.URL.Path, "/resolve/"):
			name := path.Base(r.URL.Path)
			exp := strconv.FormatInt(time.Now().Add(2*time.Hour).Unix(), 10)
			w.Header().Set("Location", srv.URL+"/cdn/"+name+"?Expires="+exp)
			w.WriteHeader(http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/cdn/"):
			if r.Header.Get("Authorization") != "" {
				t.Errorf("cdn received Authorization")
			}
			payload := []byte("{}")
			if path.Base(r.URL.Path) == "weights.bin" {
				payload = body
			}
			if raw := r.Header.Get("Range"); raw != "" && raw != "bytes=0-0" {
				ranged.Add(1)
			}
			writeRange(w, r, payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	opt := testOptions(t, srv.URL, "acme/tiny")
	if err := Pull(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(opt.Output, "config.json"), "{}")
	assertFile(t, filepath.Join(opt.Output, "weights.bin"), string(body))
	afterFirst := ranged.Load()
	if afterFirst == 0 {
		t.Fatal("weights file was not split across range requests")
	}
	if err := os.RemoveAll(opt.Output); err != nil {
		t.Fatal(err)
	}
	if err := Pull(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	if ranged.Load() != afterFirst {
		t.Fatalf("resume downloaded again: ranged %d -> %d", afterFirst, ranged.Load())
	}
	assertFile(t, filepath.Join(opt.Output, "weights.bin"), string(body))
}

func TestPullIncludeAndRejectsEscape(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tree/") {
			_, _ = w.Write([]byte(`[{"type":"file","path":"config.json","size":2},{"type":"file","path":"weights.bin","size":4},{"type":"file","path":"../evil.txt","size":1}]`))
			return
		}
		if strings.Contains(r.URL.Path, "/resolve/") {
			name := path.Base(r.URL.Path)
			w.Header().Set("Location", srv.URL+"/cdn/"+name)
			w.WriteHeader(http.StatusFound)
			return
		}
		payload := []byte("body")
		if path.Base(r.URL.Path) == "config.json" {
			payload = []byte("{}")
		}
		writeRange(w, r, payload)
	}))
	defer srv.Close()

	opt := testOptions(t, srv.URL, "acme/tiny")
	opt.Includes = []string{"weights.bin"}
	if err := Pull(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(opt.Output, "config.json")); !os.IsNotExist(err) {
		t.Fatalf("config.json was written: %v", err)
	}
	assertFile(t, filepath.Join(opt.Output, "weights.bin"), "body")

	escaped := testOptions(t, srv.URL, "acme/tiny")
	if err := Pull(context.Background(), escaped); err == nil {
		t.Fatal("path escape was accepted")
	}
}

func TestRevisionFallsBackToMaster(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/tree/main"):
			http.NotFound(w, r)
		case strings.Contains(r.URL.Path, "/tree/master"):
			_, _ = w.Write([]byte(`[{"type":"file","path":"config.json","size":2}]`))
		case strings.Contains(r.URL.Path, "/resolve/master/"):
			w.Header().Set("Location", srv.URL+"/cdn/config.json")
			w.WriteHeader(http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/cdn/"):
			writeRange(w, r, []byte("{}"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	opt := testOptions(t, srv.URL, "acme/tiny")
	if err := Pull(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(opt.Output, "config.json"), "{}")
}

func TestPullModelScope(t *testing.T) {
	body := []byte("ms-weights")
	sum := sha256.Sum256(body)
	oid := hex.EncodeToString(sum[:])
	var sawFile bool
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/repo/files"):
			if r.URL.Query().Get("Revision") != "master" || r.URL.Query().Get("Recursive") != "True" {
				t.Errorf("list query = %s", r.URL.RawQuery)
			}
			fmt.Fprintf(w, `{"Code":200,"Data":{"Files":[{"Path":"weights.bin","Size":%d,"Sha256":"%s","Type":"blob"}]}}`, len(body), oid)
		case strings.Contains(r.URL.Path, "/repo"):
			if r.URL.Query().Get("FilePath") != "weights.bin" {
				t.Errorf("file path = %s", r.URL.Query().Get("FilePath"))
			}
			sawFile = true
			w.Header().Set("Location", srv.URL+"/cdn/weights.bin?Expires="+strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
			w.WriteHeader(http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/cdn/"):
			writeRange(w, r, body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	opt := testOptions(t, srv.URL, "ms:acme/tiny")
	if err := Pull(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	if !sawFile {
		t.Fatal("modelscope file URL was not requested")
	}
	assertFile(t, filepath.Join(opt.Output, "weights.bin"), string(body))
}

func TestIntegrityFailure(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tree/") {
			_, _ = w.Write([]byte(`[{"type":"file","path":"weights.bin","size":4,"lfs":{"oid":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":4}}]`))
			return
		}
		if strings.Contains(r.URL.Path, "/resolve/") {
			w.Header().Set("Location", srv.URL+"/cdn/weights.bin")
			w.WriteHeader(http.StatusFound)
			return
		}
		writeRange(w, r, []byte("nope"))
	}))
	defer srv.Close()

	opt := testOptions(t, srv.URL, "acme/tiny")
	err := Pull(context.Background(), opt)
	if err == nil || !strings.Contains(err.Error(), "integrity check failed") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(opt.Output, "weights.bin")); !os.IsNotExist(statErr) {
		t.Fatal("corrupt file was placed into the output")
	}
}

func testOptions(t *testing.T, base, ref string) PullOptions {
	t.Helper()
	return PullOptions{
		Ref:            ref,
		Output:         t.TempDir(),
		CacheDir:       t.TempDir(),
		Token:          "secret",
		Jobs:           2,
		Connections:    4,
		MinSplit:       8,
		MaxRetries:     2,
		Progress:       "none",
		CheckIntegrity: true,
		endpointBase:   base,
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func writeRange(w http.ResponseWriter, r *http.Request, body []byte) {
	w.Header().Set("Accept-Ranges", "bytes")
	start, end := 0, len(body)-1
	if raw := r.Header.Get("Range"); raw != "" {
		var err error
		start, end, err = parseRange(raw, len(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(body)))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body[start : end+1])
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
}

func parseRange(raw string, size int) (int, int, error) {
	value := raw
	if len(value) < len("bytes=") || value[:len("bytes=")] != "bytes=" {
		return 0, 0, fmt.Errorf("bad range")
	}
	value = value[len("bytes="):]
	left, right, ok := cutByte(value, '-')
	if !ok {
		return 0, 0, fmt.Errorf("bad range")
	}
	start, err := strconv.Atoi(left)
	if err != nil || start < 0 || start >= size {
		return 0, 0, fmt.Errorf("bad range")
	}
	end := size - 1
	if right != "" {
		end, err = strconv.Atoi(right)
		if err != nil || end < start || end >= size {
			return 0, 0, fmt.Errorf("bad range")
		}
	}
	return start, end, nil
}

func cutByte(value string, sep byte) (string, string, bool) {
	for i := 0; i < len(value); i++ {
		if value[i] == sep {
			return value[:i], value[i+1:], true
		}
	}
	return "", "", false
}
