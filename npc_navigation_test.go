package gekko

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func TestNPCNavigationComponentSetTargetMarksRequestDirty(t *testing.T) {
	nav := &NPCNavigationComponent{}

	nav.SetTarget(content.Vec3{1, 2, 3})

	if !nav.Enabled || !nav.HasTarget || nav.Target != (content.Vec3{1, 2, 3}) || nav.RequestRevision != 1 || nav.Status != NPCNavigationStatusIdle {
		t.Fatalf("unexpected navigation target state: %+v", nav)
	}
}

func TestNPCNavigationRouteOptionsAllowsCorridorFallback(t *testing.T) {
	opts := npcNavigationRouteOptions(&NPCNavigationComponent{
		Enabled:             true,
		AgentProfileID:      "tiny",
		MaxTileSearchRadius: 3,
	})

	if !opts.AllowLocalCorridorFallback {
		t.Fatalf("expected NPC route options to allow corridor fallback, got %+v", opts)
	}
	if opts.LocalPath.AgentProfileID != "tiny" || opts.LocalPath.MaxTileSearchRadius != 3 {
		t.Fatalf("unexpected local path options: %+v", opts.LocalPath)
	}
}

func TestUpdateNPCNavigationRoutesStoresRefinedRoute(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "npc.gknav")
	leftCoord := content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := content.TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	leftPath := filepath.Join(root, "worlds", "npc_navtiles", "tiny_0_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "npc_navtiles", "tiny_1_0_0.gknavtile")
	if err := content.SaveNavTile(leftPath, runtimeNavServiceTestTile("nav-npc", "tiny", leftCoord, "left", content.Vec3{7, 1, 0}, content.Vec3{8, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := content.SaveNavTile(rightPath, runtimeNavServiceTestTile("nav-npc", "tiny", rightCoord, "right", content.Vec3{8, 1, 0}, content.Vec3{9, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	manifest := runtimeNavServiceTestManifest("nav-npc", "tiny", navPath, map[content.TerrainChunkCoordDef]string{
		leftCoord:  leftPath,
		rightCoord: rightPath,
	})
	service := NewRuntimeNavigationService(manifest, navPath, nil, "")
	app := NewApp()
	cmd := app.Commands()
	npc := cmd.AddEntity(
		&NPCComponent{ClassName: "monster_test"},
		&TransformComponent{Position: mgl32.Vec3{7.5, 1, 0.5}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&NPCNavigationComponent{
			Enabled:             true,
			HasTarget:           true,
			Target:              content.Vec3{8.5, 1, 0.5},
			AgentProfileID:      "tiny",
			MaxTileSearchRadius: 1,
			RequestRevision:     1,
		},
	)
	app.FlushCommands()

	updated := UpdateNPCNavigationRoutes(cmd, service)

	if updated != 1 {
		t.Fatalf("expected one updated NPC route, got %d", updated)
	}
	nav := cmd.GetComponent(npc, reflect.TypeOf(NPCNavigationComponent{})).(*NPCNavigationComponent)
	if nav.Status != NPCNavigationStatusRouteReady || nav.PlannedRevision != 1 || !nav.Route.Found || !nav.Route.Refined {
		t.Fatalf("expected refined NPC route, got %+v", nav)
	}
	if len(nav.Route.LocalPath.Steps) != 2 {
		t.Fatalf("expected local route steps, got %+v", nav.Route)
	}
}

func TestUpdateNPCNavigationRoutesReplansWhenNavigationRevisionChanges(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "npc_revision.gknav")
	leftCoord := content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := content.TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	leftPath := filepath.Join(root, "worlds", "npc_revision_navtiles", "tiny_0_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "npc_revision_navtiles", "tiny_1_0_0.gknavtile")
	if err := content.SaveNavTile(leftPath, runtimeNavServiceTestTile("nav-npc-revision", "tiny", leftCoord, "left", content.Vec3{7, 1, 0}, content.Vec3{8, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := content.SaveNavTile(rightPath, runtimeNavServiceTestTile("nav-npc-revision", "tiny", rightCoord, "right", content.Vec3{8, 1, 0}, content.Vec3{9, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	manifest := runtimeNavServiceTestManifest("nav-npc-revision", "tiny", navPath, map[content.TerrainChunkCoordDef]string{
		leftCoord:  leftPath,
		rightCoord: rightPath,
	})
	app := NewApp()
	cmd := app.Commands()
	npc := cmd.AddEntity(
		&NPCComponent{ClassName: "monster_test"},
		&TransformComponent{Position: mgl32.Vec3{7.5, 1, 0.5}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&NPCNavigationComponent{
			Enabled:             true,
			HasTarget:           true,
			Target:              content.Vec3{8.5, 1, 0.5},
			AgentProfileID:      "tiny",
			MaxTileSearchRadius: 1,
			RequestRevision:     1,
		},
	)
	app.FlushCommands()

	first := UpdateNPCNavigationRoutes(cmd, NewRuntimeNavigationServiceWithRevision(manifest, navPath, nil, "", 1))
	second := UpdateNPCNavigationRoutes(cmd, NewRuntimeNavigationServiceWithRevision(manifest, navPath, nil, "", 1))
	third := UpdateNPCNavigationRoutes(cmd, NewRuntimeNavigationServiceWithRevision(manifest, navPath, nil, "", 2))

	if first != 1 || second != 0 || third != 1 {
		t.Fatalf("expected update counts 1,0,1 across nav revision change, got %d,%d,%d", first, second, third)
	}
	nav := cmd.GetComponent(npc, reflect.TypeOf(NPCNavigationComponent{})).(*NPCNavigationComponent)
	if nav.PlannedRevision != 1 || nav.PlannedNavRevision != 2 || nav.Status != NPCNavigationStatusRouteReady {
		t.Fatalf("expected NPC route to track latest nav revision, got %+v", nav)
	}
}

func TestUpdateNPCNavigationRoutesReportsUnavailableNavigation(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	npc := cmd.AddEntity(
		&NPCComponent{ClassName: "monster_test"},
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&NPCNavigationComponent{
			Enabled:         true,
			HasTarget:       true,
			Target:          content.Vec3{8, 0, 0},
			RequestRevision: 1,
		},
	)
	app.FlushCommands()

	updated := UpdateNPCNavigationRoutes(cmd, RuntimeNavigationService{})

	if updated != 1 {
		t.Fatalf("expected one updated NPC route, got %d", updated)
	}
	nav := cmd.GetComponent(npc, reflect.TypeOf(NPCNavigationComponent{})).(*NPCNavigationComponent)
	if nav.Status != NPCNavigationStatusNavigationUnavailable || nav.Route.RefinementReason != content.NavRouteRefinementReasonNavigationUnavailable {
		t.Fatalf("expected unavailable navigation status, got %+v", nav)
	}
}

func TestStreamedLevelNPCNavigationSystemUsesRuntimeStateService(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "npc_runtime.gknav")
	leftCoord := content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := content.TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	leftPath := filepath.Join(root, "worlds", "npc_runtime_navtiles", "tiny_0_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "npc_runtime_navtiles", "tiny_1_0_0.gknavtile")
	if err := content.SaveNavTile(leftPath, runtimeNavServiceTestTile("nav-runtime-npc", "tiny", leftCoord, "left", content.Vec3{7, 1, 0}, content.Vec3{8, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := content.SaveNavTile(rightPath, runtimeNavServiceTestTile("nav-runtime-npc", "tiny", rightCoord, "right", content.Vec3{8, 1, 0}, content.Vec3{9, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	manifest := runtimeNavServiceTestManifest("nav-runtime-npc", "tiny", navPath, map[content.TerrainChunkCoordDef]string{
		leftCoord:  leftPath,
		rightCoord: rightPath,
	})
	state := &StreamedLevelRuntimeState{
		BaseNavManifestPath: navPath,
		BaseNavManifest:     manifest,
	}
	app := NewApp()
	cmd := app.Commands()
	npc := cmd.AddEntity(
		&NPCComponent{ClassName: "monster_test"},
		&TransformComponent{Position: mgl32.Vec3{7.5, 1, 0.5}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&NPCNavigationComponent{
			Enabled:             true,
			HasTarget:           true,
			Target:              content.Vec3{8.5, 1, 0.5},
			AgentProfileID:      "tiny",
			MaxTileSearchRadius: 1,
			RequestRevision:     1,
		},
	)
	app.FlushCommands()

	streamedLevelNPCNavigationSystem(cmd, state)

	nav := cmd.GetComponent(npc, reflect.TypeOf(NPCNavigationComponent{})).(*NPCNavigationComponent)
	if nav.Status != NPCNavigationStatusRouteReady || !nav.Route.Refined || nav.Route.RefinementStatus != content.NavRouteRefinementStatusRefined {
		t.Fatalf("expected streamed runtime system to store refined NPC route, got %+v", nav)
	}
}

func TestStreamedLevelNPCNavigationMovementSystemSkipsWithoutTime(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	npc := cmd.AddEntity(
		&NPCComponent{ClassName: "monster_test"},
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&NPCNavigationComponent{
			Status: NPCNavigationStatusRouteReady,
			Route: content.NavHierarchicalRouteResult{
				Found:   true,
				Refined: true,
				LocalPath: content.NavPathResult{
					Found:     true,
					Waypoints: []content.Vec3{{1, 0, 0}},
				},
			},
		},
		&NPCNavigationMovementComponent{Enabled: true, Speed: 1},
	)
	app.FlushCommands()

	streamedLevelNPCNavigationMovementSystem(cmd)

	tr := cmd.GetComponent(npc, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	if tr.Position != (mgl32.Vec3{0, 0, 0}) {
		t.Fatalf("expected movement system without Time to skip, got position %v", tr.Position)
	}
}

func TestUpdateNPCNavigationMovementFollowsRefinedWaypoints(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	npc := cmd.AddEntity(
		&NPCComponent{ClassName: "monster_test"},
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&LocalTransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&NPCNavigationComponent{
			Status:             NPCNavigationStatusRouteReady,
			PlannedRevision:    7,
			PlannedNavRevision: 3,
			Route: content.NavHierarchicalRouteResult{
				Found:   true,
				Refined: true,
				LocalPath: content.NavPathResult{
					Found:     true,
					Waypoints: []content.Vec3{{0, 0, 0}, {2, 0, 0}},
				},
			},
		},
		&NPCNavigationMovementComponent{
			Enabled:          true,
			Speed:            1,
			AcceptanceRadius: 0.05,
		},
	)
	app.FlushCommands()

	updated := UpdateNPCNavigationMovement(cmd, &Time{Dt: 1})

	if updated != 1 {
		t.Fatalf("expected one moved NPC, got %d", updated)
	}
	tr := cmd.GetComponent(npc, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	local := cmd.GetComponent(npc, reflect.TypeOf(LocalTransformComponent{})).(*LocalTransformComponent)
	move := cmd.GetComponent(npc, reflect.TypeOf(NPCNavigationMovementComponent{})).(*NPCNavigationMovementComponent)
	if !tr.Position.ApproxEqualThreshold(mgl32.Vec3{1, 0, 0}, 1e-5) || !local.Position.ApproxEqualThreshold(mgl32.Vec3{1, 0, 0}, 1e-5) {
		t.Fatalf("expected world/local transform to move to x=1, got world=%v local=%v", tr.Position, local.Position)
	}
	if move.Status != NPCNavigationMovementStatusMoving || move.WaypointIndex != 1 || move.FollowedRevision != 7 || move.FollowedNavRevision != 3 {
		t.Fatalf("unexpected movement state: %+v", move)
	}
}

func TestUpdateNPCNavigationMovementArrivesWithoutOvershoot(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	npc := cmd.AddEntity(
		&NPCComponent{ClassName: "monster_test"},
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&NPCNavigationComponent{
			Status: NPCNavigationStatusRouteReady,
			Route: content.NavHierarchicalRouteResult{
				Found:   true,
				Refined: true,
				LocalPath: content.NavPathResult{
					Found:     true,
					Waypoints: []content.Vec3{{0.25, 0, 0}},
				},
			},
		},
		&NPCNavigationMovementComponent{
			Enabled:          true,
			Speed:            10,
			AcceptanceRadius: 0.01,
		},
	)
	app.FlushCommands()

	updated := UpdateNPCNavigationMovement(cmd, &Time{Dt: 1})

	if updated != 1 {
		t.Fatalf("expected one moved NPC, got %d", updated)
	}
	tr := cmd.GetComponent(npc, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	move := cmd.GetComponent(npc, reflect.TypeOf(NPCNavigationMovementComponent{})).(*NPCNavigationMovementComponent)
	if !tr.Position.ApproxEqualThreshold(mgl32.Vec3{0.25, 0, 0}, 1e-5) {
		t.Fatalf("expected NPC to stop on final waypoint, got %v", tr.Position)
	}
	if move.Status != NPCNavigationMovementStatusMoving {
		t.Fatalf("expected first update to mark moving before arrival threshold pass, got %+v", move)
	}

	UpdateNPCNavigationMovement(cmd, &Time{Dt: 1})
	if move.Status != NPCNavigationMovementStatusArrived {
		t.Fatalf("expected second update to mark arrived, got %+v", move)
	}
}
