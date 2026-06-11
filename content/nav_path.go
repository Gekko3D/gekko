package content

import (
	"container/heap"
	"fmt"
	"math"
)

const (
	DefaultNavPathMaxTileSearchRadius  = 8
	DefaultNavPathMaxTileLoads         = 512
	DefaultNavPathEndpointSnapDistance = 0.75
)

const (
	NavPathFailureMissingTile         = "missing_tile"
	NavPathFailureDeltaEmptyTile      = "delta_empty_tile"
	NavPathFailureDisallowedTile      = "disallowed_tile"
	NavPathFailureStartPolygonMissing = "start_polygon_missing"
	NavPathFailureEndPolygonMissing   = "end_polygon_missing"
	NavPathFailureNoPath              = "no_path"
)

const (
	NavPathPortalClearanceOK        = "ok"
	NavPathPortalClearanceTooNarrow = "portal_too_narrow"
)

type NavPathOptions struct {
	AgentProfileID       string
	MaxTileSearchRadius  int
	MaxTileLoads         int
	EndpointSnapDistance float32
	AllowedTileCoords    map[TerrainChunkCoordDef]struct{}
}

type NavPathStep struct {
	Coord         TerrainChunkCoordDef
	PolygonID     string
	Source        string
	EnterKind     string `json:"enter_kind,omitempty"`
	EnterLinkID   string `json:"enter_link_id,omitempty"`
	EnterLinkKind string `json:"enter_link_kind,omitempty"`
}

type NavPathPortal struct {
	Start           Vec3
	End             Vec3
	Mandatory       bool
	Width           float32
	RequiredWidth   float32
	ClearanceOK     bool
	ClearanceReason string
}

type NavPathResult struct {
	Found             bool
	Steps             []NavPathStep
	Waypoints         []Vec3
	Portals           []NavPathPortal
	StartPoint        Vec3
	EndPoint          Vec3
	StartSnapped      bool
	EndSnapped        bool
	StartSnapDistance float32
	EndSnapDistance   float32
	FailureReason     string
	FailureCoord      TerrainChunkCoordDef
}

func FindEffectiveNavPath(baseNav *NavManifestDef, baseNavPath string, delta *WorldDeltaDef, deltaPath string, start Vec3, end Vec3, opts NavPathOptions) (NavPathResult, error) {
	if baseNav == nil {
		return NavPathResult{}, fmt.Errorf("base nav manifest is nil")
	}
	EnsureNavManifestDefaults(baseNav)
	opts = normalizeNavPathOptions(opts)
	startCoord, ok := navTileCoordForPoint(start, baseNav.ChunkSize, baseNav.VoxelResolution)
	if !ok {
		return NavPathResult{}, fmt.Errorf("invalid nav manifest chunk metrics")
	}
	endCoord, _ := navTileCoordForPoint(end, baseNav.ChunkSize, baseNav.VoxelResolution)
	ctx := &effectiveNavPathContext{
		baseNav:     baseNav,
		baseNavPath: baseNavPath,
		delta:       delta,
		deltaPath:   deltaPath,
		opts:        opts,
		profiles:    navAgentProfilesByID(baseNav.AgentProfiles),
		cache:       map[TerrainChunkCoordDef]*effectiveNavPathTile{},
	}
	startEndpoint, failure, err := ctx.resolveEndpoint(start, startCoord, NavPathFailureStartPolygonMissing)
	if err != nil || failure.FailureReason != "" {
		return failure, err
	}
	endEndpoint, failure, err := ctx.resolveEndpoint(end, endCoord, NavPathFailureEndPolygonMissing)
	if err != nil || failure.FailureReason != "" {
		return failure, err
	}
	startCoord = startEndpoint.coord
	endCoord = endEndpoint.coord
	ctx.startEndpoint = startEndpoint
	ctx.endEndpoint = endEndpoint
	startNode := navPathNode{coord: startCoord, polygonID: startEndpoint.polygon.ID}
	endNode := navPathNode{coord: endCoord, polygonID: endEndpoint.polygon.ID}
	if startNode == endNode {
		return ctx.annotateResult(NavPathResult{
			Found:     true,
			Steps:     []NavPathStep{{Coord: startCoord, PolygonID: startEndpoint.polygon.ID, Source: startEndpoint.tile.lookup.Source}},
			Waypoints: navDedupePathWaypoints([]Vec3{startEndpoint.point, endEndpoint.point}),
		}), nil
	}
	bounds := navPathSearchBounds(startCoord, endCoord, opts.MaxTileSearchRadius)
	queue := &navPathPriorityQueue{}
	heap.Push(queue, &navPathQueueItem{Node: startNode})
	costs := map[navPathNode]float32{startNode: 0}
	previous := map[navPathNode]navPathPrevious{}
	for queue.Len() > 0 {
		item := heap.Pop(queue).(*navPathQueueItem)
		current := item.Node
		if item.Cost > costs[current]+1e-5 {
			continue
		}
		if current == endNode {
			return ctx.buildResult(startNode, endNode, previous), nil
		}
		neighbors, err := ctx.neighbors(current, bounds)
		if err != nil {
			return NavPathResult{}, err
		}
		for _, neighbor := range neighbors {
			stepCost := ctx.edgeCost(current, neighbor)
			nextCost := costs[current] + stepCost
			if existing, ok := costs[neighbor.Node]; ok && existing <= nextCost {
				continue
			}
			costs[neighbor.Node] = nextCost
			previous[neighbor.Node] = navPathPrevious{From: current, Kind: neighbor.Kind, LinkID: neighbor.LinkID, LinkKind: neighbor.LinkKind}
			heap.Push(queue, &navPathQueueItem{Node: neighbor.Node, Cost: nextCost})
		}
	}
	return ctx.noPathResult(), nil
}

