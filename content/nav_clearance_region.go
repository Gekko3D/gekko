package content

import (
	"fmt"
	"sort"
)

const NavClearanceRegionPortalWalk = "walk"

type NavClearanceRegionBuildOptions struct {
	AgentProfile NavAgentProfileDef
}

type NavClearanceRegionPortalBuildOptions struct {
	AgentProfile NavAgentProfileDef
	ChunkSize    int
}

type NavClearanceLocalRegionDef struct {
	ID         string
	Coord      TerrainChunkCoordDef
	Area       string
	Cells      []NavClearancePathStep
	BoundsMin  Vec3
	BoundsMax  Vec3
	Centroid   Vec3
	Projection NavClearanceRegionProjectionDef
	Portals    []NavClearanceRegionPortalDef
}

type NavClearanceRegionProjectionDef struct {
	BoundsMin      Vec3
	BoundsMax      Vec3
	Centroid       Vec3
	HasHeightPlane bool
	PlaneBase      float32
	PlaneX         float32
	PlaneZ         float32
	HeightMin      float32
	HeightMax      float32
	CellCount      int
}

type NavClearanceRegionPortalDef struct {
	ID              string
	Kind            string
	FromRegionID    string
	ToRegionID      string
	FromCell        NavClearancePathStep
	ToCell          NavClearancePathStep
	Position        Vec3
	Width           float32
	RequiredWidth   float32
	ClearanceRadius float32
}

type NavClearanceLocalRegionBuildResult struct {
	Coord            TerrainChunkCoordDef
	AgentProfileID   string
	Regions          []NavClearanceLocalRegionDef
	SupportedCells   int
	UnsupportedCells int
}

type NavClearanceRegionPathStep struct {
	Coord      TerrainChunkCoordDef
	RegionID   string
	Area       string
	BoundsMin  Vec3
	BoundsMax  Vec3
	Centroid   Vec3
	Projection NavClearanceRegionProjectionDef
}

type NavClearanceRegionPathResult struct {
	Found             bool
	Steps             []NavClearanceRegionPathStep
	Portals           []NavClearanceRegionPortalDef
	Waypoints         []Vec3
	StartPoint        Vec3
	EndPoint          Vec3
	StartSnapped      bool
	EndSnapped        bool
	StartSnapDistance float32
	EndSnapDistance   float32
	RawSteps          []NavClearancePathStep
	RawStepCount      int
}

type navClearanceRegionCellRef struct {
	Coord TerrainChunkCoordDef
	X     int
	Y     int
	Z     int
}

func BuildNavClearanceLocalRegions(tile *NavClearanceSourceTileDef, opts NavClearanceRegionBuildOptions) (NavClearanceLocalRegionBuildResult, error) {
	if tile == nil {
		return NavClearanceLocalRegionBuildResult{}, fmt.Errorf("nav clearance source tile is nil")
	}
	EnsureNavClearanceSourceTileDefaults(tile)
	profile := opts.AgentProfile
	EnsureNavAgentProfileDefaults(&profile)
	result := NavClearanceLocalRegionBuildResult{
		Coord:          tile.Coord,
		AgentProfileID: profile.ID,
	}
	cellsByKey := make(map[[3]int]NavClearanceSourceCellDef, len(tile.Cells))
	cellsByXZ := make(map[[2]int][]NavClearanceSourceCellDef, len(tile.Cells))
	for _, cell := range tile.Cells {
		if !navClearanceSourceCellSupportsNormalizedAgent(cell, profile) {
			result.UnsupportedCells++
			continue
		}
		key := [3]int{cell.X, cell.Y, cell.Z}
		cellsByKey[key] = cell
		cellsByXZ[[2]int{cell.X, cell.Z}] = append(cellsByXZ[[2]int{cell.X, cell.Z}], cell)
		result.SupportedCells++
	}
	visited := make(map[[3]int]struct{}, len(cellsByKey))
	for _, key := range sortedNavClearanceRegionCellKeys(cellsByKey) {
		cell := cellsByKey[key]
		if _, ok := visited[key]; ok {
			continue
		}
		region := navClearanceBuildRegion(tile.Coord, len(result.Regions), cell, cellsByKey, cellsByXZ, visited, profile)
		result.Regions = append(result.Regions, region)
	}
	appendNavClearanceLocalRegionPortals(&result, cellsByKey, cellsByXZ, profile, tile.VoxelResolution)
	return result, nil
}

