package hl1

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	importcommon "github.com/gekko3d/gekko/importers/common"
)

func TestBuildGameAssetImportCatalogsAndCopiesMapAssets(t *testing.T) {
	dir := t.TempDir()
	gameDir := filepath.Join(dir, "hl")
	outDir := filepath.Join(dir, "out")
	wadPath := filepath.Join(gameDir, "valve", "halflife.wad")
	modelPath := filepath.Join(gameDir, "valve", "models", "w_9mmhandgun.mdl")
	spritePath := filepath.Join(gameDir, "valve", "sprites", "glow01.spr")
	soundPath := filepath.Join(gameDir, "valve", "sound", "buttons", "bell1.wav")
	mustWriteFile(t, wadPath, []byte("wad"))
	mustWriteFile(t, modelPath, syntheticMDL())
	mustWriteFile(t, spritePath, syntheticSPR())
	mustWriteFile(t, soundPath, []byte("sound"))

	summary := ImportSummary{
		Map: importcommon.MapImport{
			Entities: []importcommon.Entity{
				{
					ClassName: "worldspawn",
					KeyValues: map[string]string{
						"wad": "\\quiver\\valve\\halflife.wad",
					},
				},
				{
					ClassName: "weapon_9mmhandgun",
				},
				{
					ClassName: "env_sprite",
					KeyValues: map[string]string{
						"model": "sprites/glow01.spr",
					},
				},
				{
					ClassName: "ambient_generic",
					KeyValues: map[string]string{
						"message": "buttons/bell1.wav",
					},
				},
			},
		},
		Report: importcommon.ImportReport{
			Source: importcommon.SourceInfo{
				Kind:     "hl1",
				GameDir:  gameDir,
				MapName:  "crossfire",
				WADPaths: []string{wadPath},
			},
		},
	}
	result, err := BuildGameAssetImport(ImportOptions{
		GameDir:                  gameDir,
		OutputRoot:               outDir,
		VoxelResolution:          0.2,
		GameAssetVoxelResolution: 0.07,
		PickupVoxelResolution:    0.03,
	}, summary)
	if err != nil {
		t.Fatalf("BuildGameAssetImport failed: %v", err)
	}
	if len(result.Manifest.Assets) != 4 {
		t.Fatalf("assets = %+v", result.Manifest.Assets)
	}
	assertHL1AssetEntry(t, result.Manifest.Assets, "wad", wadPath, "used_for_texture_bake")
	modelEntry := assertHL1AssetEntry(t, result.Manifest.Assets, "model", modelPath, "generated_voxel_asset")
	if modelEntry.ModelInfo == nil || modelEntry.ModelInfo.TextureCount != 1 || modelEntry.ModelInfo.BodyPartCount != 1 {
		t.Fatalf("expected parsed model info, got %+v", modelEntry.ModelInfo)
	}
	if modelEntry.GeneratedAssetPath == "" || modelEntry.GeneratedVoxelCount == 0 {
		t.Fatalf("expected generated voxel asset metadata, got %+v", modelEntry)
	}
	if modelEntry.GeneratedVoxelResolution != 0.03 {
		t.Fatalf("expected pickup model voxel resolution 0.03, got %+v", modelEntry)
	}
	if modelEntry.generatedAsset == nil || len(modelEntry.generatedAsset.Parts) != 1 || modelEntry.generatedAsset.Parts[0].VoxelResolution != 0.03 {
		t.Fatalf("expected generated pickup model asset resolution 0.03, got %+v", modelEntry.generatedAsset)
	}
	spriteEntry := assertHL1AssetEntry(t, result.Manifest.Assets, "sprite", spritePath, "generated_voxel_asset")
	if spriteEntry.SpriteInfo == nil || spriteEntry.SpriteInfo.FrameCount != 1 || spriteEntry.GeneratedAssetPath == "" || spriteEntry.GeneratedVoxelCount == 0 {
		t.Fatalf("expected generated sprite asset metadata, got %+v", spriteEntry)
	}
	if spriteEntry.GeneratedVoxelResolution != 0.07 {
		t.Fatalf("expected generic game asset voxel resolution 0.07, got %+v", spriteEntry)
	}
	assertHL1AssetEntry(t, result.Manifest.Assets, "sound", soundPath, "cataloged_source_only")
	if len(result.Manifest.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", result.Manifest.Diagnostics)
	}
	if err := SaveGameAssetImport(result); err != nil {
		t.Fatalf("SaveGameAssetImport failed: %v", err)
	}
	if _, err := os.Stat(result.ManifestPath); err != nil {
		t.Fatalf("expected manifest %s: %v", result.ManifestPath, err)
	}
	for _, entry := range result.Manifest.Assets {
		if !entry.Resolved {
			continue
		}
		if _, err := os.Stat(entry.OutputPath); err != nil {
			t.Fatalf("expected copied asset %s: %v", entry.OutputPath, err)
		}
		if entry.GeneratedAssetPath != "" {
			if _, err := os.Stat(entry.GeneratedAssetPath); err != nil {
				t.Fatalf("expected generated asset %s: %v", entry.GeneratedAssetPath, err)
			}
		}
	}
}