type effectiveNavPathContext struct {
	baseNav       *NavManifestDef
	baseNavPath   string
	delta         *WorldDeltaDef
	deltaPath     string
	opts          NavPathOptions
	profiles      map[string]NavAgentProfileDef
	cache         map[TerrainChunkCoordDef]*effectiveNavPathTile
	failures      []navPathFailure
	startEndpoint navPathEndpoint
	endEndpoint   navPathEndpoint
}

type effectiveNavPathTile struct {
	lookup        NavTileLookupResult
	query         *NavTileQuery
	empty         bool
	failureReason string
}

type navPathFailure struct {
	reason string
	coord  TerrainChunkCoordDef
}

type navPathNode struct {
	coord     TerrainChunkCoordDef
	polygonID string
}

type navPathEdge struct {
	Node     navPathNode
	Kind     string
	LinkID   string
	LinkKind string
}

type navPathPrevious struct {
	From     navPathNode
	Kind     string
	LinkID   string
	LinkKind string
}

type navPathQueueItem struct {
	Node  navPathNode
	Cost  float32
	index int
}

type navPathPriorityQueue []*navPathQueueItem

func (q navPathPriorityQueue) Len() int { return len(q) }

func (q navPathPriorityQueue) Less(i, j int) bool { return q[i].Cost < q[j].Cost }

func (q navPathPriorityQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index = i
	q[j].index = j
}

func (q *navPathPriorityQueue) Push(x any) {
	item := x.(*navPathQueueItem)
	item.index = len(*q)
	*q = append(*q, item)
}

func (q *navPathPriorityQueue) Pop() any {
	old := *q
	item := old[len(old)-1]
	old[len(old)-1] = nil
	item.index = -1
	*q = old[:len(old)-1]
	return item
}

type navPathEndpoint struct {
	coord        TerrainChunkCoordDef
	tile         *effectiveNavPathTile
	polygon      NavPolygonDef
	point        Vec3
	snapped      bool
	snapDistance float32
}

type navPathCoordBounds struct {
	min TerrainChunkCoordDef
	max TerrainChunkCoordDef
}

func normalizeNavPathOptions(opts NavPathOptions) NavPathOptions {
	if opts.AgentProfileID == "" {
		opts.AgentProfileID = DefaultNavAgentProfileID
	}
	if opts.MaxTileSearchRadius <= 0 {
		opts.MaxTileSearchRadius = DefaultNavPathMaxTileSearchRadius
	}
	if opts.MaxTileLoads <= 0 {
		opts.MaxTileLoads = DefaultNavPathMaxTileLoads
	}
	if opts.EndpointSnapDistance <= 0 {
		opts.EndpointSnapDistance = DefaultNavPathEndpointSnapDistance
	}
	return opts
}

func (ctx *effectiveNavPathContext) loadTile(coord TerrainChunkCoordDef) (*effectiveNavPathTile, error) {
	if tile, ok := ctx.cache[coord]; ok {
		return tile, nil
	}
	if !ctx.coordAllowed(coord) {
		tile := &effectiveNavPathTile{empty: true, failureReason: NavPathFailureDisallowedTile}
		ctx.cache[coord] = tile
		ctx.recordFailure(tile.failureReason, coord)
		return tile, nil
	}
	if len(ctx.cache) >= ctx.opts.MaxTileLoads {
		return nil, fmt.Errorf("nav path tile load budget exceeded (%d)", ctx.opts.MaxTileLoads)
	}
	lookup, err := LoadEffectiveNavTile(ctx.baseNav, ctx.baseNavPath, ctx.delta, ctx.deltaPath, coord, ctx.opts.AgentProfileID)
	if err != nil {
		return nil, err
	}
	if !lookup.Found || lookup.Empty || lookup.Tile == nil {
		tile := &effectiveNavPathTile{lookup: lookup, empty: true, failureReason: navPathFailureReasonForLookup(lookup)}
		ctx.cache[coord] = tile
		ctx.recordFailure(tile.failureReason, coord)
		return tile, nil
	}
	query, err := NewNavTileQuery(lookup.Tile)
	if err != nil {
		return nil, err
	}
	tile := &effectiveNavPathTile{lookup: lookup, query: query}
	ctx.cache[coord] = tile
	return tile, nil
}

func (ctx *effectiveNavPathContext) coordAllowed(coord TerrainChunkCoordDef) bool {
	if len(ctx.opts.AllowedTileCoords) == 0 {
		return true
	}
	_, ok := ctx.opts.AllowedTileCoords[coord]
	return ok
}

