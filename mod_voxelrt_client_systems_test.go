package gekko

import (
	"math"
	"testing"

	"github.com/gekko3d/gekko/content"
	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

func TestSyncVoxelRtLightsUsesDaylightDirectionalLightAsSun(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	applyLevelEnvironment(cmd, &content.LevelEnvironmentDef{Preset: "daylight"})
	app.FlushCommands()

	state := &VoxelRtState{
		RtApp: &app_rt.App{
			Scene:    core.NewScene(),
			Profiler: core.NewProfiler(),
		},
	}

	syncVoxelRtLights(state, cmd)

	if len(state.RtApp.Scene.Lights) != 1 {
		t.Fatalf("expected one non-ambient GPU light, got %d", len(state.RtApp.Scene.Lights))
	}
	if state.SunIntensity <= 0 {
		t.Fatalf("expected positive sun intensity, got %f", state.SunIntensity)
	}
	if state.SunDirection.Len() <= 0 {
		t.Fatalf("expected non-zero sun direction, got %v", state.SunDirection)
	}
	if state.RtApp.Scene.AmbientLight.Len() <= 0 {
		t.Fatalf("expected non-zero ambient light, got %v", state.RtApp.Scene.AmbientLight)
	}
}

func TestSyncVoxelRtLightsDerivesSourceRadiusFromLinkedEmitterGeometry(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	server := newVoxelRtAssetServerTest(t)
	state := newVoxelRtStateTest()

	modelID := server.CreateVoxelModel(VoxModel{
		SizeX: 1,
		SizeY: 1,
		SizeZ: 1,
		Voxels: []Voxel{
			{X: 0, Y: 0, Z: 0, ColorIndex: 1},
		},
	}, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{255, 220, 96, 255})

	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, 0},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{2, 2, 2},
		},
		&VoxelModelComponent{
			VoxelModel:    modelID,
			VoxelPalette:  paletteID,
			EmitterLinkID: 77,
		},
	)
	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, 0},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&LightComponent{
			Type:          LightTypePoint,
			Color:         [3]float32{1, 1, 1},
			Intensity:     4,
			Range:         20,
			CastsShadows:  true,
			EmitterLinkID: 77,
		},
	)
	app.FlushCommands()

	voxelRtSystem(nil, state, server, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	syncVoxelRtLights(state, cmd)

	if len(state.RtApp.Scene.Lights) != 1 {
		t.Fatalf("expected one synced light, got %d", len(state.RtApp.Scene.Lights))
	}

	got := state.RtApp.Scene.Lights[0].Position[3]
	want := float32(math.Sqrt(0.12) / 2.0)
	if math.Abs(float64(got-want)) > 0.0001 {
		t.Fatalf("expected derived source radius %v, got %v", want, got)
	}
}

func TestVoxelRtDebugSystemCyclesNamedModes(t *testing.T) {
	input := &Input{}
	input.JustPressed[KeyF2] = true
	state := &VoxelRtState{
		RtApp: &app_rt.App{
			Camera: core.NewCameraState(),
		},
	}

	voxelRtDebugSystem(input, state)
	if got := state.DebugOverlayMode(); got != VoxelRtDebugModeScene {
		t.Fatalf("expected first F2 press to switch to scene debug, got %v", got)
	}

	voxelRtDebugSystem(input, state)
	if got := state.DebugOverlayMode(); got != VoxelRtDebugModeOff {
		t.Fatalf("expected second F2 press to wrap to off, got %v", got)
	}
}

