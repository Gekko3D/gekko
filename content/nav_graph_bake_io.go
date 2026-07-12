package content

import (
	"fmt"
	"sort"
)

type NavGraphBakeSummary struct {
	Validation            NavGraphValidationResult      `json:"validation"`
	SourceTiles           int                           `json:"source_tiles"`
	GraphTiles            int                           `json:"graph_tiles"`
	Spans                 int                           `json:"spans"`
	AcceptedSpans         int                           `json:"accepted_spans"`
	SpanTransitions       int                           `json:"span_transitions"`
	Regions               int                           `json:"regions"`
	RegionTransitions     int                           `json:"region_transitions"`
	BuildDiagnosticCounts []NavGraphBakeDiagnosticCount `json:"build_diagnostic_counts,omitempty"`
}

// SaveNavGraphBake validates the complete bundle before creating any output.
// Tile files are written first and the manifest is published last.
func SaveNavGraphBake(manifestPath string, bake *NavGraphBakeResult) error {
	if validation := ValidateNavGraphBake(bake); validation.HasErrors() {
		return fmt.Errorf("invalid navigation graph bake: %s", validation.Error())
	}
	sources := make(map[TerrainChunkCoordDef]*NavSourceTileDef, len(bake.SourceTiles))
	for i := range bake.SourceTiles {
		sources[bake.SourceTiles[i].Coord] = &bake.SourceTiles[i]
	}
	type graphKey struct {
		Coord   TerrainChunkCoordDef
		Profile string
	}
	graphs := make(map[graphKey]*NavGraphTileDef, len(bake.GraphTiles))
	for i := range bake.GraphTiles {
		graph := &bake.GraphTiles[i]
		graphs[graphKey{Coord: graph.Coord, Profile: graph.AgentProfileID}] = graph
	}
	for _, entry := range bake.Manifest.SourceTiles {
		if err := SaveNavSourceTile(ResolveDocumentPath(entry.TilePath, manifestPath), sources[entry.Coord]); err != nil {
			return err
		}
	}
	for _, entry := range bake.Manifest.GraphTiles {
		if err := SaveNavGraphTile(ResolveDocumentPath(entry.TilePath, manifestPath), graphs[graphKey{Coord: entry.Coord, Profile: entry.AgentProfileID}]); err != nil {
			return err
		}
	}
	return SaveNavGraphManifest(manifestPath, &bake.Manifest)
}

func LoadNavGraphBake(manifestPath string) (*NavGraphBakeResult, error) {
	manifest, err := LoadNavGraphManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	result := &NavGraphBakeResult{Manifest: *manifest}
	for _, entry := range manifest.SourceTiles {
		tile, err := LoadNavSourceTile(ResolveDocumentPath(entry.TilePath, manifestPath))
		if err != nil {
			return nil, fmt.Errorf("load navigation source tile %s: %w", TerrainChunkKey(entry.Coord), err)
		}
		result.SourceTiles = append(result.SourceTiles, *tile)
	}
	for _, entry := range manifest.GraphTiles {
		tile, err := LoadNavGraphTile(ResolveDocumentPath(entry.TilePath, manifestPath))
		if err != nil {
			return nil, fmt.Errorf("load navigation graph tile %s for %q: %w", TerrainChunkKey(entry.Coord), entry.AgentProfileID, err)
		}
		result.GraphTiles = append(result.GraphTiles, *tile)
	}
	if validation := ValidateNavGraphBake(result); validation.HasErrors() {
		return nil, fmt.Errorf("invalid navigation graph bake: %s", validation.Error())
	}
	return result, nil
}