func (ctx *effectiveNavPathContext) resolveEndpoint(point Vec3, primaryCoord TerrainChunkCoordDef, polygonMissingReason string) (navPathEndpoint, NavPathResult, error) {
	candidates, ok := navTileCoordCandidatesForPoint(point, ctx.baseNav.ChunkSize, ctx.baseNav.VoxelResolution)
	if !ok {
		return navPathEndpoint{}, NavPathResult{}, fmt.Errorf("invalid nav manifest chunk metrics")
	}
	candidates = navPathEndpointCandidateCoords(candidates, primaryCoord, ctx.baseNav.ChunkSize, ctx.baseNav.VoxelResolution, ctx.opts.EndpointSnapDistance)
	sawLoadedTile := false
	loadedTileCoord := primaryCoord
	var firstTileFailure NavPathResult
	bestSnap := NavPolygonSnapResult{}
	bestSnapCoord := TerrainChunkCoordDef{}
	var bestSnapTile *effectiveNavPathTile
	foundSnap := false
	for _, coord := range candidates {
		if !ctx.coordAllowed(coord) {
			ctx.recordFailure(NavPathFailureDisallowedTile, coord)
			if firstTileFailure.FailureReason == "" {
				firstTileFailure = NavPathResult{FailureReason: NavPathFailureDisallowedTile, FailureCoord: coord}
			}
			if coord == primaryCoord {
				return navPathEndpoint{}, firstTileFailure, nil
			}
			continue
		}
		tile, err := ctx.loadTile(coord)
		if err != nil {
			return navPathEndpoint{}, NavPathResult{}, err
		}
		if tile == nil || tile.empty {
			if firstTileFailure.FailureReason == "" {
				firstTileFailure = ctx.failureForTile(coord, tile)
			}
			if coord == primaryCoord && tile != nil && tile.failureReason == NavPathFailureDeltaEmptyTile {
				return navPathEndpoint{}, ctx.failureForTile(coord, tile), nil
			}
			continue
		}
		if !sawLoadedTile {
			sawLoadedTile = true
			loadedTileCoord = coord
		}
		polygon, ok := tile.query.FindPolygonAt(point)
		if ok {
			snap, snapOK := tile.query.SnapPointToPolygon(point, polygon.ID)
			if !snapOK {
				snap = NavPolygonSnapResult{Polygon: polygon, Point: point}
			}
			return navPathEndpoint{
				coord:        coord,
				tile:         tile,
				polygon:      polygon,
				point:        snap.Point,
				snapped:      snap.Distance > 1e-4,
				snapDistance: snap.Distance,
			}, NavPathResult{}, nil
		}
		if snap, ok := tile.query.FindNearestPolygon(point, ctx.opts.EndpointSnapDistance); ok {
			if !foundSnap || snap.Distance < bestSnap.Distance {
				bestSnap = snap
				bestSnapCoord = coord
				bestSnapTile = tile
				foundSnap = true
			}
		}
	}
	if foundSnap {
		return navPathEndpoint{
			coord:        bestSnapCoord,
			tile:         bestSnapTile,
			polygon:      bestSnap.Polygon,
			point:        bestSnap.Point,
			snapped:      bestSnap.Distance > 1e-4,
			snapDistance: bestSnap.Distance,
		}, NavPathResult{}, nil
	}
	if sawLoadedTile {
		return navPathEndpoint{}, NavPathResult{FailureReason: polygonMissingReason, FailureCoord: loadedTileCoord}, nil
	}
	if firstTileFailure.FailureReason != "" {
		return navPathEndpoint{}, firstTileFailure, nil
	}
	return navPathEndpoint{}, NavPathResult{FailureReason: polygonMissingReason, FailureCoord: primaryCoord}, nil
}

func navPathEndpointCandidateCoords(candidates []TerrainChunkCoordDef, primary TerrainChunkCoordDef, chunkSize int, voxelResolution float32, snapDistance float32) []TerrainChunkCoordDef {
	worldSize := float32(chunkSize) * voxelResolution
	if worldSize <= 0 || snapDistance <= 0 {
		return candidates
	}
	reach := int(math.Ceil(float64(snapDistance / worldSize)))
	if reach <= 0 {
		reach = 1
	}
	out := append([]TerrainChunkCoordDef{}, candidates...)
	for x := primary.X - reach; x <= primary.X+reach; x++ {
		for y := primary.Y - reach; y <= primary.Y+reach; y++ {
			for z := primary.Z - reach; z <= primary.Z+reach; z++ {
				out = appendUniqueNavPathCoord(out, TerrainChunkCoordDef{X: x, Y: y, Z: z})
			}
		}
	}
	return out
}

func appendUniqueNavPathCoord(coords []TerrainChunkCoordDef, coord TerrainChunkCoordDef) []TerrainChunkCoordDef {
	for _, existing := range coords {
		if existing == coord {
			return coords
		}
	}
	return append(coords, coord)
}