func TestVoxelRtSystemOnlyMarksTransformDirtyOnRealChanges(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	server := newVoxelRtAssetServerTest(t)
	state := newVoxelRtStateTest()

	modelID := server.CreateVoxelModel(VoxModel{
		SizeX: 1,
		SizeY: 1,
		SizeZ: 1,
		Voxels: []Voxel{
			{X: 0, Y: 0, Z: 0, ColorIndex: 1},
		},
	}, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{128, 64, 32, 255})

	transform := &TransformComponent{
		Position: mgl32.Vec3{1, 2, 3},
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}
	vox := &VoxelModelComponent{
		VoxelModel:   modelID,
		VoxelPalette: paletteID,
	}
	cmd.AddEntity(transform, vox)
	app.FlushCommands()
	frameTime := &Time{Dt: 1.0 / 60.0}

	voxelRtSystem(nil, state, server, frameTime, cmd, nil)

	if len(state.instanceMap) != 1 {
		t.Fatalf("expected one synced instance, got %d", len(state.instanceMap))
	}
	var obj *core.VoxelObject
	for _, synced := range state.instanceMap {
		obj = synced
	}
	if obj == nil {
		t.Fatalf("expected synced object")
	}

	obj.Transform.Dirty = false
	voxelRtSystem(nil, state, server, frameTime, cmd, nil)
	if obj.Transform.Dirty {
		t.Fatalf("expected steady-state sync to keep transform clean")
	}

	obj.Transform.Position = mgl32.Vec3{4, 5, 6}
	obj.Transform.Dirty = false
	voxelRtSystem(nil, state, server, frameTime, cmd, nil)
	if !obj.Transform.Dirty {
		t.Fatalf("expected transform change to mark renderer transform dirty")
	}
}

func TestVoxelRtSystemRebuildsMaterialTableWhenPaletteMutatesInPlace(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	server := newVoxelRtAssetServerTest(t)
	state := newVoxelRtStateTest()

	modelID := server.CreateVoxelModel(VoxModel{
		SizeX: 1,
		SizeY: 1,
		SizeZ: 1,
		Voxels: []Voxel{
			{X: 0, Y: 0, Z: 0, ColorIndex: 1},
		},
	}, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{32, 64, 128, 255})

	transform := &TransformComponent{
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}
	vox := &VoxelModelComponent{
		VoxelModel:   modelID,
		VoxelPalette: paletteID,
	}
	cmd.AddEntity(transform, vox)
	app.FlushCommands()
	frameTime := &Time{Dt: 1.0 / 60.0}

	voxelRtSystem(nil, state, server, frameTime, cmd, nil)

	var obj *core.VoxelObject
	for _, synced := range state.instanceMap {
		obj = synced
	}
	if obj == nil || len(obj.MaterialTable) <= 1 {
		t.Fatalf("expected synced object with material table")
	}
	initialMaterialPtr := &obj.MaterialTable[0]
	initialColor := obj.MaterialTable[1].BaseColor

	unchanged := server.voxPalettes[paletteID]
	unchanged.VoxPalette[1] = [4]uint8{200, 100, 50, 255}
	server.voxPalettes[paletteID] = unchanged

	voxelRtSystem(nil, state, server, frameTime, cmd, nil)

	if &obj.MaterialTable[0] == initialMaterialPtr {
		t.Fatalf("expected material table slice to be replaced after palette mutation")
	}
	if obj.MaterialTable[1].BaseColor == initialColor {
		t.Fatalf("expected material table contents to reflect mutated palette")
	}
}

