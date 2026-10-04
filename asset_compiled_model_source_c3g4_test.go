package gekko

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
	"github.com/go-gl/mathgl/mgl32"
)

func c3g4Primary(bricks []voxelcodec.Brick) map[[3]int]uint8 {
	out := map[[3]int]uint8{}
	for _, b := range bricks {
		n := 0
		for i := 0; i < 512; i++ {
			if b.Occupancy[i/64]&(uint64(1)<<uint(i%64)) == 0 {
				continue
			}
			out[[3]int{int(b.Coord[0])*8 + i%8, int(b.Coord[1])*8 + (i/8)%8, int(b.Coord[2])*8 + i/64}] = b.Values[n]
			n++
		}
	}
	return out
}

func c3g4Parity(t *testing.T, def *content.AssetDef, part content.AssetPartDef, path string, wantRaw VoxModel, wantDeps []string) *compiledAssetModelSource {
	t.Helper()
	before, _ := json.Marshal(def)
	partBefore, _ := json.Marshal(part)
	built, err := compileAssetPartModel(def, part, path)
	if err != nil {
		t.Fatal(err)
	}
	if built == nil || built.definition == nil || built.definition.SchemaVersion != content.CurrentCompiledAssetModelSchemaVersion || built.definition.Lattice.VoxelResolution != part.VoxelResolution || built.definition.Lattice.RasterizationVersion != "gekko-compiled-model-v1" {
		t.Fatalf("model lattice/schema: %+v", built)
	}
	if built.definition.Dimensions != [3]uint32{wantRaw.SizeX, wantRaw.SizeY, wantRaw.SizeZ} || !reflect.DeepEqual(built.dependencies, wantDeps) {
		t.Fatalf("dimensions/dependencies: %+v", built)
	}
	assets := &AssetServer{}
	geometryID, paletteID, err := modelAndPaletteFromSource(assets, def, part, path)
	if err != nil {
		t.Fatal(err)
	}
	geometry, _ := assets.GetVoxelGeometry(geometryID)
	palette, _ := assets.GetVoxelPalette(paletteID)
	if !reflect.DeepEqual(c3g4Primary(built.definition.Bricks), c3h12aGeometry(geometry.XBrickMap)) {
		t.Fatal("compiler primary differs from actual public source")
	}
	if wantRaw.SizeX != 0 || wantRaw.SizeY != 0 || wantRaw.SizeZ != 0 {
		if geometry.LocalMin != (mgl32.Vec3{}) || geometry.LocalMax != (mgl32.Vec3{float32(wantRaw.SizeX), float32(wantRaw.SizeY), float32(wantRaw.SizeZ)}) {
			t.Fatal("declared bounds differ")
		}
	}
	c3g3JSONEqual(t, built.palette, palette)
	encoded, info, err := content.EncodeCompiledAssetModel(built.definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := compileAssetPartModel(def, part, path)
	if err != nil {
		t.Fatal(err)
	}
	again, otherInfo, err := content.EncodeCompiledAssetModel(other.definition, nil)
	if err != nil || info != otherInfo || !bytes.Equal(encoded, again) {
		t.Fatal("independent preparation nondeterministic", err)
	}
	if len(built.definition.Bricks) > 0 {
		built.definition.Bricks[0].Values[0] = 99
		built.definition.Bricks[0].Occupancy[0] = 0
	}
	if built.palette.SurfaceMaterials != nil {
		for key, facts := range built.palette.SurfaceMaterials {
			if len(facts.Tags) > 0 {
				facts.Tags[0] = "changed"
			}
			delete(built.palette.SurfaceMaterials, key)
			break
		}
	}
	if len(built.palette.Materials) > 0 {
		built.palette.Materials[0].Weight = 9
		built.palette.Materials[0].Property["changed"] = true
	}
	unchanged, unchangedInfo, err := content.EncodeCompiledAssetModel(other.definition, nil)
	if err != nil || !bytes.Equal(unchanged, again) || unchangedInfo != otherInfo {
		t.Fatal("independent model storage shared", err)
	}
	c3g3JSONEqual(t, other.palette, palette)
	after, _ := json.Marshal(def)
	partAfter, _ := json.Marshal(part)
	if !bytes.Equal(before, after) || !bytes.Equal(partBefore, partAfter) {
		t.Fatal("compiler changed authoring input")
	}
	return other
}

func TestC3g4ProceduralAndVoxSourceParity(t *testing.T) {
	def, part, _, _, _ := c3g3Inputs()
	part.VoxelResolution = .125
	path := filepath.Join(t.TempDir(), "asset.gkasset")
	for _, kind := range c3g1Kinds {
		t.Run(kind, func(t *testing.T) {
			p := part
			p.Source.Kind = content.AssetSourceKindProceduralPrimitive
			p.Source.Primitive = kind
			p.Source.Params = c3g1Params()
			p.ModelScale = 1.5
			raw, err := buildProceduralPrimitiveModel(kind, p.Source.Params, p.ModelScale)
			if err != nil {
				t.Fatal(err)
			}
			c3g4Parity(t, def, p, path, raw, nil)
		})
	}
	voxPath := filepath.Join(filepath.Dir(path), "models.vox")
	writeNamedSceneVoxFixture(t, voxPath)
	file, err := LoadVoxFile(voxPath)
	if err != nil {
		t.Fatal(err)
	}
	part.Source = content.AssetSourceDef{Kind: content.AssetSourceKindVoxModel, Path: "models.vox", ModelIndex: 1, MaterialID: "m"}
	part.ModelScale = 1.5
	c3g4Parity(t, def, part, path, ScaleVoxModel(file.Models[1], 1.5), []string{voxPath})
	part.Source.Kind = content.AssetSourceKindVoxSceneNode
	part.Source.NodeName = "arm"
	part.Source.ModelIndex = 1
	// Named arm has translation: selected model primary must remain local, not baked.
	c3g4Parity(t, def, part, path, ScaleVoxModel(file.Models[1], 1.5), []string{voxPath})
}

func c3g4Write(t *testing.T, path string, model VoxModel, nodes []syntheticVoxNodeChunk, withMaterial bool) {
	t.Helper()
	var file bytes.Buffer
	file.WriteString(VOXMagicNumber)
	writeInt32ForVoxFixture(t, &file, 150)
	writeChunkForVoxFixture(t, &file, "MAIN", nil, 0)
	writeChunkForVoxFixture(t, &file, "SIZE", synthSizeChunkData(model), 0)
	writeChunkForVoxFixture(t, &file, "XYZI", synthXYZIChunkData(model), 0)
	if withMaterial {
		var b bytes.Buffer
		writeUint32ForVoxFixture(&b, 3)
		b.Write(synthDICTData(map[string]string{"_type": "_metal", "_rough": "0.25"}))
		writeChunkForVoxFixture(t, &file, "MATL", b.Bytes(), 0)
	}
	for _, node := range nodes {
		writeChunkForVoxFixture(t, &file, node.id, node.data, 0)
	}
	if err := os.WriteFile(path, file.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestC3g4DownscaleVotesAndOriginalSurfaceIndices(t *testing.T) {
	def, part, _, _, _ := c3g3Inputs()
	path := filepath.Join(t.TempDir(), "asset.gkasset")
	voxPath := filepath.Join(filepath.Dir(path), "votes.vox")
	part.Source = content.AssetSourceDef{Kind: content.AssetSourceKindVoxModel, Path: "votes.vox", MaterialID: "m"}
	part.ModelScale = .25
	part.VoxelResolution = .125
	model := VoxModel{SizeX: 4, SizeY: 1, SizeZ: 1, Voxels: []Voxel{{ColorIndex: 3}, {X: 1, ColorIndex: 3}, {X: 2, ColorIndex: 9}, {X: 3, ColorIndex: 0}}}
	c3g4Write(t, voxPath, model, nil, true)
	built := c3g4Parity(t, def, part, path, ScaleVoxModel(model, .25), []string{voxPath})
	if len(c3g4Primary(built.definition.Bricks)) != 1 || c3g4Primary(built.definition.Bricks)[[3]int{}] != 3 {
		t.Fatal("lower-count tie rejected or winner changed")
	}
	if _, ok := built.palette.SurfaceMaterials[9]; !ok {
		t.Fatal("pre-scale discarded color lost surface intent")
	}
	if _, ok := built.palette.SurfaceMaterials[0]; ok {
		t.Fatal("zero color gained surface intent")
	}
	for name, voxels := range map[string][]Voxel{"top-colors": {{ColorIndex: 3}, {X: 1, ColorIndex: 9}}, "top-zero": {{ColorIndex: 3}, {X: 1, ColorIndex: 0}}, "clamped-top": {{X: 4, ColorIndex: 3}, {X: 5, ColorIndex: 9}}} {
		t.Run(name, func(t *testing.T) {
			m := model
			m.SizeX = 1
			m.Voxels = voxels
			c3g4Write(t, voxPath, m, nil, false)
			if out, err := compileAssetPartModel(def, part, path); err == nil || out != nil {
				t.Fatal("nondeterministic highest vote tie accepted")
			}
		})
	}
	// Above declared dimensions is legal primary at identity scale.
	model.SizeX = 1
	model.Voxels = []Voxel{{X: 4, ColorIndex: 3}}
	part.ModelScale = 1
	c3g4Write(t, voxPath, model, nil, false)
	c3g4Parity(t, def, part, path, model, []string{voxPath})
}

func TestC3g4SourceErrorsWithoutPartialResult(t *testing.T) {
	def, part, _, _, _ := c3g3Inputs()
	path := filepath.Join(t.TempDir(), "asset.gkasset")
	voxPath := filepath.Join(filepath.Dir(path), "valid.vox")
	writeNamedSceneVoxFixture(t, voxPath)
	part.Source = content.AssetSourceDef{Kind: content.AssetSourceKindVoxModel, Path: "valid.vox"}
	part.ModelScale = 1
	part.VoxelResolution = .125
	for name, change := range map[string]func(*content.AssetPartDef){
		"kind":           func(p *content.AssetPartDef) { p.Source.Kind = content.AssetSourceKindGroup },
		"missing":        func(p *content.AssetPartDef) { p.Source.Path = "missing.vox" },
		"index-negative": func(p *content.AssetPartDef) { p.Source.ModelIndex = -1 },
		"index-range":    func(p *content.AssetPartDef) { p.Source.ModelIndex = 99 },
		"material":       func(p *content.AssetPartDef) { p.Source.MaterialID = "missing" },
		"primitive": func(p *content.AssetPartDef) {
			p.Source.Kind = content.AssetSourceKindProceduralPrimitive
			p.Source.Primitive = "invalid"
		},
		"scale-zero":          func(p *content.AssetPartDef) { p.ModelScale = 0 },
		"scale-negative":      func(p *content.AssetPartDef) { p.ModelScale = -1 },
		"scale-nan":           func(p *content.AssetPartDef) { p.ModelScale = float32(math.NaN()) },
		"scale-inf":           func(p *content.AssetPartDef) { p.ModelScale = float32(math.Inf(1)) },
		"resolution-zero":     func(p *content.AssetPartDef) { p.VoxelResolution = 0 },
		"resolution-negative": func(p *content.AssetPartDef) { p.VoxelResolution = -1 },
		"resolution-nan":      func(p *content.AssetPartDef) { p.VoxelResolution = float32(math.NaN()) },
		"resolution-inf":      func(p *content.AssetPartDef) { p.VoxelResolution = float32(math.Inf(1)) },
		"scene-name": func(p *content.AssetPartDef) {
			p.Source.Kind = content.AssetSourceKindVoxSceneNode
			p.Source.NodeName = "missing"
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := part
			change(&p)
			if out, err := compileAssetPartModel(def, p, path); err == nil || out != nil {
				t.Fatal("invalid source accepted or partial")
			}
		})
	}
	for name, value := range map[string]float32{"zero": 0, "negative": -1, "nan": float32(math.NaN()), "inf": float32(math.Inf(1))} {
		t.Run("procedural-param-"+name, func(t *testing.T) {
			p := part
			p.Source = content.AssetSourceDef{Kind: content.AssetSourceKindProceduralPrimitive, Primitive: "cube", Params: c3g1Params()}
			p.Source.Params["sx"] = value
			if out, err := compileAssetPartModel(def, p, path); err == nil || out != nil {
				t.Fatal("invalid used procedural parameter accepted")
			}
		})
	}
	corrupt := filepath.Join(filepath.Dir(path), "bad.vox")
	if err := os.WriteFile(corrupt, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	part.Source.Path = "bad.vox"
	if out, err := compileAssetPartModel(def, part, path); err == nil || out != nil {
		t.Fatal("corrupt file accepted")
	}
	for name, nodes := range map[string][]syntheticVoxNodeChunk{"cycle": {{id: "nTRN", data: synthTransformNodeData(0, map[string]string{"_name": "bad"}, 0, 0, 0, 0)}}, "model-range": {{id: "nTRN", data: synthTransformNodeData(0, map[string]string{"_name": "bad"}, 1, 0, 0, 0)}, {id: "nSHP", data: synthShapeNodeData(1, nil, []int{99})}}} {
		t.Run(name, func(t *testing.T) {
			c3g4Write(t, voxPath, VoxModel{SizeX: 1, SizeY: 1, SizeZ: 1, Voxels: []Voxel{{ColorIndex: 1}}}, nodes, false)
			p := part
			p.Source = content.AssetSourceDef{Kind: content.AssetSourceKindVoxSceneNode, Path: "valid.vox", NodeName: "bad"}
			if out, err := compileAssetPartModel(def, p, path); err == nil || out != nil {
				t.Fatal("malformed scene accepted")
			}
			p.Source.Kind = content.AssetSourceKindVoxModel
			c3g4Parity(t, def, p, path, VoxModel{SizeX: 1, SizeY: 1, SizeZ: 1, Voxels: []Voxel{{ColorIndex: 1}}}, []string{voxPath})
		})
	}
}
