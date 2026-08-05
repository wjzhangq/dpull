package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/spf13/cobra"
	"github.com/wjzhangq/dpull/internal/archive"
	"github.com/wjzhangq/dpull/internal/auth"
	"github.com/wjzhangq/dpull/internal/downloader"
	"github.com/wjzhangq/dpull/internal/mirror"
	"github.com/wjzhangq/dpull/internal/progress"
	"github.com/wjzhangq/dpull/internal/ref"
	"github.com/wjzhangq/dpull/internal/registry"
	"github.com/wjzhangq/dpull/internal/store"
	"github.com/wjzhangq/dpull/pkg/version"
)

var (
	outputPath     string
	continueFlag   bool
	force          bool
	checkIntegrity bool
	keepCache      bool
)

func init() {
	rootCmd.Flags().StringVarP(&outputPath, "output", "o", "", "output tar file (default: auto-generated)")
	rootCmd.Flags().BoolVarP(&continueFlag, "continue", "c", false, "continue interrupted download")
	rootCmd.Flags().BoolVar(&force, "force", false, "force re-download even if task exists")
	rootCmd.Flags().BoolVar(&checkIntegrity, "check-integrity", true, "verify sha256 after download")
	rootCmd.Flags().BoolVar(&keepCache, "keep-cache", false, "keep cached blobs after successful tar creation")
}