func navClearanceBuildRegion(coord TerrainChunkCoordDef, index int, seed NavClearanceSourceCellDef, cellsByKey map[[3]int]NavClearanceSourceCellDef, cellsByXZ map[[2]int][]NavClearanceSourceCellDef, visited map[[3]int]struct{}, profile NavAgentProfileDef) NavClearanceLocalRegionDef {
	region := NavClearanceLocalRegionDef{
		ID:        fmt.Sprintf("region:%s:%d", TerrainChunkKey(coord), index),
		Coord:     coord,
		Area:      firstNonEmptyNavString(seed.Area, NavTraversalWalk),
		BoundsMin: seed.Position,
		BoundsMax: seed.Position,
	}
	queue := []NavClearanceSourceCellDef{seed}
	regionCells := make([]NavClearanceSourceCellDef, 0)
	visited[[3]int{seed.X, seed.Y, seed.Z}] = struct{}{}
	for len(queue) > 0 {
		cell := queue[0]
		queue = queue[1:]
		regionCells = append(regionCells, cell)
		region.Cells = append(region.Cells, NavClearancePathStep{Coord: coord, X: cell.X, Y: cell.Y, Z: cell.Z})
		region.Centroid[0] += cell.Position[0]
		region.Centroid[1] += cell.Position[1]
		region.Centroid[2] += cell.Position[2]
		region.BoundsMin = navVec3Min(region.BoundsMin, cell.Position)
		region.BoundsMax = navVec3Max(region.BoundsMax, cell.Position)
		for _, neighbor := range navClearanceRegionNeighborCells(cell, cellsByXZ, profile) {
			if navClearanceRegionCellArea(neighbor) != region.Area {
				continue
			}
			key := [3]int{neighbor.X, neighbor.Y, neighbor.Z}
			if _, ok := cellsByKey[key]; !ok {
				continue
			}
			if _, ok := visited[key]; ok {
				continue
			}
			visited[key] = struct{}{}
			queue = append(queue, neighbor)
		}
	}
	if len(region.Cells) > 0 {
		inv := 1 / float32(len(region.Cells))
		region.Centroid[0] *= inv
		region.Centroid[1] *= inv
		region.Centroid[2] *= inv
	}
	region.Projection = navClearanceRegionProjection(region, regionCells)
	return region
}

func navClearanceRegionProjection(region NavClearanceLocalRegionDef, cells []NavClearanceSourceCellDef) NavClearanceRegionProjectionDef {
	projection := NavClearanceRegionProjectionDef{
		BoundsMin: region.BoundsMin,
		BoundsMax: region.BoundsMax,
		Centroid:  region.Centroid,
		CellCount: len(cells),
	}
	if len(cells) == 0 {
		return projection
	}
	projection.HeightMin = cells[0].Position[1]
	projection.HeightMax = cells[0].Position[1]
	for _, cell := range cells {
		if cell.Position[1] < projection.HeightMin {
			projection.HeightMin = cell.Position[1]
		}
		if cell.Position[1] > projection.HeightMax {
			projection.HeightMax = cell.Position[1]
		}
	}
	if plane, ok := navClearanceFitRegionHeightPlane(cells); ok {
		projection.HasHeightPlane = true
		projection.PlaneBase = plane.base
		projection.PlaneX = plane.x
		projection.PlaneZ = plane.z
		return projection
	}
	projection.HasHeightPlane = true
	projection.PlaneBase = region.Centroid[1]
	return projection
}

