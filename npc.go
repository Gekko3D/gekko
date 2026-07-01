package gekko

import (
	"reflect"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

const (
	NPCAnimationStateIdle   = "idle"
	NPCAnimationStateWalk   = "walk"
	NPCAnimationStateRun    = "run"
	NPCAnimationStateAttack = "attack"
	NPCAnimationStatePain   = "pain"
	NPCAnimationStateDeath  = "death"
)

type NPCComponent struct {
	Kind       string
	AssetPath  string
	ClassName  string
	ModelRef   string
	Health     float32
	MaxHealth  float32
	TargetName string
	Target     string
	SquadName  string
	SpawnFlags int
	SourceTag  string
	Tags       []string
}

type NPCAnimationComponent struct {
	State          string
	FallbackClipID string
	ActiveClipID   string
}

const (
	NPCNavigationStatusIdle                  = "idle"
	NPCNavigationStatusPlanning              = "planning"
	NPCNavigationStatusRouteReady            = "route_ready"
	NPCNavigationStatusCoarseRoute           = "coarse_route"
	NPCNavigationStatusNoRoute               = "no_route"
	NPCNavigationStatusError                 = "error"
	NPCNavigationStatusNavigationUnavailable = "navigation_unavailable"
)

const (
	NPCNavigationMovementStatusIdle    = "idle"
	NPCNavigationMovementStatusMoving  = "moving"
	NPCNavigationMovementStatusArrived = "arrived"
	NPCNavigationMovementStatusBlocked = "blocked"
)

type NPCNavigationComponent struct {
	Enabled                bool
	HasTarget              bool
	Target                 content.Vec3
	AgentProfileID         string
	AgentTags              []string
	AgentSpeed             float32
	MaxTileSearchRadius    int
	EndpointSnapDistance   float32
	DisableLocalRefinement bool
	RequestRevision        int64
	PendingRevision        int64
	PendingNavRevision     int64
	PendingRouteJobID      int64
	PlannedRevision        int64
	PlannedNavRevision     int64
	Status                 string
	LastError              string
	Route                  content.NavHierarchicalRouteResult
}

type NPCNavigationMovementComponent struct {
	Enabled             bool
	Speed               float32
	AcceptanceRadius    float32
	WaypointIndex       int
	FollowedRevision    int64
	FollowedNavRevision int64
	Status              string
}

func (nav *NPCNavigationComponent) SetTarget(target content.Vec3) {
	if nav == nil {
		return
	}
	nav.Enabled = true
	nav.HasTarget = true
	nav.Target = target
	nav.RequestRevision++
	nav.PendingRevision = 0
	nav.PendingNavRevision = 0
	nav.PendingRouteJobID = 0
	nav.Status = NPCNavigationStatusIdle
}

func UpdateNPCNavigationRoutes(cmd *Commands, service RuntimeNavigationService) int {
	if cmd == nil {
		return 0
	}
	updated := 0
	MakeQuery3[NPCComponent, TransformComponent, NPCNavigationComponent](cmd).Map(func(_ EntityId, _ *NPCComponent, tr *TransformComponent, nav *NPCNavigationComponent) bool {
		if nav == nil || !nav.Enabled || !nav.HasTarget {
			return true
		}
		if nav.PlannedRevision == nav.RequestRevision && nav.PlannedNavRevision == service.NavigationRevision && nav.Status != "" {
			return true
		}
		route, err := service.FindRoute(RuntimeNavigationRouteRequest{
			Start:   npcNavigationContentVec3FromTransform(tr),
			End:     nav.Target,
			Options: npcNavigationRouteOptions(nav),
		})
		if err != nil {
			nav.Status = NPCNavigationStatusError
			nav.LastError = err.Error()
			nav.PlannedRevision = nav.RequestRevision
			nav.PlannedNavRevision = service.NavigationRevision
			updated++
			return true
		}
		nav.Route = route
		nav.LastError = ""
		nav.Status = npcNavigationStatusForRoute(route)
		nav.PlannedRevision = nav.RequestRevision
		nav.PlannedNavRevision = service.NavigationRevision
		updated++
		return true
	})
	return updated
}

func UpdateNPCNavigationMovement(cmd *Commands, time *Time) int {
	if cmd == nil || time == nil || time.Dt <= 0 {
		return 0
	}
	updated := 0
	MakeQuery4[NPCComponent, TransformComponent, NPCNavigationComponent, NPCNavigationMovementComponent](cmd).Map(func(eid EntityId, _ *NPCComponent, tr *TransformComponent, nav *NPCNavigationComponent, move *NPCNavigationMovementComponent) bool {
		if tr == nil || nav == nil || move == nil || !move.Enabled {
			return true
		}
		if nav.Status != NPCNavigationStatusRouteReady || !nav.Route.Refined || len(nav.Route.LocalPath.Waypoints) == 0 {
			move.Status = NPCNavigationMovementStatusIdle
			return true
		}
		if move.FollowedRevision != nav.PlannedRevision || move.FollowedNavRevision != nav.PlannedNavRevision {
			move.WaypointIndex = 0
			move.FollowedRevision = nav.PlannedRevision
			move.FollowedNavRevision = nav.PlannedNavRevision
		}
		status, moved := updateNPCNavigationMovement(eid, cmd, tr, move, nav.Route.LocalPath.Waypoints, float32(time.Dt))
		move.Status = status
		if moved {
			updated++
		}
		return true
	})
	return updated
}

func npcNavigationRouteOptions(nav *NPCNavigationComponent) content.NavHierarchicalRouteOptions {
	if nav == nil {
		return content.NavHierarchicalRouteOptions{}
	}
	return content.NavHierarchicalRouteOptions{
		SectorPath: content.NavSectorPathOptions{
			AgentTags:  append([]string(nil), nav.AgentTags...),
			AgentSpeed: nav.AgentSpeed,
		},
		LocalPath: content.NavPathOptions{
			AgentProfileID:       nav.AgentProfileID,
			MaxTileSearchRadius:  nav.MaxTileSearchRadius,
			EndpointSnapDistance: nav.EndpointSnapDistance,
		},
		DisableLocalRefinement:     nav.DisableLocalRefinement,
		AllowLocalCorridorFallback: !nav.DisableLocalRefinement,
	}
}

func npcNavigationStatusForRoute(route content.NavHierarchicalRouteResult) string {
	if route.Refined {
		return NPCNavigationStatusRouteReady
	}
	if route.Found {
		return NPCNavigationStatusCoarseRoute
	}
	if route.RefinementReason == content.NavRouteRefinementReasonNavigationUnavailable {
		return NPCNavigationStatusNavigationUnavailable
	}
	return NPCNavigationStatusNoRoute
}

func npcNavigationContentVec3FromTransform(tr *TransformComponent) content.Vec3 {
	if tr == nil {
		return content.Vec3{}
	}
	return content.Vec3{tr.Position.X(), tr.Position.Y(), tr.Position.Z()}
}

func updateNPCNavigationMovement(eid EntityId, cmd *Commands, tr *TransformComponent, move *NPCNavigationMovementComponent, waypoints []content.Vec3, dt float32) (string, bool) {
	if tr == nil || move == nil || len(waypoints) == 0 {
		return NPCNavigationMovementStatusIdle, false
	}
	if move.WaypointIndex < 0 {
		move.WaypointIndex = 0
	}
	position := tr.Position
	acceptance := move.AcceptanceRadius
	if acceptance <= 0 {
		acceptance = 0.1
	}
	speed := move.Speed
	if speed <= 0 {
		speed = 1
	}
	for move.WaypointIndex < len(waypoints) {
		target := npcNavigationMGLVec3(waypoints[move.WaypointIndex])
		if position.Sub(target).Len() > acceptance {
			break
		}
		move.WaypointIndex++
	}
	if move.WaypointIndex >= len(waypoints) {
		return NPCNavigationMovementStatusArrived, false
	}
	target := npcNavigationMGLVec3(waypoints[move.WaypointIndex])
	delta := target.Sub(position)
	distance := delta.Len()
	if distance <= acceptance {
		move.WaypointIndex++
		if move.WaypointIndex >= len(waypoints) {
			return NPCNavigationMovementStatusArrived, false
		}
		return NPCNavigationMovementStatusMoving, false
	}
	maxStep := speed * dt
	if maxStep <= 0 {
		return NPCNavigationMovementStatusBlocked, false
	}
	step := delta
	if distance > maxStep {
		step = delta.Normalize().Mul(maxStep)
	}
	tr.Position = tr.Position.Add(step)
	if local, ok := npcNavigationLocalTransform(cmd, eid); ok {
		local.Position = local.Position.Add(step)
	}
	return NPCNavigationMovementStatusMoving, true
}

func npcNavigationMGLVec3(value content.Vec3) mgl32.Vec3 {
	return mgl32.Vec3{value[0], value[1], value[2]}
}

func npcNavigationLocalTransform(cmd *Commands, eid EntityId) (*LocalTransformComponent, bool) {
	if cmd == nil {
		return nil, false
	}
	component := cmd.GetComponent(eid, reflect.TypeOf(LocalTransformComponent{}))
	if local, ok := component.(*LocalTransformComponent); ok {
		return local, true
	}
	return nil, false
}
