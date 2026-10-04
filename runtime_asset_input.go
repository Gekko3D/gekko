package gekko

import (
	"fmt"
	"math/bits"
	"path/filepath"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Selection is explicit. Public LoadAsset remains a strict authoring JSON API.
type runtimeAssetInput struct {
	definition    *content.AssetDef
	compiled      *content.CompiledAssetHeaderDef
	compiledModel *content.CompiledAssetModelHeaderDef
}

// Extensions select one strict typed format; no probing or authoring fallback.
func isCompiledAssetPath(path string) bool {
	switch filepath.Ext(path) {
	case ".gkassetc", ".gkmodelassetc":
		return true
	default:
		return false
	}
}

func loadRuntimeAssetInput(loader *RuntimeContentLoader, path string) (runtimeAssetInput, error) {
	if filepath.Ext(path) == ".gkmodelassetc" {
		header, _, err := loader.LoadCompiledAssetModelHeader(path)
		if err != nil {
			return runtimeAssetInput{}, err
		}
		return runtimeAssetInput{definition: header.Asset, compiledModel: header}, nil
	}
	if filepath.Ext(path) == ".gkassetc" {
		header, _, err := loader.LoadCompiledAssetHeader(path)
		if err != nil {
			return runtimeAssetInput{}, err
		}
		return runtimeAssetInput{definition: header.Asset, compiled: header}, nil
	}
	definition, err := loader.LoadAsset(path)
	return runtimeAssetInput{definition: definition}, err
}

type runtimeAssetCanonicalPart struct {
	assetID        string
	lattice        content.VoxelObjectLatticeDef
	geometry       *volume.XBrickMap
	snapshot       *content.VoxelObjectSnapshotDef
	identity       string
	decodedBytes   int64
	bricks, voxels int
}

func runtimeContentOriginOpen(loader *RuntimeContentLoader) bool {
	if loader == nil || loader.scope == nil {
		return true
	}
	loader.owner.mu.Lock()
	defer loader.owner.mu.Unlock()
	return !loader.scope.closed
}

// A compiled proof leases its own entries, so rejecting an untrusted reference
// cannot release a definition already accepted by the calling scope. Only owned
// geometry and scalar metadata leave this boundary.
type runtimeAssetCanonicalOptions struct {
	proveAuthoredBase bool
	acceptMetadata    func(assetID string, lattice content.VoxelObjectLatticeDef) bool
}

func loadRuntimeAssetCanonicalPart(loader *RuntimeContentLoader, path, itemID string, needBase bool, options runtimeAssetCanonicalOptions) (runtimeAssetCanonicalPart, error) {
	var result runtimeAssetCanonicalPart
	if !runtimeContentOriginOpen(loader) {
		return result, fmt.Errorf("content origin scope is closed")
	}
	verificationLoader := loader
	if isCompiledAssetPath(path) {
		scope := loader.NewScope()
		defer scope.Close()
		verificationLoader = scope.Loader()
	}
	input, err := loadRuntimeAssetInput(verificationLoader, path)
	if err != nil {
		return result, err
	}
	def := input.definition
	if def.Runtime != nil && def.Runtime.CollapseVoxelParts {
		return result, fmt.Errorf("voxel-object payload requires individual authored parts")
	}
	var selected *content.AssetPartDef
	for i := range def.Parts {
		if def.Parts[i].ID == itemID {
			selected = &def.Parts[i]
			break
		}
	}
	if selected == nil {
		return result, fmt.Errorf("voxel-object part %q is missing", itemID)
	}
	part := *selected
	if part.Source.Kind != content.AssetSourceKindVoxelShape || part.Source.VoxelShape == nil {
		return result, fmt.Errorf("voxel-object part %q is not an authored voxel_shape", itemID)
	}
	result.assetID = def.ID
	result.lattice = authoredVoxelShapeLattice(part.VoxelResolution)
	if options.acceptMetadata != nil && !options.acceptMetadata(result.assetID, result.lattice) {
		return runtimeAssetCanonicalPart{}, fmt.Errorf("canonical asset owner or lattice mismatch")
	}
	if needBase {
		if input.compiled == nil && input.compiledModel == nil {
			result.geometry = buildAuthoredVoxelShapeMap(part)
			snapshot := VoxelObjectSnapshotFromXBrickMap(result.geometry)
			result.snapshot = snapshot
			if options.proveAuthoredBase {
				result.identity, result.decodedBytes, err = content.VoxelObjectBaseIdentity(snapshot, result.lattice, nil)
			}
			result.bricks = persistenceMapBrickCount(result.geometry)
			result.voxels = len(snapshot.Voxels)
		} else {
			var ref *content.CompiledAssetShapeRefDef
			var refs []content.CompiledAssetShapeRefDef
			if input.compiled != nil {
				refs = input.compiled.Shapes
			} else {
				refs = input.compiledModel.Shapes
			}
			for i := range refs {
				if refs[i].PartID == itemID {
					ref = &refs[i]
					break
				}
			}
			if ref == nil {
				return runtimeAssetCanonicalPart{}, fmt.Errorf("compiled shape reference is missing")
			}
			shape, info, loadErr := verificationLoader.LoadCompiledAssetShape(filepath.Join(filepath.Dir(path), filepath.FromSlash(ref.Path)))
			if loadErr != nil {
				return runtimeAssetCanonicalPart{}, loadErr
			}
			if info.ContentID != ref.ContentID || info.EncodedBytes != ref.EncodedBytes || info.DecodedBytes != ref.DecodedBytes || shape.Lattice != result.lattice {
				return runtimeAssetCanonicalPart{}, fmt.Errorf("compiled shape reference identity, size or lattice mismatch")
			}
			// E2 qualification retains its default logical proof profile independently
			// of the shipping frame's decoder profile or borrowed codec lifetime.
			result.identity, result.decodedBytes, err = content.CompiledAssetShapeBaseIdentity(shape, nil)
			if err == nil && result.identity != ref.BaseIdentity {
				err = fmt.Errorf("compiled shape original base identity mismatch")
			}
			if err == nil {
				result.geometry, result.voxels = compiledShapeGeometry(shape)
				result.bricks = len(shape.Bricks)
			}
		}
		if err != nil {
			return runtimeAssetCanonicalPart{}, err
		}
	}
	if !runtimeContentOriginOpen(loader) {
		return runtimeAssetCanonicalPart{}, fmt.Errorf("content origin scope is closed")
	}
	return result, nil
}

// The shape must have passed typed and logical-profile validation before this
// conversion. Geometry is independently owned and already contains ModelScale.
func compiledShapeGeometry(shape *content.CompiledAssetShapeDef) (*volume.XBrickMap, int) {
	voxels := 0
	geometry := volume.BuildXBrickMap(func(yield func(volume.VoxelWrite) bool) {
		for _, brick := range shape.Bricks {
			index := 0
			for word, mask := range brick.Occupancy {
				for mask != 0 {
					linear := word*64 + bits.TrailingZeros64(mask)
					write := volume.VoxelWrite{X: int(brick.Coord[0])*8 + linear%8, Y: int(brick.Coord[1])*8 + (linear/8)%8, Z: int(brick.Coord[2])*8 + linear/64, Value: brick.Values[index]}
					if !yield(write) {
						return
					}
					index++
					voxels++
					mask &= mask - 1
				}
			}
		}
	})
	geometry.ComputeAABB()
	geometry.ClearDirty()
	return geometry, voxels
}