func ValidateNavGraphBake(bake *NavGraphBakeResult) NavGraphValidationResult {
	result := NavGraphValidationResult{}
	if bake == nil {
		result.addError("nil_bake", "navigation graph bake is nil")
		return result
	}
	appendNavGraphValidation(&result, ValidateNavGraphManifest(&bake.Manifest))

	sources := make(map[TerrainChunkCoordDef]NavSourceTileDef, len(bake.SourceTiles))
	for _, source := range bake.SourceTiles {
		if _, exists := sources[source.Coord]; exists {
			result.addError("duplicate_source_tile", fmt.Sprintf("duplicate navigation source tile %s", TerrainChunkKey(source.Coord)))
		}
		sources[source.Coord] = source
		appendNavGraphValidation(&result, ValidateNavSourceTile(&source))
	}
	if len(sources) != len(bake.Manifest.SourceTiles) {
		result.addError("source_tile_count_mismatch", "navigation source tile count does not match manifest")
	}
	for _, entry := range bake.Manifest.SourceTiles {
		source, exists := sources[entry.Coord]
		if !exists {
			result.addError("missing_source_tile", fmt.Sprintf("navigation manifest source tile %s was not provided", TerrainChunkKey(entry.Coord)))
			continue
		}
		if source.NavID != bake.Manifest.NavID || source.BuilderVersion != bake.Manifest.BuilderVersion || source.SourceHash != entry.SourceHash || source.DependencyHash != entry.DependencyHash {
			result.addError("source_tile_metadata_mismatch", fmt.Sprintf("navigation source tile %s does not match manifest", TerrainChunkKey(entry.Coord)))
		}
	}

	type graphKey struct {
		Coord   TerrainChunkCoordDef
		Profile string
	}
	graphs := make(map[graphKey]NavGraphTileDef, len(bake.GraphTiles))
	for _, graph := range bake.GraphTiles {
		key := graphKey{Coord: graph.Coord, Profile: graph.AgentProfileID}
		if _, exists := graphs[key]; exists {
			result.addError("duplicate_graph_tile", fmt.Sprintf("duplicate navigation graph tile %s for %q", TerrainChunkKey(graph.Coord), graph.AgentProfileID))
		}
		graphs[key] = graph
		appendNavGraphValidation(&result, ValidateNavGraphTile(&graph))
	}
	if len(graphs) != len(bake.Manifest.GraphTiles) {
		result.addError("graph_tile_count_mismatch", "navigation graph tile count does not match manifest")
	}
	for _, entry := range bake.Manifest.GraphTiles {
		key := graphKey{Coord: entry.Coord, Profile: entry.AgentProfileID}
		graph, exists := graphs[key]
		if !exists {
			result.addError("missing_graph_tile", fmt.Sprintf("navigation manifest graph tile %s for %q was not provided", TerrainChunkKey(entry.Coord), entry.AgentProfileID))
			continue
		}
		source, sourceExists := sources[entry.Coord]
		if !sourceExists || graph.NavID != bake.Manifest.NavID || graph.BuilderVersion != bake.Manifest.BuilderVersion || graph.SourceHash != entry.SourceHash || graph.DependencyHash != entry.DependencyHash || graph.SourceHash != source.SourceHash || graph.DependencyHash != source.DependencyHash {
			result.addError("graph_tile_metadata_mismatch", fmt.Sprintf("navigation graph tile %s for %q does not match manifest/source", TerrainChunkKey(entry.Coord), entry.AgentProfileID))
		}
	}
	for key, graph := range graphs {
		for _, transition := range graph.SpanTransitions {
			target, exists := graphs[graphKey{Coord: transition.To.Tile, Profile: key.Profile}]
			if !exists || !navGraphContainsSpan(target, transition.To.Span) {
				result.addError("missing_remote_span", fmt.Sprintf("navigation span transition from %s/%d references missing %s/%d for %q", TerrainChunkKey(key.Coord), transition.From, TerrainChunkKey(transition.To.Tile), transition.To.Span, key.Profile))
			}
		}
		for _, transition := range graph.Transitions {
			target, exists := graphs[graphKey{Coord: transition.ToTile, Profile: key.Profile}]
			if !exists || int(transition.ToRegion) >= len(target.Regions) {
				result.addError("missing_remote_region", fmt.Sprintf("navigation region transition from %s/%d references missing %s/%d for %q", TerrainChunkKey(key.Coord), transition.FromRegion, TerrainChunkKey(transition.ToTile), transition.ToRegion, key.Profile))
			}
		}
	}
	return result
}

func DiagnoseNavGraphBake(bake *NavGraphBakeResult) NavGraphBakeSummary {
	summary := NavGraphBakeSummary{Validation: ValidateNavGraphBake(bake)}
	if bake == nil {
		return summary
	}
	summary.SourceTiles = len(bake.SourceTiles)
	summary.GraphTiles = len(bake.GraphTiles)
	summary.BuildDiagnosticCounts = append([]NavGraphBakeDiagnosticCount(nil), bake.Diagnostics...)
	for _, source := range bake.SourceTiles {
		summary.Spans += len(source.Spans)
	}
	for _, graph := range bake.GraphTiles {
		summary.AcceptedSpans += len(graph.SpanIDs)
		summary.SpanTransitions += len(graph.SpanTransitions)
		summary.Regions += len(graph.Regions)
		summary.RegionTransitions += len(graph.Transitions)
	}
	return summary
}

func appendNavGraphValidation(dst *NavGraphValidationResult, source NavGraphValidationResult) {
	dst.Issues = append(dst.Issues, source.Issues...)
	dst.HardErrorCount += source.HardErrorCount
}

func navGraphContainsSpan(graph NavGraphTileDef, id uint32) bool {
	index := sort.Search(len(graph.SpanIDs), func(i int) bool { return graph.SpanIDs[i] >= id })
	return index < len(graph.SpanIDs) && graph.SpanIDs[index] == id
}
