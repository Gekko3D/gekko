package gekko

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func p5lAsset(t *testing.T) (string, *content.AssetDef) {
	t.Helper()
	def := content.NewAssetDef("p5l")
	def.ID = "p5l"
	casts := false
	def.Runtime = &content.AssetRuntimeDef{CastsShadows: &casts, ShadowMaxDistance: 42}
	def.Materials = []content.AssetMaterialDef{{ID: "m", Name: "material", BaseColor: [4]uint8{10, 20, 30, 255}, IOR: 1.5, Roughness: .3, Tags: []string{"kind:stone", "solid"}}}
	transform := content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}, Pivot: content.Vec3{.25, .5, 0}}
	def.Parts = []content.AssetPartDef{{ID: "group", Name: "group", Source: content.AssetSourceDef{Kind: content.AssetSourceKindGroup}, Transform: transform}}
	for _, kind := range c3g1Kinds {
		def.Parts = append(def.Parts, content.AssetPartDef{ID: kind, Name: kind, ParentID: "group", ModelScale: 1.5, VoxelResolution: .125, Transform: transform, Source: content.AssetSourceDef{Kind: content.AssetSourceKindProceduralPrimitive, Primitive: kind, Params: c3g1Params(), MaterialID: "m"}})
	}
	def.Parts = append(def.Parts, content.AssetPartDef{ID: "shape", Name: "shape", ParentID: "group", ModelScale: 1, VoxelResolution: .25, Transform: transform, Source: content.AssetSourceDef{Kind: content.AssetSourceKindVoxelShape, VoxelShape: &content.AssetVoxelShapeDef{Palette: []content.AssetVoxelPaletteEntryDef{{Value: 3, MaterialID: "m"}}, Voxels: []content.VoxelObjectVoxelDef{{X: -1, Value: 3}, {X: 9, Y: 2, Value: 3}}}}})
	for _, part := range []struct {
		id       string
		kind     content.AssetSourceKind
		material string
	}{{"body", content.AssetSourceKindVoxModel, ""}, {"scene", content.AssetSourceKindVoxSceneNode, "m"}} {
		def.Parts = append(def.Parts, content.AssetPartDef{ID: part.id, Name: part.id, ParentID: "group", ModelScale: 1.5, VoxelResolution: .125, Transform: transform, Source: content.AssetSourceDef{Kind: part.kind, Path: "models.vox", ModelIndex: 1, NodeName: "arm", MaterialID: part.material}})
	}
	path := attachTestAnimationSet(t, def, []content.AssetAnimationClipDef{{ID: "move", Name: "move", Duration: 1, Loop: true, Tracks: []content.AssetAnimationTrackDef{{TargetID: "group", PositionKeys: []content.AssetVec3KeyDef{{Time: 0, Value: content.Vec3{}}, {Time: 1, Value: content.Vec3{10, 0, 0}}}}}}})
	writeNamedSceneVoxFixture(t, filepath.Join(filepath.Dir(path), "models.vox"))
	if err := content.SaveAsset(path, def); err != nil {
		t.Fatal(err)
	}
	return path, def
}

func p5lRuntime(t *testing.T, path string, counts []int) *s1gFixture {
	t.Helper()
	f := s1gRuntime(t, counts, 1, true, false)
	f.runtime.Config.EnableManagedPreparedAssets = true
	for coord, items := range f.runtime.PlacementsByChunk {
		for i := range items {
			items[i].AssetPath = path
		}
		f.runtime.PlacementsByChunk[coord] = items
	}
	for i := range f.runtime.Level.Placements {
		f.runtime.Level.Placements[i].AssetPath = path
	}
	return f
}

func p5lPrepare(t *testing.T, f *s1gFixture, coord ChunkCoord) streamedPreparedChunk {
	t.Helper()
	beforeModels, beforePalettes := len(f.assets.voxModels), len(f.assets.voxPalettes)
	s1fPrepared(t, f.streamedRenderHarness, coord, false)
	prepared := <-f.runtime.PreparedLoads
	if prepared.Err != nil {
		prepared.release()
		t.Fatal(prepared.Err)
	}
	if len(f.assets.voxModels) != beforeModels || len(f.assets.voxPalettes) != beforePalettes || len(f.hooks) != 0 {
		prepared.release()
		t.Fatal("worker published AssetServer assets or spawned placement entities")
	}
	return prepared
}

