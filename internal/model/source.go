package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// File is one object in a model repository.
// Link is a relative symlink target. Those files are not downloaded.
type File struct {
	Path   string
	Size   int64
	SHA256 string
	Link   string
}

// Hub talks to one Hugging Face-compatible host or ModelScope.
type Hub struct {
	Kind    Kind
	Base    string
	Token   string
	API     *http.Client
	Resolve *http.Client
	Blob    *http.Client
}

func newHub(ep Endpoint, token, proxy, baseOverride string) (*Hub, error) {
	transport, err := newTransport(proxy)
	if err != nil {
		return nil, err
	}
	base := ep.Base
	if baseOverride != "" {
		base = strings.TrimRight(baseOverride, "/")
	}
	return &Hub{
		Kind:    ep.Kind,
		Base:    base,
		Token:   token,
		API:     newAPIClient(transport),
		Resolve: newResolveClient(transport),
		Blob:    newBlobClient(transport),
	}, nil
}

// List returns the files in one revision.
func (h *Hub) List(ctx context.Context, repoID, revision string) ([]File, error) {
	if h.Kind == KindModelScope {
		return h.listModelScope(ctx, repoID, revision)
	}
	return h.listHuggingFace(ctx, repoID, revision)
}

// Open returns a resolver that refreshes expired object-storage URLs.
func (h *Hub) Open(repoID, revision string, file File) *RedirectResolver {
	return &RedirectResolver{
		source:  h.fileURL(repoID, revision, file.Path),
		token:   h.Token,
		resolve: h.Resolve,
		blob:    h.Blob,
	}
}

func (h *Hub) fileURL(repoID, revision, filePath string) string {
	if h.Kind == KindModelScope {
		query := url.Values{}
		query.Set("Revision", revision)
		query.Set("FilePath", filePath)
		return h.Base + "/api/v1/models/" + escapeSegments(repoID) + "/repo?" + query.Encode()
	}
	return h.Base + "/" + escapeSegments(repoID) + "/resolve/" + escapeSegments(revision) + "/" + escapeSegments(filePath)
}

func (h *Hub) listHuggingFace(ctx context.Context, repoID, revision string) ([]File, error) {
	next := h.Base + "/api/models/" + escapeSegments(repoID) + "/tree/" + escapeSegments(revision) + "?recursive=true"
	var files []File
	seen := map[string]bool{}
	for next != "" {
		if seen[next] {
			break
		}
		seen[next] = true
		page, link, err := h.fetchHuggingFacePage(ctx, repoID, next)
		if err != nil {
			return nil, err
		}
		files = append(files, page...)
		next = nextLink(link, h.Base)
	}
	return files, nil
}

func (h *Hub) fetchHuggingFacePage(ctx context.Context, repoID, pageURL string) ([]File, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, "", err
	}
	setHubHeaders(req, h.Token)
	resp, err := h.API.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("list %s on %s: %w", repoID, h.Base, err)
	}
	body, err := readBody(resp, 64<<20)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", responseStatus(fmt.Sprintf("list %s on %s", repoID, hostOf(h.Base)), resp, body)
	}
	page, err := decodeHuggingFace(body)
	if err != nil {
		return nil, "", fmt.Errorf("list %s: %w", repoID, err)
	}
	return page, resp.Header.Get("Link"), nil
}

func decodeHuggingFace(body []byte) ([]File, error) {
	var entries []struct {
		Type   string `json:"type"`
		Path   string `json:"path"`
		Size   int64  `json:"size"`
		Target string `json:"target"`
		LFS    *struct {
			OID  string `json:"oid"`
			Size int64  `json:"size"`
		} `json:"lfs"`
	}
	if err := json.Unmarshal(body, &entries); err != nil {
		var hubErr struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &hubErr) == nil && hubErr.Error != "" {
			return nil, fmt.Errorf("%s", hubErr.Error)
		}
		return nil, fmt.Errorf("decode file list: %w", err)
	}
	var files []File
	for _, entry := range entries {
		if entry.Path == "" || entry.Type == "directory" {
			continue
		}
		file := File{Path: entry.Path, Size: entry.Size, Link: entry.Target}
		if entry.LFS != nil {
			file.Size = entry.LFS.Size
			file.SHA256 = normalizeSHA256(entry.LFS.OID)
		}
		if file.Link != "" {
			file.Size = 0
			file.SHA256 = ""
		}
		files = append(files, file)
	}
	return files, nil
}

func (h *Hub) listModelScope(ctx context.Context, repoID, revision string) ([]File, error) {
	query := url.Values{}
	query.Set("Revision", revision)
	query.Set("Recursive", "True")
	endpoint := h.Base + "/api/v1/models/" + escapeSegments(repoID) + "/repo/files?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	setHubHeaders(req, h.Token)
	resp, err := h.API.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list %s on %s: %w", repoID, h.Base, err)
	}
	body, err := readBody(resp, 64<<20)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, responseStatus(fmt.Sprintf("list %s on %s", repoID, hostOf(h.Base)), resp, body)
	}
	files, err := decodeModelScope(body)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", repoID, err)
	}
	return files, nil
}

func decodeModelScope(body []byte) ([]File, error) {
	var payload struct {
		Code    int    `json:"Code"`
		Message string `json:"Message"`
		Data    struct {
			Files []struct {
				Path   string `json:"Path"`
				Size   int64  `json:"Size"`
				Sha256 string `json:"Sha256"`
				Type   string `json:"Type"`
				Target string `json:"Target"`
			} `json:"Files"`
		} `json:"Data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode file list: %w", err)
	}
	if payload.Code != 0 && payload.Code != http.StatusOK {
		if payload.Message == "" {
			payload.Message = "request failed"
		}
		return nil, fmt.Errorf("%s", payload.Message)
	}
	var files []File
	for _, entry := range payload.Data.Files {
		switch strings.ToLower(entry.Type) {
		case "tree", "dir", "directory":
			continue
		}
		if entry.Path == "" {
			continue
		}
		file := File{
			Path:   entry.Path,
			Size:   entry.Size,
			SHA256: normalizeSHA256(entry.Sha256),
			Link:   entry.Target,
		}
		if file.Link != "" {
			file.Size = 0
			file.SHA256 = ""
		}
		files = append(files, file)
	}
	return files, nil
}

func normalizeSHA256(value string) string {
	value = strings.Trim(strings.TrimSpace(value), `"`)
	value = strings.TrimPrefix(strings.ToLower(value), "sha256:")
	if len(value) != 64 {
		return ""
	}
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return ""
		}
	}
	return "sha256:" + value
}

func syntheticDigest(canonical, filePath string) string {
	sum := sha256.Sum256([]byte("mpull\n" + canonical + "\n" + filePath))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func fingerprint(files []File) string {
	hash := sha256.New()
	for _, file := range files {
		fmt.Fprintf(hash, "%s\t%d\t%s\t%s\n", file.Path, file.Size, file.SHA256, file.Link)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func nextLink(header, base string) string {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) && !strings.Contains(part, `rel=next`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start < 0 || end <= start {
			continue
		}
		link := part[start+1 : end]
		if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") {
			return link
		}
		return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(link, "/")
	}
	return ""
}

func responseStatus(op string, resp *http.Response, body []byte) *StatusError {
	text := trimBody(body)
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if loc := resp.Header.Get("Location"); loc != "" {
			text = "redirect to " + loc
		}
	}
	return &StatusError{Op: op, Code: resp.StatusCode, Body: text}
}

func hostOf(base string) string {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		return base
	}
	return parsed.Host
}