func (ctx *effectiveNavPathContext) neighbors(node navPathNode, bounds navPathCoordBounds) ([]navPathEdge, error) {
	tile, err := ctx.loadTile(node.coord)
	if err != nil || tile == nil || tile.empty {
		return nil, err
	}
	polygon, ok := tile.query.FindPolygonByID(node.polygonID)
	if !ok {
		return nil, nil
	}
	out := make([]navPathEdge, 0, len(polygon.Neighbors)+4)
	for _, neighborID := range polygon.Neighbors {
		if _, ok := tile.query.FindPolygonByID(neighborID); ok {
			out = appendUniqueNavPathEdge(out, navPathEdge{Node: navPathNode{coord: node.coord, polygonID: neighborID}, Kind: NavTraversalWalk})
		}
	}
	offMeshNeighbors, err := ctx.offMeshNeighbors(node.coord, polygon.ID, bounds)
	if err != nil {
		return nil, err
	}
	for _, neighbor := range offMeshNeighbors {
		out = appendUniqueNavPathEdge(out, neighbor)
	}
	if len(tile.query.Tile.Portals) > 0 {
		for _, portal := range tile.query.Tile.Portals {
			if portal.FromPolygonID != polygon.ID {
				continue
			}
			if !navPathCoordInBounds(portal.ToTileCoord, bounds) {
				continue
			}
			if !ctx.coordAllowed(portal.ToTileCoord) {
				ctx.recordFailure(NavPathFailureDisallowedTile, portal.ToTileCoord)
				continue
			}
			neighborTile, err := ctx.loadTile(portal.ToTileCoord)
			if err != nil {
				return nil, err
			}
			if neighborTile == nil || neighborTile.empty {
				continue
			}
			if _, ok := neighborTile.query.FindPolygonByID(portal.ToPolygonID); ok {
				out = appendUniqueNavPathEdge(out, navPathEdge{Node: navPathNode{coord: portal.ToTileCoord, polygonID: portal.ToPolygonID}, Kind: "portal", LinkID: portal.ID, LinkKind: firstNonEmptyNavString(portal.Area, NavTraversalWalk)})
			}
		}
	}
	profile := navPortalProfileForTile(tile.query.Tile, ctx.profiles)
	for _, dir := range navPathCardinalDirs() {
		neighborCoord := TerrainChunkCoordDef{X: node.coord.X + dir.X, Y: node.coord.Y + dir.Y, Z: node.coord.Z + dir.Z}
		if !navPathCoordInBounds(neighborCoord, bounds) {
			continue
		}
		if !ctx.coordAllowed(neighborCoord) {
			ctx.recordFailure(NavPathFailureDisallowedTile, neighborCoord)
			continue
		}
		neighborTile, err := ctx.loadTile(neighborCoord)
		if err != nil {
			return nil, err
		}
		if neighborTile == nil || neighborTile.empty {
			continue
		}
		for _, candidate := range neighborTile.query.Tile.Polygons {
			if navPolygonsConnectAcrossTileBoundary(tile.query.Tile, polygon, neighborTile.query.Tile, candidate, dir, profile) {
				out = appendUniqueNavPathEdge(out, navPathEdge{Node: navPathNode{coord: neighborCoord, polygonID: candidate.ID}, Kind: NavTraversalWalk})
			}
		}
	}
	return out, nil
}

func (ctx *effectiveNavPathContext) offMeshNeighbors(coord TerrainChunkCoordDef, polygonID string, bounds navPathCoordBounds) ([]navPathEdge, error) {
	tile, err := ctx.loadTile(coord)
	if err != nil || tile == nil || tile.empty || tile.query == nil || tile.query.Tile == nil {
		return nil, err
	}
	out := make([]navPathEdge, 0)
	for _, link := range tile.query.Tile.OffMeshLinks {
		if link.FromPolygonID != "" && link.FromPolygonID != polygonID {
			continue
		}
		if link.FromPolygonID == "" {
			startPolygon, ok := tile.query.FindPolygonAt(link.Start)
			if !ok || startPolygon.ID != polygonID {
				continue
			}
		}
		targetCoord := link.ToTileCoord
		if link.ToPolygonID == "" && targetCoord == (TerrainChunkCoordDef{}) {
			targetCoord = coord
		}
		if !navPathCoordInBounds(targetCoord, bounds) {
			continue
		}
		if !ctx.coordAllowed(targetCoord) {
			ctx.recordFailure(NavPathFailureDisallowedTile, targetCoord)
			continue
		}
		targetTile, err := ctx.loadTile(targetCoord)
		if err != nil {
			return nil, err
		}
		if targetTile == nil || targetTile.empty || targetTile.query == nil {
			continue
		}
		targetPolygonID := link.ToPolygonID
		if targetPolygonID == "" {
			targetPolygon, ok := targetTile.query.FindPolygonAt(link.End)
			if !ok {
				continue
			}
			targetPolygonID = targetPolygon.ID
		}
		if _, ok := targetTile.query.FindPolygonByID(targetPolygonID); ok {
			kind := firstNonEmptyNavString(link.Kind, NavTraversalWalk)
			out = appendUniqueNavPathEdge(out, navPathEdge{
				Node:     navPathNode{coord: targetCoord, polygonID: targetPolygonID},
				Kind:     kind,
				LinkID:   link.ID,
				LinkKind: kind,
			})
		}
	}
	return out, nil
}

