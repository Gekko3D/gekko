package gekko

import (
	"math"
	"reflect"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

const waterBodySurfaceBandEpsilon float32 = 0.05

type waterStaticCollider struct {
	Min mgl32.Vec3
	Max mgl32.Vec3
}

type waterPatchRect struct {
	minX float32
	maxX float32
	minZ float32
	maxZ float32
}

// waterBodyResolutionSystem resolves authored water bodies into deterministic
// runtime patch entities without yet switching renderer or interaction systems
// to consume those resolved patches.
func waterBodyResolutionSystem(cmd *Commands, assets *AssetServer, state *WaterBodyResolutionState) {
	if cmd == nil || state == nil {
		return
	}

	state.ensureMaps()
	staticColliders := collectWaterStaticColliders(cmd)
	staticVoxelOccupancy := collectWaterStaticVoxelOccupancy(cmd, assets)
	active := make(map[EntityId]struct{})
	transformType := reflect.TypeOf(TransformComponent{})
	splashType := reflect.TypeOf(WaterSplashEffectComponent{})

	MakeQuery1[WaterBodyComponent](cmd).Map(func(eid EntityId, body *WaterBodyComponent) bool {
		if body == nil {
			return true
		}
		active[eid] = struct{}{}
		if body.Disabled {
			clearResolvedWaterPatchEntities(cmd, state, eid)
			delete(state.ByEntity, eid)
			return true
		}

		if _, ok := state.ByEntity[eid]; ok {
			return true
		}

		var tr *TransformComponent
		if comp := cmd.GetComponent(eid, transformType); comp != nil {
			if typed, ok := comp.(TransformComponent); ok {
				copy := typed
				tr = &copy
			}
		}

		record, patches := resolveWaterBody(eid, body, tr, staticColliders, staticVoxelOccupancy)
		state.ByEntity[eid] = record
		ownerSplash, hasOwnerSplash := lookupWaterSplashEffect(cmd, eid, splashType)
		if len(patches) > 0 {
			patchIDs := make([]EntityId, 0, len(patches))
			for i := range patches {
				patch := patches[i]
				components := []any{
					&TransformComponent{Position: patch.Center, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
					&patch,
				}
				if hasOwnerSplash {
					splashCopy := ownerSplash
					components = append(components, &splashCopy)
				}
				patchID := cmd.AddEntity(components...)
				patchIDs = append(patchIDs, patchID)
			}
			state.PatchEntitiesByOwner[eid] = patchIDs
		}
		return true
	})

	for eid := range state.ByEntity {
		if _, ok := active[eid]; !ok {
			clearResolvedWaterPatchEntities(cmd, state, eid)
			delete(state.ByEntity, eid)
		}
	}
	for eid := range state.PatchEntitiesByOwner {
		if _, ok := active[eid]; !ok {
			clearResolvedWaterPatchEntities(cmd, state, eid)
		}
	}
}

func lookupWaterSplashEffect(cmd *Commands, eid EntityId, splashType reflect.Type) (WaterSplashEffectComponent, bool) {
	if cmd == nil {
		return WaterSplashEffectComponent{}, false
	}
	comp := cmd.GetComponent(eid, splashType)
	switch typed := comp.(type) {
	case WaterSplashEffectComponent:
		return typed, true
	case *WaterSplashEffectComponent:
		if typed != nil {
			return *typed, true
		}
	}
	return WaterSplashEffectComponent{}, false
}

func clearResolvedWaterPatchEntities(cmd *Commands, state *WaterBodyResolutionState, owner EntityId) {
	if cmd == nil || state == nil {
		return
	}
	for _, patchID := range state.PatchEntitiesByOwner[owner] {
		cmd.RemoveEntity(patchID)
	}
	delete(state.PatchEntitiesByOwner, owner)
}

func resolveWaterBody(owner EntityId, body *WaterBodyComponent, tr *TransformComponent, staticColliders []waterStaticCollider, staticVoxelOccupancy []waterStaticCollider) (WaterBodyResolvedRecord, []ResolvedWaterPatchComponent) {
	record := WaterBodyResolvedRecord{Status: WaterBodyResolutionStatusFailed}
	if body == nil || body.Disabled {
		return record, nil
	}
	if issues := body.ValidationIssues(); len(issues) > 0 {
		record.Warnings = append(record.Warnings, issues...)
		return record, nil
	}

	switch body.NormalizedMode() {
	case WaterBodyModeFitBounds:
		rects := fitWaterBodyBoundsFromStaticColliders(body, staticColliders)
		source := WaterFitSourceStaticCollider
		if len(rects) == 0 {
			rects = fitWaterBodyBoundsFromStaticColliders(body, staticVoxelOccupancy)
			source = WaterFitSourceVoxelOccupancy
		}
		if len(rects) == 0 {
			record.Warnings = append(record.Warnings, "no enclosed interior found from static colliders or voxel occupancy")
			return record, nil
		}
		patches := makeResolvedWaterPatches(owner, body, tr, rects, source)
		record.Status = WaterBodyResolutionStatusResolved
		record.PatchCount = uint32(len(patches))
		record.PrimarySource = source
		return record, patches
	default:
		rect := waterPatchRect{}
		center := body.anchorCenter(tr)
		ext := body.NormalizedRectHalfExtents()
		rect.minX = center.X() - ext[0]
		rect.maxX = center.X() + ext[0]
		rect.minZ = center.Z() - ext[1]
		rect.maxZ = center.Z() + ext[1]
		patches := makeResolvedWaterPatches(owner, body, tr, []waterPatchRect{rect}, "")
		record.Status = WaterBodyResolutionStatusResolved
		record.PatchCount = uint32(len(patches))
		return record, patches
	}
}

func makeResolvedWaterPatches(owner EntityId, body *WaterBodyComponent, tr *TransformComponent, rects []waterPatchRect, source WaterFitSource) []ResolvedWaterPatchComponent {
	patches := make([]ResolvedWaterPatchComponent, 0, len(rects))
	for i := range rects {
		rect := rects[i]
		if rect.maxX <= rect.minX || rect.maxZ <= rect.minZ {
			continue
		}
		patchCenter := mgl32.Vec3{
			(rect.minX + rect.maxX) * 0.5,
			body.NormalizedSurfaceY(),
			(rect.minZ + rect.maxZ) * 0.5,
		}
		patches = append(patches, ResolvedWaterPatchComponent{
			Owner:           owner,
			PatchIndex:      uint32(len(patches)),
			Kind:            WaterPatchKindSurface,
			Center:          patchCenter,
			HalfExtents:     [2]float32{(rect.maxX - rect.minX) * 0.5, (rect.maxZ - rect.minZ) * 0.5},
			Depth:           body.NormalizedDepth(),
			Color:           body.NormalizedColor(),
			AbsorptionColor: body.NormalizedAbsorptionColor(),
			Opacity:         body.NormalizedOpacity(),
			Roughness:       body.NormalizedRoughness(),
			Refraction:      body.NormalizedRefraction(),
			FlowDirection:   body.NormalizedFlowDirection(),
			FlowSpeed:       body.NormalizedFlowSpeed(),
			WaveAmplitude:   body.NormalizedWaveAmplitude(),
			Source:          source,
			DebugInset:      body.NormalizedInset(),
			DebugOverlap:    body.NormalizedOverlap(),
		})
	}
	return patches
}

func fitWaterBodyBoundsFromStaticColliders(body *WaterBodyComponent, colliders []waterStaticCollider) []waterPatchRect {
	boundsCenter := body.NormalizedBoundsCenter()
	boundsHalf := body.NormalizedBoundsHalfExtents()
	cellSize := body.NormalizedMinCellSize()
	if cellSize <= 0 {
		return nil
	}

	minX := boundsCenter.X() - boundsHalf.X()
	maxX := boundsCenter.X() + boundsHalf.X()
	minZ := boundsCenter.Z() - boundsHalf.Z()
	maxZ := boundsCenter.Z() + boundsHalf.Z()
	if maxX <= minX || maxZ <= minZ {
		return nil
	}

	cellsX := maxInt(1, int(ceilf((maxX-minX)/cellSize)))
	cellsZ := maxInt(1, int(ceilf((maxZ-minZ)/cellSize)))
	solid := make([]bool, cellsX*cellsZ)

	for _, collider := range colliders {
		if collider.Max.Y() < body.NormalizedSurfaceY()-waterBodySurfaceBandEpsilon || collider.Min.Y() > body.NormalizedSurfaceY()+waterBodySurfaceBandEpsilon {
			continue
		}
		if collider.Max.X() <= minX || collider.Min.X() >= maxX || collider.Max.Z() <= minZ || collider.Min.Z() >= maxZ {
			continue
		}
		for z := 0; z < cellsZ; z++ {
			cellMinZ := minZ + float32(z)*cellSize
			cellMaxZ := cellMinZ + cellSize
			if collider.Max.Z() <= cellMinZ || collider.Min.Z() >= cellMaxZ {
				continue
			}
			for x := 0; x < cellsX; x++ {
				cellMinX := minX + float32(x)*cellSize
				cellMaxX := cellMinX + cellSize
				if collider.Max.X() <= cellMinX || collider.Min.X() >= cellMaxX {
					continue
				}
				solid[z*cellsX+x] = true
			}
		}
	}

	outside := make([]bool, len(solid))
	queue := make([]int, 0, cellsX*cellsZ)
	pushOutside := func(x, z int) {
		if x < 0 || x >= cellsX || z < 0 || z >= cellsZ {
			return
		}
		idx := z*cellsX + x
		if solid[idx] || outside[idx] {
			return
		}
		outside[idx] = true
		queue = append(queue, idx)
	}
	for x := 0; x < cellsX; x++ {
		pushOutside(x, 0)
		pushOutside(x, cellsZ-1)
	}
	for z := 0; z < cellsZ; z++ {
		pushOutside(0, z)
		pushOutside(cellsX-1, z)
	}
	for head := 0; head < len(queue); head++ {
		idx := queue[head]
		x := idx % cellsX
		z := idx / cellsX
		pushOutside(x-1, z)
		pushOutside(x+1, z)
		pushOutside(x, z-1)
		pushOutside(x, z+1)
	}

	inside := make([]bool, len(solid))
	for i := range inside {
		inside[i] = !solid[i] && !outside[i]
	}

	consumed := make([]bool, len(inside))
	inset := body.NormalizedInset()
	rects := make([]waterPatchRect, 0, 4)
	for z := 0; z < cellsZ; z++ {
		for x := 0; x < cellsX; x++ {
			idx := z*cellsX + x
			if !inside[idx] || consumed[idx] {
				continue
			}
			width := 1
			for x+width < cellsX && inside[z*cellsX+x+width] && !consumed[z*cellsX+x+width] {
				width++
			}
			height := 1
			for {
				nextZ := z + height
				if nextZ >= cellsZ {
					break
				}
				ok := true
				for scanX := x; scanX < x+width; scanX++ {
					scanIdx := nextZ*cellsX + scanX
					if !inside[scanIdx] || consumed[scanIdx] {
						ok = false
						break
					}
				}
				if !ok {
					break
				}
				height++
			}
			for fillZ := z; fillZ < z+height; fillZ++ {
				for fillX := x; fillX < x+width; fillX++ {
					consumed[fillZ*cellsX+fillX] = true
				}
			}

			rect := waterPatchRect{
				minX: minX + float32(x)*cellSize + inset,
				maxX: minX + float32(x+width)*cellSize - inset,
				minZ: minZ + float32(z)*cellSize + inset,
				maxZ: minZ + float32(z+height)*cellSize - inset,
			}
			if rect.maxX > rect.minX && rect.maxZ > rect.minZ {
				rects = append(rects, rect)
			}
		}
	}

	maxPatches := int(body.NormalizedMaxPatchCount())
	if maxPatches > 0 && len(rects) > maxPatches {
		rects = rects[:maxPatches]
	}
	return rects
}

func collectWaterStaticColliders(cmd *Commands) []waterStaticCollider {
	if cmd == nil {
		return nil
	}
	out := make([]waterStaticCollider, 0, 8)
	MakeQuery3[TransformComponent, RigidBodyComponent, ColliderComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, rb *RigidBodyComponent, col *ColliderComponent) bool {
		if tr == nil || rb == nil || col == nil || !rb.IsStatic {
			return true
		}
		minV, maxV := waterStaticColliderAABB(tr, col)
		out = append(out, waterStaticCollider{Min: minV, Max: maxV})
		return true
	})
	return out
}

func collectWaterStaticVoxelOccupancy(cmd *Commands, assets *AssetServer) []waterStaticCollider {
	if cmd == nil || assets == nil {
		return nil
	}
	out := make([]waterStaticCollider, 0, 32)
	MakeQuery3[TransformComponent, RigidBodyComponent, VoxelModelComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, rb *RigidBodyComponent, vmc *VoxelModelComponent) bool {
		if tr == nil || rb == nil || vmc == nil || !rb.IsStatic {
			return true
		}
		geometry, voxelTransform, ok := resolveStaticVoxelOccupancyGeometry(assets, tr, vmc)
		if !ok || geometry == nil || geometry.XBrickMap == nil {
			return true
		}
		out = append(out, collectWorldVoxelOccupancyAABBs(geometry.XBrickMap, voxelTransform)...)
		return true
	})
	return out
}

