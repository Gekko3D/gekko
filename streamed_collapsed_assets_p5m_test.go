package gekko

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// P5m protects streamed expanded collapse through public spawning and placement
// observations. The direct spawn is an independent hierarchy/bake oracle.
func p5mWorkerAdoptions(t *testing.T, assets *AssetServer) uint64 {
	t.Helper()
	method := reflect.ValueOf(assets).MethodByName("AuthoredVoxelCollapseStats")
	if !method.IsValid() {
		t.Fatal("missing public collapse stats")
	}
	stats := method.Call(nil)
	if len(stats) != 1 {
		t.Fatal("invalid public collapse stats result")
	}
	field := stats[0].FieldByName("WorkerAdoptions")
	if !field.IsValid() || field.Kind() != reflect.Uint64 {
		t.Fatal("missing public AuthoredVoxelCollapseStats.WorkerAdoptions uint64 ownership-transfer observation")
	}
	return field.Uint()
}

func p5mSave(t *testing.T, def *content.AssetDef) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "collapse.gkasset")
	if err := content.SaveAsset(path, def); err != nil {
		t.Fatal(err)
	}
	return path
}
func p5mVox(t *testing.T) (string, *content.AssetDef) {
	t.Helper()
	def := p5eShapeDef()
	for i := range def.Parts {
		def.Parts[i].Source = content.AssetSourceDef{Kind: content.AssetSourceKindVoxModel, Path: "models.vox", ModelIndex: 1}
		def.Parts[i].Transform.Position = content.Vec3{float32(i * 4), 0, 0}
	}
	path := p5mSave(t, def)
	writeNamedSceneVoxFixture(t, filepath.Join(filepath.Dir(path), "models.vox"))
	return path, def
}
func p5mRuntime(t *testing.T, path string, counts []int, cpu bool) (*s1gFixture, map[string]AuthoredAssetSpawnResult) {
	t.Helper()
	f := s1gRuntime(t, counts, 1, !cpu, false)
	results := map[string]AuthoredAssetSpawnResult{}
	f.runtime.Config.PlacementHooks = []PostSpawnPlacementHook{func(cmd *Commands, ctx PostSpawnPlacementContext) {
		f.hooks[ctx.Placement.PlacementID]++
		if !cmd.EntityExists(ctx.RootEntity) {
			t.Fatal("hook observed unflushed root")
		}
		results[ctx.Placement.PlacementID] = ctx.SpawnResult
	}}
	for coord, items := range f.runtime.PlacementsByChunk {
		for i := range items {
			items[i].AssetPath = path
		}
		f.runtime.PlacementsByChunk[coord] = items
	}
	return f, results
}
func p5mSpawn(t *testing.T, app *App, assets *AssetServer, def *content.AssetDef, path string, mode VoxelPartCollapseMode) AuthoredAssetSpawnResult {
	t.Helper()
	r, err := SpawnAuthoredAssetWithOptions(app.Commands(), assets, def, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}, AuthoredAssetSpawnOptions{DocumentPath: path, CollapseVoxelParts: mode})
	app.FlushCommands()
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func p5mModel(t *testing.T, f *s1gFixture, results map[string]AuthoredAssetSpawnResult, id string) VoxelModelComponent {
	t.Helper()
	r, ok := results[id]
	if !ok || !r.Collapsed || f.hooks[id] != 1 {
		t.Fatalf("placement must collapse with exactly one callback: %+v hooks=%d err=%v", r, f.hooks[id], f.runtime.InitErr)
	}
	vm := mustVoxelModelForSpawnTest(t, f.cmd, onlyVoxelEntityForSpawnTest(t, f.cmd, r.RootEntity))
	if vm.OverrideGeometry != (AssetId{}) || vm.PivotMode != PivotModeCorner {
		t.Fatal("collapse changed ordinary ownership/pivot")
	}
	return vm
}
func p5mParity(t *testing.T, assets, oracle *AssetServer, got, want VoxelModelComponent) {
	t.Helper()
	g, gok := assets.GetVoxelGeometry(got.GeometryAsset())
	w, wok := oracle.GetVoxelGeometry(want.GeometryAsset())
	if !gok || !wok || got.VoxelResolution != want.VoxelResolution || g.LocalMin != w.LocalMin || g.LocalMax != w.LocalMax || !reflect.DeepEqual(c3h12aGeometry(g.XBrickMap), c3h12aGeometry(w.XBrickMap)) {
		t.Fatal("streamed collapse changed canonical geometry, bounds or lattice")
	}
	gp, _ := assets.GetVoxelPalette(got.VoxelPalette)
	wp, _ := oracle.GetVoxelPalette(want.VoxelPalette)
	if !reflect.DeepEqual(gp, wp) {
		t.Fatal("streamed collapse changed palette/materials")
	}
}
func p5mCommit(t *testing.T, f *s1gFixture, p streamedPreparedChunk) {
	t.Helper()
	f.runtime.PreparedLoads <- p
	f.commitStage()
	if f.runtime.InitErr != nil {
		t.Fatalf("source-free prepared commit failed: %v", f.runtime.InitErr)
	}
}