func p5lParity(t *testing.T, f *s1gFixture, id string, want *PreparedAuthoredAsset, oracle *AssetServer) {
	t.Helper()
	root, _ := s1gPlacement(t, f, id)
	group := placementItemEntityByIDForStreamedTest(f.cmd, id, "group")
	if group == 0 {
		t.Fatal("group hierarchy missing")
	}
	for _, part := range want.def.Parts {
		entity := placementItemEntityByIDForStreamedTest(f.cmd, id, part.ID)
		if part.ParentID != "" && s3cComponent[Parent](t, f.cmd, entity).Entity != group {
			t.Fatalf("part %s lost parent", part.ID)
		}
		if part.Source.Kind == content.AssetSourceKindGroup {
			continue
		}
		model := mustVoxelModelComponentForLevelTest(t, f.cmd, entity)
		if model.OverrideGeometry != (AssetId{}) {
			t.Fatalf("legacy %s acquired automatic managed override", part.ID)
		}
		geometry, ok := f.assets.GetVoxelGeometry(model.GeometryAsset())
		expected, _ := oracle.GetVoxelGeometry(want.parts[part.ID].model)
		if !ok || !reflect.DeepEqual(geometry.VoxModel, expected.VoxModel) || geometry.LocalMin != expected.LocalMin || geometry.LocalMax != expected.LocalMax || geometry.BrickSize != expected.BrickSize || geometry.SourcePath != expected.SourcePath || geometry.RuntimeOwned != expected.RuntimeOwned || !reflect.DeepEqual(c3h12aGeometry(geometry.XBrickMap), c3h12aGeometry(expected.XBrickMap)) {
			t.Fatalf("part %s changed complete geometry/header", part.ID)
		}
		gotPalette, _ := f.assets.GetVoxelPalette(model.VoxelPalette)
		expectedPalette, _ := oracle.GetVoxelPalette(want.parts[part.ID].palette)
		c3g3JSONEqual(t, gotPalette, expectedPalette)
		pivot := PivotModeCenter
		if part.Source.Kind == content.AssetSourceKindVoxelShape {
			pivot = PivotModeCustom
			if model.CustomPivot != mgl32.Vec3(part.Transform.Pivot) {
				t.Fatal("inline shape custom pivot changed")
			}
		}
		if !model.DisableShadows || model.ShadowMaxDistance != 42 || model.PivotMode != pivot || model.VoxelResolution != part.VoxelResolution {
			t.Fatalf("part %s changed pivot/lattice/shadows", part.ID)
		}
	}
	animation := s3cComponent[AuthoredAssetAnimationSetComponent](t, f.cmd, root)
	if _, ok := animation.Clips["move"]; !ok {
		t.Fatal("resolved external animation missing")
	}
}

func TestP5lLegacyWorkerCommitSourceFreeDependencies(t *testing.T) {
	for _, dependency := range []string{"models.vox", "asset.gkanim"} {
		t.Run(dependency, func(t *testing.T) {
			path, def := p5lAsset(t)
			oracle := newSpawnTestAssetServer()
			want, err := PrepareAuthoredAsset(oracle, def, path)
			if err != nil {
				t.Fatal(err)
			}
			f := p5lRuntime(t, path, []int{1})
			prepared := p5lPrepare(t, f, ChunkCoord{})
			defer prepared.release()
			// Removing a dependency, rather than only the cached .gkasset, proves
			// that ordinary commit no longer performs source IO or animation resolution.
			if err := os.Remove(filepath.Join(filepath.Dir(path), dependency)); err != nil {
				t.Fatal(err)
			}
			f.runtime.PreparedLoads <- prepared
			f.commitStage()
			if f.runtime.InitErr != nil {
				t.Fatalf("cold main commit reread worker dependency: %v", f.runtime.InitErr)
			}
			p5lParity(t, f, s1gID(0, 0), want, oracle)
			if f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatal("completed packet retained pending bytes")
			}
		})
	}
}

