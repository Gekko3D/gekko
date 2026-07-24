package hl1

import (
	"testing"

	"github.com/gekko3d/gekko/content"
	importcommon "github.com/gekko3d/gekko/importers/common"
)

func TestBuildDebugWorldVoxelBackingClassifiesInGlobalVoxelSpace(t *testing.T) {
	bsp := &BSP{
		SHA256: "source",
		Planes: []Plane{{Normal: vec3(1, 0, 0), Dist: 100}},
		Nodes:  []Node{{PlaneID: 0, Children: [2]int16{-1, -2}}},
		Leafs:  []Leaf{{Contents: ContentsSolid}, {Contents: ContentsEmpty}},
		Models: []Model{{
			Min: vec3(-200, -200, -200), Max: vec3(200, 200, 200), HeadNodes: [4]int32{0},
		}},
	}
	manifest := &content.ImportedWorldDef{ChunkSize: 16, VoxelResolution: 1}
	def, err := buildDebugWorldVoxelBacking(bsp, manifest, 1)
	if err != nil {
		t.Fatalf("buildDebugWorldVoxelBacking failed: %v", err)
	}
	if def == nil || def.PlaneTree.Planes[0].Distance != 2.54 {
		t.Fatalf("unexpected converted backing plane: %+v", def)
	}
}

func TestBuildDebugWorldVoxelBackingAddsGroundSupportWithoutExplicitFill(t *testing.T) {
	bsp := &BSP{
		SHA256: "source",
		Planes: []Plane{{Normal: vec3(1, 0, 0)}},
		Nodes:  []Node{{PlaneID: 0, Children: [2]int16{-1, -2}}},
		Leafs:  []Leaf{{Contents: ContentsSolid}, {Contents: ContentsEmpty}},
		Models: []Model{{
			Min: vec3(-200, -200, -200), Max: vec3(200, 200, 200), HeadNodes: [4]int32{0},
		}},
	}
	ground := Face{
		TextureName: "concrete",
		Normal:      vec3(0, 0, 1),
		Vertices:    []importcommon.Vec3{vec3(0, 0, 0), vec3(100, 0, 0), vec3(100, 100, 0), vec3(0, 100, 0)},
	}
	ceiling := ground
	ceiling.Normal = vec3(0, 0, -1)
	def, err := buildDebugWorldVoxelBacking(bsp, &content.ImportedWorldDef{ChunkSize: 16, VoxelResolution: 1}, 1, []Face{ground, ceiling})
	if err != nil {
		t.Fatal(err)
	}
	if len(def.SurfaceSupports) != 2 {
		t.Fatalf("ground support triangles = %d, want 2", len(def.SurfaceSupports))
	}
	for _, support := range def.SurfaceSupports {
		if support.Normal != ([3]float32{0, 1, 0}) || support.Depth != DefaultDestructionSurfaceSupportDepth {
			t.Fatalf("unexpected ground support: %+v", support)
		}
	}
}

func TestEnsureDebugWorldBackingChunksCatalogsImplicitVolume(t *testing.T) {
	emission := importcommon.ImportedWorldEmission{
		Manifest: &content.ImportedWorldDef{
			WorldID: "world", ChunkSize: 16, VoxelResolution: 1,
		},
		Chunks: make(map[[3]int]*content.ImportedWorldChunkDef),
	}
	backing := &content.VoxelBackingDef{BoundsMin: [3]int{0, 0, 0}, BoundsMax: [3]int{32, 16, 16}}
	ensureDebugWorldBackingChunks(&emission, backing, []string{"source:hl1"})
	if len(emission.Manifest.Entries) != 2 || len(emission.Chunks) != 2 {
		t.Fatalf("expected two backing-only chunks, entries=%d chunks=%d", len(emission.Manifest.Entries), len(emission.Chunks))
	}
	for _, entry := range emission.Manifest.Entries {
		if entry.NonEmptyVoxelCount != 0 {
			t.Fatalf("backing-only entry became eagerly occupied: %+v", entry)
		}
	}
}

func TestDebugWorldBackingSolidValueAvoidsAnimatedMaterialFallback(t *testing.T) {
	manifest := &content.ImportedWorldDef{Materials: []content.ImportedWorldMaterialDef{
		{PaletteIndex: 1, Kind: "baked_texture", AnimationID: "animated"},
		{PaletteIndex: 134, Kind: "baked_texture"},
	}}
	if got := debugWorldBackingSolidValue(manifest); got != 134 {
		t.Fatalf("expected stable opaque backing material 134, got %d", got)
	}
}