func TestBuildGameAssetImportReportsMissingReferences(t *testing.T) {
	dir := t.TempDir()
	gameDir := filepath.Join(dir, "hl")
	summary := ImportSummary{
		Map: importcommon.MapImport{
			Entities: []importcommon.Entity{{
				ClassName: "monster_scientist",
				KeyValues: map[string]string{
					"model": "models/scientist.mdl",
				},
			}},
		},
		Report: importcommon.ImportReport{
			Source: importcommon.SourceInfo{Kind: "hl1", GameDir: gameDir, MapName: "testmap"},
		},
	}
	result, err := BuildGameAssetImport(ImportOptions{GameDir: gameDir, OutputRoot: filepath.Join(dir, "out")}, summary)
	if err != nil {
		t.Fatalf("BuildGameAssetImport failed: %v", err)
	}
	if len(result.Manifest.Assets) != 1 || result.Manifest.Assets[0].Resolved {
		t.Fatalf("expected one unresolved asset, got %+v", result.Manifest.Assets)
	}
	if len(result.Manifest.Diagnostics) != 1 || result.Manifest.Diagnostics[0].Code != "hl1.asset_missing" {
		t.Fatalf("expected missing asset diagnostic, got %+v", result.Manifest.Diagnostics)
	}
}

func TestParseMDLInfoReadsGoldSrcHeaderMetadata(t *testing.T) {
	info, err := ParseMDLInfo(syntheticMDL())
	if err != nil {
		t.Fatalf("ParseMDLInfo failed: %v", err)
	}
	if info.Name != "test_model" || info.Version != MDLVersion10 || info.TextureCount != 1 || info.BodyPartCount != 1 {
		t.Fatalf("unexpected mdl info: %+v", info)
	}
	if len(info.Textures) != 1 || info.Textures[0].Name != "test_texture.bmp" || info.Textures[0].Width != 64 || info.Textures[0].Height != 32 {
		t.Fatalf("unexpected textures: %+v", info.Textures)
	}
	if len(info.BodyParts) != 1 || len(info.BodyParts[0].Models) != 1 {
		t.Fatalf("unexpected body parts: %+v", info.BodyParts)
	}
	model := info.BodyParts[0].Models[0]
	if model.Name != "body_model" || model.MeshCount != 1 || model.VertexCount != 8 || model.TriangleCount != 1 {
		t.Fatalf("unexpected model metadata: %+v", model)
	}
}

func TestParseMDLGeometryDecodesTexturePixelsAndTriangleCommands(t *testing.T) {
	geometry, err := ParseMDLGeometry(syntheticMDL())
	if err != nil {
		t.Fatalf("ParseMDLGeometry failed: %v", err)
	}
	if geometry.Info.DecodedTextureCount != 1 || len(geometry.Textures) != 1 {
		t.Fatalf("expected one decoded texture, got info=%+v textures=%d", geometry.Info, len(geometry.Textures))
	}
	if len(geometry.Textures[0].Pixels) != 64*32 || len(geometry.Textures[0].Palette) != 256 {
		t.Fatalf("unexpected texture payload: pixels=%d palette=%d", len(geometry.Textures[0].Pixels), len(geometry.Textures[0].Palette))
	}
	if geometry.Info.DecodedTriangleCount != 1 || len(geometry.Triangles) != 1 {
		t.Fatalf("expected one decoded triangle, got info=%+v triangles=%d", geometry.Info, len(geometry.Triangles))
	}
	tri := geometry.Triangles[0]
	if tri.TextureIndex != 0 {
		t.Fatalf("expected triangle texture 0, got %d", tri.TextureIndex)
	}
	if tri.Vertices[2].Position.Z != 1 || tri.Vertices[1].Texel != [2]int{32, 0} || tri.Vertices[2].UV != [2]float32{0, 1} {
		t.Fatalf("unexpected triangle vertices: %+v", tri.Vertices)
	}
}

