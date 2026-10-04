package gekko

import (
	"math"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func c3f1Source(t *testing.T) (*volume.XBrickMap, content.VoxelObjectLatticeDef, string, string) {
	t.Helper()
	source := volume.NewXBrickMap()
	source.SetVoxel(-9, 2, -3, 7)
	source.SetVoxel(8, -1, 1, 4)
	lattice := authoredVoxelShapeLattice(0.25)
	baseID, _, err := content.VoxelObjectBaseIdentity(VoxelObjectSnapshotFromXBrickMap(source), lattice, nil)
	if err != nil {
		t.Fatal(err)
	}
	return source, lattice, baseID, strings.Repeat("a", 64)
}

func TestC3f1ColdAdoptionTransfersSingleUseOwnedGeometryAndProvenance(t *testing.T) {
	source, lattice, baseID, contentID := c3f1Source(t)
	assets := &AssetServer{}
	registration := prepareStreamedGeometryRegistration(source)
	owned := registration.geometry
	alias := registration
	id, ok := assets.adoptCompiledAssetGeometry(contentID, lattice, baseID, source, registration)
	if !ok || id == (AssetId{}) {
		t.Fatal("compiled cold adoption failed")
	}
	geometry, exists := assets.getVoxelGeometry(id)
	key := "compiled-asset-shape:" + contentID
	if !exists || geometry.XBrickMap != owned || geometry.SourcePath != key {
		t.Fatal("cold adoption copied or lost owned prepared geometry")
	}
	if foundID, warm := assets.SharedVoxelGeometryByCacheKey(key); !warm || foundID != id || assets.authoredVoxelBaseIdentity(id, lattice) != baseID {
		t.Fatal("cold publication lost atomic key/provenance")
	}
	if registration.charge() != 0 {
		t.Fatal("adoption retained pending registration charge")
	}
	if _, available := registration.countFor(source); available {
		t.Fatal("adoption left single-use geometry available")
	}
	alias.release()
	registration.release()
	if got, exists := assets.getVoxelGeometry(id); !exists || got.XBrickMap != owned {
		t.Fatal("aliased late release deleted adopted global geometry")
	}
	source.SetVoxel(-9, 2, -3, 99)
	if found, value := owned.GetVoxel(-9, 2, -3); !found || value != 7 {
		t.Fatal("mutable caller source changed adopted registration's independent storage")
	}
	if other, ok := assets.adoptCompiledAssetGeometry(strings.Repeat("b", 64), lattice, baseID, source, registration); ok || other != (AssetId{}) {
		t.Fatal("consumed handle published another global asset")
	}
}

func TestC3f1InvalidInputsAndColdMismatchDoNotStealRegistration(t *testing.T) {
	source, lattice, baseID, contentID := c3f1Source(t)
	for _, kind := range []string{"nil-server", "nil-source", "nil-registration", "empty-content", "empty-base", "zero-resolution", "nan-resolution", "unsupported-rasterization"} {
		t.Run(kind, func(t *testing.T) {
			assets := &AssetServer{}
			server := assets
			input := source
			registration := prepareStreamedGeometryRegistration(source)
			defer registration.release()
			passed := registration
			l := lattice
			base, id := baseID, contentID
			switch kind {
			case "nil-server":
				server = nil
			case "nil-source":
				input = nil
			case "nil-registration":
				passed = nil
			case "empty-content":
				id = ""
			case "empty-base":
				base = ""
			case "zero-resolution":
				l.VoxelResolution = 0
			case "nan-resolution":
				l.VoxelResolution = float32(math.NaN())
			case "unsupported-rasterization":
				l.RasterizationVersion = "different-rasterization"
			}
			if got, ok := server.adoptCompiledAssetGeometry(id, l, base, input, passed); ok || got != (AssetId{}) {
				t.Fatal("invalid adoption inputs published geometry")
			}
			if len(assets.voxModels) != 0 {
				t.Fatal("invalid adoption left global geometry")
			}
			if _, available := registration.countFor(source); !available || registration.charge() == 0 {
				t.Fatal("invalid-input validation consumed still-valid registration")
			}
		})
	}
	assets := &AssetServer{}
	registration := prepareStreamedGeometryRegistration(source)
	defer registration.release()
	differentSource := source.Copy()
	if id, ok := assets.adoptCompiledAssetGeometry(contentID, lattice, baseID, differentSource, registration); ok || id != (AssetId{}) {
		t.Fatal("cold adoption accepted mismatched source identity")
	}
	if _, available := registration.countFor(source); !available || registration.charge() == 0 || len(assets.voxModels) != 0 {
		t.Fatal("cold mismatch consumed valid handle or published geometry")
	}
	if id, ok := assets.adoptCompiledAssetGeometry(contentID, lattice, baseID, source, registration); !ok || id == (AssetId{}) {
		t.Fatal("cold mismatch stole later valid adoption")
	}
}

func TestC3f1WarmReuseDrainsUnusedCopyAndPreservesOriginalBase(t *testing.T) {
	source, lattice, baseID, contentID := c3f1Source(t)
	assets := &AssetServer{}
	first := prepareStreamedGeometryRegistration(source)
	id, ok := assets.adoptCompiledAssetGeometry(contentID, lattice, baseID, source, first)
	if !ok {
		t.Fatal("cold fixture adoption failed")
	}
	mutable, exists := assets.GetVoxelGeometry(id)
	if !exists {
		t.Fatal("warm fixture geometry missing")
	}
	mutable.XBrickMap.SetVoxel(-9, 2, -3, 77)
	registration := prepareStreamedGeometryRegistration(source)
	alias := registration
	warmID, ok := assets.adoptCompiledAssetGeometry(contentID, lattice, baseID, source, registration)
	if !ok || warmID != id || registration.charge() != 0 {
		t.Fatal("warm reuse did not drain unused registration")
	}
	alias.release()
	registration.release()
	geometry, exists := assets.getVoxelGeometry(id)
	if !exists || geometry.XBrickMap != mutable.XBrickMap {
		t.Fatal("warm reuse replaced or deleted globally shared geometry")
	}
	if found, value := geometry.XBrickMap.GetVoxel(-9, 2, -3); !found || value != 77 {
		t.Fatal("warm reuse overwrote existing mutable shared payload")
	}
	if assets.authoredVoxelBaseIdentity(id, lattice) != baseID {
		t.Fatal("warm geometry mutation redefined canonical provenance")
	}
}

func TestC3f1WarmAbsentAndConflictingProvenance(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "conflict"}[conflict], func(t *testing.T) {
			source, lattice, baseID, contentID := c3f1Source(t)
			assets := &AssetServer{}
			key := "compiled-asset-shape:" + contentID
			id := assets.RegisterSharedVoxelGeometryWithCacheKey(key, source, key)
			existing := strings.Repeat("c", 64)
			if conflict {
				assets.recordVerifiedAuthoredVoxelBase(id, lattice, existing)
			}
			registration := prepareStreamedGeometryRegistration(source)
			got, ok := assets.adoptCompiledAssetGeometry(contentID, lattice, baseID, source, registration)
			if conflict {
				if ok || got != (AssetId{}) || assets.authoredVoxelBaseIdentity(id, lattice) != existing {
					t.Fatal("warm conflicting provenance replaced original or accepted mismatch")
				}
			} else if !ok || got != id || assets.authoredVoxelBaseIdentity(id, lattice) != baseID {
				t.Fatal("verified warm metadata failed to fill absent original provenance")
			}
			if registration.charge() != 0 {
				t.Fatal("warm result retained unused registration")
			}
			registration.release()
			if _, exists := assets.getVoxelGeometry(id); !exists {
				t.Fatal("warm rejection/release deleted original global asset")
			}
		})
	}
}