func TestP5lLegacyOrdinaryGeometryPaletteKeysAndMetadataParity(t *testing.T) {
	path, def := p5lAsset(t)
	oracle := newSpawnTestAssetServer()
	want, err := PrepareAuthoredAsset(oracle, def, path)
	if err != nil {
		t.Fatal(err)
	}
	f := p5lRuntime(t, path, []int{1})
	prepared := p5lPrepare(t, f, ChunkCoord{})
	defer prepared.release()
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	p5lParity(t, f, s1gID(0, 0), want, oracle)
	// Direct public preparation must hit the same ordinary namespaces after
	// worker publication, including the raw VOX palette domain and inline shape.
	direct, err := PrepareAuthoredAsset(f.assets, def, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range def.Parts {
		if part.Source.Kind == content.AssetSourceKindGroup {
			continue
		}
		model := mustVoxelModelComponentForLevelTest(t, f.cmd, placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), part.ID))
		if model.GeometryAsset() != direct.parts[part.ID].model || model.VoxelPalette != direct.parts[part.ID].palette {
			t.Fatalf("part %s diverged from public cache keys", part.ID)
		}
		if part.Source.Kind == content.AssetSourceKindProceduralPrimitive {
			geometry, _ := f.assets.GetVoxelGeometry(model.GeometryAsset())
			c3g1AssertModel(t, "fractional/"+part.Source.Primitive, geometry.VoxModel)
		}
	}
}

func TestP5lLegacyWarmMutableGlobalsAcrossChunksAndStop(t *testing.T) {
	path, def := p5lAsset(t)
	f := p5lRuntime(t, path, []int{2, 1})
	warm, err := PrepareAuthoredAsset(f.assets, def, path)
	if err != nil {
		t.Fatal(err)
	}
	body := warm.parts["body"]
	geometry, _ := f.assets.GetVoxelGeometry(body.model)
	geometry.XBrickMap.SetVoxel(31, 2, 3, 99)
	// Public palette storage is deliberately mutable, including nested storage.
	// Keep its old key so packet publication must preserve the warm record.
	palette, _ := f.assets.GetVoxelPalette(body.palette)
	edited := c3f4aPalette()
	c3f4aMutate(&edited)
	palette.Materials = edited.Materials
	palette.SurfaceMaterials = edited.SurfaceMaterials
	palette.Animations = edited.Animations
	palette.MaterialFrameOverrides = edited.MaterialFrameOverrides
	f.assets.voxPalettes[body.palette] = palette
	for _, coord := range []ChunkCoord{{}, {X: 1}} {
		prepared := p5lPrepare(t, f, coord)
		f.runtime.PreparedLoads <- prepared
	}
	for range 3 {
		f.commitStage()
	}
	for _, id := range []string{s1gID(0, 0), s1gID(0, 1), s1gID(1, 0)} {
		_, entity := s1gPlacement(t, f, id)
		model := mustVoxelModelComponentForLevelTest(t, f.cmd, entity)
		got, _ := f.assets.GetVoxelGeometry(model.GeometryAsset())
		gotPalette, _ := f.assets.GetVoxelPalette(model.VoxelPalette)
		if model.GeometryAsset() != body.model || model.VoxelPalette != body.palette || model.OverrideGeometry != (AssetId{}) || got.XBrickMap != geometry.XBrickMap {
			t.Fatal("warm globals replaced or managed despite legacy ordinary lifetime")
		}
		if occupied, value := got.XBrickMap.GetVoxel(31, 2, 3); !occupied || value != 99 {
			t.Fatal("worker replaced warm raw geometry edits")
		}
		if !reflect.DeepEqual(gotPalette, palette) {
			t.Fatal("worker replaced nested warm palette edits")
		}
	}
	if err := unloadStreamedChunk(f.cmd, f.runtime, ChunkCoord{}); err != nil {
		t.Fatal(err)
	}
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.assets.GetVoxelGeometry(body.model); !ok {
		t.Fatal("unload/Stop deleted ordinary global geometry")
	}
	if _, ok := f.assets.GetVoxelPalette(body.palette); !ok {
		t.Fatal("unload/Stop deleted ordinary global palette")
	}
}

func TestP5lLegacyInvalidDependencyFailsInWorkerWithoutPublication(t *testing.T) {
	for _, dependency := range []string{"models.vox", "asset.gkanim"} {
		t.Run(dependency, func(t *testing.T) {
			path, _ := p5lAsset(t)
			f := p5lRuntime(t, path, []int{1})
			if err := os.Remove(filepath.Join(filepath.Dir(path), dependency)); err != nil {
				t.Fatal(err)
			}
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			prepared := <-f.runtime.PreparedLoads
			defer prepared.release()
			if prepared.Err == nil {
				t.Fatal("worker accepted invalid legacy dependency")
			}
			if len(f.assets.voxModels) != 0 || len(f.assets.voxPalettes) != 0 || len(f.hooks) != 0 {
				t.Fatal("failed worker published assets or entities")
			}
			prepared.release()
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			if f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatal("failed preparation retained pending credit")
			}
		})
	}
}

