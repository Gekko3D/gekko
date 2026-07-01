package content

import (
	"container/heap"
	"fmt"
	"math"
	"os"
	"sort"
	"sync"
	"time"
)

const (
	NavClearancePathFailureMissingSourceTile = "missing_source_tile"
	NavClearancePathFailureStartCellMissing  = "start_cell_missing"
	NavClearancePathFailureEndCellMissing    = "end_cell_missing"
	NavClearancePathFailureNoPath            = "no_path"
)

type NavClearancePathOptions struct {
	AgentProfile         NavAgentProfileDef
	AgentProfileID       string
	MaxTileSearchRadius  int
	MaxTileLoads         int
	EndpointSnapDistance float32
	AllowedTileCoords    map[TerrainChunkCoordDef]struct{}
}

type NavClearancePathStep struct {
	Coord TerrainChunkCoordDef
	X     int
	Y     int
	Z     int
}

type NavClearancePathResult struct {
	Found             bool
	Steps             []NavClearancePathStep
	Waypoints         []Vec3
	StartPoint        Vec3
	EndPoint          Vec3
	StartSnapped      bool
	EndSnapped        bool
	StartSnapDistance float32
	EndSnapDistance   float32
	FailureReason     string
	FailureCoord      TerrainChunkCoordDef
}

type navClearancePathContext struct {
	manifest                 *NavManifestDef
	manifestPath             string
	delta                    *WorldDeltaDef
	deltaPath                string
	profile                  NavAgentProfileDef
	opts                     NavClearancePathOptions
	sourceByKey              map[string]NavClearanceSourceTileEntryDef
	deltaSourceByKey         map[string]NavigationClearanceSourceTileOverrideDef
	editedWithoutSourceByKey map[string]struct{}
	cache                    map[TerrainChunkCoordDef]navClearanceLoadedTile
	loaded                   map[TerrainChunkCoordDef]struct{}
	loads                    int
}

type navClearanceCellRef struct {
	Coord TerrainChunkCoordDef
	X     int
	Y     int
	Z     int
}

type navClearanceLoadedTile struct {
	Tile    *NavClearanceSourceTileDef
	Cells   map[[3]int]NavClearanceSourceCellDef
	CellsXZ map[[2]int][]navClearanceCellRef
}

type navClearanceSourceTileCacheEntry struct {
	Tile    *NavClearanceSourceTileDef
	ModTime time.Time
	Size    int64
}

var navClearanceSourceTileCache = struct {
	sync.Mutex
	Entries map[string]navClearanceSourceTileCacheEntry
}{Entries: map[string]navClearanceSourceTileCacheEntry{}}

type navClearanceSourceCellIndexEntry struct {
	Len   int
	Cells map[[3]int]NavClearanceSourceCellDef
}

var navClearanceSourceCellIndexCache = struct {
	sync.Mutex
	Entries map[*NavClearanceSourceTileDef]navClearanceSourceCellIndexEntry
}{Entries: map[*NavClearanceSourceTileDef]navClearanceSourceCellIndexEntry{}}

type navClearanceLoadedTileIndexEntry struct {
	Len     int
	Coord   TerrainChunkCoordDef
	Cells   map[[3]int]NavClearanceSourceCellDef
	CellsXZ map[[2]int][]navClearanceCellRef
}

var navClearanceLoadedTileIndexCache = struct {
	sync.Mutex
	Entries map[*NavClearanceSourceTileDef]navClearanceLoadedTileIndexEntry
}{Entries: map[*NavClearanceSourceTileDef]navClearanceLoadedTileIndexEntry{}}

type navClearancePathNode struct {
	Ref   navClearanceCellRef
	Cost  float32
	Score float32
	Index int
}

type navClearancePathQueue []*navClearancePathNode

func FindNavClearanceSourcePath(manifest *NavManifestDef, manifestPath string, start Vec3, end Vec3, opts NavClearancePathOptions) (NavClearancePathResult, error) {
	return FindEffectiveNavClearanceSourcePath(manifest, manifestPath, nil, "", start, end, opts)
}