func resolveStaticVoxelOccupancyGeometry(assets *AssetServer, tr *TransformComponent, vmc *VoxelModelComponent) (*VoxelGeometryAsset, TransformComponent, bool) {
	if assets == nil || tr == nil || vmc == nil {
		return nil, TransformComponent{}, false
	}
	vmcCopy := *vmc
	vmcCopy.NormalizeGeometryRefs()
	_, geometry, ok := ResolveVoxelGeometry(assets, &vmcCopy)
	if !ok || geometry == nil || geometry.XBrickMap == nil {
		return nil, TransformComponent{}, false
	}
	voxelTransform := *tr
	voxelTransform.Scale = EffectiveVoxelScale(&vmcCopy, tr)
	voxelTransform.Pivot = authoredCollapsePivot(vmcCopy, *geometry)
	return geometry, voxelTransform, true
}

func collectWorldVoxelOccupancyAABBs(xbm *volume.XBrickMap, tr TransformComponent) []waterStaticCollider {
	if xbm == nil {
		return nil
	}
	minB, maxB := xbm.ComputeAABB()
	if maxB.X() <= minB.X() || maxB.Y() <= minB.Y() || maxB.Z() <= minB.Z() {
		return nil
	}

	minX := int(math.Floor(float64(minB.X())))
	maxX := int(math.Ceil(float64(maxB.X())))
	minY := int(math.Floor(float64(minB.Y())))
	maxY := int(math.Ceil(float64(maxB.Y())))
	minZ := int(math.Floor(float64(minB.Z())))
	maxZ := int(math.Ceil(float64(maxB.Z())))

	o2w := tr.ObjectToWorld()
	out := make([]waterStaticCollider, 0, maxInt(0, (maxX-minX)*(maxY-minY)*(maxZ-minZ)))
	for z := minZ; z < maxZ; z++ {
		for y := minY; y < maxY; y++ {
			for x := minX; x < maxX; x++ {
				found, _ := xbm.GetVoxel(x, y, z)
				if !found {
					continue
				}
				minV, maxV := transformedVoxelCellAABB(o2w, x, y, z)
				out = append(out, waterStaticCollider{Min: minV, Max: maxV})
			}
		}
	}
	return out
}