func TestP5mColdVoxCollapseSourceFreeCPUAndRenderer(t *testing.T) {
	for _, cpu := range []bool{true, false} {
		t.Run(fmt.Sprint(cpu), func(t *testing.T) {
			path, def := p5mVox(t)
			oracle, app := newSpawnTestAssetServer(), NewApp()
			want := p5mSpawn(t, app, oracle, def, path, VoxelPartCollapseForce)
			wantVM := mustVoxelModelForSpawnTest(t, app.Commands(), onlyVoxelEntityForSpawnTest(t, app.Commands(), want.RootEntity))
			f, results := p5mRuntime(t, path, []int{1}, cpu)
			p := p5lPrepare(t, f, ChunkCoord{})
			defer p.release()
			if p5mWorkerAdoptions(t, f.assets) != 0 {
				t.Fatal("worker incremented live adoption counter")
			}
			if b, h := p5eCollapseStats(t, f.assets); b != 0 || h != 0 {
				t.Fatal("worker changed live collapse counters")
			}
			if err := os.Remove(filepath.Join(filepath.Dir(path), "models.vox")); err != nil {
				t.Fatal(err)
			}
			p5mCommit(t, f, p)
			vm := p5mModel(t, f, results, s1gID(0, 0))
			if p5mWorkerAdoptions(t, f.assets) != 1 {
				t.Fatal("all-cold commit did not adopt worker composite")
			}
			p5mParity(t, f.assets, oracle, vm, wantVM)
			if !reflect.DeepEqual(results[s1gID(0, 0)].CollapsedPartIDs, want.CollapsedPartIDs) {
				t.Fatal("collapsed authored IDs changed")
			}
			if b, h := p5eCollapseStats(t, f.assets); b != 1 || h != 0 {
				t.Fatalf("publication counters %d/%d", b, h)
			}
			if f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatal("commit retained pending charge")
			}
		})
	}
}
func TestP5mAnimatedCollapseFallbackSourceFree(t *testing.T) {
	path, def := p5lAsset(t)
	def.Runtime.CollapseVoxelParts = true
	if err := content.SaveAsset(path, def); err != nil {
		t.Fatal(err)
	}
	f, results := p5mRuntime(t, path, []int{1}, true)
	p := p5lPrepare(t, f, ChunkCoord{})
	defer p.release()
	for _, dep := range []string{"models.vox", "asset.gkanim"} {
		if err := os.Remove(filepath.Join(filepath.Dir(path), dep)); err != nil {
			t.Fatal(err)
		}
	}
	p5mCommit(t, f, p)
	r := results[s1gID(0, 0)]
	if r.Collapsed || len(r.EntitiesByAssetID) != len(def.Parts) || f.hooks[s1gID(0, 0)] != 1 {
		t.Fatal("animated asset lost expanded fallback")
	}
	assetAnimationSystem(&Time{Dt: .5}, f.cmd)
	local, ok := localTransformForAnimationBind(f.cmd, r.EntitiesByAssetID["group"])
	if !ok || !local.Position.ApproxEqualThreshold(mgl32.Vec3{5, 0, 0}, 1e-4) {
		t.Fatal("prepared fallback animation no longer runs")
	}
}
func TestP5mColdHierarchySourcesAndOrderedOperationsMatchPublicSpawn(t *testing.T) {
	cases := append([]string{"shape", "vox", "scene", "subtraction", "subtract-then-add", "empty"}, c3g1Kinds...)
	for _, kind := range cases {
		t.Run(kind, func(t *testing.T) {
			def := p5eShapeDef()
			switch kind {
			case "vox", "scene":
				for i := range def.Parts {
					def.Parts[i].Source = content.AssetSourceDef{Kind: content.AssetSourceKindVoxModel, Path: "models.vox", ModelIndex: 1}
					if kind == "scene" {
						def.Parts[i].Source.Kind = content.AssetSourceKindVoxSceneNode
						def.Parts[i].Source.NodeName = "arm"
					}
				}
			case "subtraction", "subtract-then-add", "empty":
				def.Parts[1].Source.VoxelShape.Voxels[0].X = 0
				def.Parts[1].Source.Operation = content.AssetShapeOperationSubtract
				if kind == "subtraction" {
					def.Parts[0].Source.VoxelShape.Voxels = append(def.Parts[0].Source.VoxelShape.Voxels, content.VoxelObjectVoxelDef{X: 1, Value: 1})
				}
				if kind == "subtract-then-add" {
					def.Parts[0], def.Parts[1] = def.Parts[1], def.Parts[0]
				}
			case "shape":
			default:
				for i := range def.Parts {
					def.Parts[i].Source = content.AssetSourceDef{Kind: content.AssetSourceKindProceduralPrimitive, Primitive: kind, Params: c3g1Params(), MaterialID: "red"}
				}
			}
			// Child before its voxel parent and group; hierarchy math must match runtime
			// even when parent pivot/resolution are unrelated to child placement.
			if kind != "empty" && kind != "subtraction" && kind != "subtract-then-add" {
				def.Parts[0].ParentID = "right"
				def.Parts[1].ParentID = "group"
				def.Parts[0].Transform.Position = content.Vec3{2, 1, 0}
				def.Parts[0].Transform.Pivot = content.Vec3{.25, .5, 0}
				def.Parts[1].Transform.Position = content.Vec3{1, 2, 0}
				def.Parts[1].Transform.Pivot = content.Vec3{3, 4, 5}
				q := mgl32.QuatRotate(.6, mgl32.Vec3{0, 0, 1})
				def.Parts = append(def.Parts, content.AssetPartDef{ID: "group", Name: "group", Source: content.AssetSourceDef{Kind: content.AssetSourceKindGroup}, Transform: content.AssetTransformDef{Position: content.Vec3{3, -2, 1}, Rotation: content.Quat{q.V[0], q.V[1], q.V[2], q.W}, Scale: content.Vec3{-1, 2, 1}, Pivot: content.Vec3{9, 8, 7}}})
			}
			path := p5mSave(t, def)
			if kind == "vox" || kind == "scene" {
				writeNamedSceneVoxFixture(t, filepath.Join(filepath.Dir(path), "models.vox"))
			}
			oracle, app := newSpawnTestAssetServer(), NewApp()
			want := p5mSpawn(t, app, oracle, def, path, VoxelPartCollapseForce)
			wvm := mustVoxelModelForSpawnTest(t, app.Commands(), onlyVoxelEntityForSpawnTest(t, app.Commands(), want.RootEntity))
			f, results := p5mRuntime(t, path, []int{1}, true)
			p := p5lPrepare(t, f, ChunkCoord{})
			defer p.release()
			p5mCommit(t, f, p)
			vm := p5mModel(t, f, results, s1gID(0, 0))
			p5mParity(t, f.assets, oracle, vm, wvm)
			if p5mWorkerAdoptions(t, f.assets) != 1 {
				t.Fatal("canonical cold hierarchy composite was baked on main instead of adopted")
			}
			direct := p5mSpawn(t, f.app, f.assets, def, path, VoxelPartCollapseForce)
			dvm := mustVoxelModelForSpawnTest(t, f.cmd, onlyVoxelEntityForSpawnTest(t, f.cmd, direct.RootEntity))
			if vm.GeometryAsset() != dvm.GeometryAsset() {
				t.Fatal("streamed collapse key differs from public DocumentPath key")
			}
		})
	}
}