func navClearanceFitRegionHeightPlane(cells []NavClearanceSourceCellDef) (navBuildHeightPlane, bool) {
	if len(cells) < 3 {
		return navBuildHeightPlane{}, false
	}
	var ata [3][3]float64
	var aty [3]float64
	for _, cell := range cells {
		row := [3]float64{1, float64(cell.Position[0]), float64(cell.Position[2])}
		y := float64(cell.Position[1])
		for r := 0; r < 3; r++ {
			aty[r] += row[r] * y
			for c := 0; c < 3; c++ {
				ata[r][c] += row[r] * row[c]
			}
		}
	}
	solution, ok := solveNavBuild3x3(ata, aty)
	if !ok {
		return navBuildHeightPlane{}, false
	}
	return navBuildHeightPlane{base: float32(solution[0]), x: float32(solution[1]), z: float32(solution[2])}, true
}

func NavClearanceLocalRegionHeightAt(region NavClearanceLocalRegionDef, x float32, z float32) (float32, bool) {
	if !region.Projection.HasHeightPlane {
		return 0, false
	}
	height := region.Projection.PlaneBase + region.Projection.PlaneX*x + region.Projection.PlaneZ*z
	height = clampNavFloat32(height, region.Projection.HeightMin, region.Projection.HeightMax)
	return height, true
}

func ConvertNavClearancePathToRegionPath(path NavClearancePathResult, results map[TerrainChunkCoordDef]*NavClearanceLocalRegionBuildResult) (NavClearanceRegionPathResult, error) {
	if !path.Found {
		return NavClearanceRegionPathResult{}, nil
	}
	regionByRef := make(map[navClearanceRegionCellRef]NavClearanceRegionPathStep)
	regionByID := make(map[string]NavClearanceLocalRegionDef)
	for coord, result := range results {
		if result == nil {
			continue
		}
		for _, region := range result.Regions {
			regionByID[region.ID] = region
			for _, step := range region.Cells {
				regionByRef[navClearanceRegionCellRef{Coord: coord, X: step.X, Y: step.Y, Z: step.Z}] = NavClearanceRegionPathStep{
					Coord:      coord,
					RegionID:   region.ID,
					Area:       region.Area,
					BoundsMin:  region.BoundsMin,
					BoundsMax:  region.BoundsMax,
					Centroid:   region.Centroid,
					Projection: region.Projection,
				}
			}
		}
	}
	out := NavClearanceRegionPathResult{
		Found:             true,
		StartPoint:        path.StartPoint,
		EndPoint:          path.EndPoint,
		StartSnapped:      path.StartSnapped,
		EndSnapped:        path.EndSnapped,
		StartSnapDistance: path.StartSnapDistance,
		EndSnapDistance:   path.EndSnapDistance,
		RawSteps:          append([]NavClearancePathStep(nil), path.Steps...),
		RawStepCount:      len(path.Steps),
	}
	if path.StartPoint != (Vec3{}) || len(path.Waypoints) > 0 {
		out.Waypoints = append(out.Waypoints, path.StartPoint)
	}
	for _, raw := range path.Steps {
		regionStep, ok := regionByRef[navClearanceRegionCellRef{Coord: raw.Coord, X: raw.X, Y: raw.Y, Z: raw.Z}]
		if !ok {
			return NavClearanceRegionPathResult{}, fmt.Errorf("missing compact region for path cell %s:%d:%d:%d", TerrainChunkKey(raw.Coord), raw.X, raw.Y, raw.Z)
		}
		if len(out.Steps) == 0 || out.Steps[len(out.Steps)-1] != regionStep {
			if len(out.Steps) > 0 {
				from := out.Steps[len(out.Steps)-1]
				portal, ok := navClearanceRegionPathPortal(regionByID[from.RegionID], regionByID[regionStep.RegionID], raw)
				if !ok {
					return NavClearanceRegionPathResult{}, fmt.Errorf("missing compact region portal from %s to %s at %s:%d:%d:%d", from.RegionID, regionStep.RegionID, TerrainChunkKey(raw.Coord), raw.X, raw.Y, raw.Z)
				}
				out.Portals = append(out.Portals, portal)
				out.Waypoints = append(out.Waypoints, portal.Position)
			}
			out.Steps = append(out.Steps, regionStep)
		}
	}
	if len(out.Portals) > 0 {
		out.Waypoints = append(out.Waypoints, path.EndPoint)
	}
	if len(out.Waypoints) == 0 || (len(out.Portals) == 0 && len(path.Waypoints) > 0) {
		out.Waypoints = append([]Vec3(nil), path.Waypoints...)
	}
	out.Waypoints = navDedupePathWaypoints(out.Waypoints)
	return out, nil
}