func TestP5lLegacyPendingDrainAndPartialStopPreserveGlobals(t *testing.T) {
	for _, terminal := range []string{"release", "stale", "cancel", "partial Stop"} {
		t.Run(terminal, func(t *testing.T) {
			path, def := p5lAsset(t)
			f := p5lRuntime(t, path, []int{2})
			warm, err := PrepareAuthoredAsset(f.assets, def, path)
			if err != nil {
				t.Fatal(err)
			}
			prepared := p5lPrepare(t, f, ChunkCoord{})
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			if f.runtime.Metrics.PendingPreparedBytes <= 0 {
				t.Fatal("worker envelope was not charged")
			}
			switch terminal {
			case "release":
				prepared.release()
				prepared.release()
			case "cancel":
				cancelStreamedPreparation(f.runtime.chunkPrepareCancels[ChunkCoord{}])
				f.runtime.PreparedLoads <- prepared
				f.commitStage()
			case "stale":
				prepared.Generation--
				f.runtime.PreparedLoads <- prepared
				f.commitStage()
			case "partial Stop":
				f.runtime.PreparedLoads <- prepared
				f.commitStage()
				s1gPlacement(t, f, s1gID(0, 0))
				if f.hooks[s1gID(0, 1)] != 0 {
					t.Fatal("partial commit ignored unit cap")
				}
				if err := StopStreamedLevelRuntime(f.cmd); err != nil {
					t.Fatal(err)
				}
				prepared.release()
			}
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			if f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatal("terminal result retained pending envelope")
			}
			if _, ok := f.assets.GetVoxelGeometry(warm.parts["body"].model); !ok {
				t.Fatal("terminal drain deleted ordinary global")
			}
		})
	}
}

// Keep the fixture's animation target observable independently of renderer/GPU work.
func TestP5lLegacyPreparedAnimationRunsAfterCommit(t *testing.T) {
	path, _ := p5lAsset(t)
	f := p5lRuntime(t, path, []int{1})
	prepared := p5lPrepare(t, f, ChunkCoord{})
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	assetAnimationSystem(&Time{Dt: .5}, f.cmd)
	group := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), "group")
	local, ok := localTransformForAnimationBind(f.cmd, group)
	if !ok || !local.Position.ApproxEqualThreshold(mgl32.Vec3{5, 0, 0}, 1e-4) {
		t.Fatalf("prepared animation changed interpolation: %+v", local)
	}
}

func TestP5lLegacyCurrentSelectedPathWinsOverCapturedPacket(t *testing.T) {
	oldPath, _ := p5lAsset(t)
	selectedPath, selected := p5lAsset(t)
	selected.ID = "p5l-current"
	setPath := filepath.Join(filepath.Dir(selectedPath), "asset.gkanim")
	set, err := content.LoadAnimationSet(setPath)
	if err != nil {
		t.Fatal(err)
	}
	set.TargetAssetID = selected.ID
	if err := content.SaveAnimationSet(setPath, set); err != nil {
		t.Fatal(err)
	}
	selected.Parts[1].Source.Params["sx"] = 5
	if err := content.SaveAsset(selectedPath, selected); err != nil {
		t.Fatal(err)
	}
	f := p5lRuntime(t, oldPath, []int{1})
	prepared := p5lPrepare(t, f, ChunkCoord{})
	defer prepared.release()
	// The caller's selected placement path must qualify a captured packet;
	// an unrelated packet never overrides the path selected for this commit.
	prepared.PlacementItems[0].AssetPath = selectedPath
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	_, entity := s1gPlacement(t, f, s1gID(0, 0))
	ref := s3cComponent[AuthoredLevelItemRefComponent](t, f.cmd, entity)
	if ref.AssetID != selected.ID || ref.AssetPath != selectedPath {
		t.Fatalf("stale packet overrode selected asset: %+v", ref)
	}
	current, err := PrepareAuthoredAsset(f.assets, selected, selectedPath)
	if err != nil {
		t.Fatal(err)
	}
	cube := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), "cube")
	model := mustVoxelModelComponentForLevelTest(t, f.cmd, cube)
	if model.GeometryAsset() != current.parts["cube"].model {
		t.Fatal("stale packet geometry won over current selection")
	}
}

