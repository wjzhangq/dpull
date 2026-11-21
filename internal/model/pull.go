package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/docker/go-units"
	"github.com/wjzhangq/dpull/internal/downloader"
	"github.com/wjzhangq/dpull/internal/progress"
	"github.com/wjzhangq/dpull/internal/store"
)

// PullOptions controls one model snapshot.
// endpointBase is a test override for the hub URL.
type PullOptions struct {
	Ref            string
	Output         string
	Revision       string
	Mirror         string
	Proxy          string
	Token          string
	Includes       []string
	Excludes       []string
	CacheDir       string
	Jobs           int
	Connections    int
	MinSplit       int64
	MaxRetries     int
	Progress       string
	Force          bool
	CheckIntegrity bool
	Stdout         io.Writer

	endpointBase string
}

type snapshot struct {
	Endpoint Endpoint
	Files    []File
	Output   string
	Includes []string
	Excludes []string
}

// Info lists a repository without downloading it.
func Info(ctx context.Context, opt PullOptions) (string, error) {
	snap, err := prepare(ctx, opt)
	if err != nil {
		return "", err
	}
	return formatSnapshot(snap), nil
}

// Pull downloads a model repository into a directory.
func Pull(ctx context.Context, opt PullOptions) error {
	opt = normalize(opt)
	snap, err := prepare(ctx, opt)
	if err != nil {
		return err
	}
	st, err := store.NewStore(opt.CacheDir)
	if err != nil {
		return err
	}
	task, taskPath, err := loadOrCreate(st, snap, opt.Force)
	if err != nil {
		return err
	}
	out := writer(opt)
	fmt.Fprintf(out, "%s @ %s\n", snap.Endpoint.ID, snap.Endpoint.Revision)
	fmt.Fprintf(out, "%d files, %s  via %s\n", len(snap.Files), units.BytesSize(float64(totalSize(snap.Files))), snap.Endpoint.Host)
	fmt.Fprintf(out, "Output: %s\n", snap.Output)

	hub, err := newHub(snap.Endpoint, opt.Token, opt.Proxy, opt.endpointBase)
	if err != nil {
		return err
	}
	tracker := progress.NewTracker(opt.Progress, totalSize(snap.Files))
	for i, file := range snap.Files {
		if file.Size <= 0 || file.Link != "" {
			continue
		}
		tracker.AddBlob(file.Path, file.Size, i+1, len(snap.Files))
		if done := task.Blobs[i].BytesDownloaded(); done > 0 {
			tracker.UpdateBlob(file.Path, done)
		}
	}
	dl := downloader.New(
		downloader.WithHTTPClient(hub.Blob),
		downloader.WithJobs(opt.Jobs),
		downloader.WithConnections(opt.Connections),
		downloader.WithMinSplitSize(opt.MinSplit),
		downloader.WithMaxRetries(opt.MaxRetries),
	)
	err = downloadAll(ctx, dl, hub, st, task, taskPath, snap, tracker, opt.CheckIntegrity, opt.Jobs)
	tracker.Wait()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Saved to %s\n", snap.Output)
	return nil
}

func prepare(ctx context.Context, opt PullOptions) (snapshot, error) {
	opt = normalize(opt)
	ref, err := Parse(opt.Ref)
	if err != nil {
		return snapshot{}, err
	}
	ep, err := Resolve(ref, opt.Mirror, opt.Revision, opt.Proxy)
	if err != nil {
		return snapshot{}, err
	}
	if opt.endpointBase != "" {
		ep.Base = strings.TrimRight(opt.endpointBase, "/")
		ep.Pinned = true
	}
	includes := append([]string(nil), opt.Includes...)
	if ep.File != "" && len(includes) == 0 {
		includes = []string{ep.File}
	}
	files, ep, err := listFiles(ctx, ep, opt)
	if err != nil {
		return snapshot{}, err
	}
	files = Filter(files, includes, opt.Excludes)
	if len(files) == 0 {
		return snapshot{}, fmt.Errorf("no files matched")
	}
	output := opt.Output
	if output == "" {
		output = filepath.Base(ep.ID)
	}
	if err := validateFiles(output, files); err != nil {
		return snapshot{}, err
	}
	return snapshot{
		Endpoint: ep,
		Files:    files,
		Output:   output,
		Includes: includes,
		Excludes: append([]string(nil), opt.Excludes...),
	}, nil
}

func listFiles(ctx context.Context, ep Endpoint, opt PullOptions) ([]File, Endpoint, error) {
	files, used, err := listEndpoint(ctx, ep, opt)
	if err == nil {
		return files, used, nil
	}
	next, ok := ep.Fallback()
	if !ok || !ShouldFailover(err) {
		return nil, ep, err
	}
	fmt.Fprintf(writer(opt), "%s failed (%v), trying %s\n", ep.Host, err, next.Host)
	return listEndpoint(ctx, next, opt)
}

