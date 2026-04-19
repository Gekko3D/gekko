package gekko

import (
	"reflect"
	"testing"
)

func TestEcs_EntityGroupMembershipCanonicalizesOnInsert(t *testing.T) {
	ecs := MakeEcs()
	entityID := ecs.addEntity(EntityGroupMembershipComponent{
		Groups: []EntityGroupKey{
			{Kind: "system", ID: "alpha"},
			{Kind: "", ID: "invalid"},
			{Kind: "bubble", ID: "online"},
			{Kind: "system", ID: "alpha"},
			{Kind: "bubble", ID: ""},
			{Kind: "system", ID: "beta"},
		},
	})

	gotGroups := ecs.getEntityGroups(entityID)
	wantGroups := []EntityGroupKey{
		{Kind: "bubble", ID: "online"},
		{Kind: "system", ID: "alpha"},
		{Kind: "system", ID: "beta"},
	}
	if !reflect.DeepEqual(gotGroups, wantGroups) {
		t.Fatalf("expected canonical groups %#v, got %#v", wantGroups, gotGroups)
	}

	component := ecs.getComponent(entityID, reflect.TypeOf(EntityGroupMembershipComponent{}))
	membership, ok := component.(*EntityGroupMembershipComponent)
	if !ok || membership == nil {
		t.Fatalf("expected group membership component, got %#v", component)
	}
	if !reflect.DeepEqual(membership.Groups, wantGroups) {
		t.Fatalf("expected stored component groups %#v, got %#v", wantGroups, membership.Groups)
	}

	if !ecs.hasGroup(entityID, EntityGroupKey{Kind: "system", ID: "alpha"}) {
		t.Fatal("expected exact group membership lookup to succeed")
	}
	if ecs.hasGroup(entityID, EntityGroupKey{Kind: "system", ID: ""}) {
		t.Fatal("expected empty-id group lookup to fail")
	}
}

func TestEcs_GetEntitiesInGroupReturnsSortedMatches(t *testing.T) {
	ecs := MakeEcs()
	group := EntityGroupKey{Kind: "system", ID: "starter"}

	first := ecs.addEntity(EntityGroupMembershipComponent{Groups: []EntityGroupKey{group}})
	_ = ecs.addEntity(EntityGroupMembershipComponent{Groups: []EntityGroupKey{{Kind: "system", ID: "other"}}})
	second := ecs.addEntity(EntityGroupMembershipComponent{Groups: []EntityGroupKey{group}})
	third := ecs.addEntity(
		EntityGroupMembershipComponent{Groups: []EntityGroupKey{{Kind: "bubble", ID: "online"}, group}},
	)

	got := ecs.getEntitiesInGroup(group)
	want := []EntityId{first, second, third}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected sorted group members %#v, got %#v", want, got)
	}
}

func TestEcs_EntityGroupIndexUpdatesWhenComponentsChange(t *testing.T) {
	ecs := MakeEcs()
	entityID := ecs.addEntity()
	systemGroup := EntityGroupKey{Kind: "system", ID: "alpha"}
	bubbleGroup := EntityGroupKey{Kind: "bubble", ID: "online"}

	ecs.addComponents(entityID, EntityGroupMembershipComponent{
		Groups: []EntityGroupKey{systemGroup, bubbleGroup, systemGroup},
	})
	if !reflect.DeepEqual(ecs.getEntityGroups(entityID), []EntityGroupKey{bubbleGroup, systemGroup}) {
		t.Fatalf("expected component add to update canonical groups, got %#v", ecs.getEntityGroups(entityID))
	}
	if got := ecs.getEntitiesInGroup(systemGroup); !reflect.DeepEqual(got, []EntityId{entityID}) {
		t.Fatalf("expected system group lookup to contain entity, got %#v", got)
	}

	ecs.removeComponents(entityID, EntityGroupMembershipComponent{})
	if groups := ecs.getEntityGroups(entityID); groups != nil {
		t.Fatalf("expected no groups after component removal, got %#v", groups)
	}
	if got := ecs.getEntitiesInGroup(systemGroup); got != nil {
		t.Fatalf("expected system group to be empty after removal, got %#v", got)
	}
}

func TestCommands_EntityGroupLookupsReflectOnlyFlushedState(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	group := EntityGroupKey{Kind: "system", ID: "starter"}

	entityID := cmd.AddEntityInGroup(group)
	if got := cmd.GetEntitiesInGroup(group); got != nil {
		t.Fatalf("expected pending entity to stay invisible before flush, got %#v", got)
	}

	app.FlushCommands()

	if got := cmd.GetEntitiesInGroup(group); !reflect.DeepEqual(got, []EntityId{entityID}) {
		t.Fatalf("expected flushed entity to become visible, got %#v", got)
	}
	if !reflect.DeepEqual(cmd.GetEntityGroups(entityID), []EntityGroupKey{group}) {
		t.Fatalf("expected flushed entity groups, got %#v", cmd.GetEntityGroups(entityID))
	}
	if !cmd.HasGroup(entityID, group) {
		t.Fatal("expected HasGroup to reflect flushed membership")
	}
}

