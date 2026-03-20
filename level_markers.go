package gekko

import "github.com/gekko3d/gekko/content"

func FindFirstLevelMarkerByKind(level *content.LevelDef, kind string) (content.LevelMarkerDef, bool) {
	if level == nil {
		return content.LevelMarkerDef{}, false
	}
	for _, marker := range level.Markers {
		if marker.Kind == kind {
			return marker, true
		}
	}
	return content.LevelMarkerDef{}, false
}

func FindLevelMarkersByKind(level *content.LevelDef, kind string) []content.LevelMarkerDef {
	if level == nil {
		return nil
	}
	markers := make([]content.LevelMarkerDef, 0, 4)
	for _, marker := range level.Markers {
		if marker.Kind == kind {
			markers = append(markers, marker)
		}
	}
	return markers
}