func TestVoxelRtSystemCopiesAmbientOcclusionModeToRendererObject(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	server := newVoxelRtAssetServerTest(t)
	state := newVoxelRtStateTest()

	modelID := server.CreateVoxelModel(VoxModel{
		SizeX: 1,
		SizeY: 1,
		SizeZ: 1,
		Voxels: []Voxel{
			{X: 0, Y: 0, Z: 0, ColorIndex: 1},
		},
	}, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{64, 96, 128, 255})

	cmd.AddEntity(
		&TransformComponent{
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&VoxelModelComponent{
			VoxelModel:           modelID,
			VoxelPalette:         paletteID,
			AmbientOcclusionMode: VoxelAODisabled,
		},
	)
	app.FlushCommands()

	voxelRtSystem(nil, state, server, &Time{Dt: 1.0 / 60.0}, cmd, nil)

	if len(state.instanceMap) != 1 {
		t.Fatalf("expected one synced instance, got %d", len(state.instanceMap))
	}
	for _, obj := range state.instanceMap {
		if obj.AmbientOcclusionMode != core.AmbientOcclusionModeDisabled {
			t.Fatalf("expected renderer object AO mode %d, got %d", core.AmbientOcclusionModeDisabled, obj.AmbientOcclusionMode)
		}
	}
}

func TestVoxelRtSystemDefaultsAmbientOcclusionModeToInherited(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	server := newVoxelRtAssetServerTest(t)
	state := newVoxelRtStateTest()

	modelID := server.CreateVoxelModel(VoxModel{
		SizeX: 1,
		SizeY: 1,
		SizeZ: 1,
		Voxels: []Voxel{
			{X: 0, Y: 0, Z: 0, ColorIndex: 1},
		},
	}, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{96, 64, 128, 255})

	cmd.AddEntity(
		&TransformComponent{
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&VoxelModelComponent{
			VoxelModel:   modelID,
			VoxelPalette: paletteID,
		},
	)
	app.FlushCommands()

	voxelRtSystem(nil, state, server, &Time{Dt: 1.0 / 60.0}, cmd, nil)

	if len(state.instanceMap) != 1 {
		t.Fatalf("expected one synced instance, got %d", len(state.instanceMap))
	}
	for _, obj := range state.instanceMap {
		if obj.AmbientOcclusionMode != core.AmbientOcclusionModeDefault {
			t.Fatalf("expected default renderer AO mode %d, got %d", core.AmbientOcclusionModeDefault, obj.AmbientOcclusionMode)
		}
	}
}

func TestEntityLODSelectionSystemUpdatesRuntimeSelection(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	state := newVoxelRtStateTest()

	cmd.AddEntity(&CameraComponent{
		Position: mgl32.Vec3{0, 0, 0},
		LookAt:   mgl32.Vec3{0, 0, -1},
		Up:       mgl32.Vec3{0, 1, 0},
		Fov:      60,
		Aspect:   1,
		Near:     0.1,
		Far:      1000,
	})
	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, -120},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&EntityLODComponent{
			Bands: []EntityLODBand{
				{MaxDistance: 50, Representation: EntityLODRepresentationFullVoxel},
				{MaxDistance: 100, Representation: EntityLODRepresentationSimplifiedVoxel},
				{MaxDistance: 0, Representation: EntityLODRepresentationImpostor},
			},
		},
	)
	app.FlushCommands()

	entityLODSelectionSystem(cmd, state)

	found := false
	MakeQuery1[EntityLODComponent](cmd).Map(func(entityId EntityId, lod *EntityLODComponent) bool {
		found = true
		if !lod.SelectionValid {
			t.Fatalf("expected runtime selection to be valid")
		}
		if lod.ActiveBandIndex != 2 {
			t.Fatalf("expected far band index 2, got %d", lod.ActiveBandIndex)
		}
		if lod.ActiveRepresentation != EntityLODRepresentationImpostor {
			t.Fatalf("expected impostor representation, got %v", lod.ActiveRepresentation)
		}
		if lod.ActiveDistance < 119.9 || lod.ActiveDistance > 120.1 {
			t.Fatalf("expected active distance near 120, got %v", lod.ActiveDistance)
		}
		return false
	})
	if !found {
		t.Fatalf("expected entity LOD component to exist")
	}
}

func TestVoxelRtSystemCapturesEntityLODSelectionForVoxelEntities(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	server := newVoxelRtAssetServerTest(t)
	state := newVoxelRtStateTest()

	modelID := server.CreateVoxelModel(VoxModel{
		SizeX: 1,
		SizeY: 1,
		SizeZ: 1,
		Voxels: []Voxel{
			{X: 0, Y: 0, Z: 0, ColorIndex: 1},
		},
	}, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{96, 160, 224, 255})

	cmd.AddEntity(&CameraComponent{
		Position: mgl32.Vec3{0, 0, 0},
		LookAt:   mgl32.Vec3{0, 0, -1},
		Up:       mgl32.Vec3{0, 1, 0},
		Fov:      60,
		Aspect:   1,
		Near:     0.1,
		Far:      1000,
	})
	eid := cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, -75},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&VoxelModelComponent{
			VoxelModel:   modelID,
			VoxelPalette: paletteID,
		},
		&EntityLODComponent{
			Bands: []EntityLODBand{
				{MaxDistance: 25, Representation: EntityLODRepresentationFullVoxel},
				{MaxDistance: 50, Representation: EntityLODRepresentationSimplifiedVoxel},
				{MaxDistance: 0, Representation: EntityLODRepresentationImpostor},
			},
		},
	)
	app.FlushCommands()

	entityLODSelectionSystem(cmd, state)
	voxelRtSystem(nil, state, server, &Time{Dt: 1.0 / 60.0}, cmd, nil)

	selection, ok := state.entityLODSelections[eid]
	if !ok {
		t.Fatalf("expected voxel runtime state to capture entity LOD selection")
	}
	if selection.BandIndex != 2 {
		t.Fatalf("expected band index 2, got %d", selection.BandIndex)
	}
	if selection.Representation != EntityLODRepresentationImpostor {
		t.Fatalf("expected impostor representation, got %v", selection.Representation)
	}
}

