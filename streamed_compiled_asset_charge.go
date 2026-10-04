package gekko

import "github.com/gekko3d/gekko/voxelrt/rt/volume"

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
		bytes = runtimeContentChargeSum(bytes, runtimeContentGraphCharge(&metadata))
	}
	return runtimeContentChargeSum(bytes, runtimeContentGraphCharge(keys))
}