func FindEffectiveNavClearanceSourcePath(manifest *NavManifestDef, manifestPath string, delta *WorldDeltaDef, deltaPath string, start Vec3, end Vec3, opts NavClearancePathOptions) (NavClearancePathResult, error) {
	if manifest == nil {
		return NavClearancePathResult{}, fmt.Errorf("nav manifest is nil")
	}
	EnsureNavManifestDefaults(manifest)
	normalized, err := normalizeNavClearancePathOptions(manifest, opts)
	if err != nil {
		return NavClearancePathResult{}, err
	}
	ctx := &navClearancePathContext{
		manifest:                 manifest,
		manifestPath:             manifestPath,
		delta:                    delta,
		deltaPath:                deltaPath,
		profile:                  normalized.AgentProfile,
		opts:                     normalized,
		sourceByKey:              make(map[string]NavClearanceSourceTileEntryDef, len(manifest.ClearanceSourceTiles)),
		deltaSourceByKey:         make(map[string]NavigationClearanceSourceTileOverrideDef),
		editedWithoutSourceByKey: map[string]struct{}{},
		cache:                    map[TerrainChunkCoordDef]navClearanceLoadedTile{},
		loaded:                   map[TerrainChunkCoordDef]struct{}{},
	}
	for _, entry := range manifest.ClearanceSourceTiles {
		ctx.sourceByKey[TerrainChunkKey(entry.Coord)] = entry
	}
	if delta != nil {
		for _, override := range delta.NavigationClearanceSourceTileOverrides {
			if override.NavID == manifest.NavID {
				ctx.deltaSourceByKey[TerrainChunkKey(override.ChunkCoord)] = override
			}
		}
		for _, override := range delta.ImportedWorldChunkOverrides {
			if manifest.SourceWorldID != "" && override.WorldID != "" && override.WorldID != manifest.SourceWorldID {
				continue
			}
			key := TerrainChunkKey(override.ChunkCoord)
			if _, ok := ctx.deltaSourceByKey[key]; !ok {
				ctx.editedWithoutSourceByKey[key] = struct{}{}
			}
		}
	}
	startRef, startPoint, startDistance, ok, err := ctx.nearestSupportedCell(start)
	if err != nil {
		return NavClearancePathResult{}, err
	}
	if !ok {
		return NavClearancePathResult{FailureReason: NavClearancePathFailureStartCellMissing}, nil
	}
	endRef, endPoint, endDistance, ok, err := ctx.nearestSupportedCell(end)
	if err != nil {
		return NavClearancePathResult{}, err
	}
	if !ok {
		return NavClearancePathResult{FailureReason: NavClearancePathFailureEndCellMissing}, nil
	}
	steps, found, failureCoord, err := ctx.findCellPath(startRef, endRef)
	if err != nil {
		return NavClearancePathResult{}, err
	}
	if !found {
		return NavClearancePathResult{FailureReason: NavClearancePathFailureNoPath, FailureCoord: failureCoord}, nil
	}
	waypoints := make([]Vec3, 0, len(steps))
	for _, step := range steps {
		if cell, ok := ctx.cell(step.ref()); ok {
			waypoints = append(waypoints, cell.Position)
		}
	}
	if len(waypoints) > 0 {
		waypoints[0] = startPoint
		waypoints[len(waypoints)-1] = endPoint
	}
	return NavClearancePathResult{
		Found:             true,
		Steps:             steps,
		Waypoints:         waypoints,
		StartPoint:        startPoint,
		EndPoint:          endPoint,
		StartSnapped:      ctx.snapDistanceIsMeaningful(startDistance),
		EndSnapped:        ctx.snapDistanceIsMeaningful(endDistance),
		StartSnapDistance: startDistance,
		EndSnapDistance:   endDistance,
	}, nil
}

