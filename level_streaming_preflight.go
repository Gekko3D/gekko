package gekko

import (
	"github.com/gekko3d/gekko/content"
)

// The current scheduler still combines sources on the legacy level grid. Run
// its existing loader admission gates before queuing entities or creating a
// streaming session; a tooling index does not grant live v3 admission.
func preflightLevelStreamingManifests(loader *RuntimeContentLoader, level *content.LevelDef, levelPath string, includePOI bool) error {
	if level.Terrain != nil && level.Terrain.ManifestPath != "" {
		if _, err := loader.LoadTerrainChunkManifest(content.ResolveDocumentPath(level.Terrain.ManifestPath, levelPath)); err != nil {
			return err
		}
	}
	if includePOI && level.BaseWorld != nil && level.BaseWorld.ManifestPath != "" {
		if _, err := loader.LoadImportedWorld(content.ResolveDocumentPath(level.BaseWorld.ManifestPath, levelPath)); err != nil {
			return err
		}
	}
	return nil
}
