package gekko

import (
	"time"

	"github.com/gekko3d/gekko/content"
)

type streamedNavigationPrewarmJob struct {
	ID                  int64
	NavigationRevision  int64
	BaseNavManifestPath string
	BaseNavManifest     *content.NavManifestDef
	WorldDeltaPath      string
	WorldDelta          *content.WorldDeltaDef
	QueryCache          *content.NavRuntimeQueryCache
}

type streamedNavigationPrewarmResult struct {
	ID                 int64
	NavigationRevision int64
	Duration           time.Duration
	Err                error
}

func streamedLevelNavigationPrewarmSystem(state *StreamedLevelRuntimeState) {
	if state == nil {
		return
	}
	if state.NavigationPrewarmResults == nil {
		state.NavigationPrewarmResults = make(chan streamedNavigationPrewarmResult, 4)
	}
	for {
		select {
		case result := <-state.NavigationPrewarmResults:
			state.NavigationPrewarmActive = false
			if result.Err == nil && result.NavigationRevision == state.NavigationRevision {
				state.NavigationPrewarmCompletedRev = result.NavigationRevision
			}
		default:
			if shouldStartStreamedNavigationPrewarm(state) {
				startStreamedNavigationPrewarmJob(state)
			}
			return
		}
	}
}

func shouldStartStreamedNavigationPrewarm(state *StreamedLevelRuntimeState) bool {
	return state != nil &&
		!state.NavigationPrewarmActive &&
		state.BaseNavManifest != nil &&
		state.BaseNavManifestPath != "" &&
		state.NavigationRevision > 0 &&
		state.NavigationPrewarmCompletedRev != state.NavigationRevision
}

func startStreamedNavigationPrewarmJob(state *StreamedLevelRuntimeState) {
	if state == nil || state.BaseNavManifest == nil || state.BaseNavManifestPath == "" {
		return
	}
	if state.NavigationPrewarmResults == nil {
		state.NavigationPrewarmResults = make(chan streamedNavigationPrewarmResult, 4)
	}
	if state.NavigationQueryCache == nil {
		state.NavigationQueryCache = content.NewNavRuntimeQueryCache()
	}
	state.nextNavigationPrewarmJobID++
	var delta *content.WorldDeltaDef
	if state.WorldDelta != nil {
		copyDelta := copyWorldDeltaForNavigationRebuild(state.WorldDelta)
		delta = &copyDelta
	}
	job := streamedNavigationPrewarmJob{
		ID:                  state.nextNavigationPrewarmJobID,
		NavigationRevision:  state.NavigationRevision,
		BaseNavManifestPath: state.BaseNavManifestPath,
		BaseNavManifest:     copyNavManifestForNavigationRebuild(state.BaseNavManifest),
		WorldDeltaPath:      state.WorldDeltaPath,
		WorldDelta:          delta,
		QueryCache:          state.NavigationQueryCache,
	}
	state.NavigationPrewarmActive = true
	go func() {
		state.NavigationPrewarmResults <- runStreamedNavigationPrewarmJob(job)
	}()
}

func runStreamedNavigationPrewarmJob(job streamedNavigationPrewarmJob) streamedNavigationPrewarmResult {
	started := time.Now()
	err := content.PrewarmNavRuntimeQueryCache(job.BaseNavManifest, job.BaseNavManifestPath, job.WorldDelta, job.WorldDeltaPath, job.QueryCache, content.NavRuntimeQueryCachePrewarmOptions{
		NavigationRevision: job.NavigationRevision,
	})
	return streamedNavigationPrewarmResult{
		ID:                 job.ID,
		NavigationRevision: job.NavigationRevision,
		Duration:           time.Since(started),
		Err:                err,
	}
}