func normalizeNavClearancePathOptions(manifest *NavManifestDef, opts NavClearancePathOptions) (NavClearancePathOptions, error) {
	if opts.MaxTileSearchRadius <= 0 {
		opts.MaxTileSearchRadius = DefaultNavPathMaxTileSearchRadius
	}
	if opts.MaxTileLoads <= 0 {
		opts.MaxTileLoads = DefaultNavPathMaxTileLoads
	}
	if opts.EndpointSnapDistance <= 0 {
		opts.EndpointSnapDistance = DefaultNavPathEndpointSnapDistance
	}
	profile := opts.AgentProfile
	if profile.ID == "" && opts.AgentProfileID != "" {
		for _, candidate := range manifest.AgentProfiles {
			if candidate.ID == opts.AgentProfileID {
				profile = candidate
				break
			}
		}
	}
	if profile.ID == "" && opts.AgentProfileID != "" {
		return opts, fmt.Errorf("agent profile %q not found", opts.AgentProfileID)
	}
	if profile.ID == "" && len(manifest.AgentProfiles) > 0 {
		profile = manifest.AgentProfiles[0]
	}
	EnsureNavAgentProfileDefaults(&profile)
	opts.AgentProfile = profile
	return opts, nil
}

func (ctx *navClearancePathContext) nearestSupportedCell(point Vec3) (navClearanceCellRef, Vec3, float32, bool, error) {
	center, ok := navTileCoordForPoint(point, ctx.manifest.ChunkSize, ctx.manifest.VoxelResolution)
	if !ok {
		return navClearanceCellRef{}, Vec3{}, 0, false, fmt.Errorf("invalid nav manifest chunk metrics")
	}
	bestRef := navClearanceCellRef{}
	bestPoint := Vec3{}
	bestDistance := float32(0)
	found := false
	for _, coord := range ctx.nearbySourceCoords(center) {
		tile, err := ctx.loadTile(coord)
		if err != nil {
			return navClearanceCellRef{}, Vec3{}, 0, false, err
		}
		if tile == nil {
			continue
		}
		for _, ref := range ctx.nearestSupportedCellCandidates(coord, point, tile) {
			cell, ok := ctx.cell(ref)
			if !ok {
				continue
			}
			if !navClearanceSourceCellSupportsNormalizedAgent(cell, ctx.profile) {
				continue
			}
			distance := navVec3Distance(point, cell.Position)
			if distance > ctx.opts.EndpointSnapDistance {
				continue
			}
			if !found || distance < bestDistance {
				bestRef = navClearanceCellRef{Coord: coord, X: cell.X, Y: cell.Y, Z: cell.Z}
				bestPoint = cell.Position
				bestDistance = distance
				found = true
			}
		}
	}
	return bestRef, bestPoint, bestDistance, found, nil
}

func (ctx *navClearancePathContext) nearestSupportedCellCandidates(coord TerrainChunkCoordDef, point Vec3, tile *NavClearanceSourceTileDef) []navClearanceCellRef {
	if ctx == nil || ctx.manifest == nil || tile == nil || ctx.manifest.ChunkSize <= 0 || ctx.manifest.VoxelResolution <= 0 {
		out := make([]navClearanceCellRef, 0, len(tile.Cells))
		for _, cell := range tile.Cells {
			out = append(out, navClearanceCellRef{Coord: coord, X: cell.X, Y: cell.Y, Z: cell.Z})
		}
		return out
	}
	loaded := ctx.cache[coord]
	if len(loaded.CellsXZ) == 0 {
		return nil
	}
	resolution := ctx.manifest.VoxelResolution
	worldSize := float32(ctx.manifest.ChunkSize) * resolution
	minX := navClearanceSnapLocalCellFloor(point[0]-ctx.opts.EndpointSnapDistance, float32(coord.X)*worldSize, resolution)
	maxX := navClearanceSnapLocalCellFloor(point[0]+ctx.opts.EndpointSnapDistance, float32(coord.X)*worldSize, resolution)
	minZ := navClearanceSnapLocalCellFloor(point[2]-ctx.opts.EndpointSnapDistance, float32(coord.Z)*worldSize, resolution)
	maxZ := navClearanceSnapLocalCellFloor(point[2]+ctx.opts.EndpointSnapDistance, float32(coord.Z)*worldSize, resolution)
	if maxX < 0 || maxZ < 0 || minX >= ctx.manifest.ChunkSize || minZ >= ctx.manifest.ChunkSize {
		return nil
	}
	if minX < 0 {
		minX = 0
	}
	if minZ < 0 {
		minZ = 0
	}
	if maxX >= ctx.manifest.ChunkSize {
		maxX = ctx.manifest.ChunkSize - 1
	}
	if maxZ >= ctx.manifest.ChunkSize {
		maxZ = ctx.manifest.ChunkSize - 1
	}
	out := make([]navClearanceCellRef, 0)
	for x := minX; x <= maxX; x++ {
		for z := minZ; z <= maxZ; z++ {
			out = append(out, loaded.CellsXZ[[2]int{x, z}]...)
		}
	}
	return out
}