func TestP5mWarmSourcesUseCurrentRawGeometryAndModelRows(t *testing.T) {
	for _, kind := range []string{"shape", "vox"} {
		for _, timing := range []string{"before-worker", "after-worker"} {
			t.Run(kind+"/"+timing, func(t *testing.T) {
				def := p5eShapeDef()
				path := p5mSave(t, def)
				if kind == "vox" {
					path, def = p5mVox(t)
				}
				f, results := p5mRuntime(t, path, []int{1}, true)
				warm := p5mSpawn(t, f.app, f.assets, def, path, VoxelPartCollapseDisable)
				source := mustSpawnedVoxelAssetForTest(t, f.cmd, f.assets, warm, "left")
				edit := func() {
					if kind == "shape" {
						source.XBrickMap.SetVoxel(23, 2, 1, 1)
					} else {
						source.VoxModel.Voxels[0].X = 23
						source.VoxModel.Voxels[0].ColorIndex = 0
					}
				}
				if timing == "before-worker" {
					edit()
				}
				p := p5lPrepare(t, f, ChunkCoord{})
				defer p.release()
				if timing == "after-worker" {
					edit()
				}
				// Public force establishes the live-input oracle; remove only its composite
				// so stream commit must rebuild from warmed inputs rather than reuse it.
				expected := p5mSpawn(t, f.app, f.assets, def, path, VoxelPartCollapseForce)
				evm := mustVoxelModelForSpawnTest(t, f.cmd, onlyVoxelEntityForSpawnTest(t, f.cmd, expected.RootEntity))
				eg, _ := f.assets.GetVoxelGeometry(evm.GeometryAsset())
				want := c3h12aGeometry(eg.XBrickMap)
				if !f.assets.DeleteVoxelGeometry(evm.GeometryAsset()) {
					t.Fatal("oracle composite deletion")
				}
				if kind == "vox" {
					if err := os.Remove(filepath.Join(filepath.Dir(path), "models.vox")); err != nil {
						t.Fatal(err)
					}
				}
				p5mCommit(t, f, p)
				vm := p5mModel(t, f, results, s1gID(0, 0))
				got, _ := f.assets.GetVoxelGeometry(vm.GeometryAsset())
				if !reflect.DeepEqual(c3h12aGeometry(got.XBrickMap), want) {
					t.Fatal("candidate overwrote current warmed source samples")
				}
				if p5mWorkerAdoptions(t, f.assets) != 0 {
					t.Fatal("warmed geometry accepted canonical worker composite")
				}
			})
		}
	}
}
func TestP5mWarmNestedPaletteIncompatibilityFallsBackSourceFree(t *testing.T) {
	for _, timing := range []string{"before-worker", "after-worker"} {
		t.Run(timing, func(t *testing.T) {
			path, def := p5mVox(t)
			def.Parts[1].Source.Path = "other.vox"
			writeNamedSceneVoxFixture(t, filepath.Join(filepath.Dir(path), "other.vox"))
			if err := content.SaveAsset(path, def); err != nil {
				t.Fatal(err)
			}
			f, results := p5mRuntime(t, path, []int{1}, true)
			warm := p5mSpawn(t, f.app, f.assets, def, path, VoxelPartCollapseDisable)
			a := mustVoxelModelForSpawnTest(t, f.cmd, warm.EntitiesByAssetID["left"])
			b := mustVoxelModelForSpawnTest(t, f.cmd, warm.EntitiesByAssetID["right"])
			if a.VoxelPalette == b.VoxelPalette {
				t.Fatal("fixture needs separate mutable palette namespaces")
			}
			for _, id := range []AssetId{a.VoxelPalette, b.VoxelPalette} {
				pal, _ := f.assets.GetVoxelPalette(id)
				pal.Materials = []VoxMaterial{{ID: 1, Property: map[string]interface{}{"rough": float64(1)}}}
				f.assets.voxPalettes[id] = pal
			}
			edit := func() {
				pal, _ := f.assets.GetVoxelPalette(b.VoxelPalette)
				pal.Materials[0].Property["rough"] = float64(.25)
			}
			if timing == "before-worker" {
				edit()
			}
			if !f.assets.DeleteVoxelGeometry(a.GeometryAsset()) || !f.assets.DeleteVoxelGeometry(b.GeometryAsset()) {
				t.Fatal("palette-only warm fixture failed to delete geometry")
			}
			p := p5lPrepare(t, f, ChunkCoord{})
			defer p.release()
			if timing == "after-worker" {
				edit()
			}
			for _, dep := range []string{"models.vox", "other.vox"} {
				if err := os.Remove(filepath.Join(filepath.Dir(path), dep)); err != nil {
					t.Fatal(err)
				}
			}
			p5mCommit(t, f, p)
			r := results[s1gID(0, 0)]
			if r.Collapsed || len(r.EntitiesByAssetID) != 2 || f.hooks[s1gID(0, 0)] != 1 {
				t.Fatal("warm palette incompatibility did not preserve expanded fallback")
			}
			if p5mWorkerAdoptions(t, f.assets) != 0 {
				t.Fatal("warmed palettes accepted worker composite")
			}
			if builds, hits := p5eCollapseStats(t, f.assets); builds != 0 || hits != 0 {
				t.Fatalf("invalid collapse counted %d/%d", builds, hits)
			}
		})
	}
}
func TestP5mWarmCompositeReuseAndConsumedCandidateRebuild(t *testing.T) {
	path, def := p5mVox(t)
	f, results := p5mRuntime(t, path, []int{3}, false)
	p := p5lPrepare(t, f, ChunkCoord{})
	defer p.release()
	if err := os.Remove(filepath.Join(filepath.Dir(path), "models.vox")); err != nil {
		t.Fatal(err)
	}
	p5mCommit(t, f, p)
	first := p5mModel(t, f, results, s1gID(0, 0))
	g, _ := f.assets.GetVoxelGeometry(first.GeometryAsset())
	canonical := c3h12aGeometry(g.XBrickMap)
	g.XBrickMap.SetVoxel(71, 2, 1, 7)
	f.commitStage()
	second := p5mModel(t, f, results, s1gID(0, 1))
	if first.GeometryAsset() != second.GeometryAsset() {
		t.Fatal("warm mutable composite lost shared global ID")
	}
	if !f.assets.DeleteVoxelGeometry(first.GeometryAsset()) {
		t.Fatal("composite deletion failed")
	}
	f.commitStage()
	third := p5mModel(t, f, results, s1gID(0, 2))
	rebuilt, _ := f.assets.GetVoxelGeometry(third.GeometryAsset())
	if third.GeometryAsset() == first.GeometryAsset() || !reflect.DeepEqual(c3h12aGeometry(rebuilt.XBrickMap), canonical) {
		t.Fatal("consumed packet rebuild aliased edited publication or reread source")
	}
	if p5mWorkerAdoptions(t, f.assets) != 1 {
		t.Fatal("consumed candidate was adopted again")
	}
	if b, h := p5eCollapseStats(t, f.assets); b != 2 || h != 1 {
		t.Fatalf("build/hit semantics %d/%d", b, h)
	}
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.assets.GetVoxelGeometry(third.GeometryAsset()); !ok {
		t.Fatal("Stop deleted ordinary collapsed global")
	}
	_ = def
}
func TestP5mCapturedDefinitionAndExactDocumentSpelling(t *testing.T) {
	path, def := p5mVox(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, path)
	if err != nil {
		t.Fatal(err)
	}
	f, results := p5mRuntime(t, path, []int{2}, true)
	f.runtime.PlacementsByChunk[ChunkCoord{}][1].AssetPath = relative
	p := p5lPrepare(t, f, ChunkCoord{})
	defer p.release()
	cached, err := f.runtime.Loader.LoadAsset(path)
	if err != nil {
		t.Fatal(err)
	}
	cached.Parts[0].Transform.Position = content.Vec3{91, 92, 93}
	p5mCommit(t, f, p)
	f.commitStage()
	first := p5mModel(t, f, results, s1gID(0, 0))
	second := p5mModel(t, f, results, s1gID(0, 1))
	for i, spelling := range []string{path, relative} {
		direct := p5mSpawn(t, f.app, f.assets, def, spelling, VoxelPartCollapseForce)
		vm := mustVoxelModelForSpawnTest(t, f.cmd, onlyVoxelEntityForSpawnTest(t, f.cmd, direct.RootEntity))
		if vm.GeometryAsset() != []VoxelModelComponent{first, second}[i].GeometryAsset() {
			t.Fatalf("captured definition or exact collapse DocumentPath domain changed for %q", spelling)
		}
	}
	if first.GeometryAsset() == second.GeometryAsset() {
		t.Fatal("same physical file merged distinct public collapse keys")
	}
}
func TestP5mPendingSourcePublicationGrowthAndAliasDrain(t *testing.T) {
	var candidateCharges []int64
	for _, n := range []int{1, 128} {
		var charges []int64
		for _, collapse := range []bool{false, true} {
			def := p5eShapeDef()
			def.Runtime.CollapseVoxelParts = collapse
			for i := range def.Parts {
				def.Parts[i].Source.VoxelShape.Voxels = make([]content.VoxelObjectVoxelDef, n)
				for j := range n {
					def.Parts[i].Source.VoxelShape.Voxels[j] = content.VoxelObjectVoxelDef{X: j * 32, Y: i, Value: 1}
				}
			}
			path := p5mSave(t, def)
			f, _ := p5mRuntime(t, path, []int{1}, true)
			prepared := p5lPrepare(t, f, ChunkCoord{})
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			charges = append(charges, f.runtime.Metrics.PendingPreparedBytes)
			alias := prepared
			prepared.release()
			alias.release()
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			if f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatal("aliased collapse envelope did not drain")
			}
		}
		candidateCharges = append(candidateCharges, charges[1]-charges[0])
	}
	// Subtract expanded preparation so its dense storage cannot hide an omitted
	// composite. At least primary cells in both independent composite maps count.
	if growth := candidateCharges[1] - candidateCharges[0]; growth < int64(2*127*512) {
		t.Fatalf("pending charge omitted candidate dense cells: growth %d", growth)
	}
}

