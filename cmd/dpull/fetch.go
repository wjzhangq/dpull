package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/wjzhangq/dpull/internal/mirror"
	"github.com/wjzhangq/dpull/internal/ref"
	"github.com/wjzhangq/dpull/internal/registry"
)

const manifestProbeTimeout = 12 * time.Second

// prepareFetch returns the registry reference to download from, its manifest,
// and the hosts a layer may fall back to. sources[0] is fetchRef.
//
// -m picks one mirror and does not consult the built-in list.
// With no -m, docker.io is tried first. A reachability failure probes the
// primary mirrors in parallel, then the backup mirrors.
func prepareFetch(ctx context.Context, client *registry.Client, image *ref.Reference, platformName string) (*ref.Reference, *registry.Manifest, []*ref.Reference, error) {
	if mirrorHost != "" {
		fetchRef, err := manualFetchRef(image)
		if err != nil {
			return nil, nil, nil, err
		}
		fmt.Printf("Using mirror: %s\n", fetchRef.String())
		manifest, err := client.GetManifest(ctx, fetchRef, platformName)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("fetch manifest: %w", err)
		}
		return fetchRef, manifest, []*ref.Reference{fetchRef}, nil
	}

	if !autoMirror || !mirror.IsDockerHub(image.Registry) {
		manifest, err := client.GetManifest(ctx, image, platformName)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("fetch manifest: %w", err)
		}
		return image, manifest, []*ref.Reference{image}, nil
	}

	tryCtx, cancel := context.WithTimeout(ctx, manifestProbeTimeout)
	manifest, err := client.GetManifest(tryCtx, image, platformName)
	cancel()
	if err == nil {
		return image, manifest, append([]*ref.Reference{image}, fallbacks(image)...), nil
	}
	if !registry.IsReachabilityError(err) {
		return nil, nil, nil, fmt.Errorf("fetch manifest: %w", err)
	}

	fmt.Printf("Registry %s unreachable: %v\n", image.Registry, err)
	fmt.Println("Trying built-in mirrors...")
	for i, tier := range mirror.ByTier() {
		chosen, found, probeErr := probeTier(ctx, client, image, platformName, tier)
		if probeErr == nil {
			label := chosen.Registry
			if site, ok := mirror.Lookup(chosen.Registry); ok {
				label = site.Title
			}
			fmt.Printf("Using mirror: %s (%s)\n", chosen.String(), label)
			rest := fallbacks(image)
			sources := []*ref.Reference{chosen}
			for _, candidate := range rest {
				if candidate.Registry != chosen.Registry {
					sources = append(sources, candidate)
				}
			}
			return chosen, found, sources, nil
		}
		if i == 0 {
			fmt.Printf("Primary mirrors failed: %v\n", probeErr)
		}
	}
	return nil, nil, nil, fmt.Errorf("docker.io and every built-in mirror failed")
}

func manualFetchRef(image *ref.Reference) (*ref.Reference, error) {
	host, repoPath, err := mirror.Resolve(mirrorHost, mirrorPath, image)
	if err != nil {
		return nil, fmt.Errorf("mirror: %w", err)
	}
	return mirror.FetchRef(host, repoPath, image), nil
}

func fallbacks(image *ref.Reference) []*ref.Reference {
	var out []*ref.Reference
	for _, group := range mirror.ByTier() {
		for _, site := range group {
			host, repoPath, err := mirror.Resolve(site.Host, "", image)
			if err != nil {
				continue
			}
			out = append(out, mirror.FetchRef(host, repoPath, image))
		}
	}
	return out
}

type probeHit struct {
	ref      *ref.Reference
	manifest *registry.Manifest
	err      error
	name     string
}

func probeTier(ctx context.Context, client *registry.Client, image *ref.Reference, platformName string, sites []mirror.Site) (*ref.Reference, *registry.Manifest, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	hits := make(chan probeHit, len(sites))
	var wg sync.WaitGroup
	for _, site := range sites {
		wg.Add(1)
		go func(site mirror.Site) {
			defer wg.Done()
			host, repoPath, err := mirror.Resolve(site.Host, "", image)
			if err != nil {
				hits <- probeHit{err: err, name: site.Name}
				return
			}
			fetchRef := mirror.FetchRef(host, repoPath, image)
			tryCtx, tryCancel := context.WithTimeout(ctx, manifestProbeTimeout)
			defer tryCancel()
			manifest, err := client.GetManifest(tryCtx, fetchRef, platformName)
			hits <- probeHit{ref: fetchRef, manifest: manifest, err: err, name: site.Name}
		}(site)
	}
	go func() {
		wg.Wait()
		close(hits)
	}()

	var failed []string
	for hit := range hits {
		if hit.err == nil && hit.manifest != nil {
			cancel()
			return hit.ref, hit.manifest, nil
		}
		failed = append(failed, fmt.Sprintf("%s: %v", hit.name, hit.err))
	}
	return nil, nil, fmt.Errorf("%s", joinLines(failed))
}

func joinLines(parts []string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += "; "
		}
		out += part
	}
	if out == "" {
		return "no mirrors"
	}
	return out
}