func TestCommands_AddEntityInGroupsMergesExplicitMembership(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	systemGroup := EntityGroupKey{Kind: "system", ID: "starter"}
	bubbleGroup := EntityGroupKey{Kind: "bubble", ID: "online"}

	entityID := cmd.AddEntityInGroup(systemGroup, &EntityGroupMembershipComponent{
		Groups: []EntityGroupKey{
			bubbleGroup,
			systemGroup,
			{Kind: "", ID: "invalid"},
		},
	})
	app.FlushCommands()

	want := []EntityGroupKey{bubbleGroup, systemGroup}
	if got := cmd.GetEntityGroups(entityID); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected merged helper membership %#v, got %#v", want, got)
	}
}

func TestCommands_GroupLookupRemovesStaleEntityIDsAfterEntityRemoval(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	group := EntityGroupKey{Kind: "system", ID: "starter"}

	first := cmd.AddEntityInGroup(group, &TransformComponent{})
	second := cmd.AddEntityInGroup(group, &TransformComponent{})
	third := cmd.AddEntityInGroup(group, &TransformComponent{})
	app.FlushCommands()

	if got := cmd.GetEntitiesInGroup(group); !reflect.DeepEqual(got, []EntityId{first, second, third}) {
		t.Fatalf("expected all grouped entities after spawn, got %#v", got)
	}

	cmd.RemoveEntity(second)
	app.FlushCommands()

	if got := cmd.GetEntitiesInGroup(group); !reflect.DeepEqual(got, []EntityId{first, third}) {
		t.Fatalf("expected removed entity to be evicted from group index, got %#v", got)
	}
	if cmd.HasGroup(second, group) {
		t.Fatal("expected removed entity to lose group membership")
	}
	if groups := cmd.GetEntityGroups(second); groups != nil {
		t.Fatalf("expected removed entity to have no indexed groups, got %#v", groups)
	}
}

func TestCommands_RemoveEntitiesInGroupIsIdempotentWithPendingRemovals(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	systemGroup := EntityGroupKey{Kind: "system", ID: "starter"}
	bubbleGroup := EntityGroupKey{Kind: "bubble", ID: "online"}

	first := cmd.AddEntityInGroup(systemGroup, &TransformComponent{})
	second := cmd.AddEntityInGroups([]EntityGroupKey{systemGroup, bubbleGroup}, &TransformComponent{})
	third := cmd.AddEntityInGroup(systemGroup, &TransformComponent{})
	app.FlushCommands()

	cmd.RemoveEntity(second)
	removedFirstPass := cmd.RemoveEntitiesInGroup(systemGroup)
	removedSecondPass := cmd.RemoveEntitiesInGroup(systemGroup)

	wantRemoved := []EntityId{first, second, third}
	if !reflect.DeepEqual(removedFirstPass, wantRemoved) {
		t.Fatalf("expected first group removal pass %#v, got %#v", wantRemoved, removedFirstPass)
	}
	if !reflect.DeepEqual(removedSecondPass, wantRemoved) {
		t.Fatalf("expected repeated group removal to remain idempotent, got %#v", removedSecondPass)
	}

	app.FlushCommands()

	if got := cmd.GetEntitiesInGroup(systemGroup); got != nil {
		t.Fatalf("expected system group to be empty after flush, got %#v", got)
	}
	if got := cmd.GetEntitiesInGroup(bubbleGroup); got != nil {
		t.Fatalf("expected overlapping bubble group to be empty after entity removal, got %#v", got)
	}
}

func TestCommands_GroupSpawnDespawnHelpersSupportRepeatedCycles(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	systemGroup := EntityGroupKey{Kind: "system", ID: "alpha"}
	bubbleGroup := EntityGroupKey{Kind: "bubble", ID: "online"}

	for cycle := 0; cycle < 3; cycle++ {
		first := cmd.AddEntityInGroup(systemGroup, &TransformComponent{})
		second := cmd.AddEntityInGroups([]EntityGroupKey{systemGroup, bubbleGroup}, &TransformComponent{})
		app.FlushCommands()

		if got := cmd.GetEntitiesInGroup(systemGroup); !reflect.DeepEqual(got, []EntityId{first, second}) {
			t.Fatalf("cycle %d: expected fresh system group members, got %#v", cycle, got)
		}

		cmd.RemoveEntity(first)
		cmd.RemoveEntitiesInGroup(systemGroup)
		app.FlushCommands()

		if got := cmd.GetEntitiesInGroup(systemGroup); got != nil {
			t.Fatalf("cycle %d: expected cleared system group, got %#v", cycle, got)
		}
		if got := cmd.GetEntitiesInGroup(bubbleGroup); got != nil {
			t.Fatalf("cycle %d: expected cleared bubble group, got %#v", cycle, got)
		}
	}
}