func TestP5mWarmCompositeOnlyRetainsLiveReuse(t *testing.T) {
	path, def := p5mVox(t)
	oracle, app := newSpawnTestAssetServer(), NewApp()
	result := p5mSpawn(t, app, oracle, def, path, VoxelPartCollapseForce)
	vm := mustVoxelModelForSpawnTest(t, app.Commands(), onlyVoxelEntityForSpawnTest(t, app.Commands(), result.RootEntity))
	geometry, _ := oracle.GetVoxelGeometry(vm.GeometryAsset())
	f, results := p5mRuntime(t, path, []int{1}, true)
	// Prime only the composite via its public cache key. All input geometry and
	// palette entries are cold, isolating this gate from the source-warm gate.
	live := f.assets.RegisterSharedVoxelGeometryWithCacheKey(geometry.SourcePath, geometry.XBrickMap, geometry.SourcePath)
	edited, _ := f.assets.GetVoxelGeometry(live)
	edited.XBrickMap.SetVoxel(71, 2, 1, 7)
	prepared := p5lPrepare(t, f, ChunkCoord{})
	defer prepared.release()
	if err := os.Remove(filepath.Join(filepath.Dir(path), "models.vox")); err != nil {
		t.Fatal(err)
	}
	p5mCommit(t, f, prepared)
	got := p5mModel(t, f, results, s1gID(0, 0))
	actual, _ := f.assets.GetVoxelGeometry(got.GeometryAsset())
	occupied, value := actual.XBrickMap.GetVoxel(71, 2, 1)
	if got.GeometryAsset() != live || !occupied || value != 7 {
		t.Fatal("cold inputs replaced edited warm composite")
	}
	if p5mWorkerAdoptions(t, f.assets) != 0 {
		t.Fatal("warm composite accepted a cold worker transfer")
	}
	if builds, hits := p5eCollapseStats(t, f.assets); builds != 0 || hits != 1 {
		t.Fatalf("warm composite counters %d/%d", builds, hits)
	}
}