func navClearanceSnapLocalCellFloor(world float32, tileMin float32, resolution float32) int {
	if resolution <= 0 {
		return 0
	}
	return int(math.Floor(float64((world - tileMin) / resolution)))
}

func (ctx *navClearancePathContext) nearbySourceCoords(center TerrainChunkCoordDef) []TerrainChunkCoordDef {
	out := make([]TerrainChunkCoordDef, 0)
	seen := map[TerrainChunkCoordDef]struct{}{}
	for _, entry := range ctx.sourceByKey {
		if !ctx.coordAllowed(entry.Coord) {
			continue
		}
		dx := absNavIntAsInt(entry.Coord.X - center.X)
		dy := absNavIntAsInt(entry.Coord.Y - center.Y)
		dz := absNavIntAsInt(entry.Coord.Z - center.Z)
		if dx <= ctx.opts.MaxTileSearchRadius && dy <= ctx.opts.MaxTileSearchRadius && dz <= ctx.opts.MaxTileSearchRadius {
			seen[entry.Coord] = struct{}{}
		}
	}
	for _, override := range ctx.deltaSourceByKey {
		if !ctx.coordAllowed(override.ChunkCoord) {
			continue
		}
		dx := absNavIntAsInt(override.ChunkCoord.X - center.X)
		dy := absNavIntAsInt(override.ChunkCoord.Y - center.Y)
		dz := absNavIntAsInt(override.ChunkCoord.Z - center.Z)
		if dx <= ctx.opts.MaxTileSearchRadius && dy <= ctx.opts.MaxTileSearchRadius && dz <= ctx.opts.MaxTileSearchRadius {
			seen[override.ChunkCoord] = struct{}{}
		}
	}
	for coord := range seen {
		out = append(out, coord)
	}
	sort.Slice(out, func(i, j int) bool {
		return terrainChunkCoordLess(out[i], out[j])
	})
	return out
}

func (ctx *navClearancePathContext) loadTile(coord TerrainChunkCoordDef) (*NavClearanceSourceTileDef, error) {
	if !ctx.coordAllowed(coord) {
		return nil, nil
	}
	if _, ok := ctx.editedWithoutSourceByKey[TerrainChunkKey(coord)]; ok {
		ctx.loaded[coord] = struct{}{}
		return nil, nil
	}
	if _, ok := ctx.loaded[coord]; ok {
		return ctx.cache[coord].Tile, nil
	}
	resolved, err := ResolveEffectiveNavClearanceSourceTile(ctx.manifest, ctx.manifestPath, ctx.delta, ctx.deltaPath, coord)
	if err != nil {
		return nil, err
	}
	if !resolved.Found || resolved.Empty {
		ctx.loaded[coord] = struct{}{}
		return nil, nil
	}
	if ctx.loads >= ctx.opts.MaxTileLoads {
		ctx.loaded[coord] = struct{}{}
		return nil, nil
	}
	ctx.loads++
	tile, err := loadCachedNavClearanceSourceTile(resolved.TilePath)
	if err != nil {
		return nil, err
	}
	ctx.cache[coord] = newNavClearanceLoadedTile(coord, tile)
	ctx.loaded[coord] = struct{}{}
	return tile, nil
}