func listEndpoint(ctx context.Context, ep Endpoint, opt PullOptions) ([]File, Endpoint, error) {
	hub, err := newHub(ep, opt.Token, opt.Proxy, opt.endpointBase)
	if err != nil {
		return nil, ep, err
	}
	files, err := hub.List(ctx, ep.ID, ep.Revision)
	if err == nil {
		return files, ep, nil
	}
	altRev, ok := alternateRevision(ep.Revision)
	if !ok || !ep.DefaultRevision || !isNotFound(err) {
		return nil, ep, err
	}
	alt := ep
	alt.Revision = altRev
	alt.DefaultRevision = false
	files, err = hub.List(ctx, alt.ID, alt.Revision)
	if err != nil {
		return nil, ep, err
	}
	return files, alt, nil
}

func loadOrCreate(st *store.Store, snap snapshot, force bool) (*store.TaskState, string, error) {
	id := taskID(snap.Endpoint.Canonical(), snap.Includes, snap.Excludes)
	path := st.TaskPath(id)
	fp := fingerprint(snap.Files)
	if !force && st.TaskExists(id) {
		task, err := store.LoadTask(path)
		if err != nil {
			return nil, "", err
		}
		if task.ManifestDigest != fp {
			return nil, "", fmt.Errorf("files changed since the last run; use --force to restart")
		}
		if err := sameLayout(task, snap.Files); err != nil {
			return nil, "", err
		}
		return task, path, nil
	}
	task := store.NewTaskState(id, snap.Endpoint.Canonical(), "model")
	task.ManifestDigest = fp
	task.Mirror = store.MirrorInfo{Endpoint: snap.Endpoint.Host, Path: snap.Endpoint.ID}
	task.TotalSize = totalSize(snap.Files)
	task.Blobs = blobsFor(snap.Endpoint, snap.Files)
	if err := task.Save(path); err != nil {
		return nil, "", err
	}
	return task, path, nil
}

func blobsFor(ep Endpoint, files []File) []store.BlobState {
	out := make([]store.BlobState, len(files))
	for i, file := range files {
		blob := store.BlobState{Path: file.Path, Size: file.Size, State: "pending"}
		switch {
		case file.Link != "":
			blob.Digest = "link"
			blob.State = "complete"
			blob.Verified = true
		case file.SHA256 != "":
			blob.Digest = file.SHA256
		default:
			blob.Digest = syntheticDigest(ep.Canonical(), file.Path)
		}
		out[i] = blob
	}
	return out
}

func sameLayout(task *store.TaskState, files []File) error {
	if len(task.Blobs) != len(files) {
		return fmt.Errorf("files changed since the last run; use --force to restart")
	}
	for i, file := range files {
		if task.Blobs[i].Path != file.Path {
			return fmt.Errorf("files changed since the last run; use --force to restart")
		}
	}
	return nil
}

func downloadAll(
	ctx context.Context,
	dl *downloader.Downloader,
	hub *Hub,
	st *store.Store,
	task *store.TaskState,
	taskPath string,
	snap snapshot,
	tracker progress.Tracker,
	check bool,
	jobs int,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if jobs < 1 {
		jobs = 1
	}
	var (
		wg       sync.WaitGroup
		errMu    sync.Mutex
		firstErr error
		sem      = make(chan struct{}, jobs)
		locks    keyLock
	)
	for i := range snap.Files {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
				defer func() { <-sem }()
			}
			err := downloadOne(ctx, dl, hub, st, task, taskPath, snap, tracker, check, &locks, i)
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				errMu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	return firstErr
}

func downloadOne(
	ctx context.Context,
	dl *downloader.Downloader,
	hub *Hub,
	st *store.Store,
	task *store.TaskState,
	taskPath string,
	snap snapshot,
	tracker progress.Tracker,
	check bool,
	locks *keyLock,
	index int,
) error {
	file := snap.Files[index]
	blob := &task.Blobs[index]
	if file.Link != "" {
		if err := placeLink(snap.Output, file.Path, file.Link); err != nil {
			return err
		}
		tracker.CompleteBlob(file.Path)
		return nil
	}
	dest, err := SafeJoin(snap.Output, file.Path)
	if err != nil {
		return err
	}
	if blob.State == "complete" && blob.Verified && st.BlobExists(blob.Digest) {
		if err := placeFile(st.BlobPath(blob.Digest), dest); err != nil {
			return err
		}
		tracker.CompleteBlob(file.Path)
		return nil
	}

	locks.get(blob.Digest).Lock()
	defer locks.get(blob.Digest).Unlock()

	ready, err := cacheReady(st, file, blob.Digest)
	if err != nil {
		return err
	}
	if ready {
		if err := markVerified(task, taskPath, blob); err != nil {
			return err
		}
		if err := placeFile(st.BlobPath(blob.Digest), dest); err != nil {
			return err
		}
		tracker.CompleteBlob(file.Path)
		return nil
	}

	commit := func(fn func()) error { return task.Mutate(taskPath, fn) }
	resolver := hub.Open(snap.Endpoint.ID, snap.Endpoint.Revision, file)
	err = dl.DownloadBlob(ctx, resolver, st.BlobPath(blob.Digest), file.Size, blob, commit, func(done int64) {
		tracker.UpdateBlob(file.Path, done)
	})
	if err != nil {
		return fmt.Errorf("download %s: %w", file.Path, err)
	}
	if err := verifyFile(st, task, taskPath, file, blob, check); err != nil {
		return err
	}
	if err := placeFile(st.BlobPath(blob.Digest), dest); err != nil {
		return err
	}
	tracker.CompleteBlob(file.Path)
	return nil
}