func TestP5mTerminalDrainAndPartialStopPreserveGlobalLifetime(t *testing.T) {
	for _, terminal := range []string{"stale", "cancel", "partial Stop", "hook failure"} {
		t.Run(terminal, func(t *testing.T) {
			path, _ := p5mVox(t)
			f, results := p5mRuntime(t, path, []int{2}, false)
			p := p5lPrepare(t, f, ChunkCoord{})
			defer p.release()
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			if f.runtime.Metrics.PendingPreparedBytes <= 0 {
				t.Fatal("uncharged prepared envelope")
			}
			switch terminal {
			case "stale":
				p.Generation--
				f.runtime.PreparedLoads <- p
				f.commitStage()
			case "cancel":
				cancelStreamedPreparation(f.runtime.chunkPrepareCancels[ChunkCoord{}])
				f.runtime.PreparedLoads <- p
				f.commitStage()
			default:
				if terminal == "hook failure" {
					f.runtime.Config.PlacementHooks = append(f.runtime.Config.PlacementHooks, func(_ *Commands, _ PostSpawnPlacementContext) { f.runtime.InitErr = fmt.Errorf("P5m hook failure") })
				}
				p5mCommitErr := func() {
					f.runtime.PreparedLoads <- p
					commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
					f.app.FlushCommands()
				}
				p5mCommitErr()
				vm := p5mModel(t, f, results, s1gID(0, 0))
				if f.hooks[s1gID(0, 1)] != 0 {
					t.Fatal("partial placement cap ignored")
				}
				if terminal == "hook failure" {
					refreshStreamedRuntimeMetricsCounts(f.runtime)
					if f.runtime.InitErr == nil || f.runtime.Metrics.PendingPreparedBytes <= 0 {
						t.Fatal("fatal callback did not retain transaction")
					}
					commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
					f.app.FlushCommands()
					if f.hooks[s1gID(0, 0)] != 1 {
						t.Fatal("fatal callback retried")
					}
				}
				if err := StopStreamedLevelRuntime(f.cmd); err != nil {
					t.Fatal(err)
				}
				if _, ok := f.assets.GetVoxelGeometry(vm.GeometryAsset()); !ok {
					t.Fatal("Stop removed collapsed global")
				}
			}
			p.release()
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			if f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatal("terminal collapse envelope retained charge")
			}
		})
	}
}
func TestP5mWorkerPressureAndCancelledConstructionDrain(t *testing.T) {
	path, _ := p5mVox(t)
	f, _ := p5mRuntime(t, path, []int{1}, true)
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
		t.Fatal("pressure did not retry with drained envelope")
	}
	hold.release()
	cancel := make(chan struct{})
	close(cancel)
	job.prepareCancel = cancel
	p := prepareStreamedChunkLoad(job)
	defer p.release()
	p.release()
	if p.Err != nil || owner.snapshot().Bytes != 0 || f.runtime.Loader.Stats().PinnedBytes != pinned || len(f.assets.voxModels) != 0 || len(f.assets.voxPalettes) != 0 {
		t.Fatal("cancelled worker retained state or published IDs")
	}
}

