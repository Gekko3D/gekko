package gekko

import (
	"fmt"
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"path/filepath"
)

// A CPU packet owns its metadata and distinct geometry sources. It contains no
// cached shape definitions or global asset IDs. Each registration owns a second,
// independent copy suitable for a later single-use main-thread transfer.
type compiledAssetPacket struct {
	def          *content.AssetDef
	documentPath string
	animations   *content.ResolvedAssetAnimations
	parts        map[string]string
	shapes       map[string]*compiledAssetPacketShape
}

type compiledAssetPacketShape struct {
	contentID    string
	lattice      content.VoxelObjectLatticeDef
	baseIdentity string
	source       *volume.XBrickMap
	registration *streamedGeometryRegistration
}

// Release only pending handles. Already adopted storage stays with AssetServer;
// repeated release through packet aliases is harmless.
func (packet *compiledAssetPacket) release() {
	if packet == nil {
		return
	}
	for _, shape := range packet.shapes {
		shape.registration.release()
	}
}

func prepareCompiledAssetPacket(path string, loader *RuntimeContentLoader, cancelled func() bool) (*compiledAssetPacket, error) {
	session, err := verifyCompiledAssetInput(path, loader, cancelled)
	if err != nil {
		return nil, err
	}
	defer session.close()
	packet := &compiledAssetPacket{def: session.def, documentPath: path, animations: session.animations, parts: make(map[string]string, len(session.shapes)), shapes: make(map[string]*compiledAssetPacketShape, len(session.shapes))}
	success := false
	defer func() {
		if !success {
			packet.release()
		}
	}()
	// Preserve authored part order for deterministic cancellation boundaries.
	for _, part := range packet.def.Parts {
		verified, exists := session.shapes[part.ID]
		if !exists {
			continue
		}
		if err := checkCompiledAssetWork(loader, cancelled); err != nil {
			return nil, err
		}
		packet.parts[part.ID] = verified.contentID
		if _, exists := packet.shapes[verified.contentID]; exists {
			continue
		}
		source, _ := compiledShapeGeometry(verified.definition)
		registration := prepareStreamedGeometryRegistration(source)
		packet.shapes[verified.contentID] = &compiledAssetPacketShape{contentID: verified.contentID, lattice: verified.definition.Lattice, baseIdentity: verified.baseIdentity, source: source, registration: registration}
	}
	if err := checkCompiledAssetWork(loader, cancelled); err != nil {
		return nil, err
	}
	success = true
	return packet, nil
}

// Publish only verified packet data. No frame or authoring source is reread here;
// ordinary global geometry keeps its existing lifetime independently of packets.
func publishCompiledAssetPacket(packet *compiledAssetPacket, assets *AssetServer, loader *RuntimeContentLoader) (*PreparedAuthoredAsset, error) {
	if err := checkCompiledAssetWork(loader, nil); err != nil {
		return nil, err
	}
	if packet == nil || packet.def == nil {
		return nil, fmt.Errorf("compiled asset packet is missing")
	}
	prepared := &PreparedAuthoredAsset{def: packet.def, documentPath: packet.documentPath, animations: packet.animations, parts: make(map[string]preparedAuthoredPart, len(packet.def.Parts))}
	models := make(map[string]AssetId, len(packet.shapes))
	if assets != nil {
		for _, part := range packet.def.Parts {
			contentID, exists := packet.parts[part.ID]
			if !exists {
				continue
			}
			if _, exists := models[contentID]; exists {
				continue
			}
			shape := packet.shapes[contentID]
			if shape == nil {
				return nil, fmt.Errorf("compiled packet shape is missing")
			}
			id, adopted := assets.adoptCompiledAssetGeometry(shape.contentID, shape.lattice, shape.baseIdentity, shape.source, shape.registration)
			if !adopted {
				// A warm conflict must never be bypassed by defensive registration.
				// Rebuild only a consumed cold handle after public key deletion.
				if _, warm := assets.SharedVoxelGeometryByCacheKey("compiled-asset-shape:" + shape.contentID); warm || shape.registration.charge() != 0 {
					return nil, fmt.Errorf("compiled packet geometry adoption rejected")
				}
				fresh := prepareStreamedGeometryRegistration(shape.source)
				id, adopted = assets.adoptCompiledAssetGeometry(shape.contentID, shape.lattice, shape.baseIdentity, shape.source, fresh)
				fresh.release()
				if !adopted {
					return nil, fmt.Errorf("compiled packet geometry rebuild rejected")
				}
			}
			models[contentID] = id
		}
	}
	for _, part := range packet.def.Parts {
		preparedPart := preparedAuthoredPart{}
		if assets != nil {
			if contentID, exists := packet.parts[part.ID]; exists {
				palette, err := authoredVoxelShapePalette(assets, packet.def, part)
				if err != nil {
					return nil, err
				}
				preparedPart = preparedAuthoredPart{model: models[contentID], palette: palette}
			}
		}
		prepared.parts[part.ID] = preparedPart
	}
	if err := checkCompiledAssetWork(loader, nil); err != nil {
		return nil, err
	}
	return prepared, nil
}

func compiledAssetPacketKey(path, levelPath string) (string, error) {
	resolved := content.ResolveDocumentPath(path, levelPath)
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}
