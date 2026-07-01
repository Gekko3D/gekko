package gekko

import (
	"fmt"
	"reflect"
	"time"

	"github.com/gekko3d/gekko/content"
)

type RuntimeNavigationAsyncRouteResult struct {
	JobID              int64
	Owner              EntityId
	RequestRevision    int64
	NavigationRevision int64
	Priority           string
	Route              content.NavHierarchicalRouteResult
	Duration           time.Duration
	Err                error
}

type streamedNavigationRouteJob struct {
	ID                  int64
	Owner               EntityId
	RequestRevision     int64
	NavigationRevision  int64
	Priority            string
	BaseNavManifestPath string
	BaseNavManifest     *content.NavManifestDef
	WorldDeltaPath      string
	WorldDelta          *content.WorldDeltaDef
	Start               content.Vec3
	End                 content.Vec3
	Options             content.NavHierarchicalRouteOptions
	QueryCache          *content.NavRuntimeQueryCache
}

type streamedNavigationRouteResult struct {
	ID                 int64
	Owner              EntityId
	RequestRevision    int64
	NavigationRevision int64
	Priority           string
	Route              content.NavHierarchicalRouteResult
	Duration           time.Duration
	Err                error
}

func EnqueueStreamedNavigationRoute(state *StreamedLevelRuntimeState, owner EntityId, start, end content.Vec3, requestRevision int64, opts content.NavHierarchicalRouteOptions, priority string) (int64, bool) {
	if state == nil || state.BaseNavManifest == nil || state.BaseNavManifestPath == "" {
		return 0, false
	}
	if state.NavigationRouteResults == nil {
		state.NavigationRouteResults = make(chan streamedNavigationRouteResult, 16)
	}
	if state.NavigationRoutePendingByOwner == nil {
		state.NavigationRoutePendingByOwner = make(map[EntityId]int64)
	}
	if state.NavigationQueryCache == nil {
		state.NavigationQueryCache = content.NewNavRuntimeQueryCache()
	}
	opts.LocalPath.QueryCache = nil
	state.nextNavigationRouteJobID++
	jobID := state.nextNavigationRouteJobID
	var delta *content.WorldDeltaDef
	if state.WorldDelta != nil {
		copyDelta := copyWorldDeltaForNavigationRebuild(state.WorldDelta)
		delta = &copyDelta
	}
	job := streamedNavigationRouteJob{
		ID:                  jobID,
		Owner:               owner,
		RequestRevision:     requestRevision,
		NavigationRevision:  state.NavigationRevision,
		Priority:            priority,
		BaseNavManifestPath: state.BaseNavManifestPath,
		BaseNavManifest:     copyNavManifestForNavigationRebuild(state.BaseNavManifest),
		WorldDeltaPath:      state.WorldDeltaPath,
		WorldDelta:          delta,
		Start:               start,
		End:                 end,
		Options:             opts,
		QueryCache:          state.NavigationQueryCache,
	}
	state.NavigationRouteJobs = removeQueuedStreamedNavigationRouteJobsForOwner(state.NavigationRouteJobs, owner)
	state.NavigationRouteJobs = append(state.NavigationRouteJobs, job)
	state.NavigationRoutePendingByOwner[owner] = jobID
	startNextStreamedNavigationRouteJob(state)
	return jobID, true
}

func ApplyStreamedNavigationRouteResultsToNPCs(cmd *Commands, state *StreamedLevelRuntimeState, observe func(RuntimeNavigationAsyncRouteResult)) int {
	if cmd == nil || state == nil {
		return 0
	}
	applied := 0
	for _, result := range PollStreamedNavigationRouteResults(state) {
		component := cmd.GetComponent(result.Owner, reflect.TypeOf(NPCNavigationComponent{}))
		nav, _ := component.(*NPCNavigationComponent)
		if nav == nil || nav.PendingRouteJobID != result.JobID || nav.PendingRevision != result.RequestRevision {
			continue
		}
		nav.PendingRevision = 0
		nav.PendingNavRevision = 0
		nav.PendingRouteJobID = 0
		if observe != nil {
			observe(result)
		}
		if result.NavigationRevision != state.NavigationRevision {
			nav.Status = NPCNavigationStatusIdle
			nav.LastError = "navigation_revision_changed"
			applied++
			continue
		}
		nav.PlannedRevision = result.RequestRevision
		nav.PlannedNavRevision = result.NavigationRevision
		if result.Err != nil {
			nav.Status = NPCNavigationStatusError
			nav.LastError = result.Err.Error()
			applied++
			continue
		}
		nav.Route = result.Route
		nav.LastError = ""
		nav.Status = npcNavigationStatusForRoute(result.Route)
		applied++
	}
	return applied
}

