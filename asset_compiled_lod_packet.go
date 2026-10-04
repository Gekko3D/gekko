package gekko

import (
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Proof storage is constructed inside an authenticated verification session.
// Its maps stay private and immutable, separate from mutable packet/publication
// sources. Ordinary asset adoption must retain its own independent proof copy.
type compiledAssetLODProof struct {
	sourceContentID string
	contentID       string
	lattice         content.VoxelObjectLatticeDef
	value           uint8
	full            *volume.XBrickMap
	coarse          *volume.XBrickMap
}

type compiledAssetPacketLOD struct {
	contentID       string
	sourceContentID string
	value           uint8
	source          *volume.XBrickMap
	registration    *streamedGeometryRegistration
	proof           *compiledAssetLODProof
}

// The session has already authenticated exact factor-2 source coverage. Read
// its derivative bricks directly; never regenerate reduction or retain a frame.
func prepareVerifiedCompiledAssetPacketLOD(verified verifiedCompiledAssetLOD, fullBaseline *volume.XBrickMap) *compiledAssetPacketLOD {
	def := verified.definition
	coarse, _ := compiledShapeGeometry(&content.CompiledAssetShapeDef{Lattice: def.SourceLattice, Bricks: def.Bricks})
	source := coarse.Copy()
	return &compiledAssetPacketLOD{
		contentID: verified.contentID, sourceContentID: def.SourceContentID, value: def.Value,
		source: source, registration: prepareStreamedGeometryRegistration(source),
		proof: &compiledAssetLODProof{sourceContentID: def.SourceContentID, contentID: verified.contentID,
			lattice: def.SourceLattice, value: def.Value, full: fullBaseline, coarse: coarse},
	}
}