func appendUniqueNavPathNode(nodes []navPathNode, node navPathNode) []navPathNode {
	if node.polygonID == "" {
		return nodes
	}
	for _, existing := range nodes {
		if existing == node {
			return nodes
		}
	}
	return append(nodes, node)
}

func appendUniqueNavPathEdge(edges []navPathEdge, edge navPathEdge) []navPathEdge {
	if edge.Node.polygonID == "" {
		return edges
	}
	for _, existing := range edges {
		if existing.Node == edge.Node {
			return edges
		}
	}
	return append(edges, edge)
}

func (ctx *effectiveNavPathContext) buildResult(start navPathNode, end navPathNode, previous map[navPathNode]navPathPrevious) NavPathResult {
	reversed := []navPathNode{end}
	for current := end; current != start; {
		current = previous[current].From
		reversed = append(reversed, current)
	}
	steps := make([]NavPathStep, len(reversed))
	waypoints := make([]Vec3, 0, len(reversed))
	for i := range reversed {
		node := reversed[len(reversed)-1-i]
		tile := ctx.cache[node.coord]
		center, _ := tile.query.PolygonCenter(node.polygonID)
		steps[i] = NavPathStep{Coord: node.coord, PolygonID: node.polygonID, Source: tile.lookup.Source}
		if i > 0 {
			prev := previous[node]
			steps[i].EnterKind = prev.Kind
			steps[i].EnterLinkID = prev.LinkID
			steps[i].EnterLinkKind = prev.LinkKind
		}
		waypoints = append(waypoints, center)
	}
	if segments, ok := ctx.pathPortalSegments(steps); ok {
		waypoints = navBuildStringPulledWaypoints(ctx.startEndpoint.point, ctx.endEndpoint.point, segments)
		portals := make([]NavPathPortal, 0, len(segments))
		for _, segment := range segments {
			portals = append(portals, navPathPortalFromSegment(segment, ctx.profile()))
		}
		return ctx.annotateResult(NavPathResult{Found: true, Steps: steps, Waypoints: waypoints, Portals: portals})
	}
	return ctx.annotateResult(NavPathResult{Found: true, Steps: steps, Waypoints: waypoints})
}

func (ctx *effectiveNavPathContext) profile() NavAgentProfileDef {
	profile, ok := ctx.profiles[ctx.opts.AgentProfileID]
	if !ok {
		profile = DefaultHL1NavAgentProfile()
	}
	EnsureNavAgentProfileDefaults(&profile)
	return profile
}

func navPathPortalFromSegment(segment navPathPortalSegment, profile NavAgentProfileDef) NavPathPortal {
	width := navVec2Length(segment.End[0]-segment.Start[0], segment.End[2]-segment.Start[2])
	required := profile.Radius * 2
	reason := NavPathPortalClearanceOK
	clearanceOK := true
	if width+1e-4 < required {
		clearanceOK = false
		reason = NavPathPortalClearanceTooNarrow
	}
	return NavPathPortal{
		Start:           segment.Start,
		End:             segment.End,
		Mandatory:       segment.Mandatory,
		Width:           width,
		RequiredWidth:   required,
		ClearanceOK:     clearanceOK,
		ClearanceReason: reason,
	}
}

func (ctx *effectiveNavPathContext) pathPortalSegments(steps []NavPathStep) ([]navPathPortalSegment, bool) {
	if len(steps) < 2 {
		return nil, true
	}
	segments := make([]navPathPortalSegment, 0, len(steps)-1)
	for i := 1; i < len(steps); i++ {
		segment, ok := ctx.pathPortalSegment(steps[i-1], steps[i])
		if !ok {
			return nil, false
		}
		segments = append(segments, segment)
	}
	return segments, true
}

func (ctx *effectiveNavPathContext) pathPortalSegment(from NavPathStep, to NavPathStep) (navPathPortalSegment, bool) {
	fromTile := ctx.cache[from.Coord]
	toTile := ctx.cache[to.Coord]
	if fromTile == nil || toTile == nil || fromTile.empty || toTile.empty {
		return navPathPortalSegment{}, false
	}
	fromPolygon, fromOK := fromTile.query.FindPolygonByID(from.PolygonID)
	toPolygon, toOK := toTile.query.FindPolygonByID(to.PolygonID)
	if !fromOK || !toOK {
		return navPathPortalSegment{}, false
	}
	if to.EnterKind == "portal" && to.EnterLinkID != "" {
		for _, portal := range fromTile.query.Tile.Portals {
			if portal.ID == to.EnterLinkID && portal.FromPolygonID == from.PolygonID && portal.ToPolygonID == to.PolygonID && portal.ToTileCoord == to.Coord {
				return navPathPortalSegment{
					Start:     portal.Start,
					End:       portal.End,
					Mandatory: navPathTransitionRequiresWaypoint(fromPolygon.Area, toPolygon.Area, firstNonEmptyNavString(portal.Area, to.EnterLinkKind), portal.Start, portal.End),
				}, true
			}
		}
	}
	if to.EnterKind != "" && to.EnterKind != NavTraversalWalk && to.EnterKind != "portal" {
		return navPathPortalSegment{}, false
	}
	profile := navPortalProfileForTile(fromTile.query.Tile, ctx.profiles)
	segment, ok := navSharedPortalSegment(fromTile.query.Tile, fromPolygon, toTile.query.Tile, toPolygon, profile)
	if ok {
		segment.Mandatory = navPathTransitionRequiresWaypoint(fromPolygon.Area, toPolygon.Area, to.EnterLinkKind, segment.Start, segment.End)
	}
	return segment, ok
}