func navClearanceRegionPathPortal(from NavClearanceLocalRegionDef, to NavClearanceLocalRegionDef, arrival NavClearancePathStep) (NavClearanceRegionPortalDef, bool) {
	for _, portal := range from.Portals {
		if portal.ToRegionID != to.ID {
			continue
		}
		if portal.ToCell == arrival || portal.FromCell == arrival {
			return portal, true
		}
	}
	for _, portal := range from.Portals {
		if portal.ToRegionID == to.ID {
			return portal, true
		}
	}
	return NavClearanceRegionPortalDef{}, false
}

func appendNavClearanceLocalRegionPortals(result *NavClearanceLocalRegionBuildResult, cellsByKey map[[3]int]NavClearanceSourceCellDef, cellsByXZ map[[2]int][]NavClearanceSourceCellDef, profile NavAgentProfileDef, voxelResolution float32) {
	if result == nil || len(result.Regions) < 2 {
		return
	}
	regionByCell := make(map[[3]int]int, len(cellsByKey))
	for regionIndex, region := range result.Regions {
		for _, step := range region.Cells {
			regionByCell[[3]int{step.X, step.Y, step.Z}] = regionIndex
		}
	}
	seen := map[string]struct{}{}
	for _, key := range sortedNavClearanceRegionCellKeys(cellsByKey) {
		cell := cellsByKey[key]
		fromRegion, ok := regionByCell[key]
		if !ok {
			continue
		}
		for _, neighbor := range navClearanceRegionNeighborCells(cell, cellsByXZ, profile) {
			neighborKey := [3]int{neighbor.X, neighbor.Y, neighbor.Z}
			toRegion, ok := regionByCell[neighborKey]
			if !ok || fromRegion == toRegion {
				continue
			}
			portalKey := navClearanceRegionPortalKey(fromRegion, toRegion, cell, neighbor)
			if _, ok := seen[portalKey]; ok {
				continue
			}
			seen[portalKey] = struct{}{}
			portal := navClearanceRegionPortal(result.Regions[fromRegion].ID, result.Regions[toRegion].ID, result.Coord, result.Coord, cell, neighbor, profile, voxelResolution)
			result.Regions[fromRegion].Portals = append(result.Regions[fromRegion].Portals, portal)
		}
	}
	for i := range result.Regions {
		sort.Slice(result.Regions[i].Portals, func(a, b int) bool {
			return result.Regions[i].Portals[a].ID < result.Regions[i].Portals[b].ID
		})
	}
}

func navClearanceRegionPortal(fromRegionID string, toRegionID string, fromCoord TerrainChunkCoordDef, toCoord TerrainChunkCoordDef, from NavClearanceSourceCellDef, to NavClearanceSourceCellDef, profile NavAgentProfileDef, voxelResolution float32) NavClearanceRegionPortalDef {
	clearanceRadius := minNavFloat32(from.ClearanceRadius, to.ClearanceRadius)
	width := clearanceRadius * 2
	if width <= 0 && voxelResolution > 0 {
		width = voxelResolution
	}
	position := Vec3{
		(from.Position[0] + to.Position[0]) * 0.5,
		(from.Position[1] + to.Position[1]) * 0.5,
		(from.Position[2] + to.Position[2]) * 0.5,
	}
	return NavClearanceRegionPortalDef{
		ID:              fmt.Sprintf("portal:%s:%s:%d:%d:%d:%d:%d:%d", fromRegionID, toRegionID, from.X, from.Y, from.Z, to.X, to.Y, to.Z),
		Kind:            NavClearanceRegionPortalWalk,
		FromRegionID:    fromRegionID,
		ToRegionID:      toRegionID,
		FromCell:        NavClearancePathStep{Coord: fromCoord, X: from.X, Y: from.Y, Z: from.Z},
		ToCell:          NavClearancePathStep{Coord: toCoord, X: to.X, Y: to.Y, Z: to.Z},
		Position:        position,
		Width:           width,
		RequiredWidth:   profile.Radius * 2,
		ClearanceRadius: clearanceRadius,
	}
}

