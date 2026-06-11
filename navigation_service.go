package gekko

import (
	"github.com/gekko3d/gekko/content"
)

type RuntimeNavigationService struct {
	BaseNavManifestPath string
	BaseNavManifest     *content.NavManifestDef
	WorldDeltaPath      string
	WorldDelta          *content.WorldDeltaDef
	NavigationRevision  int64
}

type RuntimeNavigationRouteRequest struct {
	Start   content.Vec3
	End     content.Vec3
	Options content.NavHierarchicalRouteOptions
}

func NewRuntimeNavigationService(baseNav *content.NavManifestDef, baseNavPath string, delta *content.WorldDeltaDef, deltaPath string) RuntimeNavigationService {
	return NewRuntimeNavigationServiceWithRevision(baseNav, baseNavPath, delta, deltaPath, 0)
}

func NewRuntimeNavigationServiceWithRevision(baseNav *content.NavManifestDef, baseNavPath string, delta *content.WorldDeltaDef, deltaPath string, revision int64) RuntimeNavigationService {
	return RuntimeNavigationService{
		BaseNavManifestPath: baseNavPath,
		BaseNavManifest:     baseNav,
		WorldDeltaPath:      deltaPath,
		WorldDelta:          delta,
		NavigationRevision:  revision,
	}
}

func RuntimeNavigationServiceFromStreamedLevelState(state *StreamedLevelRuntimeState) RuntimeNavigationService {
	if state == nil {
		return RuntimeNavigationService{}
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	return NewRuntimeNavigationServiceWithRevision(state.BaseNavManifest, state.BaseNavManifestPath, state.WorldDelta, state.WorldDeltaPath, state.NavigationRevision)
}

func (s RuntimeNavigationService) Available() bool {
	return s.BaseNavManifest != nil && s.BaseNavManifestPath != ""
}

func (s RuntimeNavigationService) FindRoute(req RuntimeNavigationRouteRequest) (content.NavHierarchicalRouteResult, error) {
	if !s.Available() {
		return content.NavHierarchicalRouteResult{
			RefinementStatus: content.NavRouteRefinementStatusNotAttempted,
			RefinementReason: content.NavRouteRefinementReasonNavigationUnavailable,
		}, nil
	}
	return content.FindHierarchicalNavRoute(s.BaseNavManifest, s.BaseNavManifestPath, s.WorldDelta, s.WorldDeltaPath, req.Start, req.End, req.Options)
}
