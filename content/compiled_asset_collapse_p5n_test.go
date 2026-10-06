package content_test

import (
	"github.com/gekko3d/gekko/content"
	"reflect"
	"testing"
)

func TestP5nCollapseHeaderVersionAndRoundtrip(t *testing.T) {
	if content.CurrentCompiledAssetHeaderSchemaVersion != 1 || content.CompiledAssetLODHeaderSchemaVersion != 2 || content.CompiledAssetCollapseHeaderSchemaVersion != 3 || content.CompiledAssetCollapseHeaderCompilerVersion != "gekko-compiled-asset-v3" {
		t.Fatal("compiled format defaults or opt-in constants changed")
	}
	for _, lod := range []bool{false, true} {
		h := c3bHeader()
		if lod {
			h = c3h3Header()
		}
		h.SchemaVersion = 3
		h.CompilerVersion = "gekko-compiled-asset-v3"
		h.Asset.Runtime.CollapseVoxelParts = true
		frame, info, err := content.EncodeCompiledAssetHeader(h, nil)
		if err != nil {
			t.Fatal(err)
		}
		decoded, gotInfo, err := content.DecodeCompiledAssetHeader(frame, nil)
		if err != nil || gotInfo != info || decoded.SchemaVersion != 3 || !decoded.Asset.Runtime.CollapseVoxelParts || !reflect.DeepEqual(decoded.Asset.Parts, h.Asset.Parts) || len(decoded.LODs) != len(h.LODs) {
			t.Fatal("schema3 lost authored part order or authenticated references", err)
		}
	}
}
func TestP5nCollapseHeaderRejectsVersionMismatchAndLegacyCollapse(t *testing.T) {
	for _, kind := range []string{"schema1", "schema2", "wrong-compiler", "future", "model-source", "missing-shape", "runtime-nil", "runtime-false"} {
		t.Run(kind, func(t *testing.T) {
			h := c3bHeader()
			h.Asset.Runtime.CollapseVoxelParts = true
			h.SchemaVersion = 3
			h.CompilerVersion = "gekko-compiled-asset-v3"
			switch kind {
			case "runtime-nil":
				h.Asset.Runtime = nil
			case "runtime-false":
				h.Asset.Runtime.CollapseVoxelParts = false
			case "schema1":
				h.SchemaVersion = 1
				h.CompilerVersion = content.CurrentCompiledAssetCompilerVersion
			case "schema2":
				h.SchemaVersion = 2
				h.CompilerVersion = content.CompiledAssetLODHeaderCompilerVersion
			case "wrong-compiler":
				h.CompilerVersion = content.CompiledAssetLODHeaderCompilerVersion
			case "future":
				h.SchemaVersion = 4
			case "model-source":
				h.Asset.Parts[0].Source.Kind = content.AssetSourceKindVoxModel
				h.Asset.Parts[0].Source.Path = "model.vox"
			case "missing-shape":
				h.Shapes = h.Shapes[:1]
			}
			if _, _, err := content.EncodeCompiledAssetHeader(h, nil); err == nil {
				t.Fatal("invalid compiled collapse contract accepted")
			}
		})
	}
}