func UpdateNPCNavigationRoutesAsync(cmd *Commands, state *StreamedLevelRuntimeState) int {
	if cmd == nil || state == nil {
		return 0
	}
	service := RuntimeNavigationServiceFromStreamedLevelState(state)
	if !service.Available() {
		return 0
	}
	updated := 0
	MakeQuery3[NPCComponent, TransformComponent, NPCNavigationComponent](cmd).Map(func(eid EntityId, _ *NPCComponent, tr *TransformComponent, nav *NPCNavigationComponent) bool {
		if tr == nil || nav == nil || !nav.Enabled || !nav.HasTarget {
			return true
		}
		if nav.PlannedRevision == nav.RequestRevision && nav.PlannedNavRevision == service.NavigationRevision && nav.Status != "" {
			return true
		}
		if nav.PendingRouteJobID != 0 && nav.PendingRevision == nav.RequestRevision && nav.PendingNavRevision == service.NavigationRevision {
			return true
		}
		jobID, ok := EnqueueStreamedNavigationRoute(state, eid, npcNavigationContentVec3FromTransform(tr), nav.Target, nav.RequestRevision, npcNavigationRouteOptions(nav), "")
		if !ok {
			nav.Status = NPCNavigationStatusNavigationUnavailable
			nav.LastError = ""
			return true
		}
		nav.PendingRevision = nav.RequestRevision
		nav.PendingNavRevision = service.NavigationRevision
		nav.PendingRouteJobID = jobID
		nav.Status = NPCNavigationStatusPlanning
		nav.LastError = ""
		updated++
		return true
	})
	return updated
}

func PollStreamedNavigationRouteResults(state *StreamedLevelRuntimeState) []RuntimeNavigationAsyncRouteResult {
	if state == nil {
		return nil
	}
	if state.NavigationRouteResults == nil {
		state.NavigationRouteResults = make(chan streamedNavigationRouteResult, 16)
	}
	results := make([]RuntimeNavigationAsyncRouteResult, 0)
	for {
		select {
		case result := <-state.NavigationRouteResults:
			state.NavigationRouteActive = false
			if state.NavigationRoutePendingByOwner != nil && state.NavigationRoutePendingByOwner[result.Owner] == result.ID {
				delete(state.NavigationRoutePendingByOwner, result.Owner)
			}
			results = append(results, RuntimeNavigationAsyncRouteResult{
				JobID:              result.ID,
				Owner:              result.Owner,
				RequestRevision:    result.RequestRevision,
				NavigationRevision: result.NavigationRevision,
				Priority:           result.Priority,
				Route:              result.Route,
				Duration:           result.Duration,
				Err:                result.Err,
			})
		default:
			startNextStreamedNavigationRouteJob(state)
			return results
		}
	}
}

func DrainStreamedNavigationRouteJobs(state *StreamedLevelRuntimeState, timeout time.Duration) ([]RuntimeNavigationAsyncRouteResult, error) {
	if state == nil {
		return nil, nil
	}
	started := time.Now()
	results := make([]RuntimeNavigationAsyncRouteResult, 0)
	for {
		results = append(results, PollStreamedNavigationRouteResults(state)...)
		if len(state.NavigationRouteJobs) == 0 && !state.NavigationRouteActive {
			results = append(results, PollStreamedNavigationRouteResults(state)...)
			return results, nil
		}
		if timeout > 0 && time.Since(started) >= timeout {
			return results, fmt.Errorf("timed out draining streamed navigation route jobs: queued=%d active=%t", len(state.NavigationRouteJobs), state.NavigationRouteActive)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func PendingStreamedNavigationRouteJobCount(state *StreamedLevelRuntimeState) int {
	if state == nil {
		return 0
	}
	count := len(state.NavigationRouteJobs)
	if state.NavigationRouteActive {
		count++
	}
	return count
}

func cancelQueuedStreamedNavigationRouteJobs(state *StreamedLevelRuntimeState) {
	if state == nil {
		return
	}
	state.NavigationRouteJobs = nil
	state.NavigationRoutePendingByOwner = make(map[EntityId]int64)
}

func removeQueuedStreamedNavigationRouteJobsForOwner(jobs []streamedNavigationRouteJob, owner EntityId) []streamedNavigationRouteJob {
	if len(jobs) == 0 {
		return jobs
	}
	write := 0
	for _, job := range jobs {
		if job.Owner == owner {
			continue
		}
		jobs[write] = job
		write++
	}
	clear(jobs[write:])
	return jobs[:write]
}

func startNextStreamedNavigationRouteJob(state *StreamedLevelRuntimeState) {
	if state == nil || state.NavigationRouteActive || len(state.NavigationRouteJobs) == 0 {
		return
	}
	if state.NavigationRouteResults == nil {
		state.NavigationRouteResults = make(chan streamedNavigationRouteResult, 16)
	}
	job := state.NavigationRouteJobs[0]
	copy(state.NavigationRouteJobs, state.NavigationRouteJobs[1:])
	state.NavigationRouteJobs = state.NavigationRouteJobs[:len(state.NavigationRouteJobs)-1]
	state.NavigationRouteActive = true
	go func() {
		state.NavigationRouteResults <- runStreamedNavigationRouteJob(job)
	}()
}

func runStreamedNavigationRouteJob(job streamedNavigationRouteJob) streamedNavigationRouteResult {
	started := time.Now()
	service := NewRuntimeNavigationServiceWithRevision(job.BaseNavManifest, job.BaseNavManifestPath, job.WorldDelta, job.WorldDeltaPath, job.NavigationRevision)
	service.QueryCache = job.QueryCache
	route, err := service.FindRoute(RuntimeNavigationRouteRequest{
		Start:   job.Start,
		End:     job.End,
		Options: job.Options,
	})
	return streamedNavigationRouteResult{
		ID:                 job.ID,
		Owner:              job.Owner,
		RequestRevision:    job.RequestRevision,
		NavigationRevision: job.NavigationRevision,
		Priority:           job.Priority,
		Route:              route,
		Duration:           time.Since(started),
		Err:                err,
	}
}
