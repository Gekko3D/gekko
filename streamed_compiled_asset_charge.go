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
		metadata.shapes = make(map[string]*compiledAssetPacketShape, len(packet.shapes))
		for contentID, shape := range packet.shapes {
			if shape == nil {
				metadata.shapes[contentID] = nil
				continue
			}
			copy := *shape
			copy.source = nil
			copy.registration = nil
			metadata.shapes[contentID] = &copy
			if _, exists := seenSources[shape.source]; !exists {
				seenSources[shape.source] = struct{}{}
				bytes = runtimeContentChargeSum(bytes, streamedPendingGeometryCharge(shape.source))
			}
			if _, exists := seenRegistrations[shape.registration]; !exists {
				seenRegistrations[shape.registration] = struct{}{}
				bytes = runtimeContentChargeSum(bytes, shape.registration.charge())
			}
		}
		metadata.palettes = make(map[string]*compiledAssetPacketPalette, len(packet.palettes))
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
		bytes = runtimeContentChargeSum(bytes, runtimeContentGraphCharge(&metadata))
	}
	return runtimeContentChargeSum(bytes, runtimeContentGraphCharge(keys))
}