func TestVoxelRtSystemUsesSimplifiedGeometryForSimplifiedLOD(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	server := newVoxelRtAssetServerTest(t)
	state := newVoxelRtStateTest()

	modelID := server.CreateCubeModel(8, 8, 8, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{120, 220, 160, 255})

	cmd.AddEntity(&CameraComponent{
		Position: mgl32.Vec3{0, 0, 0},
		LookAt:   mgl32.Vec3{0, 0, -1},
		Up:       mgl32.Vec3{0, 1, 0},
		Fov:      60,
		Aspect:   1,
		Near:     0.1,
		Far:      1000,
	})
	eid := cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, -75},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&VoxelModelComponent{
			VoxelModel:   modelID,
			VoxelPalette: paletteID,
			PivotMode:    PivotModeCenter,
		},
		&EntityLODComponent{
			Bands: []EntityLODBand{
				{MaxDistance: 25, Representation: EntityLODRepresentationFullVoxel},
				{MaxDistance: 100, Representation: EntityLODRepresentationSimplifiedVoxel},
				{MaxDistance: 0, Representation: EntityLODRepresentationImpostor},
			},
		},
	)
	app.FlushCommands()

	entityLODSelectionSystem(cmd, state)
	voxelRtSystem(nil, state, server, &Time{Dt: 1.0 / 60.0}, cmd, nil)

	obj, ok := state.instanceMap[eid]
	if !ok || obj == nil {
		t.Fatal("expected voxel object to remain synced")
	}
	source, ok := server.GetVoxelGeometry(modelID)
	if !ok || source.XBrickMap == nil {
		t.Fatal("expected source geometry")
	}
	simplifiedID, simplified, ok := server.entityLODSimplifiedGeometry(modelID, paletteID, &source)
	if !ok || simplified == nil || simplified.XBrickMap == nil {
		t.Fatal("expected simplified geometry")
	}
	if obj.XBrickMap != simplified.XBrickMap {
		t.Fatal("expected renderer object to swap to simplified geometry")
	}
	if obj.Transform.Scale.X() <= VoxelSize {
		t.Fatalf("expected simplified render scale compensation above base voxel size, got %v", obj.Transform.Scale)
	}
	if _, exists := state.loadedModels[simplifiedID]; !exists {
		t.Fatal("expected simplified geometry template to be cached")
	}
	if len(state.runtimeSprites) != 0 {
		t.Fatalf("expected simplified voxel path to avoid runtime sprites, got %d", len(state.runtimeSprites))
	}
}