func navClearanceRegionPortalKey(fromRegion int, toRegion int, from NavClearanceSourceCellDef, to NavClearanceSourceCellDef) string {
	return fmt.Sprintf("%d:%d:%d:%d:%d:%d:%d:%d", fromRegion, toRegion, from.X, from.Y, from.Z, to.X, to.Y, to.Z)
}

func BuildNavClearanceCrossTileRegionPortals(results map[TerrainChunkCoordDef]*NavClearanceLocalRegionBuildResult, tiles map[TerrainChunkCoordDef]*NavClearanceSourceTileDef, opts NavClearanceRegionPortalBuildOptions) error {
	if opts.ChunkSize <= 0 {
		return fmt.Errorf("nav clearance region portal chunk_size must be positive")
	}
	profile := opts.AgentProfile
	EnsureNavAgentProfileDefaults(&profile)
	cellsByRef := make(map[navClearanceRegionCellRef]NavClearanceSourceCellDef)
	cellsByCoordXZ := make(map[TerrainChunkCoordDef]map[[2]int][]NavClearanceSourceCellDef)
	regionByRef := make(map[navClearanceRegionCellRef]int)
	for coord, tile := range tiles {
		if tile == nil {
			continue
		}
		if cellsByCoordXZ[coord] == nil {
			cellsByCoordXZ[coord] = make(map[[2]int][]NavClearanceSourceCellDef)
		}
		for _, cell := range tile.Cells {
			if !navClearanceSourceCellSupportsNormalizedAgent(cell, profile) {
				continue
			}
			ref := navClearanceRegionCellRef{Coord: coord, X: cell.X, Y: cell.Y, Z: cell.Z}
			cellsByRef[ref] = cell
			cellsByCoordXZ[coord][[2]int{cell.X, cell.Z}] = append(cellsByCoordXZ[coord][[2]int{cell.X, cell.Z}], cell)
		}
	}
	for coord, result := range results {
		if result == nil {
			continue
		}
		for regionIndex, region := range result.Regions {
			for _, step := range region.Cells {
				regionByRef[navClearanceRegionCellRef{Coord: coord, X: step.X, Y: step.Y, Z: step.Z}] = regionIndex
			}
		}
	}
	seen := map[string]struct{}{}
	for _, ref := range sortedNavClearanceRegionRefs(cellsByRef) {
		fromResult := results[ref.Coord]
		if fromResult == nil {
			continue
		}
		fromRegionIndex, ok := regionByRef[ref]
		if !ok {
			continue
		}
		from := cellsByRef[ref]
		for _, wrapped := range navClearanceRegionWrappedNeighborRefs(ref, opts.ChunkSize) {
			if wrapped.Coord == ref.Coord {
				continue
			}
			toResult := results[wrapped.Coord]
			if toResult == nil {
				continue
			}
			candidates := cellsByCoordXZ[wrapped.Coord][[2]int{wrapped.X, wrapped.Z}]
			for _, to := range candidates {
				toRef := navClearanceRegionCellRef{Coord: wrapped.Coord, X: to.X, Y: to.Y, Z: to.Z}
				toRegionIndex, ok := regionByRef[toRef]
				if !ok {
					continue
				}
				if !navClearanceCellsCanConnectNormalized(from, to, profile) {
					continue
				}
				key := navClearanceCrossTileRegionPortalKey(ref, toRef, fromRegionIndex, toRegionIndex)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				portal := navClearanceRegionPortal(fromResult.Regions[fromRegionIndex].ID, toResult.Regions[toRegionIndex].ID, ref.Coord, toRef.Coord, from, to, profile, tiles[ref.Coord].VoxelResolution)
				fromResult.Regions[fromRegionIndex].Portals = append(fromResult.Regions[fromRegionIndex].Portals, portal)
			}
		}
	}
	for _, result := range results {
		if result == nil {
			continue
		}
		for i := range result.Regions {
			sort.Slice(result.Regions[i].Portals, func(a, b int) bool {
				return result.Regions[i].Portals[a].ID < result.Regions[i].Portals[b].ID
			})
		}
	}
	return nil
}