func TestP5lLegacyPressureRetryAndCancelledWorkerDrain(t *testing.T) {
	path, _ := p5lAsset(t)
	f := p5lRuntime(t, path, []int{1})
	job := buildStreamedChunkLoadJob(f.runtime, ChunkCoord{})
	owner := newStreamedPendingPreparedOwner(1)
	hold, ok := owner.reserve(1)
	if !ok {
		t.Fatal("pressure fixture")
	}
	defer hold.release()
	job.pendingOwner = owner
	pinned := f.runtime.Loader.Stats().PinnedBytes
	denied := prepareStreamedChunkLoad(job)
	defer denied.release()
	if denied.Err != nil || denied.retryCost <= 0 || owner.snapshot().Bytes != 1 {
		t.Fatalf("pressure failed to drain and retry: err=%v cost=%d bytes=%d", denied.Err, denied.retryCost, owner.snapshot().Bytes)
	}
	if f.runtime.Loader.Stats().PinnedBytes != pinned || len(f.assets.voxModels) != 0 || len(f.assets.voxPalettes) != 0 {
		t.Fatal("pressure retained worker inputs or published assets")
	}
	hold.release()
	cancel := make(chan struct{})
	close(cancel)
	job.prepareCancel = cancel
	cancelled := prepareStreamedChunkLoad(job)
	defer cancelled.release()
	cancelled.release()
	if cancelled.Err != nil || owner.snapshot().Bytes != 0 || f.runtime.Loader.Stats().PinnedBytes != pinned {
		t.Fatal("cancelled preparation retained envelope or decoded pins")
	}
}

func TestP5lLegacyCollapsedCompatibilityKeepsSeparatePreparation(t *testing.T) {
	def := p5eShapeDef()
	path := filepath.Join(t.TempDir(), "collapsed.gkasset")
	if err := content.SaveAsset(path, def); err != nil {
		t.Fatal(err)
	}
	f := p5lRuntime(t, path, []int{1})
	var result AuthoredAssetSpawnResult
	f.runtime.Config.PlacementHooks = []PostSpawnPlacementHook{func(_ *Commands, ctx PostSpawnPlacementContext) {
		f.hooks[ctx.Placement.PlacementID]++
		result = ctx.SpawnResult
	}}
	prepared := p5lPrepare(t, f, ChunkCoord{})
	defer prepared.release()
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	model := p5eCollapsedModel(t, f.app, result)
	if model.OverrideGeometry != (AssetId{}) || f.hooks[s1gID(0, 0)] != 1 {
		t.Fatal("collapsed compatibility gained automatic managed ownership or changed callbacks")
	}
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.assets.GetVoxelGeometry(model.GeometryAsset()); !ok {
		t.Fatal("collapsed global geometry lost ordinary lifetime")
	}
}

func TestP5lLegacyRelativeSourceSpellingUsesPublicOrdinaryKeys(t *testing.T) {
	path, def := p5lAsset(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, path)
	if err != nil {
		t.Fatal(err)
	}
	f := p5lRuntime(t, relative, []int{1})
	prepared := p5lPrepare(t, f, ChunkCoord{})
	defer prepared.release()
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	resolved := content.ResolveDocumentPath(relative, f.runtime.LevelPath)
	public, err := PrepareAuthoredAsset(f.assets, def, resolved)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range def.Parts {
		if part.Source.Kind == content.AssetSourceKindGroup {
			continue
		}
		model := mustVoxelModelComponentForLevelTest(t, f.cmd, placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), part.ID))
		if model.GeometryAsset() != public.parts[part.ID].model || model.VoxelPalette != public.parts[part.ID].palette {
			t.Fatalf("relative source spelling changed ordinary keys for %s", part.ID)
		}
	}
}