func cacheReady(st *store.Store, file File, digest string) (bool, error) {
	if !st.BlobExists(digest) {
		return false, nil
	}
	if file.SHA256 != "" {
		return st.VerifyBlob(file.SHA256)
	}
	if file.Size > 0 && st.BlobSize(digest) != file.Size {
		return false, nil
	}
	return true, nil
}

func verifyFile(st *store.Store, task *store.TaskState, taskPath string, file File, blob *store.BlobState, check bool) error {
	if file.SHA256 != "" && check {
		ok, err := st.VerifyBlob(file.SHA256)
		if err != nil {
			return fmt.Errorf("verify %s: %w", file.Path, err)
		}
		if !ok {
			_ = st.RemoveBlob(file.SHA256)
			_ = resetBlob(task, taskPath, blob)
			return fmt.Errorf("integrity check failed for %s", file.Path)
		}
	} else if file.Size > 0 && st.BlobSize(blob.Digest) != file.Size {
		_ = st.RemoveBlob(blob.Digest)
		_ = resetBlob(task, taskPath, blob)
		return fmt.Errorf("size mismatch for %s", file.Path)
	}
	return markVerified(task, taskPath, blob)
}

func markVerified(task *store.TaskState, taskPath string, blob *store.BlobState) error {
	return task.Mutate(taskPath, func() {
		blob.State = "complete"
		blob.Verified = true
	})
}

func resetBlob(task *store.TaskState, taskPath string, blob *store.BlobState) error {
	return task.Mutate(taskPath, func() {
		blob.State = "pending"
		blob.Verified = false
		blob.Bitfield = ""
		blob.PieceSize = 0
		blob.Pieces = 0
	})
}

type keyLock struct {
	mu sync.Mutex
	m  map[string]*sync.Mutex
}

func (k *keyLock) get(key string) *sync.Mutex {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.m == nil {
		k.m = map[string]*sync.Mutex{}
	}
	lock, ok := k.m[key]
	if !ok {
		lock = &sync.Mutex{}
		k.m[key] = lock
	}
	return lock
}

func taskID(canonical string, includes, excludes []string) string {
	inc := append([]string(nil), includes...)
	exc := append([]string(nil), excludes...)
	sort.Strings(inc)
	sort.Strings(exc)
	sum := sha256.Sum256([]byte(canonical + "\x00" + strings.Join(inc, "\n") + "\x00" + strings.Join(exc, "\n")))
	return hex.EncodeToString(sum[:8])
}

func totalSize(files []File) int64 {
	var total int64
	for _, file := range files {
		if file.Link == "" && file.Size > 0 {
			total += file.Size
		}
	}
	return total
}

func validateFiles(root string, files []File) error {
	seen := map[string]bool{}
	for _, file := range files {
		if seen[file.Path] {
			return fmt.Errorf("duplicate path %s", file.Path)
		}
		seen[file.Path] = true
		if _, err := SafeJoin(root, file.Path); err != nil {
			return err
		}
		if file.Link != "" {
			if err := validLink(file.Link); err != nil {
				return err
			}
		}
	}
	return nil
}

func formatSnapshot(snap snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Source:    %s\n", snap.Endpoint.Host)
	fmt.Fprintf(&b, "Model:     %s\n", snap.Endpoint.ID)
	fmt.Fprintf(&b, "Revision:  %s\n", snap.Endpoint.Revision)
	fmt.Fprintf(&b, "Files:     %d\n", len(snap.Files))
	fmt.Fprintf(&b, "Download:  %s\n", units.BytesSize(float64(totalSize(snap.Files))))
	fmt.Fprintf(&b, "Output:    %s\n", snap.Output)
	for _, file := range snap.Files {
		note := ""
		if file.Link != "" {
			note = " -> " + file.Link
		} else if len(file.SHA256) >= 15 {
			note = "  " + file.SHA256[:15]
		}
		fmt.Fprintf(&b, "  %8s  %s%s\n", units.BytesSize(float64(file.Size)), file.Path, note)
	}
	return b.String()
}

func normalize(opt PullOptions) PullOptions {
	if opt.Jobs < 1 {
		opt.Jobs = 3
	}
	if opt.Connections < 1 {
		opt.Connections = 8
	}
	if opt.Connections > 32 {
		opt.Connections = 32
	}
	if opt.MinSplit <= 0 {
		opt.MinSplit = 20 << 20
	}
	if opt.MaxRetries < 0 {
		opt.MaxRetries = 10
	}
	if opt.Progress == "" {
		opt.Progress = "bar"
	}
	if opt.CacheDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			opt.CacheDir = filepath.Join(home, ".mpull", "cache")
		}
	}
	if opt.Stdout == nil {
		opt.Stdout = io.Discard
	}
	return opt
}

func writer(opt PullOptions) io.Writer {
	if opt.Stdout != nil {
		return opt.Stdout
	}
	return io.Discard
}
