package gekko

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func s2hAssetFiles(t *testing.T, count int) ([]string, int64) {
	t.Helper()
	paths := make([]string, count)
	root := t.TempDir()
	asset := content.NewAssetDef("s2h")
	asset.Parts = []content.AssetPartDef{{ID: "part", Name: "part", Source: testProceduralPartSource()}}
	for i := range paths {
		paths[i] = filepath.Join(root, fmt.Sprintf("asset-%02d.gkasset", i))
		if err := content.SaveAsset(paths[i], asset); err != nil {
			t.Fatal(err)
		}
	}
	probe := NewRuntimeContentLoader()
	s2hLoadAsset(t, probe, paths[0])
	charge := probe.Stats().Bytes
	if charge <= 1 {
		t.Fatalf("fixture must exceed one-byte budget: %d", charge)
	}
	return paths, charge
}

func s2hLoadAsset(t *testing.T, loader *RuntimeContentLoader, path string) *content.AssetDef {
	t.Helper()
	asset, err := loader.LoadAsset(path)
	if err != nil || asset == nil {
		t.Fatalf("load asset: definition=%v error=%v", asset, err)
	}
	return asset
}

func TestS2hLoaderAllPinnedPressureAndFinalScopeClose(t *testing.T) {
	const count = 32
	paths, charge := s2hAssetFiles(t, count)
	loader := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: 1})
	a, b := loader.NewScope(), loader.NewScope()
	t.Cleanup(a.Close)
	t.Cleanup(b.Close)
	values := make([]*content.AssetDef, count)
	for i, path := range paths {
		values[i] = s2hLoadAsset(t, a.Loader(), path)
		if got := s2hLoadAsset(t, b.Loader(), path); got != values[i] {
			t.Fatalf("scopes did not share asset-%d", i)
		}
		if visits := loader.Stats().EvictionCandidateVisits; visits != 0 {
			t.Fatalf("all-pinned admission examined candidates: %d", visits)
		}
	}
	before := loader.Stats()
	if before.Entries != count || before.Bytes != count*charge || before.PinnedBytes != before.Bytes || before.OverBudgetBytes != before.Bytes-1 {
		t.Fatalf("all-pinned fixture charge mismatch: %+v charge=%d", before, charge)
	}
	loader.Clear()
	a.Close()
	a.Close()
	if stats := loader.Stats(); stats.Bytes != before.Bytes || stats.PinnedBytes != before.PinnedBytes || stats.EvictionCandidateVisits != 0 {
		t.Fatalf("Clear or first scope close visited pinned entries or lost leases: %+v", stats)
	}
	b.Close()
	after := loader.Stats()
	if after.Entries != 0 || after.Bytes != 0 || after.PinnedBytes != 0 || after.OverBudgetBytes != 0 ||
		after.Evictions-before.Evictions != count || after.EvictionCandidateVisits != count {
		t.Fatalf("final close must visit exactly its unpinned victims: before=%+v after=%+v", before, after)
	}
	for i, value := range values {
		if value.Name != "s2h" || len(value.Parts) != 1 || value.Parts[0].ID != "part" {
			t.Fatalf("eviction damaged borrowed asset-%d", i)
		}
	}
	loader.Clear()
	b.Close()
	if visits := loader.Stats().EvictionCandidateVisits; visits != count {
		t.Fatalf("reads, empty Clear or repeated close visited candidates: %d", visits)
	}
}

func TestS2hLoaderUnpinPreservesLastLoadRecency(t *testing.T) {
	for _, release := range []string{"scope close", "rejected value release"} {
		for _, recentHit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/hit=%t", release, recentHit), func(t *testing.T) {
				paths, charge := s2hAssetFiles(t, 3)
				loader := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: 2 * charge})
				scope := loader.NewScope()
				t.Cleanup(scope.Close)
				a := s2hLoadAsset(t, scope.Loader(), paths[0])
				b := s2hLoadAsset(t, loader, paths[1])
				if recentHit && s2hLoadAsset(t, scope.Loader(), paths[0]) != a {
					t.Fatal("pinned hit lost identity")
				}
				if release == "scope close" {
					scope.Close()
				} else {
					scope.Loader().releaseScopedValue(a)
				}
				before := loader.Stats()
				if before.Bytes != 2*charge || before.PinnedBytes != 0 || before.EvictionCandidateVisits != 0 {
					t.Fatalf("no-pressure unpin changed ownership or visited candidates: %+v", before)
				}
				s2hLoadAsset(t, loader, paths[2])
				after := loader.Stats()
				if after.Bytes != 2*charge || after.PinnedBytes != 0 || after.Entries != 2 || after.OverBudgetBytes != 0 ||
					after.Evictions-before.Evictions != 1 || after.EvictionCandidateVisits-before.EvictionCandidateVisits != 1 {
					t.Fatalf("pressure must select exactly one victim: before=%+v after=%+v", before, after)
				}
				retainedPath, retained := paths[1], b
				victimPath := paths[0]
				if recentHit {
					retainedPath, retained, victimPath = paths[0], a, paths[1]
				}
				if got := s2hLoadAsset(t, loader, retainedPath); got != retained {
					t.Fatal("unpin changed last-load eviction priority")
				}
				if err := os.Remove(victimPath); err != nil {
					t.Fatal(err)
				}
				if got, err := loader.LoadAsset(victimPath); err == nil || got != nil {
					t.Fatal("oldest last-load entry remained warm after pressure")
				}
				if a.Name != "s2h" || b.Parts[0].ID != "part" {
					t.Fatal("eviction changed borrowed decoded data")
				}
				if visits := loader.Stats().EvictionCandidateVisits; visits != after.EvictionCandidateVisits {
					t.Fatalf("reads and failed decode advanced candidate visits: %d", visits)
				}
				loader.Clear()
				if stats := loader.Stats(); stats.Bytes != 0 || stats.EvictionCandidateVisits != after.EvictionCandidateVisits {
					t.Fatalf("Clear must remove warm data without pressure visits: %+v", stats)
				}
			})
		}
	}
}

func TestS2hRuntimeDecodedEvictionCandidateMetric(t *testing.T) {
	paths, _ := s2hAssetFiles(t, 1)
	loader := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: 1})
	_, _, state, _ := s2aStartRuntime(t, StreamedLevelRuntimeConfig{Loader: loader})
	scope := loader.NewScope()
	t.Cleanup(scope.Close)
	asset := s2hLoadAsset(t, scope.Loader(), paths[0])
	scope.Close()
	stats := loader.Stats()
	if stats.EvictionCandidateVisits != 1 || asset.Name != "s2h" {
		t.Fatalf("fixture did not evict its external asset lease: %+v", stats)
	}
	refreshStreamedRuntimeMetricsCounts(state)
	if visits := state.Metrics.DecodedContentCacheEvictionCandidateVisits; visits != stats.EvictionCandidateVisits {
		t.Fatalf("runtime decoded visits=%d, loader visits=%d", visits, stats.EvictionCandidateVisits)
	}
	refreshStreamedRuntimeMetricsCounts(state)
	if state.Metrics.DecodedContentCacheEvictionCandidateVisits != 1 || loader.Stats().EvictionCandidateVisits != 1 {
		t.Fatal("metrics refresh advanced decoded candidate visits")
	}
}