func TestP5lLegacyMixedSourceSpellingsPreserveEachOrdinaryDomain(t *testing.T) {
	path, def := p5lAsset(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, path)
	if err != nil {
		t.Fatal(err)
	}
	// Both spellings select the same physical .gkasset, but public VOX source
	// keys preserve the relative/absolute document spelling returned by resolution.
	f := p5lRuntime(t, path, []int{2})
	f.runtime.PlacementsByChunk[ChunkCoord{}][1].AssetPath = relative
	prepared := p5lPrepare(t, f, ChunkCoord{})
	defer prepared.release()
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	f.commitStage()
	first := mustVoxelModelComponentForLevelTest(t, f.cmd, placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), "body"))
	second := mustVoxelModelComponentForLevelTest(t, f.cmd, placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 1), "body"))
	for i, spelling := range []string{path, relative} {
		public, err := PrepareAuthoredAsset(f.assets, def, spelling)
		if err != nil {
			t.Fatal(err)
		}
		model := []VoxelModelComponent{first, second}[i]
		if model.GeometryAsset() != public.parts["body"].model || model.VoxelPalette != public.parts["body"].palette {
			t.Fatalf("same physical asset lost ordinary source domain %q", spelling)
		}
		geometry, _ := f.assets.GetVoxelGeometry(model.GeometryAsset())
		if geometry.SourcePath != content.ResolveDocumentPath("models.vox", spelling) {
			t.Fatalf("source spelling changed: %q", geometry.SourcePath)
		}
	}
	if first.GeometryAsset() == second.GeometryAsset() || first.VoxelPalette == second.VoxelPalette {
		t.Fatal("absolute packet dedup merged distinct ordinary VOX source keys")
	}
}

func TestP5lLegacyWarmShapeDifferentLatticesKeepCanonicalProvenance(t *testing.T) {
	path, def := p5lAsset(t)
	var shape content.AssetPartDef
	for _, part := range def.Parts {
		if part.ID == "shape" {
			shape = part
			break
		}
	}
	second := shape
	second.ID, second.Name, second.VoxelResolution = "shape-fine", "shape fine", .125
	def.Parts = append(def.Parts, second)
	if err := content.SaveAsset(path, def); err != nil {
		t.Fatal(err)
	}
	f := p5lRuntime(t, path, []int{1})
	global, err := authoredVoxelShapeGeometry(f.assets, shape)
	if err != nil {
		t.Fatal(err)
	}
	geometry, _ := f.assets.GetVoxelGeometry(global)
	geometry.XBrickMap.SetVoxel(31, 2, 3, 99)
	_, firstLattice, firstBase := e2b1Canonical(t, shape)
	_, secondLattice, secondBase := e2b1Canonical(t, second)
	if firstBase == secondBase || f.assets.authoredVoxelBaseIdentity(global, secondLattice) != "" {
		t.Fatal("fixture did not isolate missing second-lattice provenance")
	}
	prepared := p5lPrepare(t, f, ChunkCoord{})
	defer prepared.release()
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	for _, part := range []content.AssetPartDef{shape, second} {
		model := mustVoxelModelComponentForLevelTest(t, f.cmd, placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), part.ID))
		if model.GeometryAsset() != global || model.OverrideGeometry != (AssetId{}) || model.VoxelResolution != part.VoxelResolution {
			t.Fatal("same shape acquired different global geometry or wrong lattice")
		}
	}
	current, _ := f.assets.GetVoxelGeometry(global)
	if current.XBrickMap != geometry.XBrickMap {
		t.Fatal("worker replaced edited warm shared shape")
	}
	if occupied, value := current.XBrickMap.GetVoxel(31, 2, 3); !occupied || value != 99 {
		t.Fatal("warm shape edit lost")
	}
	if f.assets.authoredVoxelBaseIdentity(global, firstLattice) != firstBase || f.assets.authoredVoxelBaseIdentity(global, secondLattice) != secondBase {
		t.Fatal("original base came from edited warm geometry instead of authored canonical source")
	}
}

func TestP5lLegacyColdDuplicatePlacementsShareOrdinaryIDs(t *testing.T) {
	path, def := p5lAsset(t)
	f := p5lRuntime(t, path, []int{2})
	prepared := p5lPrepare(t, f, ChunkCoord{})
	defer prepared.release()
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	f.commitStage()
	for _, part := range def.Parts {
		if part.Source.Kind == content.AssetSourceKindGroup {
			continue
		}
		first := mustVoxelModelComponentForLevelTest(t, f.cmd, placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), part.ID))
		second := mustVoxelModelComponentForLevelTest(t, f.cmd, placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 1), part.ID))
		if first.GeometryAsset() != second.GeometryAsset() || first.VoxelPalette != second.VoxelPalette || first.OverrideGeometry != (AssetId{}) || second.OverrideGeometry != (AssetId{}) {
			t.Fatalf("cold duplicate %s lost global sharing", part.ID)
		}
	}
}

