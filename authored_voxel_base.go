package gekko

import (
	"math"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// This version binds the existing snapshot conversion and ModelScale resampling
// rules. A rasterization change must use a new version before reusing bases.
const authoredVoxelShapeRasterizationVersion = "gekko-voxel-shape-v1"

type authoredVoxelBase struct {
	identity string
	lattice  content.VoxelObjectLatticeDef
}

func authoredVoxelShapeLattice(resolution float32) content.VoxelObjectLatticeDef {
	return content.VoxelObjectLatticeDef{
		VoxelResolution:      VoxelResolutionOrDefault(&VoxelModelComponent{VoxelResolution: resolution}),
		RasterizationVersion: authoredVoxelShapeRasterizationVersion,
	}
}

func validAuthoredVoxelShapeLattice(lattice content.VoxelObjectLatticeDef) bool {
	return lattice.VoxelResolution > 0 && !math.IsInf(float64(lattice.VoxelResolution), 0) && !math.IsNaN(float64(lattice.VoxelResolution))
}

func (assets *AssetServer) authoredVoxelBaseIdentity(id AssetId, lattice content.VoxelObjectLatticeDef) string {
	assets.mu.RLock()
	defer assets.mu.RUnlock()
	return assets.authoredVoxelBases[id][lattice]
}

// Record only canonical, freshly constructed authored geometry. This metadata
// never treats a mutable cached asset map as its original construction source.
// Invalid identity/lattice falls back without changing legacy build behavior.
func (assets *AssetServer) recordAuthoredVoxelBase(id AssetId, lattice content.VoxelObjectLatticeDef, source *volume.XBrickMap) {
	identity, _, err := content.VoxelObjectBaseIdentity(VoxelObjectSnapshotFromXBrickMap(source), lattice, nil)
	if err != nil {
		return
	}
	assets.mu.Lock()
	defer assets.mu.Unlock()
	if _, exists := assets.voxModels[id]; !exists {
		return
	}
	if assets.authoredVoxelBases == nil {
		assets.authoredVoxelBases = make(map[AssetId]map[content.VoxelObjectLatticeDef]string)
	}
	bases := assets.authoredVoxelBases[id]
	if bases == nil {
		bases = make(map[content.VoxelObjectLatticeDef]string)
		assets.authoredVoxelBases[id] = bases
	}
	if bases[lattice] == "" {
		bases[lattice] = identity
	}
}

// managedVoxelGeometryBase reads captured provenance without scanning geometry.
// Tracking remains construction-relative across ordinary managed writes. A
// changed lattice, owner, reference, qualification or exposure selects full
// fallback; no cached raw payload is used to invent authored provenance.
func managedVoxelGeometryBase(cmd *Commands, assets *AssetServer, eid EntityId) (string, content.VoxelObjectLatticeDef, bool) {
	vmc, entry, enabled := managedVoxelEntity(cmd, assets, eid)
	if !enabled || entry.exposed || entry.authoredBase.identity == "" {
		return "", content.VoxelObjectLatticeDef{}, false
	}
	base := entry.authoredBase
	if base.lattice.VoxelResolution != VoxelResolutionOrDefault(&vmc) {
		return "", content.VoxelObjectLatticeDef{}, false
	}
	if err := managedVoxelRuntimeQualification(cmd, assets, eid); err != nil {
		return "", content.VoxelObjectLatticeDef{}, false
	}
	return base.identity, base.lattice, true
}

// Qualify the isolated construction snapshot, not the public source pointer.
// Foreign overrides and exposed managed sources start unbound construction bases.
func (assets *AssetServer) qualifyManagedAuthoredVoxelBase(source VoxelModelComponent, sourceEntry *managedVoxelGeometry, override AssetId) {
	if source.OverrideGeometry != (AssetId{}) || sourceEntry != nil && sourceEntry.exposed {
		return
	}
	lattice := authoredVoxelShapeLattice(source.VoxelResolution)
	if !validAuthoredVoxelShapeLattice(lattice) {
		return
	}
	expected := assets.authoredVoxelBaseIdentity(source.GeometryAsset(), lattice)
	if expected == "" {
		return
	}
	isolated, ok := assets.getVoxelGeometry(override)
	if !ok || isolated.XBrickMap == nil {
		return
	}
	identity, _, err := content.VoxelObjectBaseIdentity(VoxelObjectSnapshotFromXBrickMap(isolated.XBrickMap), lattice, nil)
	if err != nil || identity != expected {
		return
	}
	assets.mu.Lock()
	defer assets.mu.Unlock()
	if entry := assets.managedVoxelGeometry[override]; entry != nil && !entry.exposed {
		entry.authoredBase = authoredVoxelBase{identity: identity, lattice: lattice}
	}
}
