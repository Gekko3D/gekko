package content

import (
	"path/filepath"
	"testing"
)

func TestI08IndependentNavigationRequiresExactOwnerIdentityAndGrid(t *testing.T) {
	for _, c := range []struct {
		name, identity, code string
		resolution           float32
	}{
		{"exact-owner", "poi-i08", "", .1},
		{"unqualified-owner", "", "navigation_source_world_id_mismatch", .1},
		{"different-small-grid", "poi-i08", "navigation_voxel_resolution_mismatch", .10005},
	} {
		t.Run(c.name, func(t *testing.T) {
			level, path, _, poi := i08ValidationFixture(t)
			nav := &NavGraphManifestDef{NavID: "nav-owner-review", BuilderVersion: "i08", SourceWorldID: c.identity, ChunkSize: poi.ChunkSize, VoxelResolution: c.resolution}
			if err := SaveNavGraphManifest(filepath.Join(filepath.Dir(path), "review.gknav"), nav); err != nil {
				t.Fatal(err)
			}
			level.Navigation = &LevelNavigationDef{ManifestPath: "review.gknav"}
			result := i08Validate(t, level, path)
			if c.code == "" {
				if result.HasErrors() {
					t.Fatalf("exact independent owner rejected: %+v", result.Issues)
				}
			} else {
				assertHasLevelValidationCode(t, result, c.code)
			}
		})
	}
}

func TestI08LegacyNavigationRetainsOptionalIdentityAndGridTolerance(t *testing.T) {
	level, path, _, poi := i08ValidationFixture(t)
	level.Terrain = nil
	level.ChunkSize, level.VoxelResolution = poi.ChunkSize, poi.VoxelResolution
	nav := &NavGraphManifestDef{NavID: "legacy-nav-review", BuilderVersion: "i08", ChunkSize: poi.ChunkSize, VoxelResolution: .10005}
	if err := SaveNavGraphManifest(filepath.Join(filepath.Dir(path), "review.gknav"), nav); err != nil {
		t.Fatal(err)
	}
	level.Navigation = &LevelNavigationDef{ManifestPath: "review.gknav"}
	if result := i08Validate(t, level, path); result.HasErrors() {
		t.Fatalf("legacy navigation compatibility changed: %+v", result.Issues)
	}
}