func TestVoxelRtSystemUsesRuntimeImpostorSpritesForFarLOD(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	server := newVoxelRtAssetServerTest(t)
	state := newVoxelRtStateTest()

	modelID := server.CreateFrameModel(12, 18, 12, 2, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{112, 206, 255, 255})

	cmd.AddEntity(&CameraComponent{
		Position: mgl32.Vec3{0, 0, 0},
		LookAt:   mgl32.Vec3{0, 0, -1},
		Up:       mgl32.Vec3{0, 1, 0},
		Fov:      60,
		Aspect:   1,
		Near:     0.1,
		Far:      1000,
	})
	eid := cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, -150},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&VoxelModelComponent{
			VoxelModel:   modelID,
			VoxelPalette: paletteID,
			PivotMode:    PivotModeCenter,
		},
		&EntityLODComponent{
			Bands: []EntityLODBand{
				{MaxDistance: 50, Representation: EntityLODRepresentationFullVoxel},
				{MaxDistance: 100, Representation: EntityLODRepresentationSimplifiedVoxel},
				{MaxDistance: 0, Representation: EntityLODRepresentationImpostor},
			},
		},
	)
	app.FlushCommands()

	entityLODSelectionSystem(cmd, state)
	voxelRtSystem(nil, state, server, &Time{Dt: 1.0 / 60.0}, cmd, nil)

	if _, ok := state.instanceMap[eid]; ok {
		t.Fatal("expected impostor LOD to suppress voxel object sync")
	}
	if len(state.runtimeSprites) != 1 {
		t.Fatalf("expected one runtime sprite, got %d", len(state.runtimeSprites))
	}
	sprite := state.runtimeSprites[0]
	if sprite.Texture == (AssetId{}) {
		t.Fatal("expected runtime impostor sprite to have a texture")
	}
	if sprite.BillboardMode != BillboardSpherical {
		t.Fatalf("expected spherical billboard, got %v", sprite.BillboardMode)
	}
	if sprite.Unlit {
		t.Fatal("expected impostor sprite to use lit world-sprite shading")
	}
	if sprite.Size[0] <= 0 || sprite.Size[1] <= 0 {
		t.Fatalf("expected positive sprite size, got %v", sprite.Size)
	}
	spriteBytes, spriteCount, batches := spritesSync(state, cmd)
	if len(spriteBytes) == 0 || spriteCount != 1 || len(batches) != 1 {
		t.Fatalf("expected runtime sprite sync output, got bytes=%d count=%d batches=%d", len(spriteBytes), spriteCount, len(batches))
	}
}

func TestVoxelRtSystemFallsBackToDotSpritesWhenImpostorGenerationFails(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	server := newVoxelRtAssetServerTest(t)
	state := newVoxelRtStateTest()

	modelID := server.CreateVoxelModel(VoxModel{}, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{255, 120, 232, 255})

	cmd.AddEntity(&CameraComponent{
		Position: mgl32.Vec3{0, 0, 0},
		LookAt:   mgl32.Vec3{0, 0, -1},
		Up:       mgl32.Vec3{0, 1, 0},
		Fov:      60,
		Aspect:   1,
		Near:     0.1,
		Far:      1000,
	})
	eid := cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, -150},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&VoxelModelComponent{
			VoxelModel:   modelID,
			VoxelPalette: paletteID,
			PivotMode:    PivotModeCenter,
		},
		&EntityLODComponent{
			Bands: []EntityLODBand{
				{MaxDistance: 10, Representation: EntityLODRepresentationFullVoxel},
				{MaxDistance: 0, Representation: EntityLODRepresentationImpostor},
			},
		},
	)
	app.FlushCommands()

	entityLODSelectionSystem(cmd, state)
	voxelRtSystem(nil, state, server, &Time{Dt: 1.0 / 60.0}, cmd, nil)

	if _, ok := state.instanceMap[eid]; ok {
		t.Fatal("expected far fallback path to suppress voxel object sync")
	}
	if len(state.runtimeSprites) != 1 {
		t.Fatalf("expected one dot fallback sprite, got %d", len(state.runtimeSprites))
	}
	sprite := state.runtimeSprites[0]
	if sprite.Texture != server.entityLODDotTexture() {
		t.Fatal("expected fallback sprite to use shared dot texture")
	}
	if sprite.Color[0] >= 1 || sprite.Color[1] >= 1 || sprite.Color[2] >= 1 {
		t.Fatalf("expected dot fallback sprite brightness tint below full white, got %v", sprite.Color)
	}
	if sprite.Size[0] < 2*VoxelSize || sprite.Size[1] < 2*VoxelSize {
		t.Fatalf("expected clamped dot sprite size, got %v", sprite.Size)
	}
}

