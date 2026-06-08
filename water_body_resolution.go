package gekko

import (
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

const waterBodySurfaceBandEpsilon float32 = 0.05

type waterStaticCollider struct {
	Min  mgl32.Vec3
	Max  mgl32.Vec3
	Tags []string
}

type waterPatchRect struct {
	minX float32
	maxX float32
	minZ float32
	maxZ float32
}

type waterBodyResolutionSignature struct {
	Mode                 WaterBodyMode
	SurfaceY             float32
	Depth                float32
	RectHalfExtents      [2]float32
	BoundsCenter         mgl32.Vec3
	BoundsHalfExtents    mgl32.Vec3
	Inset                float32
	Overlap              float32
	MinCellSize          float32
	SourceTag            string
	ContinuityGroup      string
	EnableSkirt          bool
	MaxPatchCount        uint32
	Color                [3]float32
	AbsorptionColor      [3]float32
	Opacity              float32
	Roughness            float32
	Refraction           float32
	DirectLightOcclusion float32
	FlowDirection        [2]float32
	FlowSpeed            float32
	WaveAmplitude        float32
	VisualCellSize       float32
	AnchorPosition       mgl32.Vec3
	HasSplash            bool
	Splash               WaterSplashEffectComponent
	SourceHash           uint64
}

// waterBodyResolutionSystem resolves authored water bodies into deterministic
// runtime patch entities consumed by renderer and interaction systems.
func waterBodyResolutionSystem(cmd *Commands, assets *AssetServer, state *WaterBodyResolutionState) {
	if cmd == nil || state == nil {
		return
	}

	state.ensureMaps()
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
			delete(state.signaturesByEntity, eid)
			return true
		}
		return true
	})

	for eid := range state.ByEntity {
		if _, ok := active[eid]; !ok {
			clearResolvedWaterPatchEntities(cmd, state, eid)
			delete(state.ByEntity, eid)
			delete(state.signaturesByEntity, eid)
		}
	}
	for eid := range state.PatchEntitiesByOwner {
		if _, ok := active[eid]; !ok {
			clearResolvedWaterPatchEntities(cmd, state, eid)
			delete(state.signaturesByEntity, eid)
		}
	}

	var staticColliders []waterStaticCollider
	staticCollidersReady := false
	loadStaticColliders := func() []waterStaticCollider {
		if !staticCollidersReady {
			staticColliders = collectWaterStaticColliders(cmd)
			staticCollidersReady = true
		}
		return staticColliders
	}

	var staticVoxelOccupancy []waterStaticCollider
	staticVoxelOccupancyReady := false
	loadStaticVoxelOccupancy := func() []waterStaticCollider {
		if !staticVoxelOccupancyReady {
			staticVoxelOccupancy = collectWaterStaticVoxelOccupancy(cmd, assets)
			staticVoxelOccupancyReady = true
		}
		return staticVoxelOccupancy
	}

	MakeQuery1[WaterBodyComponent](cmd).Map(func(eid EntityId, body *WaterBodyComponent) bool {
		if body == nil || body.Disabled {
			return true
		}
		var tr *TransformComponent
		if comp := cmd.GetComponent(eid, transformType); comp != nil {
			if typed, ok := comp.(*TransformComponent); ok {
				tr = typed
			} else if typed, ok := comp.(TransformComponent); ok {
				copy := typed
				tr = &copy
			}
		}

		var colliders []waterStaticCollider
		var voxelOccupancyLoader func() []waterStaticCollider
		sourceHash := waterStaticColliderSignatureHash(nil)
		if body.NormalizedMode() == WaterBodyModeFitBounds {
			sourceHash = waterBodySourceInventoryHash(cmd, assets, body)
			voxelOccupancyLoader = loadStaticVoxelOccupancy
		}

		ownerSplash, hasOwnerSplash := lookupWaterSplashEffect(cmd, eid, splashType)
		signature := waterBodyResolutionSignatureFor(body, tr, ownerSplash, hasOwnerSplash, sourceHash)
		if existing, ok := state.signaturesByEntity[eid]; ok {
			if _, hasRecord := state.ByEntity[eid]; hasRecord && existing == signature {
				return true
			}
		}

		clearResolvedWaterPatchEntities(cmd, state, eid)
		if body.NormalizedMode() == WaterBodyModeFitBounds {
			colliders = loadStaticColliders()
		}
		record, patches := resolveWaterBody(eid, body, tr, colliders, voxelOccupancyLoader)
		state.ByEntity[eid] = record
		state.signaturesByEntity[eid] = signature
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

func waterBodyResolutionSignatureFor(body *WaterBodyComponent, tr *TransformComponent, splash WaterSplashEffectComponent, hasSplash bool, sourceHash uint64) waterBodyResolutionSignature {
	if body == nil {
		return waterBodyResolutionSignature{}
	}
	sig := waterBodyResolutionSignature{
		Mode:                 body.NormalizedMode(),
		SurfaceY:             body.NormalizedSurfaceY(),
		Depth:                body.NormalizedDepth(),
		RectHalfExtents:      body.NormalizedRectHalfExtents(),
		BoundsCenter:         body.NormalizedBoundsCenter(),
		BoundsHalfExtents:    body.NormalizedBoundsHalfExtents(),
		Inset:                body.NormalizedInset(),
		Overlap:              body.NormalizedOverlap(),
		MinCellSize:          body.NormalizedMinCellSize(),
		SourceTag:            strings.TrimSpace(body.SourceTag),
		ContinuityGroup:      strings.TrimSpace(body.ContinuityGroup),
		EnableSkirt:          body.NormalizedEnableSkirt(),
		MaxPatchCount:        body.NormalizedMaxPatchCount(),
		Color:                body.NormalizedColor(),
		AbsorptionColor:      body.NormalizedAbsorptionColor(),
		Opacity:              body.NormalizedOpacity(),
		Roughness:            body.NormalizedRoughness(),
		Refraction:           body.NormalizedRefraction(),
		DirectLightOcclusion: body.NormalizedDirectLightOcclusion(),
		FlowDirection:        body.NormalizedFlowDirection(),
		FlowSpeed:            body.NormalizedFlowSpeed(),
		WaveAmplitude:        body.NormalizedWaveAmplitude(),
		VisualCellSize:       body.NormalizedVisualCellSize(),
		HasSplash:            hasSplash,
		Splash:               splash,
	}
	if tr != nil && sig.Mode == WaterBodyModeExplicitRect {
		sig.AnchorPosition = tr.Position
	}
	if sig.Mode == WaterBodyModeFitBounds {
		sig.SourceHash = sourceHash
	}
	return sig
}

func resolveWaterBody(owner EntityId, body *WaterBodyComponent, tr *TransformComponent, staticColliders []waterStaticCollider, loadStaticVoxelOccupancy func() []waterStaticCollider) (WaterBodyResolvedRecord, []ResolvedWaterPatchComponent) {
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
		if len(rects) == 0 && loadStaticVoxelOccupancy != nil {
			rects = fitWaterBodyBoundsFromStaticColliders(body, loadStaticVoxelOccupancy())
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
			Owner:                owner,
			PatchIndex:           uint32(len(patches)),
			Kind:                 WaterPatchKindSurface,
			Center:               patchCenter,
			HalfExtents:          [2]float32{(rect.maxX - rect.minX) * 0.5, (rect.maxZ - rect.minZ) * 0.5},
			Depth:                body.NormalizedDepth(),
			Color:                body.NormalizedColor(),
			AbsorptionColor:      body.NormalizedAbsorptionColor(),
			Opacity:              body.NormalizedOpacity(),
			Roughness:            body.NormalizedRoughness(),
			Refraction:           body.NormalizedRefraction(),
			DirectLightOcclusion: body.NormalizedDirectLightOcclusion(),
			FlowDirection:        body.NormalizedFlowDirection(),
			FlowSpeed:            body.NormalizedFlowSpeed(),
			WaveAmplitude:        body.NormalizedWaveAmplitude(),
			VisualCellSize:       body.NormalizedVisualCellSize(),
			Source:               source,
			ContinuityGroup:      strings.TrimSpace(body.ContinuityGroup),
			DebugInset:           body.NormalizedInset(),
			DebugOverlap:         body.NormalizedOverlap(),
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
	minY := boundsCenter.Y() - boundsHalf.Y()
	maxY := boundsCenter.Y() + boundsHalf.Y()
	minZ := boundsCenter.Z() - boundsHalf.Z()
	maxZ := boundsCenter.Z() + boundsHalf.Z()
	if maxX <= minX || maxY <= minY || maxZ <= minZ {
		return nil
	}
	surfaceY := body.NormalizedSurfaceY()
	if surfaceY < minY-waterBodySurfaceBandEpsilon || surfaceY > maxY+waterBodySurfaceBandEpsilon {
		return nil
	}

	cellsX := maxInt(1, int(ceilf((maxX-minX)/cellSize)))
	cellsZ := maxInt(1, int(ceilf((maxZ-minZ)/cellSize)))
	solid := make([]bool, cellsX*cellsZ)

	for _, collider := range filterWaterStaticCollidersForBodySource(body, colliders) {
		if collider.Max.Y() < minY || collider.Min.Y() > maxY {
			continue
		}
		if collider.Max.Y() < surfaceY-waterBodySurfaceBandEpsilon || collider.Min.Y() > surfaceY+waterBodySurfaceBandEpsilon {
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
	overlap := float32(0)
	if body.NormalizedEnableSkirt() {
		overlap = body.NormalizedOverlap()
	}
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

			rawMinX := minX + float32(x)*cellSize
			rawMaxX := minX + float32(x+width)*cellSize
			rawMinZ := minZ + float32(z)*cellSize
			rawMaxZ := minZ + float32(z+height)*cellSize
			rect := waterPatchRect{
				minX: clampWaterFitCoord(rawMinX+inset-overlap, minX, maxX),
				maxX: clampWaterFitCoord(rawMaxX-inset+overlap, minX, maxX),
				minZ: clampWaterFitCoord(rawMinZ+inset-overlap, minZ, maxZ),
				maxZ: clampWaterFitCoord(rawMaxZ-inset+overlap, minZ, maxZ),
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

func filterWaterStaticCollidersForBodySource(body *WaterBodyComponent, colliders []waterStaticCollider) []waterStaticCollider {
	sourceTag := ""
	if body != nil {
		sourceTag = strings.TrimSpace(body.SourceTag)
	}
	if sourceTag == "" {
		return colliders
	}
	filtered := make([]waterStaticCollider, 0, len(colliders))
	for _, collider := range colliders {
		if waterSourceTagsContain(collider.Tags, sourceTag) {
			filtered = append(filtered, collider)
		}
	}
	return filtered
}

func waterSourceTagsContain(tags []string, sourceTag string) bool {
	sourceTag = strings.TrimSpace(sourceTag)
	if sourceTag == "" {
		return true
	}
	for _, tag := range tags {
		if strings.TrimSpace(tag) == sourceTag {
			return true
		}
	}
	return false
}

func waterStaticColliderSignatureHash(colliders []waterStaticCollider) uint64 {
	const offset64 = uint64(14695981039346656037)
	const prime64 = uint64(1099511628211)
	hash := offset64
	writeUint32 := func(v uint32) {
		for shift := 0; shift < 32; shift += 8 {
			hash ^= uint64(byte(v >> shift))
			hash *= prime64
		}
	}
	writeFloat := func(v float32) {
		writeUint32(math.Float32bits(v))
	}
	writeString := func(v string) {
		for i := 0; i < len(v); i++ {
			hash ^= uint64(v[i])
			hash *= prime64
		}
		hash ^= 0xff
		hash *= prime64
	}
	writeUint32(uint32(len(colliders)))
	for _, collider := range colliders {
		writeFloat(collider.Min.X())
		writeFloat(collider.Min.Y())
		writeFloat(collider.Min.Z())
		writeFloat(collider.Max.X())
		writeFloat(collider.Max.Y())
		writeFloat(collider.Max.Z())
		writeUint32(uint32(len(collider.Tags)))
		for _, tag := range collider.Tags {
			writeString(strings.TrimSpace(tag))
		}
	}
	return hash
}

func waterCombineSourceHashes(staticHash, voxelHash uint64) uint64 {
	const prime64 = uint64(1099511628211)
	hash := uint64(14695981039346656037)
	hash ^= staticHash
	hash *= prime64
	hash ^= voxelHash
	hash *= prime64
	return hash
}

type waterHash64 struct {
	value uint64
}

func newWaterHash64() waterHash64 {
	return waterHash64{value: 14695981039346656037}
}

func (h *waterHash64) writeUint32(v uint32) {
	const prime64 = uint64(1099511628211)
	for shift := 0; shift < 32; shift += 8 {
		h.value ^= uint64(byte(v >> shift))
		h.value *= prime64
	}
}

func (h *waterHash64) writeUint64(v uint64) {
	const prime64 = uint64(1099511628211)
	for shift := 0; shift < 64; shift += 8 {
		h.value ^= uint64(byte(v >> shift))
		h.value *= prime64
	}
}

func (h *waterHash64) writeInt(v int) {
	h.writeUint64(uint64(int64(v)))
}

func (h *waterHash64) writeFloat(v float32) {
	h.writeUint32(math.Float32bits(v))
}

func (h *waterHash64) writeString(v string) {
	const prime64 = uint64(1099511628211)
	for i := 0; i < len(v); i++ {
		h.value ^= uint64(v[i])
		h.value *= prime64
	}
	h.value ^= 0xff
	h.value *= prime64
}

func (h *waterHash64) writeVec3(v mgl32.Vec3) {
	h.writeFloat(v.X())
	h.writeFloat(v.Y())
	h.writeFloat(v.Z())
}

func (h *waterHash64) writeQuat(v mgl32.Quat) {
	h.writeFloat(v.V.X())
	h.writeFloat(v.V.Y())
	h.writeFloat(v.V.Z())
	h.writeFloat(v.W)
}

func (h *waterHash64) writeAssetID(id AssetId) {
	h.writeUint64(uint64(id.UUID.ID()))
	h.writeString(id.UUID.String())
}

func (h *waterHash64) writeTags(tags []string) {
	h.writeUint32(uint32(len(tags)))
	for _, tag := range tags {
		h.writeString(strings.TrimSpace(tag))
	}
}

func waterBodySourceInventoryHash(cmd *Commands, assets *AssetServer, body *WaterBodyComponent) uint64 {
	if cmd == nil {
		return waterStaticColliderSignatureHash(nil)
	}
	staticHash := waterStaticColliderInventoryHash(cmd, body)
	voxelHash := waterStaticVoxelInventoryHash(cmd, assets, body)
	return waterCombineSourceHashes(staticHash, voxelHash)
}

func waterStaticColliderInventoryHash(cmd *Commands, body *WaterBodyComponent) uint64 {
	if cmd == nil {
		return waterStaticColliderSignatureHash(nil)
	}
	h := newWaterHash64()
	count := uint32(0)
	MakeQuery3[TransformComponent, RigidBodyComponent, ColliderComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, rb *RigidBodyComponent, col *ColliderComponent) bool {
		if tr == nil || rb == nil || col == nil || rb.BodyMode != BodyModeStatic {
			return true
		}
		tags := collectWaterSourceTags(cmd, eid)
		if !waterBodySourceTagsMatch(body, tags) {
			return true
		}
		minV, maxV := waterStaticColliderAABB(tr, col)
		count++
		h.writeUint64(uint64(eid))
		h.writeVec3(minV)
		h.writeVec3(maxV)
		h.writeUint32(uint32(col.Shape))
		h.writeVec3(col.HalfExtents)
		h.writeFloat(col.Radius)
		h.writeFloat(col.CapsuleHalfHeight)
		h.writeTags(tags)
		return true
	})
	final := newWaterHash64()
	final.writeUint32(count)
	final.writeUint64(h.value)
	return final.value
}

func waterStaticVoxelInventoryHash(cmd *Commands, assets *AssetServer, body *WaterBodyComponent) uint64 {
	if cmd == nil {
		return waterStaticColliderSignatureHash(nil)
	}
	h := newWaterHash64()
	count := uint32(0)
	MakeQuery2[TransformComponent, VoxelModelComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, vmc *VoxelModelComponent) bool {
		if tr == nil || vmc == nil || !waterVoxelOccupancySourceEnabled(cmd, eid) {
			return true
		}
		tags := collectWaterSourceTags(cmd, eid)
		if !waterBodySourceTagsMatch(body, tags) {
			return true
		}
		vmcCopy := *vmc
		vmcCopy.NormalizeGeometryRefs()
		geometryID := vmcCopy.GeometryAsset()
		count++
		h.writeUint64(uint64(eid))
		h.writeVec3(tr.Position)
		h.writeQuat(tr.Rotation)
		h.writeVec3(tr.Scale)
		h.writeVec3(tr.Pivot)
		h.writeAssetID(geometryID)
		h.writeFloat(vmcCopy.VoxelResolution)
		h.writeUint32(uint32(vmcCopy.PivotMode))
		h.writeVec3(vmcCopy.CustomPivot)
		h.writeTags(tags)
		if assets != nil {
			_, geometry, ok := ResolveVoxelGeometry(assets, &vmcCopy)
			h.writeUint32(boolToWaterHashBit(ok && geometry != nil && geometry.XBrickMap != nil))
			if ok && geometry != nil && geometry.XBrickMap != nil {
				h.writeVec3(geometry.LocalMin)
				h.writeVec3(geometry.LocalMax)
				h.writeVoxelMapDirtyState(geometry.XBrickMap)
			}
		}
		return true
	})
	final := newWaterHash64()
	final.writeUint32(count)
	final.writeUint64(h.value)
	return final.value
}

func boolToWaterHashBit(v bool) uint32 {
	if v {
		return 1
	}
	return 0
}

func (h *waterHash64) writeVoxelMapDirtyState(xbm *volume.XBrickMap) {
	if xbm == nil {
		h.writeUint32(0)
		return
	}
	h.writeUint32(boolToWaterHashBit(xbm.StructureDirty))
	h.writeUint32(boolToWaterHashBit(xbm.AABBDirty))

	sectorKeys := make([][3]int, 0, len(xbm.DirtySectors))
	for key := range xbm.DirtySectors {
		sectorKeys = append(sectorKeys, key)
	}
	sort.Slice(sectorKeys, func(i, j int) bool {
		return compareWaterInt3(sectorKeys[i], sectorKeys[j]) < 0
	})
	h.writeUint32(uint32(len(sectorKeys)))
	for _, key := range sectorKeys {
		h.writeInt(key[0])
		h.writeInt(key[1])
		h.writeInt(key[2])
	}

	brickKeys := make([][6]int, 0, len(xbm.DirtyBricks))
	for key := range xbm.DirtyBricks {
		brickKeys = append(brickKeys, key)
	}
	sort.Slice(brickKeys, func(i, j int) bool {
		return compareWaterInt6(brickKeys[i], brickKeys[j]) < 0
	})
	h.writeUint32(uint32(len(brickKeys)))
	for _, key := range brickKeys {
		for _, coord := range key {
			h.writeInt(coord)
		}
		sector := xbm.Sectors[[3]int{key[0], key[1], key[2]}]
		if sector == nil {
			h.writeUint32(0)
			continue
		}
		brick := sector.GetBrick(key[3], key[4], key[5])
		if brick == nil {
			h.writeUint32(0)
			continue
		}
		h.writeUint32(1)
		h.writeUint64(brick.OccupancyMask64)
		words := brick.DenseOccupancyWords()
		for _, word := range words {
			h.writeUint32(word)
		}
	}
}

func compareWaterInt3(a, b [3]int) int {
	for i := 0; i < 3; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func compareWaterInt6(a, b [6]int) int {
	for i := 0; i < 6; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func waterBodySourceTagsMatch(body *WaterBodyComponent, tags []string) bool {
	sourceTag := ""
	if body != nil {
		sourceTag = strings.TrimSpace(body.SourceTag)
	}
	return waterSourceTagsContain(tags, sourceTag)
}

func clampWaterFitCoord(v, lo, hi float32) float32 {
	return minf(maxf(v, lo), hi)
}

func collectWaterStaticColliders(cmd *Commands) []waterStaticCollider {
	if cmd == nil {
		return nil
	}
	out := make([]waterStaticCollider, 0, 8)
	MakeQuery3[TransformComponent, RigidBodyComponent, ColliderComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, rb *RigidBodyComponent, col *ColliderComponent) bool {
		if tr == nil || rb == nil || col == nil || rb.BodyMode != BodyModeStatic {
			return true
		}
		minV, maxV := waterStaticColliderAABB(tr, col)
		out = append(out, waterStaticCollider{Min: minV, Max: maxV, Tags: collectWaterSourceTags(cmd, eid)})
		return true
	})
	return out
}

func collectWaterStaticVoxelOccupancy(cmd *Commands, assets *AssetServer) []waterStaticCollider {
	if cmd == nil || assets == nil {
		return nil
	}
	out := make([]waterStaticCollider, 0, 32)
	MakeQuery2[TransformComponent, VoxelModelComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, vmc *VoxelModelComponent) bool {
		if tr == nil || vmc == nil || !waterVoxelOccupancySourceEnabled(cmd, eid) {
			return true
		}
		geometry, voxelTransform, ok := resolveStaticVoxelOccupancyGeometry(assets, tr, vmc)
		if !ok || geometry == nil || geometry.XBrickMap == nil {
			return true
		}
		tags := collectWaterSourceTags(cmd, eid)
		out = append(out, collectWorldVoxelOccupancyAABBs(geometry.XBrickMap, voxelTransform, tags)...)
		return true
	})
	return out
}

func waterVoxelOccupancySourceEnabled(cmd *Commands, eid EntityId) bool {
	if cmd == nil {
		return false
	}
	comp := cmd.GetComponent(eid, reflect.TypeOf(RigidBodyComponent{}))
	switch rb := comp.(type) {
	case nil:
		return true
	case *RigidBodyComponent:
		return rb == nil || rb.BodyMode == BodyModeStatic || rb.BodyMode == BodyModePresentationOnly
	case RigidBodyComponent:
		return rb.BodyMode == BodyModeStatic || rb.BodyMode == BodyModePresentationOnly
	default:
		return true
	}
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

func collectWorldVoxelOccupancyAABBs(xbm *volume.XBrickMap, tr TransformComponent, tags []string) []waterStaticCollider {
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
				out = append(out, waterStaticCollider{Min: minV, Max: maxV, Tags: append([]string(nil), tags...)})
			}
		}
	}
	return out
}

func collectWaterSourceTags(cmd *Commands, eid EntityId) []string {
	if cmd == nil {
		return nil
	}
	var tags []string
	add := func(values ...string) {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" || waterSourceTagsContain(tags, value) {
				continue
			}
			tags = append(tags, value)
		}
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		switch typed := comp.(type) {
		case *AuthoredMarkerComponent:
			if typed != nil {
				add(typed.Name, typed.Kind)
				add(typed.Tags...)
			}
		case AuthoredMarkerComponent:
			add(typed.Name, typed.Kind)
			add(typed.Tags...)
		case *AuthoredLevelPlacementRefComponent:
			if typed != nil {
				add(typed.PlacementID, typed.VolumeID)
				add(typed.Tags...)
			}
		case AuthoredLevelPlacementRefComponent:
			add(typed.PlacementID, typed.VolumeID)
			add(typed.Tags...)
		case *AuthoredLevelItemRefComponent:
			if typed != nil {
				add(typed.PlacementID, typed.ItemID, typed.AssetID, typed.VolumeID)
				add(typed.Tags...)
			}
		case AuthoredLevelItemRefComponent:
			add(typed.PlacementID, typed.ItemID, typed.AssetID, typed.VolumeID)
			add(typed.Tags...)
		}
	}
	return tags
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
