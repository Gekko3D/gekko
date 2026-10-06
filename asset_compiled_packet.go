package gekko

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// A CPU packet owns metadata, distinct geometry sources and full palettes. It
// contains no cached shape definitions or global asset IDs. Each registration owns a second,
// independent copy suitable for a later single-use main-thread transfer.
type compiledAssetPacket struct {
	def          *content.AssetDef
	documentPath string
	animations   *content.ResolvedAssetAnimations
	parts        map[string]string
	shapes       map[string]*compiledAssetPacketShape
	partPalettes map[string]string
	palettes     map[string]*compiledAssetPacketPalette
	partLODs     map[string]string
	lods         map[string]*compiledAssetPacketLOD
}

type compiledAssetPacketPalette struct {
	source       *VoxelPaletteAsset
	registration *compiledPaletteRegistration
}

type compiledAssetPacketShape struct {
	contentID        string
	lattice          content.VoxelObjectLatticeDef
	baseIdentity     string
	baseDecodedBytes int64
	model            bool
	dimensions       [3]uint32
	source           *volume.XBrickMap
	registration     *streamedGeometryRegistration
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
	for _, palette := range packet.palettes {
		palette.registration.release()
	}
	for _, lod := range packet.lods {
		if lod != nil {
			lod.registration.release()
		}
	}
}

func prepareCompiledAssetPacket(path string, loader *RuntimeContentLoader, cancelled func() bool) (*compiledAssetPacket, error) {
	session, err := verifyCompiledAssetInput(path, loader, cancelled)
	if err != nil {
		return nil, err
	}
	defer session.close()
	return prepareCompiledAssetPacketFromVerification(path, session, loader, cancelled, session.def.Parts)
}