func TestEntityLODImpostorBaseSizeUsesFull3DBounds(t *testing.T) {
	server := newVoxelRtAssetServerTest(t)
	modelID := server.CreateCubeModel(2, 2, 40, 1.0)
	source, ok := server.GetVoxelGeometry(modelID)
	if !ok || source.XBrickMap == nil {
		t.Fatal("expected source geometry")
	}

	vox := &VoxelModelComponent{VoxelModel: modelID, PivotMode: PivotModeCenter}
	transform := &TransformComponent{Scale: mgl32.Vec3{1, 1, 1}}
	size := entityLODImpostorBaseSize(vox, transform, &source)
	extentX, extentY, extentZ := entityLODGeometryExtents(&source)
	baseScale := EffectiveVoxelScale(vox, transform)
	worldX := float32(math.Abs(float64(baseScale.X()))) * extentX
	worldY := float32(math.Abs(float64(baseScale.Y()))) * extentY
	worldZ := float32(math.Abs(float64(baseScale.Z()))) * extentZ
	want := float32(math.Sqrt(float64(worldX*worldX+worldY*worldY+worldZ*worldZ))) * 1.1
	if math.Abs(float64(size-want)) > 1e-4 {
		t.Fatalf("expected billboard size %v from 3D bounds, got %v", want, size)
	}
	if size <= max(worldX, worldY)*1.1 {
		t.Fatalf("expected billboard size to include the dominant Z extent, got %v", size)
	}
}

func TestEntityLODImpostorSpriteSizePreservesProjectedAspect(t *testing.T) {
	server := newVoxelRtAssetServerTest(t)
	modelID := server.CreateCubeModel(24, 8, 8, 1.0)
	source, ok := server.GetVoxelGeometry(modelID)
	if !ok || source.XBrickMap == nil {
		t.Fatal("expected source geometry")
	}

	size := entityLODImpostorSpriteSize(
		&VoxelModelComponent{VoxelModel: modelID, PivotMode: PivotModeCenter},
		&TransformComponent{Scale: mgl32.Vec3{1, 1, 1}},
		&source,
	)
	if size[0] <= size[1] {
		t.Fatalf("expected wide model impostor sprite to preserve projected aspect, got %v", size)
	}
}

func TestEntityLODImpostorBrightnessTintUsesDirectionalFacing(t *testing.T) {
	state := &VoxelRtState{
		RtApp: &app_rt.App{
			Scene: core.NewScene(),
		},
	}
	state.RtApp.Scene.AmbientLight = mgl32.Vec3{0.1, 0.1, 0.1}
	state.RtApp.Scene.Lights = []core.Light{
		{
			Direction: [4]float32{0, 0, -1, 0},
			Color:     [4]float32{1, 1, 1, 0.8},
			Params:    [4]float32{0, 0, float32(LightTypeDirectional), 0},
		},
	}

	facing := entityLODImpostorBrightnessTint(state, &TransformComponent{Rotation: mgl32.QuatIdent()})
	turned := entityLODImpostorBrightnessTint(state, &TransformComponent{
		Rotation: mgl32.QuatRotate(mgl32.DegToRad(180), mgl32.Vec3{0, 1, 0}),
	})
	if facing <= turned {
		t.Fatalf("expected front-facing impostor tint %v to exceed turned-away tint %v", facing, turned)
	}
}

func TestEntityLODDotBrightnessTintStaysBelowImpostorTint(t *testing.T) {
	state := &VoxelRtState{
		RtApp: &app_rt.App{
			Scene: core.NewScene(),
		},
	}
	state.RtApp.Scene.AmbientLight = mgl32.Vec3{0.1, 0.1, 0.1}
	state.RtApp.Scene.Lights = []core.Light{
		{
			Direction: [4]float32{0, 0, -1, 0},
			Color:     [4]float32{1, 1, 1, 0.8},
			Params:    [4]float32{0, 0, float32(LightTypeDirectional), 0},
		},
	}

	transform := &TransformComponent{Rotation: mgl32.QuatIdent()}
	impostor := entityLODImpostorBrightnessTint(state, transform)
	dot := entityLODDotBrightnessTint(state, transform)
	if dot >= impostor {
		t.Fatalf("expected dot tint %v to stay below impostor tint %v", dot, impostor)
	}
	if dot > 0.6 {
		t.Fatalf("expected dot tint to be capped, got %v", dot)
	}
}