func TestParseSPRGeometryDecodesPaletteAndFrame(t *testing.T) {
	geometry, err := ParseSPRGeometry(syntheticSPR())
	if err != nil {
		t.Fatalf("ParseSPRGeometry failed: %v", err)
	}
	if geometry.Info.Version != SPRVersion2 || geometry.Info.MaxWidth != 4 || geometry.Info.MaxHeight != 2 || geometry.Info.DecodedFrames != 1 {
		t.Fatalf("unexpected spr info: %+v", geometry.Info)
	}
	if len(geometry.Palette) != 256 || len(geometry.Frames) != 1 {
		t.Fatalf("unexpected spr payload: palette=%d frames=%d", len(geometry.Palette), len(geometry.Frames))
	}
	frame := geometry.Frames[0]
	if frame.OriginX != -2 || frame.OriginY != 1 || frame.Width != 4 || frame.Height != 2 || len(frame.Pixels) != 8 {
		t.Fatalf("unexpected frame: %+v", frame)
	}
}

func TestBuildSPRVoxelAssetBuildsVisibleCard(t *testing.T) {
	geometry, err := ParseSPRGeometry(syntheticSPR())
	if err != nil {
		t.Fatalf("ParseSPRGeometry failed: %v", err)
	}
	asset, voxelCount, err := BuildSPRVoxelAsset(geometry, SPRVoxelAssetOptions{Name: "glow", SourceRef: "sprites/glow01.spr", VoxelResolution: 0.1})
	if err != nil {
		t.Fatalf("BuildSPRVoxelAsset failed: %v", err)
	}
	if voxelCount != 5 || len(asset.Parts) != 1 || len(asset.Materials) == 0 {
		t.Fatalf("unexpected sprite asset: voxels=%d asset=%+v", voxelCount, asset)
	}
	if asset.Parts[0].Source.VoxelShape == nil || len(asset.Parts[0].Source.VoxelShape.Voxels) != voxelCount {
		t.Fatalf("missing voxel shape: %+v", asset.Parts[0].Source)
	}
}

func assertHL1AssetEntry(t *testing.T, entries []GameAssetManifestEntry, kind string, sourcePath string, convertState string) GameAssetManifestEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.Kind == kind && filepath.Clean(entry.SourcePath) == filepath.Clean(sourcePath) {
			if !entry.Resolved {
				t.Fatalf("%s entry was not resolved: %+v", kind, entry)
			}
			if entry.ConvertState != convertState {
				t.Fatalf("%s convert state = %q, want %q", kind, entry.ConvertState, convertState)
			}
			if entry.SizeBytes == 0 || entry.SHA256 == "" || entry.OutputPath == "" {
				t.Fatalf("%s entry missing copied-file metadata: %+v", kind, entry)
			}
			return entry
		}
	}
	t.Fatalf("missing %s entry for %s in %+v", kind, sourcePath, entries)
	return GameAssetManifestEntry{}
}

func syntheticMDL() []byte {
	const (
		headerOffset      = 0
		textureOffset     = mdlHeaderSize
		bodyPartOffset    = textureOffset + 80
		modelOffset       = bodyPartOffset + 76
		meshOffset        = modelOffset + 112
		vertexOffset      = meshOffset + 20
		triCommandOffset  = vertexOffset + 8*12
		skinOffset        = triCommandOffset + 2 + 3*8 + 2
		textureDataOffset = skinOffset + 2
		totalSize         = textureDataOffset + 64*32 + 256*3
	)
	data := make([]byte, totalSize)
	copy(data[0:4], MDLIdentGoldSrc)
	writeTestInt32(data, headerOffset+4, MDLVersion10)
	writeTestCString(data[headerOffset+8:headerOffset+72], "test_model")
	writeTestInt32(data, headerOffset+72, totalSize)
	writeTestFloat32(data, headerOffset+76, 1)
	writeTestFloat32(data, headerOffset+80, 2)
	writeTestFloat32(data, headerOffset+84, 3)
	writeTestFloat32(data, headerOffset+88, -4)
	writeTestFloat32(data, headerOffset+92, -5)
	writeTestFloat32(data, headerOffset+96, -6)
	writeTestFloat32(data, headerOffset+100, 4)
	writeTestFloat32(data, headerOffset+104, 5)
	writeTestFloat32(data, headerOffset+108, 6)
	writeTestInt32(data, headerOffset+140, 1)
	writeTestInt32(data, headerOffset+156, 2)
	writeTestInt32(data, headerOffset+164, 3)
	writeTestInt32(data, headerOffset+180, 1)
	writeTestInt32(data, headerOffset+184, textureOffset)
	writeTestInt32(data, headerOffset+192, 1)
	writeTestInt32(data, headerOffset+196, 1)
	writeTestInt32(data, headerOffset+200, skinOffset)
	writeTestInt32(data, headerOffset+204, 1)
	writeTestInt32(data, headerOffset+208, bodyPartOffset)
	writeTestInt32(data, headerOffset+212, 4)

	writeTestCString(data[textureOffset:textureOffset+64], "test_texture.bmp")
	writeTestInt32(data, textureOffset+68, 64)
	writeTestInt32(data, textureOffset+72, 32)
	writeTestInt32(data, textureOffset+76, textureDataOffset)

	writeTestCString(data[bodyPartOffset:bodyPartOffset+64], "body")
	writeTestInt32(data, bodyPartOffset+64, 1)
	writeTestInt32(data, bodyPartOffset+68, 1)
	writeTestInt32(data, bodyPartOffset+72, modelOffset)

	writeTestCString(data[modelOffset:modelOffset+64], "body_model")
	writeTestFloat32(data, modelOffset+68, 8.5)
	writeTestInt32(data, modelOffset+72, 1)
	writeTestInt32(data, modelOffset+76, meshOffset)
	writeTestInt32(data, modelOffset+80, 8)
	writeTestInt32(data, modelOffset+88, vertexOffset)
	writeTestInt32(data, modelOffset+92, 6)
	writeTestInt32(data, modelOffset+104, 0)

	writeTestInt32(data, meshOffset, 1)
	writeTestInt32(data, meshOffset+4, triCommandOffset)
	writeTestInt32(data, meshOffset+8, 0)

	writeTestVec3(data, vertexOffset+0, 0, 0, 0)
	writeTestVec3(data, vertexOffset+12, 1, 0, 0)
	writeTestVec3(data, vertexOffset+24, 0, 0, 1)
	writeTestInt16(data, triCommandOffset, 3)
	writeTestTriangleCommandVertex(data, triCommandOffset+2, 0, 0, 0, 0)
	writeTestTriangleCommandVertex(data, triCommandOffset+10, 1, 0, 32, 0)
	writeTestTriangleCommandVertex(data, triCommandOffset+18, 2, 0, 0, 32)
	writeTestInt16(data, triCommandOffset+26, 0)
	writeTestInt16(data, skinOffset, 0)
	for i := 0; i < 64*32; i++ {
		data[textureDataOffset+i] = byte(i % 256)
	}
	for i := 0; i < 256; i++ {
		base := textureDataOffset + 64*32 + i*3
		data[base] = byte(i)
		data[base+1] = byte(255 - i)
		data[base+2] = byte(i / 2)
	}
	return data
}