func TestP5lLegacyDefaultCPUOnlyWorkerCommitSourceFree(t *testing.T) {
	path, _ := p5lAsset(t)
	f := s1gRuntime(t, []int{1}, 0, false, false)
	for i := range f.runtime.PlacementsByChunk[ChunkCoord{}] {
		f.runtime.PlacementsByChunk[ChunkCoord{}][i].AssetPath = path
	}
	prepared := p5lPrepare(t, f, ChunkCoord{})
	defer prepared.release()
	if err := os.Remove(filepath.Join(filepath.Dir(path), "models.vox")); err != nil {
		t.Fatal(err)
	}
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	_, entity := s1gPlacement(t, f, s1gID(0, 0))
	if mustVoxelModelComponentForLevelTest(t, f.cmd, entity).OverrideGeometry != (AssetId{}) {
		t.Fatal("CPU-only legacy placement gained automatic ownership")
	}
}

func TestP5lLegacyWorkerOwnsCapturedDefinitionAndNestedMetadata(t *testing.T) {
	path, def := p5lAsset(t)
	oracle := newSpawnTestAssetServer()
	want, err := PrepareAuthoredAsset(oracle, def, path)
	if err != nil {
		t.Fatal(err)
	}
	f := p5lRuntime(t, path, []int{1})
	prepared := p5lPrepare(t, f, ChunkCoord{})
	defer prepared.release()
	cached, err := f.runtime.Loader.LoadAsset(path)
	if err != nil {
		t.Fatal(err)
	}
	cached.Materials[0].Tags[0] = "kind:changed"
	cached.Parts[1].Source.Params["sx"] = 17
	cached.Parts[1].Transform.Position = content.Vec3{91, 92, 93}
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	p5lParity(t, f, s1gID(0, 0), want, oracle)
	entity := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), "cube")
	local := s3cComponent[LocalTransformComponent](t, f.cmd, entity)
	if local.Position != mgl32.Vec3(want.def.Parts[1].Transform.Position) {
		t.Fatal("worker metadata aliased cached definition")
	}
}

func TestP5lLegacyPendingEnvelopeGrowsWithOwnedShapeStorage(t *testing.T) {
	var charges []int64
	for _, count := range []int{1, 128} {
		path, def := p5lAsset(t)
		// Placement metadata stays fixed; inline geometry and its JSON shape key
		// grow. Separate bricks require independent dense source/publication cells.
		for i := range def.Parts {
			if def.Parts[i].ID != "shape" {
				continue
			}
			voxels := make([]content.VoxelObjectVoxelDef, count)
			for j := range voxels {
				voxels[j] = content.VoxelObjectVoxelDef{X: j * 32, Value: 3}
			}
			def.Parts[i].Source.VoxelShape.Voxels = voxels
		}
		if err := content.SaveAsset(path, def); err != nil {
			t.Fatal(err)
		}
		f := p5lRuntime(t, path, []int{1})
		prepared := p5lPrepare(t, f, ChunkCoord{})
		refreshStreamedRuntimeMetricsCounts(f.runtime)
		charges = append(charges, f.runtime.Metrics.PendingPreparedBytes)
		alias := prepared
		prepared.release()
		alias.release()
		refreshStreamedRuntimeMetricsCounts(f.runtime)
		if f.runtime.Metrics.PendingPreparedBytes != 0 {
			t.Fatal("aliased packet release retained pending bytes")
		}
	}
	// Each added brick owns at least 512 primary byte cells in each of the
	// independent source and single-use publication copies. This deliberately
	// excludes key/metadata growth, allocator overhead and map capacity.
	const minimumDenseGrowth = int64(2 * 127 * 512)
	if growth := charges[1] - charges[0]; growth < minimumDenseGrowth {
		t.Fatalf("legacy pending envelope omitted dense ownership: small=%d large=%d growth=%d want at least %d", charges[0], charges[1], growth, minimumDenseGrowth)
	}
}