func TestSpriteAtlasTextureLooksUpTextureByAtlasKey(t *testing.T) {
	server := newVoxelRtAssetServerTest(t)
	atlasID := server.CreateTextureFromTexels(
		[]uint8{
			255, 0, 0, 255,
			0, 255, 0, 255,
			0, 0, 255, 255,
			255, 255, 255, 255,
		},
		2, 2, 1, TextureDimension2D, TextureFormatRGBA8Unorm,
	)

	texAsset, ok := spriteAtlasTexture(server, spriteAtlasKey(atlasID))
	if !ok {
		t.Fatalf("expected atlas lookup to succeed")
	}
	if texAsset.Width != 2 || texAsset.Height != 2 {
		t.Fatalf("expected 2x2 texture, got %dx%d", texAsset.Width, texAsset.Height)
	}
	if _, ok := spriteAtlasTexture(server, "not-a-valid-atlas-key"); ok {
		t.Fatalf("expected invalid atlas key lookup to fail")
	}
}

func TestVoxelObjectAllowsOcclusionKeepsTerrainAndGroupedChunksEligible(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	terrainEntity := cmd.AddEntity(
		&VoxelModelComponent{IsTerrainChunk: true, ShadowGroupID: 17},
		&AuthoredTerrainChunkRefComponent{},
	)
	importedEntity := cmd.AddEntity(
		&VoxelModelComponent{ShadowGroupID: 23},
		&AuthoredImportedWorldChunkRefComponent{},
	)
	app.FlushCommands()

	if !voxelObjectAllowsOcclusion(cmd, terrainEntity, &VoxelModelComponent{IsTerrainChunk: true, ShadowGroupID: 17}) {
		t.Fatal("expected terrain chunk to remain occlusion-eligible")
	}
	if !voxelObjectAllowsOcclusion(cmd, importedEntity, &VoxelModelComponent{ShadowGroupID: 23}) {
		t.Fatal("expected imported world chunk to remain occlusion-eligible")
	}
}

func TestBuildWaterSurfaceHostsNormalizesAndSortsResults(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{4, 3, 2},
			Scale:    mgl32.Vec3{2, 1, 3},
		},
		&WaterSurfaceComponent{
			HalfExtents:   [2]float32{2, 1},
			Depth:         2,
			FlowDirection: [2]float32{0, 3},
		},
	)
	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{1, 2, 3},
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&WaterSurfaceComponent{
			Disabled:    true,
			HalfExtents: [2]float32{3, 3},
			Depth:       1,
		},
	)
	app.FlushCommands()

	hosts, ripples := buildWaterSurfaceHosts(cmd, nil)
	if len(hosts) != 1 {
		t.Fatalf("expected one enabled water host, got %d", len(hosts))
	}
	if len(ripples) != 0 {
		t.Fatalf("expected no ripple hosts, got %d", len(ripples))
	}
	if hosts[0].Position != (mgl32.Vec3{4, 3, 2}) {
		t.Fatalf("unexpected host position: %v", hosts[0].Position)
	}
	if hosts[0].HalfExtents != ([2]float32{4, 3}) {
		t.Fatalf("unexpected host half extents: %v", hosts[0].HalfExtents)
	}
	if hosts[0].FlowDirection != ([2]float32{0, 1}) {
		t.Fatalf("unexpected normalized flow direction: %v", hosts[0].FlowDirection)
	}
}

func TestBuildWaterSurfaceHostsIncludesResolvedPatches(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{5, 2, 1}},
		&ResolvedWaterPatchComponent{
			Owner:           99,
			PatchIndex:      0,
			Kind:            WaterPatchKindSurface,
			Center:          mgl32.Vec3{5, 2, 1},
			HalfExtents:     [2]float32{3, 2},
			Depth:           4,
			Color:           [3]float32{0.1, 0.2, 0.3},
			AbsorptionColor: [3]float32{0.4, 0.5, 0.6},
			Opacity:         0.7,
			Roughness:       0.15,
			Refraction:      0.25,
			FlowDirection:   [2]float32{1, 0},
			FlowSpeed:       0.9,
			WaveAmplitude:   0.03,
		},
	)
	app.FlushCommands()

	hosts, _ := buildWaterSurfaceHosts(cmd, nil)
	if len(hosts) != 1 {
		t.Fatalf("expected one resolved water host, got %d", len(hosts))
	}
	if hosts[0].Position != (mgl32.Vec3{5, 2, 1}) || hosts[0].Depth != 4 || hosts[0].HalfExtents != ([2]float32{3, 2}) {
		t.Fatalf("unexpected resolved host %+v", hosts[0])
	}
}

