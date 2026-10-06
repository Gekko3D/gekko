package gekko

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func p5nCompile(t *testing.T, def *content.AssetDef, lod bool) (string, string) {
	t.Helper()
	input := p5mSave(t, def)
	out := filepath.Join(t.TempDir(), "asset.gkassetc")
	if _, err := CompileAuthoredCollapsedAssetWithOptions(input, out, nil, CompiledAssetCompileOptions{EnableLOD2: lod}); err != nil {
		t.Fatal(err)
	}
	return input, out
}
func p5nDeleteClosure(t *testing.T, f *s1gFixture, path string) {
	t.Helper()
	h, _, err := content.LoadCompiledAssetHeader(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, s := range h.Shapes {
		p := filepath.Join(filepath.Dir(path), s.Path)
		if !seen[p] {
			seen[p] = true
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, l := range h.LODs {
		p := filepath.Join(filepath.Dir(path), l.Path)
		if !seen[p] {
			seen[p] = true
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	f.runtime.Loader.Clear()
}
func p5nDirect(t *testing.T, app *App, assets *AssetServer, path string, mode VoxelPartCollapseMode) AuthoredAssetSpawnResult {
	t.Helper()
	p, err := LoadAndPrepareAuthoredAsset(path, assets, NewRuntimeContentLoader())
	if err != nil {
		t.Fatal(err)
	}
	r, err := spawnAuthoredAssetWithOptions(app.Commands(), assets, p.def, p, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}, AuthoredAssetSpawnOptions{DocumentPath: path, CollapseVoxelParts: mode})
	app.FlushCommands()
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestP5nExplicitCompilerPreservesOrdinaryDefaultsAndRejection(t *testing.T) {
	for _, lod := range []bool{false, true} {
		t.Run(map[bool]string{false: "schema1", true: "schema2"}[lod], func(t *testing.T) {
			def := p5eShapeDef()
			def.Runtime.CollapseVoxelParts = false
			input := p5mSave(t, def)
			old := filepath.Join(t.TempDir(), "old.gkassetc")
			out := filepath.Join(t.TempDir(), "new.gkassetc")
			options := CompiledAssetCompileOptions{EnableLOD2: lod}
			if _, err := CompileAuthoredAssetWithOptions(input, old, nil, options); err != nil {
				t.Fatal(err)
			}
			if _, err := CompileAuthoredCollapsedAssetWithOptions(input, out, nil, options); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(c3cRead(t, old), c3cRead(t, out)) {
				t.Fatal("explicit wrapper changed ordinary compiler bytes")
			}
			def.Runtime.CollapseVoxelParts = true
			if err := content.SaveAsset(input, def); err != nil {
				t.Fatal(err)
			}
			if _, err := CompileAuthoredAssetWithOptions(input, old, nil, options); err == nil {
				t.Fatal("default compiler now silently permits collapse")
			}
			if _, err := CompileAuthoredModelAssetWithOptions(input, filepath.Join(t.TempDir(), "model.gkmodelassetc"), nil, options); err == nil {
				t.Fatal("model compiler now permits collapse without original sample history")
			}
			before := c3cRead(t, input)
			r, err := CompileAuthoredCollapsedAssetWithOptions(input, out, nil, options)
			if err != nil {
				t.Fatal(err)
			}
			h, _, err := content.LoadCompiledAssetHeader(out, nil)
			if err != nil || h.SchemaVersion != 3 || h.CompilerVersion != "gekko-compiled-asset-v3" || !h.Asset.Runtime.CollapseVoxelParts {
				t.Fatal("missing explicit collapse shipping schema", err)
			}
			repeat, err := CompileAuthoredCollapsedAssetWithOptions(input, out, nil, options)
			if err != nil || repeat.HeaderWrote || repeat.HeaderInfo != r.HeaderInfo || !bytes.Equal(before, c3cRead(t, input)) {
				t.Fatal("collapse compile no-op or authoring immutability changed", err)
			}
		})
	}
}
func TestP5nColdCompiledCollapseMatchesAuthoredForceAndTransfersWorker(t *testing.T) {
	for _, kind := range []string{"hierarchy", "partial-subtract", "subtract-add", "empty"} {
		t.Run(kind, func(t *testing.T) {
			def := p5eShapeDef()
			if kind == "hierarchy" {
				def.Parts[0].ParentID = "right"
				def.Parts[1].ParentID = "group"
				def.Parts[0].ModelScale = 2
				def.Parts[0].Source.VoxelShape.Voxels = append(def.Parts[0].Source.VoxelShape.Voxels, content.VoxelObjectVoxelDef{X: 0, Value: 1}, content.VoxelObjectVoxelDef{X: -2, Y: -1, Value: 1})
				def.Parts[0].Transform.Pivot = content.Vec3{.25, .5, 0}
				def.Parts[1].Transform.Position = content.Vec3{2, 1, 0}
				q := mgl32.QuatRotate(.6, mgl32.Vec3{0, 0, 1})
				def.Parts = append(def.Parts, content.AssetPartDef{ID: "group", Name: "group", Source: content.AssetSourceDef{Kind: content.AssetSourceKindGroup}, Transform: content.AssetTransformDef{Rotation: content.Quat{q.V[0], q.V[1], q.V[2], q.W}, Scale: content.Vec3{-1, 2, 1}, Pivot: content.Vec3{9, 8, 7}}})
			} else {
				def.Parts[1].Source.Operation = content.AssetShapeOperationSubtract
				def.Parts[1].Source.VoxelShape.Voxels[0].X = 0
				if kind == "partial-subtract" {
					def.Parts[0].Source.VoxelShape.Voxels = append(def.Parts[0].Source.VoxelShape.Voxels, content.VoxelObjectVoxelDef{X: 1, Value: 1})
				}
				if kind == "subtract-add" {
					def.Parts[0], def.Parts[1] = def.Parts[1], def.Parts[0]
				}
			}
			input, path := p5nCompile(t, def, false)
			oracle, oa := newSpawnTestAssetServer(), NewApp()
			want := p5mSpawn(t, oa, oracle, def, input, VoxelPartCollapseForce)
			wvm := mustVoxelModelForSpawnTest(t, oa.Commands(), onlyVoxelEntityForSpawnTest(t, oa.Commands(), want.RootEntity))
			directAssets, da := newSpawnTestAssetServer(), NewApp()
			direct := p5nDirect(t, da, directAssets, path, VoxelPartCollapseDefault)
			dvm := mustVoxelModelForSpawnTest(t, da.Commands(), onlyVoxelEntityForSpawnTest(t, da.Commands(), direct.RootEntity))
			p5mParity(t, directAssets, oracle, dvm, wvm)
			if p5mWorkerAdoptions(t, directAssets) != 0 {
				t.Fatal("direct preparation/spawn counted worker handoff")
			}
			f, results := p5mRuntime(t, path, []int{2}, kind == "empty")
			p := p5lPrepare(t, f, ChunkCoord{})
			defer p.release()
			if p5mWorkerAdoptions(t, f.assets) != 0 {
				t.Fatal("worker published adoption")
			}
			p5nDeleteClosure(t, f, path)
			p5mCommit(t, f, p)
			vm := p5mModel(t, f, results, s1gID(0, 0))
			p5mParity(t, f.assets, oracle, vm, wvm)
			if p5mWorkerAdoptions(t, f.assets) != 1 {
				t.Fatal("cold compiled composite did not transfer worker storage")
			}
			// Hook results are mutable public metadata; corrupting one must not affect a
			// sibling placement from the same immutable worker packet.
			delete(results[s1gID(0, 0)].CollapsedPartIDs, "left")
			f.commitStage()
			p5mModel(t, f, results, s1gID(0, 1))
			if !reflect.DeepEqual(results[s1gID(0, 1)].CollapsedPartIDs, want.CollapsedPartIDs) {
				t.Fatal("public result escaped immutable candidate IDs")
			}
			if p5mWorkerAdoptions(t, f.assets) != 1 {
				t.Fatal("single-use candidate transferred twice")
			}
		})
	}
}
func TestP5nWarmSourceEditsAndDeletedCompositeRebuildSourceFree(t *testing.T) {
	for _, timing := range []string{"before-worker", "after-worker"} {
		t.Run(timing, func(t *testing.T) {
			def := p5eShapeDef()
			_, path := p5nCompile(t, def, false)
			f, results := p5mRuntime(t, path, []int{1}, true)
			expanded := p5nDirect(t, f.app, f.assets, path, VoxelPartCollapseDisable)
			source := mustSpawnedVoxelAssetForTest(t, f.cmd, f.assets, expanded, "left")
			edit := func() { source.XBrickMap.SetVoxel(23, 2, 1, 1) }
			if timing == "before-worker" {
				edit()
			}
			p := p5lPrepare(t, f, ChunkCoord{})
			defer p.release()
			if timing == "after-worker" {
				edit()
			}
			expected := p5nDirect(t, f.app, f.assets, path, VoxelPartCollapseForce)
			evm := mustVoxelModelForSpawnTest(t, f.cmd, onlyVoxelEntityForSpawnTest(t, f.cmd, expected.RootEntity))
			eg, _ := f.assets.GetVoxelGeometry(evm.GeometryAsset())
			want := c3h12aGeometry(eg.XBrickMap)
			if want[[3]int{23, 2, 1}] != 1 {
				t.Fatal("live-input oracle did not contain current raw edit")
			}
			// If preparation initially interned a canonical composite, force reuse must
			// validate it; deletion then requires a fresh live-input bake at stream commit.
			f.assets.DeleteVoxelGeometry(evm.GeometryAsset())
			baseline := p5mWorkerAdoptions(t, f.assets)
			p5nDeleteClosure(t, f, path)
			p5mCommit(t, f, p)
			vm := p5mModel(t, f, results, s1gID(0, 0))
			g, _ := f.assets.GetVoxelGeometry(vm.GeometryAsset())
			if !reflect.DeepEqual(c3h12aGeometry(g.XBrickMap), want) {
				t.Fatal("compiled warmed geometry edits lost")
			}
			if p5mWorkerAdoptions(t, f.assets) != baseline {
				t.Fatal("warm inputs incorrectly adopted canonical candidate")
			}
		})
	}
}
func TestP5nCompositeIdentityTracksAuthenticatedReferences(t *testing.T) {
	def := p5eShapeDef()
	input, path := p5nCompile(t, def, false)
	app, assets := NewApp(), newSpawnTestAssetServer()
	first := p5nDirect(t, app, assets, path, VoxelPartCollapseForce)
	a := mustVoxelModelForSpawnTest(t, app.Commands(), onlyVoxelEntityForSpawnTest(t, app.Commands(), first.RootEntity))
	// The stripped header part definitions stay identical when only source cells
	// change; authenticated reference identity must still change the collapse key.
	def.Parts[0].Source.VoxelShape.Voxels[0].X = 17
	if err := content.SaveAsset(input, def); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileAuthoredCollapsedAssetWithOptions(input, path, nil, CompiledAssetCompileOptions{}); err != nil {
		t.Fatal(err)
	}
	second := p5nDirect(t, app, assets, path, VoxelPartCollapseForce)
	b := mustVoxelModelForSpawnTest(t, app.Commands(), onlyVoxelEntityForSpawnTest(t, app.Commands(), second.RootEntity))
	if a.GeometryAsset() == b.GeometryAsset() {
		t.Fatal("same-path changed authenticated shape reused stale composite")
	}
	g, _ := assets.GetVoxelGeometry(b.GeometryAsset())
	if found, _ := g.XBrickMap.GetVoxel(17, 0, 0); !found {
		t.Fatal("new referenced shape absent from composite")
	}
}
func TestP5nIneligibleCollapseRetainsExpandedSourceFreeFallback(t *testing.T) {
	for _, kind := range []string{"lattice", "palette", "animation"} {
		t.Run(kind, func(t *testing.T) {
			def := p5eShapeDef()
			var input string
			if kind == "lattice" {
				def.Parts[1].VoxelResolution = .5
			}
			if kind == "palette" {
				def.Materials = append(def.Materials, content.AssetMaterialDef{ID: "blue", Name: "Blue", BaseColor: [4]uint8{1, 2, 255, 255}, IOR: 1.5})
				def.Parts[1].Source.VoxelShape.Palette[0].MaterialID = "blue"
			}
			if kind == "animation" {
				input = attachTestAnimationSet(t, def, []content.AssetAnimationClipDef{{ID: "move", Name: "Move", Duration: 1, Tracks: []content.AssetAnimationTrackDef{{TargetID: "left", PositionKeys: []content.AssetVec3KeyDef{{Time: 0}, {Time: 1, Value: content.Vec3{10, 0, 0}}}}}}})
				if err := content.SaveAsset(input, def); err != nil {
					t.Fatal(err)
				}
			} else {
				input = p5mSave(t, def)
			}
			path := filepath.Join(t.TempDir(), "asset.gkassetc")
			if _, err := CompileAuthoredCollapsedAssetWithOptions(input, path, nil, CompiledAssetCompileOptions{}); err != nil {
				t.Fatal(err)
			}
			f, results := p5mRuntime(t, path, []int{1}, true)
			p := p5lPrepare(t, f, ChunkCoord{})
			defer p.release()
			p5nDeleteClosure(t, f, path)
			if kind == "animation" {
				if err := os.RemoveAll(filepath.Join(filepath.Dir(path), "dependencies")); err != nil {
					t.Fatal(err)
				}
			}
			p5mCommit(t, f, p)
			r := results[s1gID(0, 0)]
			if r.Collapsed || len(r.EntitiesByAssetID) != 2 || p5mWorkerAdoptions(t, f.assets) != 0 {
				t.Fatal("ineligible compiled collapse lost ordinary fallback")
			}
			if kind == "animation" {
				assetAnimationSystem(&Time{Dt: .5}, f.cmd)
				local, ok := localTransformForAnimationBind(f.cmd, r.EntitiesByAssetID["left"])
				if !ok || !local.Position.ApproxEqualThreshold(mgl32.Vec3{5, 0, 0}, 1e-4) {
					t.Fatal("compiled fallback animation stopped")
				}
			}
		})
	}
}
func TestP5nCompiledLODAndSelectedPartDoNotReplaceFullAuthority(t *testing.T) {
	def := p5eShapeDef()
	for i := range def.Parts {
		def.Parts[i].Source.VoxelShape.Voxels = []content.VoxelObjectVoxelDef{{X: 0, Value: 1}, {X: 1, Value: 1}, {X: 2, Value: 1}, {X: 3, Value: 1}}
	}
	_, path := p5nCompile(t, def, true)
	h, _, err := content.LoadCompiledAssetHeader(path, nil)
	if err != nil || len(h.LODs) != 2 {
		t.Fatal("schema3 lost verified LOD references", err)
	}
	app, assets := NewApp(), newSpawnTestAssetServer()
	collapsed := p5nDirect(t, app, assets, path, VoxelPartCollapseDefault)
	vm := mustVoxelModelForSpawnTest(t, app.Commands(), onlyVoxelEntityForSpawnTest(t, app.Commands(), collapsed.RootEntity))
	g, _ := assets.GetVoxelGeometry(vm.GeometryAsset())
	if g.XBrickMap.GetVoxelCount() != 4 {
		t.Fatal("collapse used coarse derivative")
	}
	for _, e := range collapsed.Entities {
		c3h13aNoIntent(t, app.Commands(), e)
	}
	expanded := p5nDirect(t, app, assets, path, VoxelPartCollapseDisable)
	for _, e := range expanded.EntitiesByAssetID {
		if !hasComponentOfType[compiledAssetLODComponent](app.Commands(), e) {
			t.Fatal("disabled collapse lost declared per-part LOD intent")
		}
	}
	selected := newSpawnTestAssetServer()
	part, resolution, err := loadAuthoredLevelVoxelPart(selected, NewRuntimeContentLoader(), path)
	if err != nil || part.model == (AssetId{}) || resolution != 1 {
		t.Fatal("selected-part consumer failed", err)
	}
	if p5mWorkerAdoptions(t, selected) != 0 || len(selected.voxModels) != 2 {
		t.Fatal("selected-part consumer prepared/adopted whole-asset composite")
	}
	f, results := p5mRuntime(t, path, []int{1}, true)
	packet := p5lPrepare(t, f, ChunkCoord{})
	defer packet.release()
	p5nDeleteClosure(t, f, path)
	p5mCommit(t, f, packet)
	streamed := p5mModel(t, f, results, s1gID(0, 0))
	full, _ := f.assets.GetVoxelGeometry(streamed.GeometryAsset())
	if p5mWorkerAdoptions(t, f.assets) != 1 || full.XBrickMap.GetVoxelCount() != 4 {
		t.Fatal("worker LOD packet did not adopt full-authority composite")
	}
	for _, entity := range results[s1gID(0, 0)].Entities {
		c3h13aNoIntent(t, f.cmd, entity)
	}

}
func TestP5nPendingCollapseCandidateChargesAndTerminalDrain(t *testing.T) {
	for _, terminal := range []string{"release", "stale", "cancel", "partial Stop"} {
		t.Run(terminal, func(t *testing.T) {
			_, path := p5nCompile(t, p5eShapeDef(), false)
			f, results := p5mRuntime(t, path, []int{2}, false)
			p := p5lPrepare(t, f, ChunkCoord{})
			defer p.release()
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			if f.runtime.Metrics.PendingPreparedBytes <= 0 {
				t.Fatal("compiled candidate uncharged")
			}
			switch terminal {
			case "release":
				alias := p
				p.release()
				alias.release()
			case "stale":
				p.Generation--
				f.runtime.PreparedLoads <- p
				f.commitStage()
			case "cancel":
				cancelStreamedPreparation(f.runtime.chunkPrepareCancels[ChunkCoord{}])
				f.runtime.PreparedLoads <- p
				f.commitStage()
			case "partial Stop":
				p5mCommit(t, f, p)
				vm := p5mModel(t, f, results, s1gID(0, 0))
				if f.hooks[s1gID(0, 1)] != 0 {
					t.Fatal("partial cap ignored")
				}
				if err := StopStreamedLevelRuntime(f.cmd); err != nil {
					t.Fatal(err)
				}
				if _, ok := f.assets.GetVoxelGeometry(vm.GeometryAsset()); !ok {
					t.Fatal("Stop deleted ordinary compiled composite")
				}
			}
			p.release()
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			if f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatal("terminal compiled candidate charge retained")
			}
		})
	}
}

func TestP5nPublicPreparationKeepsCallerEditsBeforeSourceFreeSpawn(t *testing.T) {
	_, path := p5nCompile(t, p5eShapeDef(), false)
	assets, app := newSpawnTestAssetServer(), NewApp()
	prepared, err := LoadAndPrepareAuthoredAsset(path, assets, NewRuntimeContentLoader())
	if err != nil {
		t.Fatal(err)
	}
	first, ok := PreparedAuthoredAssetPartGeometry(prepared, "left")
	if !ok {
		t.Fatal("public prepared part missing")
	}
	geometry, _ := assets.GetVoxelGeometry(first)
	geometry.XBrickMap.SetVoxel(23, 2, 1, 1)
	if p5mWorkerAdoptions(t, assets) != 0 {
		t.Fatal("synchronous preparation adopted a worker candidate")
	}
	h, _, err := content.LoadCompiledAssetHeader(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range h.Shapes {
		_ = os.Remove(filepath.Join(filepath.Dir(path), s.Path))
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	result, err := SpawnPreparedAuthoredAsset(app.Commands(), assets, prepared, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}})
	app.FlushCommands()
	if err != nil || !result.Collapsed {
		t.Fatal("public prepared collapse reread compiled frames", err)
	}
	vm := mustVoxelModelForSpawnTest(t, app.Commands(), onlyVoxelEntityForSpawnTest(t, app.Commands(), result.RootEntity))
	g, _ := assets.GetVoxelGeometry(vm.GeometryAsset())
	if found, value := g.XBrickMap.GetVoxel(23, 2, 1); !found || value != 1 {
		t.Fatal("prepared canonical data overwrote caller raw edits")
	}
	if p5mWorkerAdoptions(t, assets) != 0 {
		t.Fatal("direct spawn counted worker adoption")
	}
}
func TestP5nWarmPaletteOnlyPreservesNestedEditsWithoutCandidateAdoption(t *testing.T) {
	_, path := p5nCompile(t, p5eShapeDef(), false)
	f, results := p5mRuntime(t, path, []int{1}, true)
	expanded := p5nDirect(t, f.app, f.assets, path, VoxelPartCollapseDisable)
	seenGeometry := map[AssetId]bool{}
	seenPalette := map[AssetId]bool{}
	for _, entity := range expanded.EntitiesByAssetID {
		vm := mustVoxelModelForSpawnTest(t, f.cmd, entity)
		if !seenGeometry[vm.GeometryAsset()] {
			seenGeometry[vm.GeometryAsset()] = true
			if !f.assets.DeleteVoxelGeometry(vm.GeometryAsset()) {
				t.Fatal("warm palette fixture geometry deletion")
			}
		}
		if !seenPalette[vm.VoxelPalette] {
			seenPalette[vm.VoxelPalette] = true
			pal, _ := f.assets.GetVoxelPalette(vm.VoxelPalette)
			pal.Materials = []VoxMaterial{{ID: 1, Property: map[string]interface{}{"current": float64(.25)}}}
			f.assets.voxPalettes[vm.VoxelPalette] = pal
		}
	}
	p := p5lPrepare(t, f, ChunkCoord{})
	defer p.release()
	p5nDeleteClosure(t, f, path)
	p5mCommit(t, f, p)
	vm := p5mModel(t, f, results, s1gID(0, 0))
	pal, _ := f.assets.GetVoxelPalette(vm.VoxelPalette)
	if len(pal.Materials) != 1 || pal.Materials[0].Property["current"] != float64(.25) {
		t.Fatal("warm nested palette edits replaced by worker canonical values")
	}
	if p5mWorkerAdoptions(t, f.assets) != 0 {
		t.Fatal("warm palette accepted worker collapse candidate")
	}
}
func TestP5nCandidatePendingChargeExceedsExpandedControlAndDrains(t *testing.T) {
	var growth [2]int64
	for mode := range 2 {
		var charges [2]int64
		for sample, n := range []int{1, 64} {
			def := p5eShapeDef()
			def.Runtime.CollapseVoxelParts = mode == 1
			for i := range def.Parts {
				def.Parts[i].Source.VoxelShape.Voxels = make([]content.VoxelObjectVoxelDef, n)
				for j := range n {
					def.Parts[i].Source.VoxelShape.Voxels[j] = content.VoxelObjectVoxelDef{X: j * 32, Y: i, Value: 1}
				}
			}
			_, path := p5nCompile(t, def, false)
			f, _ := p5mRuntime(t, path, []int{1}, true)
			p := p5lPrepare(t, f, ChunkCoord{})
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			charges[sample] = f.runtime.Metrics.PendingPreparedBytes
			alias := p
			p.release()
			alias.release()
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			if f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatal("aliased candidate result retained pending charge")
			}
		}
		growth[mode] = charges[1] - charges[0]
	}
	if extra := growth[1] - growth[0]; extra < int64(2*63*512) {
		t.Fatalf("candidate charge omitted source/publication dense cells: extra=%d", extra)
	}
}

func TestP5nWarmCompositeOnlyRetainsEditedGlobalAndLiveValidation(t *testing.T) {
	_, path := p5nCompile(t, p5eShapeDef(), false)
	oracle, app := newSpawnTestAssetServer(), NewApp()
	result := p5nDirect(t, app, oracle, path, VoxelPartCollapseForce)
	vm := mustVoxelModelForSpawnTest(t, app.Commands(), onlyVoxelEntityForSpawnTest(t, app.Commands(), result.RootEntity))
	g, _ := oracle.GetVoxelGeometry(vm.GeometryAsset())
	f, results := p5mRuntime(t, path, []int{1}, true)
	id := f.assets.RegisterSharedVoxelGeometryWithCacheKey(g.SourcePath, g.XBrickMap.Copy(), g.SourcePath)
	current, _ := f.assets.GetVoxelGeometry(id)
	current.XBrickMap.SetVoxel(71, 2, 1, 7)
	p := p5lPrepare(t, f, ChunkCoord{})
	defer p.release()
	p5nDeleteClosure(t, f, path)
	p5mCommit(t, f, p)
	got := p5mModel(t, f, results, s1gID(0, 0))
	live, _ := f.assets.GetVoxelGeometry(got.GeometryAsset())
	if got.GeometryAsset() != id {
		t.Fatal("cold-input candidate replaced preexisting composite ID")
	}
	if found, value := live.XBrickMap.GetVoxel(71, 2, 1); !found || value != 7 {
		t.Fatal("preexisting composite edits lost")
	}
	if p5mWorkerAdoptions(t, f.assets) != 0 {
		t.Fatal("warm composite counted cold worker transfer")
	}
	if builds, hits := p5eCollapseStats(t, f.assets); builds != 0 || hits != 1 {
		t.Fatalf("warm composite counters=%d/%d", builds, hits)
	}
}

func TestP5nRelativeAndAbsolutePlacementsShareCanonicalCompiledComposite(t *testing.T) {
	_, path := p5nCompile(t, p5eShapeDef(), false)
	f, results := p5mRuntime(t, path, []int{2}, false)
	relative, err := filepath.Rel(filepath.Dir(f.runtime.LevelPath), path)
	if err != nil {
		t.Fatal(err)
	}
	// The level-relative spelling must never resolve through the cwd existence
	// shortcut, including before the compiled header is removed.
	if _, err := os.Stat(relative); !os.IsNotExist(err) {
		t.Fatalf("relative fixture has ambiguous cwd interpretation: %q err=%v", relative, err)
	}
	f.runtime.PlacementsByChunk[ChunkCoord{}][0].AssetPath = relative
	packet := p5lPrepare(t, f, ChunkCoord{})
	defer packet.release()
	p5nDeleteClosure(t, f, path)
	p5mCommit(t, f, packet)
	first := p5mModel(t, f, results, s1gID(0, 0))
	if adoptions := p5mWorkerAdoptions(t, f.assets); adoptions != 1 {
		t.Fatalf("relative placement worker adoptions=%d", adoptions)
	}
	if builds, hits := p5eCollapseStats(t, f.assets); builds != 1 || hits != 0 {
		t.Fatalf("relative placement rebaked unused canonical candidate: builds/hits=%d/%d", builds, hits)
	}
	f.commitStage()
	second := p5mModel(t, f, results, s1gID(0, 1))
	if first.GeometryAsset() != second.GeometryAsset() {
		t.Fatal("relative and absolute compiled spellings registered separate composites")
	}
	if adoptions := p5mWorkerAdoptions(t, f.assets); adoptions != 1 {
		t.Fatalf("same document transferred worker candidate twice: %d", adoptions)
	}
	if builds, hits := p5eCollapseStats(t, f.assets); builds != 1 || hits != 1 {
		t.Fatalf("same compiled document did not reuse canonical composite: builds/hits=%d/%d", builds, hits)
	}
}