func (ctx *effectiveNavPathContext) edgeCost(from navPathNode, edge navPathEdge) float32 {
	fromCenter, fromOK := ctx.nodeCenter(from)
	toCenter, toOK := ctx.nodeCenter(edge.Node)
	if !fromOK || !toOK {
		return 1
	}
	cost := navVec3Distance(fromCenter, toCenter)
	if cost <= 1e-4 {
		cost = 1
	}
	if edge.LinkID != "" {
		if linkCost, ok := ctx.linkCost(from, edge); ok && linkCost > 0 {
			cost += linkCost
		}
	}
	return cost
}

func (ctx *effectiveNavPathContext) nodeCenter(node navPathNode) (Vec3, bool) {
	if node.coord == ctx.startEndpoint.coord && ctx.startEndpoint.polygon.ID == node.polygonID {
		return ctx.startEndpoint.point, true
	}
	if node.coord == ctx.endEndpoint.coord && ctx.endEndpoint.polygon.ID == node.polygonID {
		return ctx.endEndpoint.point, true
	}
	tile := ctx.cache[node.coord]
	if tile == nil || tile.query == nil {
		return Vec3{}, false
	}
	return tile.query.PolygonCenter(node.polygonID)
}

func (ctx *effectiveNavPathContext) linkCost(from navPathNode, edge navPathEdge) (float32, bool) {
	tile := ctx.cache[from.coord]
	if tile == nil || tile.query == nil {
		return 0, false
	}
	for _, portal := range tile.query.Tile.Portals {
		if portal.ID == edge.LinkID {
			return portal.Cost, true
		}
	}
	for _, link := range tile.query.Tile.OffMeshLinks {
		if link.ID == edge.LinkID {
			return link.Cost, true
		}
	}
	return 0, false
}

func (ctx *effectiveNavPathContext) annotateResult(result NavPathResult) NavPathResult {
	result.StartPoint = ctx.startEndpoint.point
	result.EndPoint = ctx.endEndpoint.point
	result.StartSnapped = ctx.startEndpoint.snapped
	result.EndSnapped = ctx.endEndpoint.snapped
	result.StartSnapDistance = ctx.startEndpoint.snapDistance
	result.EndSnapDistance = ctx.endEndpoint.snapDistance
	return result
}

func (ctx *effectiveNavPathContext) failureForTile(coord TerrainChunkCoordDef, tile *effectiveNavPathTile) NavPathResult {
	reason := NavPathFailureMissingTile
	if tile != nil && tile.failureReason != "" {
		reason = tile.failureReason
	}
	return NavPathResult{FailureReason: reason, FailureCoord: coord}
}

func (ctx *effectiveNavPathContext) noPathResult() NavPathResult {
	for _, reason := range []string{NavPathFailureDisallowedTile, NavPathFailureDeltaEmptyTile, NavPathFailureMissingTile} {
		for _, failure := range ctx.failures {
			if failure.reason == reason {
				return NavPathResult{FailureReason: reason, FailureCoord: failure.coord}
			}
		}
	}
	return NavPathResult{FailureReason: NavPathFailureNoPath}
}

func (ctx *effectiveNavPathContext) recordFailure(reason string, coord TerrainChunkCoordDef) {
	if reason == "" {
		return
	}
	ctx.failures = append(ctx.failures, navPathFailure{reason: reason, coord: coord})
}

func navPathFailureReasonForLookup(lookup NavTileLookupResult) string {
	if lookup.Empty && lookup.Source == NavTileLookupSourceDelta {
		return NavPathFailureDeltaEmptyTile
	}
	return NavPathFailureMissingTile
}

func navTileCoordForPoint(point Vec3, chunkSize int, voxelResolution float32) (TerrainChunkCoordDef, bool) {
	candidates, ok := navTileCoordCandidatesForPoint(point, chunkSize, voxelResolution)
	if !ok || len(candidates) == 0 {
		return TerrainChunkCoordDef{}, ok
	}
	return candidates[0], true
}