// The caller owns the live verification session and closes it after construction.
// Whole-closure verification precedes even a selected first-part packet build.
func prepareCompiledAssetPacketFromVerification(path string, session *compiledAssetVerification, loader *RuntimeContentLoader, cancelled func() bool, parts []content.AssetPartDef) (*compiledAssetPacket, error) {
	packet := &compiledAssetPacket{
		def: session.def, documentPath: path, animations: session.animations,
		parts:        make(map[string]string, len(session.shapes)+len(session.models)),
		shapes:       make(map[string]*compiledAssetPacketShape, len(session.shapes)+len(session.models)),
		partPalettes: make(map[string]string, len(session.shapes)+len(session.models)),
		palettes:     make(map[string]*compiledAssetPacketPalette),
	}
	// Baselines are created only for authenticated derivatives, before the
	// verification scope closes or any mutable packet storage is published.
	var fullBaselines map[string]*volume.XBrickMap
	if len(session.lods) != 0 {
		packet.partLODs = make(map[string]string, len(session.lods))
		packet.lods = make(map[string]*compiledAssetPacketLOD, len(session.lods))
		fullBaselines = make(map[string]*volume.XBrickMap)
	}
	success := false
	defer func() {
		if !success {
			packet.release()
		}
	}()
	// This memo is local to the fixed materials and animations of this document.
	// Preserve the complete ordered binding slice, including nil/empty distinctions.
	paletteBindings := make(map[string]string)
	// Preserve authored part order for deterministic cancellation boundaries.
	for _, part := range parts {
		if model, exists := session.models[part.ID]; exists {
			if err := prepareCompiledAssetPacketModel(packet, part.ID, model, paletteBindings, loader, cancelled); err != nil {
				return nil, err
			}
			continue
		}
		verified, exists := session.shapes[part.ID]
		if !exists {
			continue
		}
		if err := checkCompiledAssetWork(loader, cancelled); err != nil {
			return nil, err
		}
		packet.parts[part.ID] = verified.contentID
		if _, exists := packet.shapes[verified.contentID]; !exists {
			source, _ := compiledShapeGeometry(verified.definition)
			registration := prepareStreamedGeometryRegistration(source)
			packet.shapes[verified.contentID] = &compiledAssetPacketShape{contentID: verified.contentID, lattice: verified.definition.Lattice, baseIdentity: verified.baseIdentity, baseDecodedBytes: verified.baseDecodedBytes, source: source, registration: registration}
		}
		if err := checkCompiledAssetWork(loader, cancelled); err != nil {
			return nil, err
		}
		if derivative, exists := session.lods[part.ID]; exists {
			packet.partLODs[part.ID] = derivative.contentID
			if _, exists := packet.lods[derivative.contentID]; !exists {
				baseline := fullBaselines[verified.contentID]
				if baseline == nil {
					baseline = packet.shapes[verified.contentID].source.Copy()
					fullBaselines[verified.contentID] = baseline
				}
				packet.lods[derivative.contentID] = prepareVerifiedCompiledAssetPacketLOD(derivative, baseline)
			}
			if err := checkCompiledAssetWork(loader, cancelled); err != nil {
				return nil, err
			}
		}
		binding, err := json.Marshal(part.Source.VoxelShape.Palette)
		if err != nil {
			return nil, err
		}
		bindingKey := string(binding)
		if key, exists := paletteBindings[bindingKey]; exists {
			packet.partPalettes[part.ID] = key
			continue
		}
		palette, err := buildAuthoredVoxelShapePalette(packet.def, part)
		if err != nil {
			return nil, err
		}
		registration, err := prepareCompiledPaletteRegistration(&palette)
		if err != nil {
			return nil, err
		}
		key := registration.key
		if _, exists := packet.palettes[key]; exists {
			registration.release()
		} else {
			packet.palettes[key] = &compiledAssetPacketPalette{source: &palette, registration: registration}
		}
		paletteBindings[bindingKey] = key
		packet.partPalettes[part.ID] = key
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
			id, adopted := adoptCompiledAssetPacketGeometry(assets, shape, shape.source, shape.registration)
			if !adopted {
				// A warm conflict must never be bypassed by defensive registration.
				// Rebuild only a consumed cold handle after public key deletion.
				if _, warm := assets.SharedVoxelGeometryByCacheKey(compiledAssetPacketGeometryKey(shape)); warm || shape.registration.charge() != 0 {
					return nil, fmt.Errorf("compiled packet geometry adoption rejected")
				}
				source := shape.source
				if proof := packet.compiledLODProofForSource(shape.contentID); proof != nil {
					source = proof.full.Copy()
				}
				fresh := prepareStreamedGeometryRegistration(source)
				id, adopted = adoptCompiledAssetPacketGeometry(assets, shape, source, fresh)
				fresh.release()
				if !adopted {
					return nil, fmt.Errorf("compiled packet geometry rebuild rejected")
				}
			}
			models[contentID] = id
		}
	}
	if assets != nil {
		for _, part := range packet.def.Parts {
			if lodID, exists := packet.partLODs[part.ID]; exists {
				if !assets.adoptCompiledAssetPacketLOD(models[packet.parts[part.ID]], packet.lods[lodID]) {
					return nil, fmt.Errorf("compiled packet LOD adoption rejected")
				}
			}
		}
	}
	palettes := make(map[string]AssetId, len(packet.palettes))
	if assets != nil {
		for _, part := range packet.def.Parts {
			key, exists := packet.partPalettes[part.ID]
			if !exists {
				continue
			}
			if _, exists := palettes[key]; exists {
				continue
			}
			palette := packet.palettes[key]
			if palette == nil || palette.registration == nil {
				return nil, fmt.Errorf("compiled packet palette is missing")
			}
			id, adopted := assets.adoptCompiledAssetPalette(key, palette.source, palette.registration)
			if !adopted {
				// A stale key is an existing ownership conflict. Only a consumed
				// cold handle with no key can be rebuilt from the packet source.
				assets.mu.RLock()
				_, keyed := assets.voxPaletteKeys[key]
				assets.mu.RUnlock()
				if keyed || palette.registration.charge() != 0 {
					return nil, fmt.Errorf("compiled packet palette adoption rejected")
				}
				fresh, err := prepareCompiledPaletteRegistration(palette.source)
				if err != nil {
					return nil, err
				}
				id, adopted = assets.adoptCompiledAssetPalette(key, palette.source, fresh)
				fresh.release()
				if !adopted {
					return nil, fmt.Errorf("compiled packet palette rebuild rejected")
				}
			}
			palettes[key] = id
		}
	}
	for _, part := range packet.def.Parts {
		preparedPart := preparedAuthoredPart{}
		if assets != nil {
			if contentID, exists := packet.parts[part.ID]; exists {
				preparedPart = preparedAuthoredPart{model: models[contentID], palette: palettes[packet.partPalettes[part.ID]]}
				if _, declared := packet.partLODs[part.ID]; declared {
					if binding, exists := assets.compiledAssetLODForGeometry(preparedPart.model); exists {
						preparedPart.compiledLOD = binding.coarseID
					}
				}
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
