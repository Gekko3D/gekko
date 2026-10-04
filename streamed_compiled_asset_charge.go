package gekko

import (
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Owned metadata uses the decoded graph estimator. Dense sources have bounded
// storage traversal; registration charges are captured independently of mutexes.
func streamedCompiledAssetPacketsCharge(packets map[string]*compiledAssetPacket) int64 {
	if len(packets) == 0 {
		return 0
	}
	keys := make(map[string]*compiledAssetPacket, len(packets))
	seenPackets := make(map[*compiledAssetPacket]struct{}, len(packets))
	seenSources := make(map[*volume.XBrickMap]struct{})
	seenRegistrations := make(map[*streamedGeometryRegistration]struct{})
	seenPaletteSources := make(map[*VoxelPaletteAsset]struct{})
	seenPaletteRegistrations := make(map[*compiledPaletteRegistration]struct{})
	var bytes int64
	addSource := func(source *volume.XBrickMap) {
		if _, exists := seenSources[source]; !exists {
			seenSources[source] = struct{}{}
			bytes = runtimeContentChargeSum(bytes, streamedPendingGeometryCharge(source))
		}
	}
	addRegistration := func(registration *streamedGeometryRegistration) {
		if registration == nil {
			return
		}
		if _, exists := seenRegistrations[registration]; !exists {
			seenRegistrations[registration] = struct{}{}
			bytes = runtimeContentChargeSum(bytes, int64(unsafe.Sizeof(streamedGeometryRegistration{})), registration.charge())
		}
	}
	for key, packet := range packets {
		keys[key] = nil
		if packet == nil {
			continue
		}
		if _, exists := seenPackets[packet]; exists {
			continue
		}
		seenPackets[packet] = struct{}{}
		metadata := *packet
		if packet.shapes != nil {
			metadata.shapes = make(map[string]*compiledAssetPacketShape, len(packet.shapes))
		}
		for contentID, shape := range packet.shapes {
			if shape == nil {
				metadata.shapes[contentID] = nil
				continue
			}
			copy := *shape
			copy.source = nil
			copy.registration = nil
			metadata.shapes[contentID] = &copy
			addSource(shape.source)
			addRegistration(shape.registration)
		}
		if packet.palettes != nil {
			metadata.palettes = make(map[string]*compiledAssetPacketPalette, len(packet.palettes))
		}
		for key, palette := range packet.palettes {
			if palette == nil {
				metadata.palettes[key] = nil
				continue
			}
			// Reflect only the owned scalar graph, never live handle mutexes or
			// borrowed registration pointers. Packet sources are immutable.
			metadata.palettes[key] = &compiledAssetPacketPalette{}
			if _, exists := seenPaletteSources[palette.source]; !exists {
				seenPaletteSources[palette.source] = struct{}{}
				bytes = runtimeContentChargeSum(bytes, runtimeContentGraphCharge(palette.source))
			}
			registration := palette.registration
			if registration != nil {
				if _, exists := seenPaletteRegistrations[registration]; !exists {
					seenPaletteRegistrations[registration] = struct{}{}
					// Map keys retain the sealed key; fixed handle metadata survives consumption.
					bytes = runtimeContentChargeSum(bytes, int64(unsafe.Sizeof(compiledPaletteRegistration{})), registration.charge())
				}
			}
		}
		if packet.lods != nil {
			metadata.lods = make(map[string]*compiledAssetPacketLOD, len(packet.lods))
		}
		// Preserve scalar graph aliases while excluding volume storage and live
		// handle mutexes from reflection. Constructor-owned copies are disjoint.
		lodCopies := make(map[*compiledAssetPacketLOD]*compiledAssetPacketLOD)
		proofCopies := make(map[*compiledAssetLODProof]*compiledAssetLODProof)
		for key, lod := range packet.lods {
			if lod == nil {
				metadata.lods[key] = nil
				continue
			}
			if copy, exists := lodCopies[lod]; exists {
				metadata.lods[key] = copy
				continue
			}
			copy := *lod
			copy.source, copy.registration = nil, nil
			lodCopies[lod] = &copy
			metadata.lods[key] = &copy
			addSource(lod.source)
			addRegistration(lod.registration)
			if lod.proof != nil {
				if proof, exists := proofCopies[lod.proof]; exists {
					copy.proof = proof
				} else {
					proof := *lod.proof
					proof.full, proof.coarse = nil, nil
					copy.proof = &proof
					proofCopies[lod.proof] = &proof
					addSource(lod.proof.full)
					addSource(lod.proof.coarse)
				}
			}
		}
		bytes = runtimeContentChargeSum(bytes, runtimeContentGraphCharge(&metadata))
	}
	return runtimeContentChargeSum(bytes, runtimeContentGraphCharge(keys))
}