func newNavClearanceLoadedTile(coord TerrainChunkCoordDef, tile *NavClearanceSourceTileDef) navClearanceLoadedTile {
	out := navClearanceLoadedTile{Tile: tile}
	if tile == nil || len(tile.Cells) == 0 {
		return out
	}
	navClearanceLoadedTileIndexCache.Lock()
	if cached, ok := navClearanceLoadedTileIndexCache.Entries[tile]; ok && cached.Len == len(tile.Cells) && cached.Coord == coord {
		out.Cells = cached.Cells
		out.CellsXZ = cached.CellsXZ
		navClearanceLoadedTileIndexCache.Unlock()
		return out
	}
	navClearanceLoadedTileIndexCache.Unlock()

	out.Cells = make(map[[3]int]NavClearanceSourceCellDef, len(tile.Cells))
	out.CellsXZ = make(map[[2]int][]navClearanceCellRef)
	for _, cell := range tile.Cells {
		out.Cells[[3]int{cell.X, cell.Y, cell.Z}] = cell
		key := [2]int{cell.X, cell.Z}
		out.CellsXZ[key] = append(out.CellsXZ[key], navClearanceCellRef{Coord: coord, X: cell.X, Y: cell.Y, Z: cell.Z})
	}
	navClearanceLoadedTileIndexCache.Lock()
	navClearanceLoadedTileIndexCache.Entries[tile] = navClearanceLoadedTileIndexEntry{
		Len:     len(tile.Cells),
		Coord:   coord,
		Cells:   out.Cells,
		CellsXZ: out.CellsXZ,
	}
	navClearanceLoadedTileIndexCache.Unlock()
	return out
}

func FindNavClearanceSourceCell(tile *NavClearanceSourceTileDef, x int, y int, z int) (NavClearanceSourceCellDef, bool) {
	if tile == nil || len(tile.Cells) == 0 {
		return NavClearanceSourceCellDef{}, false
	}
	key := [3]int{x, y, z}
	navClearanceSourceCellIndexCache.Lock()
	if cached, ok := navClearanceSourceCellIndexCache.Entries[tile]; ok && cached.Len == len(tile.Cells) {
		cell, found := cached.Cells[key]
		navClearanceSourceCellIndexCache.Unlock()
		return cell, found
	}
	navClearanceSourceCellIndexCache.Unlock()

	cells := make(map[[3]int]NavClearanceSourceCellDef, len(tile.Cells))
	for _, cell := range tile.Cells {
		cells[[3]int{cell.X, cell.Y, cell.Z}] = cell
	}
	cell, found := cells[key]

	navClearanceSourceCellIndexCache.Lock()
	navClearanceSourceCellIndexCache.Entries[tile] = navClearanceSourceCellIndexEntry{
		Len:   len(tile.Cells),
		Cells: cells,
	}
	navClearanceSourceCellIndexCache.Unlock()
	return cell, found
}

func loadCachedNavClearanceSourceTile(path string) (*NavClearanceSourceTileDef, error) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		return nil, statErr
	}
	navClearanceSourceTileCache.Lock()
	if cached, ok := navClearanceSourceTileCache.Entries[path]; ok && cached.ModTime.Equal(info.ModTime()) && cached.Size == info.Size() {
		tile := cached.Tile
		navClearanceSourceTileCache.Unlock()
		return tile, nil
	}
	navClearanceSourceTileCache.Unlock()

	tile, err := LoadNavClearanceSourceTile(path)
	if err != nil {
		return nil, err
	}
	navClearanceSourceTileCache.Lock()
	navClearanceSourceTileCache.Entries[path] = navClearanceSourceTileCacheEntry{
		Tile:    tile,
		ModTime: info.ModTime(),
		Size:    info.Size(),
	}
	navClearanceSourceTileCache.Unlock()
	return tile, nil
}