func runPull(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	canonical := args[0]

	// Parse image reference
	imageRef, err := ref.Parse(canonical)
	if err != nil {
		return fmt.Errorf("parse image reference: %w", err)
	}

	// Determine platform
	targetPlatform := platform
	if targetPlatform == "" {
		targetPlatform = fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// Initialize store
	st, err := store.NewStore(cacheDir)
	if err != nil {
		return fmt.Errorf("init store: %w", err)
	}

	// Generate task ID (hash of canonical + platform)
	taskID := generateTaskID(canonical, targetPlatform)
	taskPath := st.TaskPath(taskID)

	// Load or create task state
	var taskState *store.TaskState
	if continueFlag && st.TaskExists(taskID) {
		taskState, err = store.LoadTask(taskPath)
		if err != nil {
			return fmt.Errorf("load task: %w", err)
		}
		fmt.Printf("Resuming task %s\n", taskID)
	} else if st.TaskExists(taskID) && !force {
		taskState, err = store.LoadTask(taskPath)
		if err != nil {
			return fmt.Errorf("load task: %w", err)
		}
		// Check if already complete
		if taskState.Progress() >= 1.0 {
			fmt.Println("Download already complete, assembling tar...")
			return assembleTarFromTask(taskState, st, canonical, targetPlatform)
		}
		fmt.Printf("Continuing existing task %s\n", taskID)
	} else {
		taskState = store.NewTaskState(taskID, canonical, targetPlatform)
	}

	// Setup registry client with proxy
	regClient := setupRegistryClient(imageRef)

	// Apply mirror rewrite if configured
	fetchRef := imageRef
	if mirrorHost != "" {
		m := mirror.NewMirror(mirrorHost, mirrorPath)
		rewrittenPath := m.RewriteRef(imageRef)
		taskState.Mirror.Endpoint = mirrorHost
		taskState.Mirror.Path = rewrittenPath

		// Construct mirror reference (still use original for manifest lookup)
		fmt.Printf("Using mirror: %s%s\n", mirrorHost, rewrittenPath)
	}

	// Fetch manifest
	fmt.Printf("Fetching manifest for %s (%s)...\n", canonical, targetPlatform)
	manifest, err := regClient.GetManifest(fetchRef, targetPlatform)
	if err != nil {
		return fmt.Errorf("fetch manifest: %w", err)
	}

	// Update task state with manifest info
	if taskState.ManifestDigest == "" {
		taskState.ManifestDigest = manifest.Digest
		taskState.ConfigDigest = manifest.Config.Digest
		taskState.TotalSize = manifest.TotalSize()
	} else if taskState.ManifestDigest != manifest.Digest && !force {
		return fmt.Errorf("manifest digest changed since last run (use --force to restart)")
	}

	// Initialize blob states if needed
	if len(taskState.Blobs) == 0 {
		taskState.Blobs = make([]store.BlobState, len(manifest.Layers))
		for i, layer := range manifest.Layers {
			taskState.Blobs[i] = store.BlobState{
				Digest: layer.Digest,
				Size:   layer.Size,
				State:  "pending",
			}
		}
	}

	if err := taskState.Save(taskPath); err != nil {
		return fmt.Errorf("save task state: %w", err)
	}

	// Download config blob
	configPath := st.BlobPath(manifest.Config.Digest)
	if !st.BlobExists(manifest.Config.Digest) {
		fmt.Printf("Downloading config (%d bytes)...\n", manifest.Config.Size)
		configResolver := registry.NewBlobURLResolver(regClient, fetchRef, manifest.Config.Digest)
		dl := downloader.New(
			downloader.WithHTTPClient(regClient.HTTPClient()),
			downloader.WithMaxRetries(maxRetries),
		)
		configState := &store.BlobState{
			Digest: manifest.Config.Digest,
			Size:   manifest.Config.Size,
			State:  "pending",
		}
		if err := dl.DownloadBlob(ctx, configResolver, configPath, manifest.Config.Size, configState, nil); err != nil {
			return fmt.Errorf("download config: %w", err)
		}
	}

	// Setup progress tracker
	tracker := progress.NewTracker(progressMode, manifest.TotalSize())
	for i, layer := range manifest.Layers {
		tracker.AddBlob(layer.Digest, layer.Size, i+1, len(manifest.Layers))
	}

	// Setup downloader with proxy-enabled client
	dl := downloader.New(
		downloader.WithHTTPClient(regClient.HTTPClient()),
		downloader.WithJobs(jobs),
		downloader.WithConnections(conns),
		downloader.WithMinSplitSize(minSplit),
		downloader.WithMaxRetries(maxRetries),
	)

	// Download all layers
	fmt.Printf("\nDownloading %d layers (%d total)...\n", len(manifest.Layers), manifest.TotalSize())
	for i, layer := range manifest.Layers {
		blobState := &taskState.Blobs[i]

		// Skip if already complete and verified
		if blobState.State == "complete" && blobState.Verified {
			tracker.CompleteBlob(layer.Digest)
			continue
		}

		layerPath := st.BlobPath(layer.Digest)
		urlResolver := registry.NewBlobURLResolver(regClient, fetchRef, layer.Digest)

		// Download with progress updates
		onProgress := func(downloaded int64) {
			tracker.UpdateBlob(layer.Digest, downloaded)
			if err := taskState.Save(taskPath); err != nil {
				fmt.Fprintf(os.Stderr, "warn: save task state: %v\n", err)
			}
		}

		if err := dl.DownloadBlob(ctx, urlResolver, layerPath, layer.Size, blobState, onProgress); err != nil {
			tracker.Wait()
			return fmt.Errorf("download layer %d/%d: %w", i+1, len(manifest.Layers), err)
		}

		// Verify integrity
		if checkIntegrity && !blobState.Verified {
			verified, err := st.VerifyBlob(layer.Digest)
			if err != nil {
				tracker.Wait()
				return fmt.Errorf("verify layer %d: %w", i+1, err)
			}
			if !verified {
				tracker.Wait()
				st.RemoveBlob(layer.Digest)
				return fmt.Errorf("layer %d integrity check failed (removed, retry download)", i+1)
			}
			blobState.Verified = true
		}

		tracker.CompleteBlob(layer.Digest)
		taskState.Save(taskPath)
	}

	tracker.Wait()
	fmt.Println("\nAll layers downloaded successfully")

	// Assemble docker-archive tar
	return assembleTarFromTask(taskState, st, canonical, targetPlatform)
}

func assembleTarFromTask(taskState *store.TaskState, st *store.Store, canonical, targetPlatform string) error {
	// Determine output path
	output := outputPath
	if output == "" {
		output = archive.GenerateOutputPath(canonical, targetPlatform)
	}

	fmt.Printf("Assembling tar: %s\n", output)

	// Build manifest from task state
	manifest := &registry.Manifest{
		Config: registry.Descriptor{
			Digest: taskState.ConfigDigest,
			Size:   0, // Not needed for tar assembly
		},
		Layers: make([]registry.Descriptor, len(taskState.Blobs)),
	}

	layerPaths := make([]string, len(taskState.Blobs))
	for i, blob := range taskState.Blobs {
		manifest.Layers[i] = registry.Descriptor{
			Digest: blob.Digest,
			Size:   blob.Size,
		}
		layerPaths[i] = st.BlobPath(blob.Digest)
	}

	configPath := st.BlobPath(taskState.ConfigDigest)

	if err := archive.AssembleTar(manifest, configPath, layerPaths, canonical, output); err != nil {
		return fmt.Errorf("assemble tar: %w", err)
	}

	fmt.Printf("\nSaved to: %s\n", output)
	fmt.Printf("Load with: docker load -i %s\n", output)

	// Cleanup if requested
	if !keepCache {
		fmt.Println("Cleaning up cache...")
		st.RemoveTask(taskState.TaskID)
		for _, blob := range taskState.Blobs {
			st.RemoveBlob(blob.Digest)
		}
		st.RemoveBlob(taskState.ConfigDigest)
	}

	return nil
}

func setupRegistryClient(r *ref.Reference) *registry.Client {
	// Load Docker credentials
	kc, err := auth.LoadKeychain()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: load keychain: %v\n", err)
	}

	var authenticator authn.Authenticator = authn.Anonymous
	if kc != nil {
		if cred, ok := kc.Resolve(r.Registry); ok {
			authenticator = authn.FromConfig(authn.AuthConfig{
				Username: cred.Username,
				Password: cred.Password,
			})
		} else if helper, ok := kc.UsesExternalHelper(r.Registry); ok {
			fmt.Fprintf(os.Stderr, "warn: registry %s uses credential helper %q (not supported, using anonymous)\n", r.Registry, helper)
		}
	}

	opts := []registry.Option{
		registry.WithAuth(authenticator),
		registry.WithUserAgent("dpull/" + version.Version),
	}

	if proxy != "" {
		opts = append(opts, registry.WithProxy(proxy))
	}

	if plainHTTP {
		opts = append(opts, registry.WithPlainHTTP(true))
	}

	opts = append(opts, registry.WithTimeout(60*time.Second))

	return registry.NewClient(opts...)
}

func generateTaskID(canonical, platform string) string {
	h := sha256.New()
	h.Write([]byte(canonical))
	h.Write([]byte(platform))
	return hex.EncodeToString(h.Sum(nil))[:16]
}
