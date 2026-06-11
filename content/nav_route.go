package content

import (
	"fmt"
	"math"
)

const (
	NavRouteRefinementStatusNotAttempted = "not_attempted"
	NavRouteRefinementStatusDisabled     = "disabled"
	NavRouteRefinementStatusRefined      = "refined"
	NavRouteRefinementStatusCoarseOnly   = "coarse_only"
)

const (
	NavRouteRefinementReasonStartSectorMissing    = "start_sector_missing"
	NavRouteRefinementReasonEndSectorMissing      = "end_sector_missing"
	NavRouteRefinementReasonCoarseRouteMissing    = "coarse_route_missing"
	NavRouteRefinementReasonNavigationUnavailable = "navigation_unavailable"
	NavRouteRefinementReasonDisabled              = "disabled"
)

type NavHierarchicalRouteOptions struct {
	SectorGraph                NavSectorGraphOptions
	SectorPath                 NavSectorPathOptions
	LocalPath                  NavPathOptions
	DisableLocalRefinement     bool
	AllowLocalCorridorFallback bool
}

type NavHierarchicalRouteResult struct {
	Found                     bool
	StartSector               TerrainChunkCoordDef
	EndSector                 TerrainChunkCoordDef
	SectorPath                NavSectorPathResult
	LocalPath                 NavPathResult
	LocalPathCorridorFallback bool
	Refined                   bool
	RefinementStatus          string
	RefinementReason          string
	RefinementTile            TerrainChunkCoordDef
}

func FindHierarchicalNavRoute(baseNav *NavManifestDef, baseNavPath string, delta *WorldDeltaDef, deltaPath string, start Vec3, end Vec3, opts NavHierarchicalRouteOptions) (NavHierarchicalRouteResult, error) {
	if baseNav == nil {
		return NavHierarchicalRouteResult{}, fmt.Errorf("base nav manifest is nil")
	}
	EnsureNavManifestDefaults(baseNav)
	startSector, ok := FindNavSectorContainingPoint(baseNav, start)
	if !ok {
		return NavHierarchicalRouteResult{
			RefinementStatus: NavRouteRefinementStatusNotAttempted,
			RefinementReason: NavRouteRefinementReasonStartSectorMissing,
		}, nil
	}
	endSector, ok := FindNavSectorContainingPoint(baseNav, end)
	if !ok {
		return NavHierarchicalRouteResult{
			StartSector:      startSector,
			RefinementStatus: NavRouteRefinementStatusNotAttempted,
			RefinementReason: NavRouteRefinementReasonEndSectorMissing,
		}, nil
	}
	result := NavHierarchicalRouteResult{
		StartSector:      startSector,
		EndSector:        endSector,
		RefinementStatus: NavRouteRefinementStatusNotAttempted,
	}
	graph, err := BuildNavSectorGraph(baseNav, opts.SectorGraph)
	if err != nil {
		return NavHierarchicalRouteResult{}, err
	}
	sectorPath := FindNavSectorPathInGraph(graph, startSector, endSector, opts.SectorPath)
	if !sectorPath.Found {
		result.RefinementReason = NavRouteRefinementReasonCoarseRouteMissing
		return result, nil
	}
	result.Found = true
	result.SectorPath = sectorPath
	if opts.DisableLocalRefinement {
		result.RefinementStatus = NavRouteRefinementStatusDisabled
		result.RefinementReason = NavRouteRefinementReasonDisabled
		return result, nil
	}
	localOpts := opts.LocalPath
	localOpts.AllowedTileCoords = navRouteAllowedTileCoordsForSectorPath(baseNav, sectorPath, localOpts.AllowedTileCoords)
	localPath, err := FindEffectiveNavPath(baseNav, baseNavPath, delta, deltaPath, start, end, localOpts)
	if err != nil {
		return NavHierarchicalRouteResult{}, err
	}
	if !localPath.Found && opts.AllowLocalCorridorFallback && localPath.FailureReason == NavPathFailureDisallowedTile {
		fallbackPath, err := FindEffectiveNavPath(baseNav, baseNavPath, delta, deltaPath, start, end, opts.LocalPath)
		if err != nil {
			return NavHierarchicalRouteResult{}, err
		}
		if fallbackPath.Found {
			localPath = fallbackPath
			result.LocalPathCorridorFallback = true
		}
	}
	result.LocalPath = localPath
	result.Refined = localPath.Found
	if localPath.Found {
		result.RefinementStatus = NavRouteRefinementStatusRefined
	} else {
		result.RefinementStatus = NavRouteRefinementStatusCoarseOnly
		result.RefinementReason = localPath.FailureReason
		result.RefinementTile = localPath.FailureCoord
	}
	return result, nil
}

func navRouteAllowedTileCoordsForSectorPath(manifest *NavManifestDef, path NavSectorPathResult, existing map[TerrainChunkCoordDef]struct{}) map[TerrainChunkCoordDef]struct{} {
	if manifest == nil || !path.Found || len(path.SectorCoords) == 0 {
		return copyNavTileCoordSet(existing)
	}
	worldSize := float32(manifest.ChunkSize) * manifest.VoxelResolution
	if worldSize <= 0 {
		return copyNavTileCoordSet(existing)
	}
	sectors := make(map[TerrainChunkCoordDef]NavSectorEntryDef, len(manifest.Sectors))
	for _, sector := range manifest.Sectors {
		sectors[sector.Coord] = sector
	}
	corridor := map[TerrainChunkCoordDef]struct{}{}
	for _, coord := range path.SectorCoords {
		sector, ok := sectors[coord]
		if !ok {
			continue
		}
		minCoord, maxCoord := navTileCoordRangeForBounds(sector.BoundsMin, sector.BoundsMax, worldSize)
		for x := minCoord.X; x <= maxCoord.X; x++ {
			for y := minCoord.Y; y <= maxCoord.Y; y++ {
				for z := minCoord.Z; z <= maxCoord.Z; z++ {
					corridor[TerrainChunkCoordDef{X: x, Y: y, Z: z}] = struct{}{}
				}
			}
		}
	}
	if len(existing) == 0 {
		return corridor
	}
	out := map[TerrainChunkCoordDef]struct{}{}
	for coord := range corridor {
		if _, ok := existing[coord]; ok {
			out[coord] = struct{}{}
		}
	}
	return out
}

func navTileCoordRangeForBounds(min [3]float32, max [3]float32, worldSize float32) (TerrainChunkCoordDef, TerrainChunkCoordDef) {
	epsilon := worldSize * 1e-4
	if epsilon <= 0 {
		epsilon = 1e-4
	}
	return TerrainChunkCoordDef{
			X: int(math.Floor(float64(min[0] / worldSize))),
			Y: int(math.Floor(float64(min[1] / worldSize))),
			Z: int(math.Floor(float64(min[2] / worldSize))),
		}, TerrainChunkCoordDef{
			X: int(math.Floor(float64((max[0] - epsilon) / worldSize))),
			Y: int(math.Floor(float64((max[1] - epsilon) / worldSize))),
			Z: int(math.Floor(float64((max[2] - epsilon) / worldSize))),
		}
}

func copyNavTileCoordSet(in map[TerrainChunkCoordDef]struct{}) map[TerrainChunkCoordDef]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[TerrainChunkCoordDef]struct{}, len(in))
	for coord := range in {
		out[coord] = struct{}{}
	}
	return out
}