func (ctx *navClearancePathContext) coordAllowed(coord TerrainChunkCoordDef) bool {
	if len(ctx.opts.AllowedTileCoords) == 0 {
		return true
	}
	_, ok := ctx.opts.AllowedTileCoords[coord]
	return ok
}

func (ctx *navClearancePathContext) snapDistanceIsMeaningful(distance float32) bool {
	threshold := float32(1e-4)
	if ctx != nil && ctx.manifest != nil && ctx.manifest.VoxelResolution > 0 {
		threshold = ctx.manifest.VoxelResolution * 0.75
	}
	return distance > threshold
}

func (ctx *navClearancePathContext) findCellPath(start navClearanceCellRef, end navClearanceCellRef) ([]NavClearancePathStep, bool, TerrainChunkCoordDef, error) {
	open := &navClearancePathQueue{}
	heap.Init(open)
	startKey := start.key()
	cameFrom := map[string]navClearanceCellRef{}
	costs := map[string]float32{startKey: 0}
	heap.Push(open, &navClearancePathNode{Ref: start, Cost: 0, Score: ctx.clearanceHeuristic(start, end)})
	closed := map[string]struct{}{}
	failureCoord := start.Coord
	for open.Len() > 0 {
		current := heap.Pop(open).(*navClearancePathNode)
		currentKey := current.Ref.key()
		if _, ok := closed[currentKey]; ok {
			continue
		}
		if current.Ref == end {
			return ctx.reconstructPath(cameFrom, current.Ref), true, TerrainChunkCoordDef{}, nil
		}
		closed[currentKey] = struct{}{}
		failureCoord = current.Ref.Coord
		neighbors, err := ctx.neighbors(current.Ref)
		if err != nil {
			return nil, false, TerrainChunkCoordDef{}, err
		}
		for _, next := range neighbors {
			nextKey := next.key()
			if _, ok := closed[nextKey]; ok {
				continue
			}
			stepCost := ctx.clearanceHeuristic(current.Ref, next)
			newCost := costs[currentKey] + stepCost
			if oldCost, ok := costs[nextKey]; ok && newCost >= oldCost {
				continue
			}
			costs[nextKey] = newCost
			cameFrom[nextKey] = current.Ref
			heap.Push(open, &navClearancePathNode{Ref: next, Cost: newCost, Score: newCost + ctx.clearanceHeuristic(next, end)})
		}
	}
	return nil, false, failureCoord, nil
}

func (ctx *navClearancePathContext) neighbors(ref navClearanceCellRef) ([]navClearanceCellRef, error) {
	out := make([]navClearanceCellRef, 0, 4)
	for _, offset := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		candidates, err := ctx.cellsAtWrappedXZ(ref.Coord, ref.X+offset[0], ref.Z+offset[1])
		if err != nil {
			return nil, err
		}
		current, ok := ctx.cell(ref)
		if !ok {
			continue
		}
		for _, candidate := range candidates {
			nextCell, ok := ctx.cell(candidate)
			if !ok || !navClearanceSourceCellSupportsNormalizedAgent(nextCell, ctx.profile) {
				continue
			}
			if !navClearanceCellsCanConnectNormalized(current, nextCell, ctx.profile) {
				continue
			}
			out = append(out, candidate)
		}
	}
	return out, nil
}

func (ctx *navClearancePathContext) cellsAtWrappedXZ(coord TerrainChunkCoordDef, x int, z int) ([]navClearanceCellRef, error) {
	if ctx.manifest.ChunkSize <= 0 {
		return nil, fmt.Errorf("invalid nav manifest chunk_size")
	}
	target := coord
	for x < 0 {
		target.X--
		x += ctx.manifest.ChunkSize
	}
	for x >= ctx.manifest.ChunkSize {
		target.X++
		x -= ctx.manifest.ChunkSize
	}
	for z < 0 {
		target.Z--
		z += ctx.manifest.ChunkSize
	}
	for z >= ctx.manifest.ChunkSize {
		target.Z++
		z -= ctx.manifest.ChunkSize
	}
	tile, err := ctx.loadTile(target)
	if err != nil || tile == nil {
		return nil, err
	}
	loaded := ctx.cache[target]
	if len(loaded.CellsXZ) == 0 {
		return nil, nil
	}
	return append([]navClearanceCellRef(nil), loaded.CellsXZ[[2]int{x, z}]...), nil
}

