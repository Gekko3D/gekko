package gekko

import (
	"math"
	"slices"
	"strings"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

// VoxelMaterialSemanticIdentityBudgetBytes bounds private current static-palette
// snapshots, canonical keys and sealed rows. Instance copies and GPU resources
// have separate owners. Unsupported or over-budget inputs remain private.
const VoxelMaterialSemanticIdentityBudgetBytes = 8 << 20

type voxelMaterialSemantic struct {
	palette VoxelPaletteAsset
	key     string
	table   *core.ImmutableMaterialTable
	bytes   uint64
}

func (s *VoxelRtState) publishMaterialSemanticOwnership() {
	s.VoxelMaterialSemanticIdentityCount = len(s.materialSemantics)
	s.VoxelMaterialSemanticIdentityBytes = s.materialSemanticBytes
}

func (s *VoxelRtState) beginMaterialSemanticSync(server *AssetServer) {
	if s.materialSemanticServer != server {
		s.materialSemanticServer = server
		s.materialSemantics, s.lastMaterialSemantics = nil, nil
		s.materialSemanticBytes = 0
	}
	s.publishMaterialSemanticOwnership()
}

func finiteMaterialSemantic(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// Preflight precedes copying and encoding. The existing bounded fingerprint
// estimator covers supported scalar properties and all ordinary palette fields.
// A conservative multiplier also covers worst-case JSON escaping, duplicated
// sealed identity bytes and row encodings. Surface metadata is charged here.
func materialSemanticCharge(p *VoxelPaletteAsset, limit uint64) (uint64, bool) {
	const sealedOverhead = uint64(32768)
	if p == nil || len(p.Animations) != 0 || len(p.MaterialFrameOverrides) != 0 || limit < sealedOverhead {
		return 0, false
	}
	for _, value := range []float32{p.Roughness, p.Metalness, p.Emission, p.IOR, p.Transparency} {
		if !finiteMaterialSemantic(float64(value)) {
			return 0, false
		}
	}
	for _, material := range p.Materials {
		if !finiteMaterialSemantic(float64(material.Weight)) {
			return 0, false
		}
		for _, value := range material.Property {
			switch value := value.(type) {
			case float32:
				if !finiteMaterialSemantic(float64(value)) {
					return 0, false
				}
			case float64:
				if !finiteMaterialSemantic(value) {
					return 0, false
				}
			}
		}
	}
	base, supported := materialFingerprintBytes(p, (limit-sealedOverhead)/16)
	if !supported {
		return 0, false
	}
	bytes := sealedOverhead + 16*base
	add := func(n int, size uint64) bool {
		if uint64(n) > (limit-bytes)/size {
			return false
		}
		bytes += uint64(n) * size
		return true
	}
	if !add(len(p.SurfaceMaterials), 2048) {
		return 0, false
	}
	for _, surface := range p.SurfaceMaterials {
		if !add(len(surface.Kind), 16) || !add(len(surface.Tags), 512) {
			return 0, false
		}
		for _, tag := range surface.Tags {
			if !add(len(tag), 16) {
				return 0, false
			}
		}
	}
	return bytes, true
}

func equalMaterialSemanticPalette(a, b *VoxelPaletteAsset) bool {
	if !equalMaterialFingerprintPalette(a, b) || (a.Materials == nil) != (b.Materials == nil) ||
		(a.Animations == nil) != (b.Animations == nil) || (a.MaterialFrameOverrides == nil) != (b.MaterialFrameOverrides == nil) ||
		(a.SurfaceMaterials == nil) != (b.SurfaceMaterials == nil) || len(a.SurfaceMaterials) != len(b.SurfaceMaterials) {
		return false
	}
	for i, material := range a.Materials {
		if (material.Property == nil) != (b.Materials[i].Property == nil) {
			return false
		}
	}
	for index, surface := range a.SurfaceMaterials {
		other, exists := b.SurfaceMaterials[index]
		if !exists || surface.Kind != other.Kind || (surface.Tags == nil) != (other.Tags == nil) || !slices.Equal(surface.Tags, other.Tags) {
			return false
		}
	}
	return true
}

func cloneMaterialSemanticPalette(p *VoxelPaletteAsset) VoxelPaletteAsset {
	copyPalette := cloneMaterialFingerprintPalette(p)
	// Preserve nil/empty distinctions present in the canonical encoding.
	if p.Materials != nil && copyPalette.Materials == nil {
		copyPalette.Materials = []VoxMaterial{}
	}
	if p.Animations != nil {
		copyPalette.Animations = []VoxelPaletteAnimation{}
	}
	if p.MaterialFrameOverrides != nil {
		copyPalette.MaterialFrameOverrides = map[uint8]VoxelPaletteMaterialFrameOverride{}
	}
	for i := range copyPalette.Materials {
		if p.Materials[i].Property != nil && copyPalette.Materials[i].Property == nil {
			copyPalette.Materials[i].Property = map[string]interface{}{}
		}
	}
	if p.SurfaceMaterials != nil {
		copyPalette.SurfaceMaterials = make(map[uint8]VoxelSurfaceMaterial, len(p.SurfaceMaterials))
		for index, surface := range p.SurfaceMaterials {
			surface.Kind = strings.Clone(surface.Kind)
			if surface.Tags != nil {
				tags := make([]string, len(surface.Tags))
				for i, tag := range surface.Tags {
					tags[i] = strings.Clone(tag)
				}
				surface.Tags = tags
			}
			copyPalette.SurfaceMaterials[index] = surface
		}
	}
	return copyPalette
}

func (s *VoxelRtState) materialSemantic(id AssetId, palette *VoxelPaletteAsset) *voxelMaterialSemantic {
	defer s.publishMaterialSemanticOwnership()
	old := s.materialSemantics[id]
	if old != nil && equalMaterialSemanticPalette(&old.palette, palette) {
		return old
	}
	if old != nil {
		delete(s.materialSemantics, id)
		s.materialSemanticBytes -= old.bytes
	}
	bytes, supported := materialSemanticCharge(palette, VoxelMaterialSemanticIdentityBudgetBytes-s.materialSemanticBytes)
	if !supported {
		return nil
	}
	s.VoxelMaterialSemanticIdentityBuildCount++
	key, err := checkedVoxelPaletteAssetCacheKey(*palette)
	if err != nil {
		return nil
	}
	entry := &voxelMaterialSemantic{palette: cloneMaterialSemanticPalette(palette), key: key, bytes: bytes}
	if s.materialSemantics == nil {
		s.materialSemantics = make(map[AssetId]*voxelMaterialSemantic)
	}
	s.materialSemantics[id] = entry
	s.materialSemanticBytes += bytes
	return entry
}

func (s *VoxelRtState) syncMaterialBinding(obj *core.VoxelObject, key materialTableCacheKey, palette *VoxelPaletteAsset, semantic *voxelMaterialSemantic) {
	previousKey, hasKey := s.lastMaterialKeys[obj]
	previousSemantic, hasSemantic := s.lastMaterialSemantics[obj]
	rowsChanged := !hasKey || previousKey != key
	semanticChanged := !hasSemantic || previousSemantic != semantic
	// Observe direct raw edits every sync. A detached proof stays private until
	// the palette owner actually changes the semantic or rendering binding.
	obj.ImmutableMaterialTable()
	if !rowsChanged && !semanticChanged {
		return
	}
	var rows []core.Material
	if rowsChanged {
		rows = s.buildMaterialTable(key, palette)
		s.lastMaterialKeys[obj] = key
	}
	if semantic != nil {
		if semantic.table == nil {
			table := s.materialTableCache[key]
			semantic.table, _ = core.NewImmutableMaterialTable(table, semantic.key)
		}
		obj.SetImmutableMaterialTable(semantic.table)
	} else {
		obj.SetImmutableMaterialTable(nil)
		if rowsChanged {
			obj.MaterialTable = append([]core.Material(nil), rows...)
		}
	}
	if s.lastMaterialSemantics == nil {
		s.lastMaterialSemantics = make(map[*core.VoxelObject]*voxelMaterialSemantic)
	}
	s.lastMaterialSemantics[obj] = semantic
}

func (s *VoxelRtState) pruneMaterialSemantics(used map[AssetId]*voxelMaterialSemantic) {
	retained := 0
	for id, entry := range s.materialSemantics {
		if used[id] == entry {
			retained++
		}
	}
	if retained == 0 {
		s.materialSemantics = nil
		s.materialSemanticBytes = 0
		s.publishMaterialSemanticOwnership()
		return
	}
	if retained == len(s.materialSemantics) {
		s.publishMaterialSemanticOwnership()
		return
	}
	var entries map[AssetId]*voxelMaterialSemantic
	var bytes uint64
	for id, entry := range s.materialSemantics {
		if used[id] == entry {
			if entries == nil {
				entries = make(map[AssetId]*voxelMaterialSemantic)
			}
			entries[id] = entry
			bytes += entry.bytes
		}
	}
	s.materialSemantics, s.materialSemanticBytes = entries, bytes
	s.publishMaterialSemanticOwnership()
}
