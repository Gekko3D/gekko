package gekko

import (
	"reflect"
	"testing"
)

type s3cRevisionValue struct{ Value int }
type s3cRevisionMarker struct{}

func TestS3cStructuralRevisionNilAndSharedStorage(t *testing.T) {
	var nilEcs *Ecs
	var nilCommands *Commands
	for name, got := range map[string]uint64{
		"nil ECS":              nilEcs.StructuralRevision(),
		"zero ECS":             (&Ecs{}).StructuralRevision(),
		"nil commands":         nilCommands.StructuralRevision(),
		"zero commands":        (&Commands{}).StructuralRevision(),
		"commands without ECS": (&Commands{app: &App{}}).StructuralRevision(),
	} {
		if got != 0 {
			t.Fatalf("%s revision=%d, want zero", name, got)
		}
	}

	app := NewApp()
	cmd := app.Commands()
	shared := *app.ecs
	before := shared.StructuralRevision()
	entity := cmd.AddEntity(s3cRevisionValue{Value: 1})
	if shared.StructuralRevision() != before || cmd.StructuralRevision() != before {
		t.Fatal("enqueue must leave the committed revision unchanged")
	}
	app.FlushCommands()
	if got := shared.StructuralRevision(); got <= before || got != app.ecs.StructuralRevision() || got != cmd.StructuralRevision() {
		t.Fatalf("copied ECS and commands must read the storage's committed stamp: shared=%d ECS=%d commands=%d", got, app.ecs.StructuralRevision(), cmd.StructuralRevision())
	}
	before = shared.StructuralRevision()
	cmd.RemoveEntity(entity)
	app.FlushCommands()
	if shared.StructuralRevision() <= before || shared.StructuralRevision() != cmd.StructuralRevision() {
		t.Fatal("a wrapper copied before mutation must observe later removal")
	}
}

func TestS3cStructuralRevisionCommittedMutations(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	var first, second EntityId
	steps := []struct {
		name    string
		enqueue func()
	}{
		{"first insertion", func() { first = cmd.AddEntity(s3cRevisionValue{Value: 1}) }},
		{"existing archetype insertion", func() { second = cmd.AddEntity(s3cRevisionValue{Value: 2}) }},
		{"component migration", func() { cmd.AddComponents(first, s3cRevisionMarker{}) }},
		{"same archetype replacement", func() { cmd.AddComponents(first, s3cRevisionValue{Value: 3}) }},
		{"equal value replacement", func() { cmd.AddComponents(first, s3cRevisionValue{Value: 3}) }},
		{"component removal", func() { cmd.RemoveComponents(first, s3cRevisionMarker{}) }},
		// Removing an absent type still relocates a live row in the current ECS.
		{"absent component removal", func() { cmd.RemoveComponents(first, s3cRevisionMarker{}) }},
		{"existing archetype removal", func() { cmd.RemoveEntity(second) }},
		{"final removal", func() { cmd.RemoveEntity(first) }},
		{"empty entity insertion", func() { cmd.AddEntity() }},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			before := cmd.StructuralRevision()
			step.enqueue()
			if got := cmd.StructuralRevision(); got != before {
				t.Fatalf("enqueue revision=%d, want committed %d", got, before)
			}
			app.FlushCommands()
			if got := cmd.StructuralRevision(); got <= before {
				t.Fatalf("committed structural mutation must advance: before=%d after=%d", before, got)
			}
		})
	}
}

func TestS3cStructuralRevisionReadsWritesAndIgnoredCommands(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	entity := cmd.AddEntity(s3cRevisionValue{Value: 1})
	app.FlushCommands()
	before := cmd.StructuralRevision()
	cmd.GetAllComponents(entity)
	cmd.GetComponent(entity, reflect.TypeOf(s3cRevisionValue{}))
	cmd.EntityExists(entity)
	// A query registers this previously unseen type, but commits no membership.
	MakeQuery1[s3cRevisionMarker](cmd).Map(func(EntityId, *s3cRevisionMarker) bool { return true })
	MakeQuery1[s3cRevisionValue](cmd).Map(func(_ EntityId, value *s3cRevisionValue) bool {
		value.Value = 9
		return true
	})
	app.FlushCommands()
	if cmd.StructuralRevision() != before {
		t.Fatal("reads, type registration, direct field writes and empty flushes must preserve the stamp")
	}
	var nilComponent *s3cRevisionValue
	cmd.AddComponents(entity)
	cmd.AddComponents(entity, nil, nilComponent)
	cmd.RemoveComponents(entity)
	cmd.RemoveComponents(entity, nil, nilComponent)
	cmd.AddComponents(InvalidEntityId, s3cRevisionMarker{})
	cmd.RemoveComponents(InvalidEntityId, s3cRevisionValue{})
	cmd.RemoveEntity(InvalidEntityId)
	app.FlushCommands()
	if cmd.StructuralRevision() != before {
		t.Fatal("sanitized empty commands and missing-entity commands must preserve the stamp")
	}
	value := cmd.GetComponent(entity, reflect.TypeOf(s3cRevisionValue{})).(*s3cRevisionValue)
	if value.Value != 9 {
		t.Fatal("ignored commands must preserve directly written component values")
	}
}