func (ctx *navClearancePathContext) cell(ref navClearanceCellRef) (NavClearanceSourceCellDef, bool) {
	loaded := ctx.cache[ref.Coord]
	if len(loaded.Cells) == 0 {
		return NavClearanceSourceCellDef{}, false
	}
	cell, ok := loaded.Cells[[3]int{ref.X, ref.Y, ref.Z}]
	return cell, ok
}

func (ctx *navClearancePathContext) reconstructPath(cameFrom map[string]navClearanceCellRef, current navClearanceCellRef) []NavClearancePathStep {
	reversed := []NavClearancePathStep{current.step()}
	for {
		prev, ok := cameFrom[current.key()]
		if !ok {
			break
		}
		current = prev
		reversed = append(reversed, current.step())
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed
}

func (ctx *navClearancePathContext) clearanceHeuristic(a navClearanceCellRef, b navClearanceCellRef) float32 {
	aCell, aOK := ctx.cell(a)
	bCell, bOK := ctx.cell(b)
	if !aOK || !bOK {
		dx := float32(a.Coord.X-b.Coord.X)*float32(ctx.manifest.ChunkSize) + float32(a.X-b.X)
		dz := float32(a.Coord.Z-b.Coord.Z)*float32(ctx.manifest.ChunkSize) + float32(a.Z-b.Z)
		return float32(math.Sqrt(float64(dx*dx + dz*dz)))
	}
	return navVec3Distance(aCell.Position, bCell.Position)
}

func navClearanceCellsCanConnect(a NavClearanceSourceCellDef, b NavClearanceSourceCellDef, profile NavAgentProfileDef) bool {
	EnsureNavAgentProfileDefaults(&profile)
	return navClearanceCellsCanConnectNormalized(a, b, profile)
}

func navClearanceCellsCanConnectNormalized(a NavClearanceSourceCellDef, b NavClearanceSourceCellDef, profile NavAgentProfileDef) bool {
	if !navClearanceSourceCellSupportsNormalizedAgent(a, profile) || !navClearanceSourceCellSupportsNormalizedAgent(b, profile) {
		return false
	}
	if absNavFloat32(a.Position[1]-b.Position[1]) > profile.StepHeight+1e-4 {
		return false
	}
	return true
}

func (r navClearanceCellRef) key() string {
	return fmt.Sprintf("%s:%d:%d:%d", TerrainChunkKey(r.Coord), r.X, r.Y, r.Z)
}

func (r navClearanceCellRef) step() NavClearancePathStep {
	return NavClearancePathStep{Coord: r.Coord, X: r.X, Y: r.Y, Z: r.Z}
}

func (s NavClearancePathStep) ref() navClearanceCellRef {
	return navClearanceCellRef{Coord: s.Coord, X: s.X, Y: s.Y, Z: s.Z}
}

func (q navClearancePathQueue) Len() int { return len(q) }

func (q navClearancePathQueue) Less(i, j int) bool {
	if q[i].Score != q[j].Score {
		return q[i].Score < q[j].Score
	}
	return q[i].Cost < q[j].Cost
}

func (q navClearancePathQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].Index = i
	q[j].Index = j
}

func (q *navClearancePathQueue) Push(x any) {
	item := x.(*navClearancePathNode)
	item.Index = len(*q)
	*q = append(*q, item)
}

func (q *navClearancePathQueue) Pop() any {
	old := *q
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.Index = -1
	*q = old[:n-1]
	return item
}