// Keep raw occupancy validation distinct from sample presence on warm inputs.
func TestP5mWarmRawSampleValidationPreservesAutomaticFallback(t *testing.T) {
	def := p5eShapeDef()
	path := p5mSave(t, def)
	f, results := p5mRuntime(t, path, []int{1}, true)
	warm := p5mSpawn(t, f.app, f.assets, def, path, VoxelPartCollapseDisable)
	source := mustSpawnedVoxelAssetForTest(t, f.cmd, f.assets, warm, "left")
	p := p5lPrepare(t, f, ChunkCoord{})
	defer p.release()
	for _, sector := range source.XBrickMap.Sectors {
		for _, brick := range sector.PackedBricks {
			brick.Payload = [volume.BrickSize][volume.BrickSize][volume.BrickSize]uint8{}
			brick.Flags |= volume.BrickFlagSolid
		}
	}
	if source.XBrickMap.GetVoxelCount() == 0 {
		t.Fatal("fixture needs misleading occupied flags")
	}
	p5mCommit(t, f, p)
	if results[s1gID(0, 0)].Collapsed {
		t.Fatal("worker candidate bypassed live raw sample validation")
	}
}

func TestP5mIneligibleLatticeKeepsPreparedExpandedFallback(t *testing.T) {
	path, def := p5mVox(t)
	def.Parts[1].VoxelResolution = .5
	if err := content.SaveAsset(path, def); err != nil {
		t.Fatal(err)
	}
	f, results := p5mRuntime(t, path, []int{1}, true)
	p := p5lPrepare(t, f, ChunkCoord{})
	defer p.release()
	if err := os.Remove(filepath.Join(filepath.Dir(path), "models.vox")); err != nil {
		t.Fatal(err)
	}
	p5mCommit(t, f, p)
	r := results[s1gID(0, 0)]
	if r.Collapsed || len(r.EntitiesByAssetID) != 2 || f.hooks[s1gID(0, 0)] != 1 {
		t.Fatal("ineligible collapse lost prepared expanded fallback")
	}
	if p5mWorkerAdoptions(t, f.assets) != 0 {
		t.Fatal("ineligible lattice adopted canonical composite")
	}
}
func TestP5mInvalidSourceFailsWorkerBeforePublication(t *testing.T) {
	path, _ := p5mVox(t)
	f, _ := p5mRuntime(t, path, []int{1}, true)
	if err := os.Remove(filepath.Join(filepath.Dir(path), "models.vox")); err != nil {
		t.Fatal(err)
	}
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	p := <-f.runtime.PreparedLoads
	defer p.release()
	if p.Err == nil {
		t.Fatal("collapse worker accepted missing dependency")
	}
	p.release()
	refreshStreamedRuntimeMetricsCounts(f.runtime)
	if len(f.assets.voxModels) != 0 || len(f.assets.voxPalettes) != 0 || len(f.hooks) != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("invalid collapse worker published state or retained charge")
	}
}