func transformedVoxelCellAABB(o2w mgl32.Mat4, x, y, z int) (mgl32.Vec3, mgl32.Vec3) {
	corners := [8]mgl32.Vec3{
		{float32(x), float32(y), float32(z)},
		{float32(x + 1), float32(y), float32(z)},
		{float32(x), float32(y + 1), float32(z)},
		{float32(x + 1), float32(y + 1), float32(z)},
		{float32(x), float32(y), float32(z + 1)},
		{float32(x + 1), float32(y), float32(z + 1)},
		{float32(x), float32(y + 1), float32(z + 1)},
		{float32(x + 1), float32(y + 1), float32(z + 1)},
	}
	minV := mgl32.Vec3{float32(1e20), float32(1e20), float32(1e20)}
	maxV := mgl32.Vec3{float32(-1e20), float32(-1e20), float32(-1e20)}
	for _, corner := range corners {
		world := o2w.Mul4x1(corner.Vec4(1)).Vec3()
		minV = mgl32.Vec3{minf(minV.X(), world.X()), minf(minV.Y(), world.Y()), minf(minV.Z(), world.Z())}
		maxV = mgl32.Vec3{maxf(maxV.X(), world.X()), maxf(maxV.Y(), world.Y()), maxf(maxV.Z(), world.Z())}
	}
	return minV, maxV
}