func navTileCoordCandidatesForPoint(point Vec3, chunkSize int, voxelResolution float32) ([]TerrainChunkCoordDef, bool) {
	worldSize := float32(chunkSize) * voxelResolution
	if worldSize <= 0 {
		return nil, false
	}
	xs := navAxisTileCoordCandidates(point[0], worldSize)
	ys := navAxisTileCoordCandidates(point[1], worldSize)
	zs := navAxisTileCoordCandidates(point[2], worldSize)
	seen := make(map[TerrainChunkCoordDef]struct{}, len(xs)*len(ys)*len(zs))
	out := make([]TerrainChunkCoordDef, 0, len(xs)*len(ys)*len(zs))
	for _, x := range xs {
		for _, y := range ys {
			for _, z := range zs {
				coord := TerrainChunkCoordDef{X: x, Y: y, Z: z}
				if _, ok := seen[coord]; ok {
					continue
				}
				seen[coord] = struct{}{}
				out = append(out, coord)
			}
		}
	}
	return out, true
}

func navAxisTileCoordCandidates(value float32, worldSize float32) []int {
	base := int(math.Floor(float64(value / worldSize)))
	out := []int{base}
	const baseEpsilon = float32(1e-4)
	epsilon := maxNavFloat32(baseEpsilon, worldSize*1e-5)
	boundaryIndex := int(math.Round(float64(value / worldSize)))
	boundary := float32(boundaryIndex) * worldSize
	if absNavFloat32(value-boundary) <= epsilon {
		out = appendUniqueNavInt(out, boundaryIndex)
		out = appendUniqueNavInt(out, boundaryIndex-1)
	}
	return out
}

