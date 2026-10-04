package gekko

import (
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
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