func syntheticSPR() []byte {
	const (
		width         = 4
		height        = 2
		paletteCount  = 256
		headerSize    = sprHeaderSize
		paletteOffset = headerSize + 2
		frameOffset   = paletteOffset + paletteCount*3
		totalSize     = frameOffset + 20 + width*height
	)
	data := make([]byte, totalSize)
	copy(data[0:4], SPRIdentGoldSrc)
	writeTestInt32(data, 4, SPRVersion2)
	writeTestInt32(data, 8, 2)
	writeTestInt32(data, 12, 1)
	writeTestFloat32(data, 16, 4)
	writeTestInt32(data, 20, width)
	writeTestInt32(data, 24, height)
	writeTestInt32(data, 28, 1)
	writeTestFloat32(data, 32, 0)
	writeTestInt32(data, 36, 1)
	binary.LittleEndian.PutUint16(data[headerSize:], paletteCount)
	data[paletteOffset+1*3+0] = 255
	data[paletteOffset+1*3+1] = 64
	data[paletteOffset+1*3+2] = 16
	data[paletteOffset+2*3+0] = 32
	data[paletteOffset+2*3+1] = 128
	data[paletteOffset+2*3+2] = 255
	writeTestInt32(data, frameOffset+0, 0)
	writeTestInt32(data, frameOffset+4, -2)
	writeTestInt32(data, frameOffset+8, 1)
	writeTestInt32(data, frameOffset+12, width)
	writeTestInt32(data, frameOffset+16, height)
	copy(data[frameOffset+20:], []byte{0, 1, 1, 0, 2, 2, 1, 0})
	return data
}

func writeTestInt32(data []byte, offset int, value int) {
	binary.LittleEndian.PutUint32(data[offset:offset+4], uint32(int32(value)))
}

func writeTestInt16(data []byte, offset int, value int) {
	binary.LittleEndian.PutUint16(data[offset:offset+2], uint16(int16(value)))
}

func writeTestFloat32(data []byte, offset int, value float32) {
	binary.LittleEndian.PutUint32(data[offset:offset+4], math.Float32bits(value))
}

func writeTestVec3(data []byte, offset int, x float32, y float32, z float32) {
	writeTestFloat32(data, offset, x)
	writeTestFloat32(data, offset+4, y)
	writeTestFloat32(data, offset+8, z)
}

func writeTestTriangleCommandVertex(data []byte, offset int, vertex int, normal int, s int, t int) {
	writeTestInt16(data, offset, vertex)
	writeTestInt16(data, offset+2, normal)
	writeTestInt16(data, offset+4, s)
	writeTestInt16(data, offset+6, t)
}

func writeTestCString(data []byte, value string) {
	copy(data, []byte(value))
}
