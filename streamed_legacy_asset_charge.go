package gekko

import "unsafe"

func streamedLegacyAssetPacketsCharge(packets map[string]*legacyAssetPacket) int64 {
	keys := make(map[string]*legacyAssetPacket, len(packets))
	seen := map[*legacyAssetPacket]bool{}
	var bytes int64
	for key, p := range packets {
		keys[key] = nil
		if p == nil || seen[p] {
			continue
		}
		seen[p] = true
		metadata := *p
		if p.collapse != nil {
			var charge int64
			metadata.collapse, charge = authoredCollapseCandidateChargeMetadata(p.collapse)
			bytes = runtimeContentChargeSum(bytes, charge)
		}
		metadata.geometries = make(map[string]*legacyAssetGeometry, len(p.geometries))
		for key, g := range p.geometries {
			copy := *g
			copy.source = VoxelGeometryAsset{}
			copy.registration = nil
			metadata.geometries[key] = &copy
			bytes = runtimeContentChargeSum(bytes, legacyGeometrySourceCharge(g.source), int64(unsafe.Sizeof(legacyGeometryRegistration{})), int64(len(g.registration.key)))
			g.registration.mu.Lock()
			if g.registration.asset != nil {
				bytes = runtimeContentChargeSum(bytes, legacyGeometrySourceCharge(*g.registration.asset))
			}
			g.registration.mu.Unlock()
		}
		metadata.palettes = make(map[string]*compiledAssetPacketPalette, len(p.palettes))
		for key, v := range p.palettes {
			metadata.palettes[key] = &compiledAssetPacketPalette{source: v.source}
			bytes = runtimeContentChargeSum(bytes, int64(unsafe.Sizeof(compiledPaletteRegistration{})), int64(len(v.registration.key)), v.registration.charge())
		}
		bytes = runtimeContentChargeSum(bytes, runtimeContentGraphCharge(&metadata))
	}
	return runtimeContentChargeSum(bytes, runtimeContentGraphCharge(keys))
}

// Scalar metadata keeps the immutable palette graph, while volume storage and
// live registration mutexes are charged separately rather than reflected.
func authoredCollapseCandidateChargeMetadata(source *authoredCollapseCandidate) (*authoredCollapseCandidate, int64) {
	candidate := *source
	geometry := *candidate.geometry
	geometry.source = VoxelGeometryAsset{}
	geometry.registration = nil
	candidate.geometry = &geometry
	candidate.palette = &compiledAssetPacketPalette{source: source.palette.source}
	r := source.geometry.registration
	bytes := runtimeContentChargeSum(legacyGeometrySourceCharge(source.geometry.source), int64(unsafe.Sizeof(legacyGeometryRegistration{})), int64(len(r.key)))
	r.mu.Lock()
	if r.asset != nil {
		bytes = runtimeContentChargeSum(bytes, legacyGeometrySourceCharge(*r.asset))
	}
	r.mu.Unlock()
	palette := source.palette.registration
	bytes = runtimeContentChargeSum(bytes, int64(unsafe.Sizeof(compiledPaletteRegistration{})), int64(len(palette.key)), palette.charge())
	return &candidate, bytes
}