func TestC3f1ConcurrentMatchingAdoptionPublishesOneGlobalOwner(t *testing.T) {
	source, lattice, baseID, contentID := c3f1Source(t)
	assets := &AssetServer{}
	const workers = 6
	type result struct {
		id    AssetId
		ok    bool
		base  string
		keyID AssetId
		warm  bool
	}
	registrations := make([]*streamedGeometryRegistration, workers)
	sources := make([]*volume.XBrickMap, workers)
	for i := range registrations {
		sources[i] = source.Copy()
		registrations[i] = prepareStreamedGeometryRegistration(sources[i])
	}
	results := make(chan result, workers)
	ready := make(chan struct{}, workers)
	start := make(chan struct{})
	for i := range registrations {
		i := i
		go func() {
			ready <- struct{}{}
			<-start
			id, ok := assets.adoptCompiledAssetGeometry(contentID, lattice, baseID, sources[i], registrations[i])
			keyID, warm := assets.SharedVoxelGeometryByCacheKey("compiled-asset-shape:" + contentID)
			results <- result{id, ok, assets.authoredVoxelBaseIdentity(id, lattice), keyID, warm}
		}()
	}
	for i := 0; i < workers; i++ {
		<-ready
	}
	close(start)
	completed := make([]result, workers)
	for i := range completed {
		completed[i] = <-results
	}
	id := completed[0].id
	for _, got := range completed {
		if !got.ok || got.id != id || got.base != baseID || !got.warm || got.keyID != id {
			t.Fatal("concurrent adoption did not publish one atomic global key/base")
		}
	}
	if len(assets.voxModels) != 1 {
		t.Fatal("concurrent matching adoption registered duplicate geometry")
	}
	for i, registration := range registrations {
		if registration.charge() != 0 {
			t.Fatal("concurrent adoption retained unused handle")
		}
		if _, available := registration.countFor(sources[i]); available {
			t.Fatal("concurrent adoption left handle available")
		}
		registration.release()
	}
	if _, exists := assets.getVoxelGeometry(id); !exists {
		t.Fatal("late concurrent release deleted global geometry")
	}
}

func TestC3f1AdoptionSharesDirectCompiledPreparationNamespace(t *testing.T) {
	_, path, _ := c3d3Fixture(t)
	assets := c3d3Server()
	prepared, err := LoadAndPrepareAuthoredAsset(path, assets, nil)
	if err != nil {
		t.Fatal(err)
	}
	existing, ok := PreparedAuthoredAssetPartGeometry(prepared, "shape")
	if !ok {
		t.Fatal("direct compiled geometry missing")
	}
	header, _, err := content.LoadCompiledAssetHeader(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	ref := header.Shapes[0]
	geometry, exists := assets.getVoxelGeometry(existing)
	if !exists {
		t.Fatal("direct compiled registered geometry missing")
	}
	source := geometry.XBrickMap.Copy()
	registration := prepareStreamedGeometryRegistration(source)
	id, ok := assets.adoptCompiledAssetGeometry(ref.ContentID, authoredVoxelShapeLattice(header.Asset.Parts[0].VoxelResolution), ref.BaseIdentity, source, registration)
	if !ok || id != existing || registration.charge() != 0 {
		t.Fatal("adoption namespace differs from direct compiled preparation")
	}
}
