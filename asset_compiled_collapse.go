package gekko

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/gekko3d/gekko/content"
)

type compiledCollapseBinding struct {
	PartID    string
	ContentID string
	Lattice   content.VoxelObjectLatticeDef
}

// Public synchronous preparation retains live IDs; only streamed whole-asset
// preparation can install an adopted worker candidate in this private marker.
type compiledPreparedCollapse struct {
	documentPath string
	key          string
	bindings     []compiledCollapseBinding
	adopted      *collapsedAuthoredVoxelBuild
}

func compiledCollapseGeometryKey(def *content.AssetDef, documentPath string, bindings []compiledCollapseBinding) (string, error) {
	absolute, err := filepath.Abs(documentPath)
	if err != nil {
		return "", err
	}
	payload := struct {
		DocumentPath string
		Asset        *content.AssetDef
		Bindings     []compiledCollapseBinding
	}{filepath.Clean(absolute), def, bindings}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return "compiled-authored-collapse-v1:" + string(data), nil
}

func (prepared *compiledPreparedCollapse) keyForDocument(def *content.AssetDef, documentPath string) (string, error) {
	if documentPath == "" {
		documentPath = prepared.documentPath
	}
	absolute, err := filepath.Abs(documentPath)
	if err != nil {
		return "", err
	}
	if filepath.Clean(absolute) == prepared.documentPath {
		return prepared.key, nil
	}
	return compiledCollapseGeometryKey(def, documentPath, prepared.bindings)
}

func prepareCompiledCollapse(packet *compiledAssetPacket, loader *RuntimeContentLoader, cancelled func() bool, buildCandidate bool) error {
	if !packet.wholeAsset || packet.def.Runtime == nil || !packet.def.Runtime.CollapseVoxelParts {
		return nil
	}
	for _, part := range packet.def.Parts {
		if part.Source.Kind == content.AssetSourceKindGroup {
			continue
		}
		shape := packet.shapes[packet.parts[part.ID]]
		if shape == nil || shape.model {
			return fmt.Errorf("compiled collapse requires complete inline shape inputs")
		}
		packet.collapseBindings = append(packet.collapseBindings, compiledCollapseBinding{PartID: part.ID, ContentID: shape.contentID, Lattice: shape.lattice})
	}
	var err error
	packet.collapseKey, err = compiledCollapseGeometryKey(packet.def, packet.documentPath, packet.collapseBindings)
	if err != nil {
		return err
	}
	if !buildCandidate || len(packet.def.AnimationSetPaths) != 0 {
		return nil
	}
	parts, err := resolveAuthoredCollapsePartsFromSources(packet.def, func(part content.AssetPartDef) (VoxelGeometryAsset, VoxelPaletteAsset, error) {
		shape := packet.shapes[packet.parts[part.ID]]
		palette := packet.palettes[packet.partPalettes[part.ID]]
		return VoxelGeometryAsset{XBrickMap: shape.source, LocalMin: shape.source.GetAABBMin(), LocalMax: shape.source.GetAABBMax(), BrickSize: [3]uint32{8, 8, 8}, SourcePath: compiledAssetPacketGeometryKey(shape), RuntimeOwned: true}, *palette.source, nil
	})
	if err != nil {
		return nil
	}
	packet.collapse, err = prepareResolvedAuthoredCollapseCandidate(packet.def, packet.documentPath, packet.collapseKey, parts, loader, cancelled)
	return err
}