func TestBuildPlanetBodyHostsNormalizesAndSortsResults(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{8, 9, 10},
			Scale:    mgl32.Vec3{2, 2, 2},
			Rotation: mgl32.QuatRotate(mgl32.DegToRad(15), mgl32.Vec3{0, 1, 0}),
		},
		&PlanetBodyComponent{
			Radius:             20,
			OceanRadius:        21,
			AtmosphereRimWidth: 3,
			HeightAmplitude:    5,
			HandoffNearAlt:     9,
			HandoffFarAlt:      24,
		},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{1, 2, 3}},
		&PlanetBodyComponent{
			Disabled: true,
			Radius:   12,
		},
	)
	app.FlushCommands()

	hosts := buildPlanetBodyHosts(cmd)
	if len(hosts) != 1 {
		t.Fatalf("expected one enabled planet host, got %d", len(hosts))
	}
	if hosts[0].Position != (mgl32.Vec3{8, 9, 10}) {
		t.Fatalf("unexpected host position: %v", hosts[0].Position)
	}
	if hosts[0].Radius != 40 {
		t.Fatalf("expected scaled radius 40, got %v", hosts[0].Radius)
	}
	if hosts[0].OceanRadius != 42 {
		t.Fatalf("expected scaled ocean radius 42, got %v", hosts[0].OceanRadius)
	}
	if hosts[0].HeightAmplitude != 10 {
		t.Fatalf("expected scaled height amplitude 10, got %v", hosts[0].HeightAmplitude)
	}
	if hosts[0].AtmosphereRimWidth != 6 {
		t.Fatalf("expected scaled atmosphere rim width 6, got %v", hosts[0].AtmosphereRimWidth)
	}
	if hosts[0].HeightSteps != 6 {
		t.Fatalf("expected default height steps 6, got %d", hosts[0].HeightSteps)
	}
	if hosts[0].HandoffNearAlt != 18 {
		t.Fatalf("expected scaled handoff near altitude 18, got %v", hosts[0].HandoffNearAlt)
	}
	if hosts[0].HandoffFarAlt != 48 {
		t.Fatalf("expected scaled handoff far altitude 48, got %v", hosts[0].HandoffFarAlt)
	}
}

func newVoxelRtStateTest() *VoxelRtState {
	return &VoxelRtState{
		RtApp: &app_rt.App{
			Scene:    core.NewScene(),
			Camera:   core.NewCameraState(),
			Profiler: core.NewProfiler(),
		},
		loadedModels:        make(map[AssetId]*core.VoxelObject),
		instanceMap:         make(map[EntityId]*core.VoxelObject),
		entityLODSelections: make(map[EntityId]EntityLODSelection),
		runtimeSprites:      make([]SpriteComponent, 0, 8),
		lastMaterialKeys:    make(map[*core.VoxelObject]materialTableCacheKey),
		materialTableCache:  make(map[materialTableCacheKey][]core.Material),
		particlePools:       make(map[EntityId]*particlePool),
		caVolumeMap:         make(map[EntityId]*core.VoxelObject),
		objectToEntity:      make(map[*core.VoxelObject]EntityId),
		skyboxLayers:        make(map[EntityId]SkyboxLayerComponent),
	}
}

func newVoxelRtAssetServerTest(t *testing.T) *AssetServer {
	t.Helper()
	return &AssetServer{
		meshes:         make(map[AssetId]MeshAsset),
		materials:      make(map[AssetId]MaterialAsset),
		textures:       make(map[AssetId]TextureAsset),
		textureKeys:    make(map[string]AssetId),
		samplers:       make(map[AssetId]SamplerAsset),
		voxModels:      make(map[AssetId]VoxelGeometryAsset),
		voxModelKeys:   make(map[string]AssetId),
		voxPalettes:    make(map[AssetId]VoxelPaletteAsset),
		voxPaletteKeys: make(map[string]AssetId),
		voxFiles:       make(map[AssetId]*VoxFile),
	}
}