func appendUniqueNavInt(values []int, value int) []int {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func navPathSearchBounds(a TerrainChunkCoordDef, b TerrainChunkCoordDef, radius int) navPathCoordBounds {
	return navPathCoordBounds{
		min: TerrainChunkCoordDef{
			X: minNavInt(a.X, b.X) - radius,
			Y: minNavInt(a.Y, b.Y) - radius,
			Z: minNavInt(a.Z, b.Z) - radius,
		},
		max: TerrainChunkCoordDef{
			X: maxNavInt(a.X, b.X) + radius,
			Y: maxNavInt(a.Y, b.Y) + radius,
			Z: maxNavInt(a.Z, b.Z) + radius,
		},
	}
}

func navPathCoordInBounds(coord TerrainChunkCoordDef, bounds navPathCoordBounds) bool {
	return coord.X >= bounds.min.X && coord.X <= bounds.max.X &&
		coord.Y >= bounds.min.Y && coord.Y <= bounds.max.Y &&
		coord.Z >= bounds.min.Z && coord.Z <= bounds.max.Z
}

func navPathCardinalDirs() []TerrainChunkCoordDef {
	return navPortalNeighborOffsets()
}

type navPolygonBounds struct {
	min Vec3
	max Vec3
}

func navPolygonBoundsForTile(tile *NavTileDef, polygon NavPolygonDef) (navPolygonBounds, bool) {
	if tile == nil || len(polygon.Vertices) == 0 {
		return navPolygonBounds{}, false
	}
	var bounds navPolygonBounds
	set := false
	for _, index := range polygon.Vertices {
		if index < 0 || index >= len(tile.Vertices) {
			return navPolygonBounds{}, false
		}
		v := tile.Vertices[index]
		if !set {
			bounds.min = v
			bounds.max = v
			set = true
			continue
		}
		for axis := 0; axis < 3; axis++ {
			if v[axis] < bounds.min[axis] {
				bounds.min[axis] = v[axis]
			}
			if v[axis] > bounds.max[axis] {
				bounds.max[axis] = v[axis]
			}
		}
	}
	return bounds, set
}

func navPolygonsConnectAcrossTileBoundary(tileA *NavTileDef, polygonA NavPolygonDef, tileB *NavTileDef, polygonB NavPolygonDef, dir TerrainChunkCoordDef, profile NavAgentProfileDef) bool {
	return len(navPolygonsBoundaryPortalSegments(tileA, polygonA, tileB, polygonB, dir, profile)) > 0
}

func navPolygonsSameSourceOverlapAcrossTiles(tileA *NavTileDef, polygonA NavPolygonDef, tileB *NavTileDef, polygonB NavPolygonDef, dir TerrainChunkCoordDef) bool {
	a, ok := navPolygonBoundsForTile(tileA, polygonA)
	if !ok {
		return false
	}
	b, ok := navPolygonBoundsForTile(tileB, polygonB)
	if !ok {
		return false
	}
	const epsilon = float32(1e-4)
	switch {
	case dir.X != 0:
		return navRangesOverlapOrTouch(a.min[0], a.max[0], b.min[0], b.max[0], epsilon) &&
			navRangesOverlapOrTouch(a.min[1], a.max[1], b.min[1], b.max[1], epsilon) &&
			navRangesOverlapPositive(a.min[2], a.max[2], b.min[2], b.max[2], epsilon)
	case dir.Y != 0:
		return navRangesOverlapPositive(a.min[0], a.max[0], b.min[0], b.max[0], epsilon) &&
			navRangesOverlapOrTouch(a.min[1], a.max[1], b.min[1], b.max[1], epsilon) &&
			navRangesOverlapPositive(a.min[2], a.max[2], b.min[2], b.max[2], epsilon)
	case dir.Z != 0:
		return navRangesOverlapPositive(a.min[0], a.max[0], b.min[0], b.max[0], epsilon) &&
			navRangesOverlapOrTouch(a.min[1], a.max[1], b.min[1], b.max[1], epsilon) &&
			navRangesOverlapOrTouch(a.min[2], a.max[2], b.min[2], b.max[2], epsilon)
	default:
		return false
	}
}

func navPolygonsTouchAcrossTileBoundary(tileA *NavTileDef, polygonA NavPolygonDef, tileB *NavTileDef, polygonB NavPolygonDef, dir TerrainChunkCoordDef) bool {
	a, ok := navPolygonBoundsForTile(tileA, polygonA)
	if !ok {
		return false
	}
	b, ok := navPolygonBoundsForTile(tileB, polygonB)
	if !ok {
		return false
	}
	const epsilon = float32(1e-4)
	switch {
	case dir.X == 1:
		return navPolygonBoundsCrossesPlane(a, tileA.BoundsMax[0], 0, epsilon) && navPolygonBoundsCrossesPlane(b, tileB.BoundsMin[0], 0, epsilon) && navAlmostEqual(tileA.BoundsMax[0], tileB.BoundsMin[0], epsilon) && navRangesOverlapOrTouch(a.min[1], a.max[1], b.min[1], b.max[1], epsilon) && navRangesOverlapPositive(a.min[2], a.max[2], b.min[2], b.max[2], epsilon)
	case dir.X == -1:
		return navPolygonBoundsCrossesPlane(a, tileA.BoundsMin[0], 0, epsilon) && navPolygonBoundsCrossesPlane(b, tileB.BoundsMax[0], 0, epsilon) && navAlmostEqual(tileA.BoundsMin[0], tileB.BoundsMax[0], epsilon) && navRangesOverlapOrTouch(a.min[1], a.max[1], b.min[1], b.max[1], epsilon) && navRangesOverlapPositive(a.min[2], a.max[2], b.min[2], b.max[2], epsilon)
	case dir.Y == 1:
		return navPolygonBoundsCrossesPlane(a, tileA.BoundsMax[1], 1, epsilon) && navPolygonBoundsCrossesPlane(b, tileB.BoundsMin[1], 1, epsilon) && navAlmostEqual(tileA.BoundsMax[1], tileB.BoundsMin[1], epsilon) && navRangesOverlapPositive(a.min[0], a.max[0], b.min[0], b.max[0], epsilon) && navRangesOverlapPositive(a.min[2], a.max[2], b.min[2], b.max[2], epsilon)
	case dir.Y == -1:
		return navPolygonBoundsCrossesPlane(a, tileA.BoundsMin[1], 1, epsilon) && navPolygonBoundsCrossesPlane(b, tileB.BoundsMax[1], 1, epsilon) && navAlmostEqual(tileA.BoundsMin[1], tileB.BoundsMax[1], epsilon) && navRangesOverlapPositive(a.min[0], a.max[0], b.min[0], b.max[0], epsilon) && navRangesOverlapPositive(a.min[2], a.max[2], b.min[2], b.max[2], epsilon)
	case dir.Z == 1:
		return navPolygonBoundsCrossesPlane(a, tileA.BoundsMax[2], 2, epsilon) && navPolygonBoundsCrossesPlane(b, tileB.BoundsMin[2], 2, epsilon) && navAlmostEqual(tileA.BoundsMax[2], tileB.BoundsMin[2], epsilon) && navRangesOverlapPositive(a.min[0], a.max[0], b.min[0], b.max[0], epsilon) && navRangesOverlapOrTouch(a.min[1], a.max[1], b.min[1], b.max[1], epsilon)
	case dir.Z == -1:
		return navPolygonBoundsCrossesPlane(a, tileA.BoundsMin[2], 2, epsilon) && navPolygonBoundsCrossesPlane(b, tileB.BoundsMax[2], 2, epsilon) && navAlmostEqual(tileA.BoundsMin[2], tileB.BoundsMax[2], epsilon) && navRangesOverlapPositive(a.min[0], a.max[0], b.min[0], b.max[0], epsilon) && navRangesOverlapOrTouch(a.min[1], a.max[1], b.min[1], b.max[1], epsilon)
	default:
		return false
	}
}

func navPolygonBoundsCrossesPlane(bounds navPolygonBounds, plane float32, axis int, epsilon float32) bool {
	return bounds.min[axis] <= plane+epsilon && bounds.max[axis] >= plane-epsilon
}

func navAlmostEqual(a, b, epsilon float32) bool {
	return absNavFloat32(a-b) <= epsilon
}

func navRangesOverlapPositive(aMin, aMax, bMin, bMax, epsilon float32) bool {
	overlap := minNavFloat32(aMax, bMax) - maxNavFloat32(aMin, bMin)
	return overlap > epsilon
}

func navRangesOverlapOrTouch(aMin, aMax, bMin, bMax, epsilon float32) bool {
	overlap := minNavFloat32(aMax, bMax) - maxNavFloat32(aMin, bMin)
	return overlap >= -epsilon
}

func minNavInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxNavInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