func navClearanceRegionWrappedNeighborRefs(ref navClearanceRegionCellRef, chunkSize int) []navClearanceRegionCellRef {
	out := make([]navClearanceRegionCellRef, 0, 4)
	for _, offset := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		coord := ref.Coord
		x := ref.X + offset[0]
		z := ref.Z + offset[1]
		for x < 0 {
			coord.X--
			x += chunkSize
		}
		for x >= chunkSize {
			coord.X++
			x -= chunkSize
		}
		for z < 0 {
			coord.Z--
			z += chunkSize
		}
		for z >= chunkSize {
			coord.Z++
			z -= chunkSize
		}
		out = append(out, navClearanceRegionCellRef{Coord: coord, X: x, Y: ref.Y, Z: z})
	}
	return out
}

func navClearanceCrossTileRegionPortalKey(from navClearanceRegionCellRef, to navClearanceRegionCellRef, fromRegion int, toRegion int) string {
	return fmt.Sprintf("%s:%d:%d:%d:%d:%s:%d:%d:%d:%d", TerrainChunkKey(from.Coord), fromRegion, from.X, from.Y, from.Z, TerrainChunkKey(to.Coord), toRegion, to.X, to.Y, to.Z)
}

func sortedNavClearanceRegionRefs(cells map[navClearanceRegionCellRef]NavClearanceSourceCellDef) []navClearanceRegionCellRef {
	refs := make([]navClearanceRegionCellRef, 0, len(cells))
	for ref := range cells {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Coord != refs[j].Coord {
			return terrainChunkCoordLess(refs[i].Coord, refs[j].Coord)
		}
		if refs[i].X != refs[j].X {
			return refs[i].X < refs[j].X
		}
		if refs[i].Z != refs[j].Z {
			return refs[i].Z < refs[j].Z
		}
		return refs[i].Y < refs[j].Y
	})
	return refs
}

func navClearanceRegionNeighborCells(cell NavClearanceSourceCellDef, cellsByXZ map[[2]int][]NavClearanceSourceCellDef, profile NavAgentProfileDef) []NavClearanceSourceCellDef {
	out := make([]NavClearanceSourceCellDef, 0, 4)
	for _, offset := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		for _, candidate := range cellsByXZ[[2]int{cell.X + offset[0], cell.Z + offset[1]}] {
			if navClearanceCellsCanConnectNormalized(cell, candidate, profile) {
				out = append(out, candidate)
			}
		}
	}
	return out
}

func navClearanceRegionCellArea(cell NavClearanceSourceCellDef) string {
	return firstNonEmptyNavString(cell.Area, NavTraversalWalk)
}

func sortedNavClearanceRegionCellKeys(cells map[[3]int]NavClearanceSourceCellDef) [][3]int {
	keys := make([][3]int, 0, len(cells))
	for key := range cells {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		if keys[i][2] != keys[j][2] {
			return keys[i][2] < keys[j][2]
		}
		return keys[i][1] < keys[j][1]
	})
	return keys
}

func navVec3Min(a Vec3, b Vec3) Vec3 {
	out := a
	for i := 0; i < 3; i++ {
		if b[i] < out[i] {
			out[i] = b[i]
		}
	}
	return out
}

func navVec3Max(a Vec3, b Vec3) Vec3 {
	out := a
	for i := 0; i < 3; i++ {
		if b[i] > out[i] {
			out[i] = b[i]
		}
	}
	return out
}