func waterStaticColliderAABB(tr *TransformComponent, col *ColliderComponent) (mgl32.Vec3, mgl32.Vec3) {
	if tr == nil || col == nil {
		return mgl32.Vec3{}, mgl32.Vec3{}
	}
	switch col.Shape {
	case ShapeSphere:
		radius := col.Radius
		if radius <= 0 {
			radius = maxf(col.AABBHalfExtents.X(), maxf(col.AABBHalfExtents.Y(), col.AABBHalfExtents.Z()))
		}
		scale := maxf(absWaterFloat(tr.Scale.X()), maxf(absWaterFloat(tr.Scale.Y()), absWaterFloat(tr.Scale.Z())))
		ext := mgl32.Vec3{radius * scale, radius * scale, radius * scale}
		return tr.Position.Sub(ext), tr.Position.Add(ext)
	default:
		half := col.HalfExtents
		if half == (mgl32.Vec3{}) {
			half = col.AABBHalfExtents
		}
		scaled := mgl32.Vec3{
			absWaterFloat(tr.Scale.X()) * half.X(),
			absWaterFloat(tr.Scale.Y()) * half.Y(),
			absWaterFloat(tr.Scale.Z()) * half.Z(),
		}
		rotMat := tr.Rotation.Mat4()
		axes := [3]mgl32.Vec3{rotMat.Col(0).Vec3(), rotMat.Col(1).Vec3(), rotMat.Col(2).Vec3()}
		extents := mgl32.Vec3{}
		for i := 0; i < 3; i++ {
			for j := 0; j < 3; j++ {
				extents[i] += absf(axes[j][i]) * scaled[j]
			}
		}
		return tr.Position.Sub(extents), tr.Position.Add(extents)
	}
}

func (w *WaterBodyComponent) anchorCenter(tr *TransformComponent) mgl32.Vec3 {
	if w == nil {
		return mgl32.Vec3{}
	}
	if w.NormalizedMode() == WaterBodyModeFitBounds {
		center := w.NormalizedBoundsCenter()
		center[1] = w.NormalizedSurfaceY()
		return center
	}
	if tr != nil {
		center := tr.Position
		center[1] = w.NormalizedSurfaceY()
		return center
	}
	center := w.NormalizedBoundsCenter()
	center[1] = w.NormalizedSurfaceY()
	return center
}

func ceilf(v float32) float32 {
	return float32(math.Ceil(float64(v)))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
