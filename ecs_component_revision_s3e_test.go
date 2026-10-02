package gekko

import (
	"reflect"
	"testing"
)

type s3eValue struct{ Value int }
type s3eOther struct{ Value int }
type s3eUnseen struct{}

var s3eValueType = reflect.TypeOf(s3eValue{})
var s3eOtherType = reflect.TypeOf(s3eOther{})
var s3eUnseenType = reflect.TypeOf(s3eUnseen{})

func s3eRevisions(cmd *Commands) [3]uint64 {
	return [3]uint64{cmd.ComponentRevision(s3eValueType), cmd.ComponentRevision(s3eOtherType), cmd.ComponentRevision(s3eUnseenType)}
}

func s3eAssertPublications(t *testing.T, cmd *Commands, before [3]uint64, changed [3]bool) {
	t.Helper()
	after := s3eRevisions(cmd)
	for i := range before {
		if changed[i] && after[i] <= before[i] {
			t.Fatalf("type %d must publish: before=%v after=%v", i, before, after)
		}
		if !changed[i] && after[i] != before[i] {
			t.Fatalf("type %d must retain revision: before=%v after=%v", i, before, after)
		}
	}
}

func s3eValueOf(t *testing.T, cmd *Commands, entity EntityId) int {
	t.Helper()
	value, ok := cmd.GetComponent(entity, s3eValueType).(*s3eValue)
	if !ok || value == nil {
		t.Fatalf("entity %d missing value component", entity)
	}
	return value.Value
}

func TestS3eComponentRevisionSafeOwnersAndCanonicalTypes(t *testing.T) {
	var nilEcs *Ecs
	var nilCommands *Commands
	for name, ecs := range map[string]*Ecs{"nil": nilEcs, "zero": {}} {
		if ecs.ComponentRevision(s3eValueType) != 0 || ecs.MarkComponentChanged(InvalidEntityId, s3eValueType) {
			t.Fatalf("%s ECS must return zero/false", name)
		}
	}
	for name, cmd := range map[string]*Commands{"nil": nilCommands, "zero": {}, "no ECS": {app: &App{}}} {
		if cmd.ComponentRevision(s3eValueType) != 0 || cmd.MarkComponentChanged(InvalidEntityId, s3eValueType) {
			t.Fatalf("%s commands must return zero/false", name)
		}
	}
	app := NewApp()
	cmd := app.Commands()
	entity := cmd.AddEntity(s3eValue{Value: 1})
	app.FlushCommands()
	before := s3eRevisions(cmd)
	invalid := []reflect.Type{nil, reflect.TypeOf(0), reflect.TypeOf(""), reflect.TypeOf([]s3eValue{}), reflect.TypeOf((**s3eValue)(nil)), reflect.TypeOf((*int)(nil))}
	for _, typ := range invalid {
		if cmd.ComponentRevision(typ) != 0 || app.ecs.ComponentRevision(typ) != 0 || cmd.MarkComponentChanged(entity, typ) || app.ecs.MarkComponentChanged(entity, typ) {
			t.Fatalf("invalid type %v must return zero/false", typ)
		}
	}
	if cmd.ComponentRevision(s3eUnseenType) != 0 || app.ecs.ComponentRevision(s3eUnseenType) != 0 || cmd.MarkComponentChanged(entity, s3eUnseenType) || app.ecs.MarkComponentChanged(entity, s3eUnseenType) {
		t.Fatal("unseen type must return zero/false")
	}
	MakeQuery1[s3eUnseen](cmd).Map(func(EntityId, *s3eUnseen) bool { return true })
	if cmd.MarkComponentChanged(entity, s3eUnseenType) || app.ecs.MarkComponentChanged(entity, s3eUnseenType) {
		t.Fatal("registered but absent component must reject marks")
	}
	app.FlushCommands()
	s3eAssertPublications(t, cmd, before, [3]bool{})
	pointerType := reflect.TypeOf((*s3eValue)(nil))
	if cmd.ComponentRevision(pointerType) != before[0] || app.ecs.ComponentRevision(pointerType) != before[0] {
		t.Fatal("struct and one pointer must read the same revision")
	}
	if !cmd.MarkComponentChanged(entity, pointerType) || cmd.ComponentRevision(pointerType) != cmd.ComponentRevision(s3eValueType) {
		t.Fatal("one pointer must mark the canonical struct publication")
	}
}

func TestS3eComponentRevisionCommittedAttribution(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	var first, second EntityId
	steps := []struct {
		name    string
		enqueue func()
		changed [3]bool
	}{
		{"insert", func() { first = cmd.AddEntity(s3eValue{1}, s3eOther{2}) }, [3]bool{true, true}},
		{"same archetype insert", func() { second = cmd.AddEntity(s3eValue{3}, s3eOther{4}) }, [3]bool{true, true}},
		{"add with copied components", func() { cmd.AddComponents(first, s3eUnseen{}) }, [3]bool{false, false, true}},
		{"replace", func() { cmd.AddComponents(first, s3eValue{5}) }, [3]bool{true}},
		{"equal replacement", func() { cmd.AddComponents(first, s3eValue{5}) }, [3]bool{true}},
		{"remove present and absent", func() { cmd.RemoveComponents(second, s3eOther{}, s3eUnseen{}) }, [3]bool{false, true}},
		{"remove absent", func() { cmd.RemoveComponents(second, s3eOther{}) }, [3]bool{}},
		{"remove entity", func() { cmd.RemoveEntity(first) }, [3]bool{true, true, true}},
		{"final value removal", func() { cmd.RemoveEntity(second) }, [3]bool{true}},
		{"empty entity", func() { cmd.AddEntity() }, [3]bool{}},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			before := s3eRevisions(cmd)
			structural := cmd.StructuralRevision()
			step.enqueue()
			s3eAssertPublications(t, cmd, before, [3]bool{})
			app.FlushCommands()
			s3eAssertPublications(t, cmd, before, step.changed)
			if cmd.StructuralRevision() <= structural {
				t.Fatal("committed mutation must retain existing structural publication")
			}
			if cmd.EntityExists(first) {
				want := 1
				if step.name == "replace" || step.name == "equal replacement" || step.name == "remove present and absent" || step.name == "remove absent" {
					want = 5
				}
				if s3eValueOf(t, cmd, first) != want {
					t.Fatal("migration/replacement corrupted first value")
				}
			}
			if cmd.EntityExists(second) && s3eValueOf(t, cmd, second) != 3 {
				t.Fatal("migration corrupted copied second value")
			}
		})
	}
	retired := s3eRevisions(cmd)
	cmd.AddEntity(s3eValue{9}, s3eOther{10}, s3eUnseen{})
	app.FlushCommands()
	s3eAssertPublications(t, cmd, retired, [3]bool{true, true, true})
}

func TestS3eComponentRevisionDuplicateArgumentsPublishOnce(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	before := s3eRevisions(cmd)
	entity := cmd.AddEntity(s3eValue{1}, &s3eValue{2}, s3eOther{3}, &s3eOther{4})
	app.FlushCommands()
	assertOnce := func(before [3]uint64, changed [3]bool) {
		t.Helper()
		after := s3eRevisions(cmd)
		for i := range before {
			want := before[i]
			if changed[i] {
				want++
			}
			if after[i] != want {
				t.Fatalf("duplicate outer publication: before=%v after=%v changed=%v", before, after, changed)
			}
		}
	}
	assertOnce(before, [3]bool{true, true})
	if s3eValueOf(t, cmd, entity) != 2 || cmd.GetComponent(entity, s3eOtherType).(*s3eOther).Value != 4 {
		t.Fatal("duplicate insert must retain final supplied values")
	}
	before = s3eRevisions(cmd)
	cmd.AddComponents(entity, s3eValue{5}, &s3eValue{6}, s3eUnseen{}, &s3eUnseen{})
	app.FlushCommands()
	assertOnce(before, [3]bool{true, false, true})
	if s3eValueOf(t, cmd, entity) != 6 || cmd.GetComponent(entity, s3eOtherType).(*s3eOther).Value != 4 {
		t.Fatal("duplicate add must retain final values and copied components")
	}
	before = s3eRevisions(cmd)
	cmd.RemoveComponents(entity, s3eValue{}, &s3eValue{}, s3eUnseen{}, &s3eUnseen{})
	app.FlushCommands()
	assertOnce(before, [3]bool{true, false, true})
	if cmd.GetComponent(entity, s3eValueType) != nil || cmd.GetComponent(entity, s3eUnseenType) != nil || cmd.GetComponent(entity, s3eOtherType).(*s3eOther).Value != 4 {
		t.Fatal("duplicate removal must remove requested types and preserve copied value")
	}
}

func TestS3eComponentRevisionDirectWritesAndImmediateMarks(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	entity := cmd.AddEntity(s3eValue{1}, s3eOther{2})
	if cmd.MarkComponentChanged(entity, s3eValueType) {
		t.Fatal("pending insertion must not be markable")
	}
	app.FlushCommands()
	before := s3eRevisions(cmd)
	structural := cmd.StructuralRevision()
	MakeQuery1[s3eValue](cmd).Map(func(_ EntityId, value *s3eValue) bool { value.Value = 9; return true })
	cmd.GetAllComponents(entity)
	app.FlushCommands()
	s3eAssertPublications(t, cmd, before, [3]bool{})
	for i := 0; i < 2; i++ {
		before = s3eRevisions(cmd)
		if !cmd.MarkComponentChanged(entity, s3eValueType) {
			t.Fatal("committed component must accept explicit publication, including equal values")
		}
		s3eAssertPublications(t, cmd, before, [3]bool{true})
		if cmd.StructuralRevision() != structural || !cmd.EntityExists(entity) || len(cmd.GetAllComponents(entity)) != 2 || s3eValueOf(t, cmd, entity) != 9 || cmd.GetComponent(entity, s3eOtherType).(*s3eOther).Value != 2 {
			t.Fatal("mark must preserve values, membership and structural revision")
		}
	}
	cmd.AddComponents(entity, s3eUnseen{})
	if cmd.MarkComponentChanged(entity, s3eUnseenType) {
		t.Fatal("pending component addition must not be markable")
	}
	cmd.RemoveComponents(entity, s3eValue{})
	if !cmd.MarkComponentChanged(entity, s3eValueType) {
		t.Fatal("pending component removal remains committed and markable")
	}
	app.FlushCommands()
	if cmd.MarkComponentChanged(entity, s3eValueType) || cmd.MarkComponentChanged(InvalidEntityId, s3eOtherType) {
		t.Fatal("absent components/entities must reject marks")
	}
	cmd.RemoveEntity(entity)
	if !app.ecs.MarkComponentChanged(entity, s3eOtherType) {
		t.Fatal("pending entity removal remains markable through ECS")
	}
	app.FlushCommands()
	before = s3eRevisions(cmd)
	if app.ecs.MarkComponentChanged(entity, s3eOtherType) {
		t.Fatal("removed entity must reject marks")
	}
	s3eAssertPublications(t, cmd, before, [3]bool{})
}

func TestS3eComponentRevisionSanitationAndBufferedOrdering(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	entity := cmd.AddEntity(s3eValue{1}, s3eOther{2})
	app.FlushCommands()
	before := s3eRevisions(cmd)
	var nilValue *s3eValue
	cmd.AddComponents(entity)
	cmd.AddComponents(entity, nil, nilValue)
	cmd.RemoveComponents(entity)
	cmd.RemoveComponents(entity, nil, nilValue)
	cmd.AddComponents(InvalidEntityId, s3eUnseen{})
	cmd.RemoveComponents(InvalidEntityId, s3eValue{})
	cmd.RemoveEntity(InvalidEntityId)
	cmd.AddEntity(nil, nilValue)
	app.FlushCommands()
	s3eAssertPublications(t, cmd, before, [3]bool{})
	app.FlushCommands()
	s3eAssertPublications(t, cmd, before, [3]bool{})
	cmd.AddComponents(entity, s3eValue{3})
	cmd.RemoveComponents(entity, s3eValue{})
	cmd.RemoveComponents(entity, s3eValue{})
	cmd.AddComponents(entity, s3eValue{4})
	s3eAssertPublications(t, cmd, before, [3]bool{})
	app.FlushCommands()
	s3eAssertPublications(t, cmd, before, [3]bool{true})
	if s3eValueOf(t, cmd, entity) != 4 || cmd.GetComponent(entity, s3eOtherType).(*s3eOther).Value != 2 {
		t.Fatal("component removals must precede ordered additions and preserve copied values")
	}
	before = s3eRevisions(cmd)
	cmd.AddComponents(entity, s3eValue{5}, s3eUnseen{})
	cmd.RemoveEntity(entity)
	app.FlushCommands()
	s3eAssertPublications(t, cmd, before, [3]bool{true, true})
	if cmd.EntityExists(entity) {
		t.Fatal("entity removal must precede and discard additions to retired entity")
	}
	before = s3eRevisions(cmd)
	pending := cmd.AddEntity(s3eValue{6})
	cmd.RemoveEntity(pending)
	if cmd.MarkComponentChanged(pending, s3eValueType) {
		t.Fatal("pending entity must remain unmarkable despite queued removal")
	}
	s3eAssertPublications(t, cmd, before, [3]bool{})
	app.FlushCommands()
	s3eAssertPublications(t, cmd, before, [3]bool{true})
	if !cmd.EntityExists(pending) || s3eValueOf(t, cmd, pending) != 6 {
		t.Fatal("removal of uncommitted ID is ignored before its pending insertion")
	}
}

func TestS3eComponentRevisionSharedOwnerGrowthAndIsolation(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	shared := *app.ecs
	copiedCommands := *cmd
	first := cmd.AddEntity(s3eValue{1}, s3eOther{2})
	app.FlushCommands()
	if shared.ComponentRevision(s3eValueType) != cmd.ComponentRevision(s3eValueType) || copiedCommands.ComponentRevision(s3eOtherType) != cmd.ComponentRevision(s3eOtherType) {
		t.Fatal("copied wrappers must read the shared storage owner")
	}
	other := NewApp()
	otherCmd := other.Commands()
	otherFirst := otherCmd.AddEntity(s3eOther{20}, s3eValue{10})
	other.FlushCommands()
	if first != otherFirst {
		t.Fatal("fixture requires matching entity IDs in independent owners")
	}
	otherBefore := s3eRevisions(otherCmd)
	before := s3eRevisions(cmd)
	if !shared.MarkComponentChanged(first, s3eValueType) || !copiedCommands.MarkComponentChanged(first, s3eOtherType) {
		t.Fatal("copied wrappers must publish into the shared owner")
	}
	s3eAssertPublications(t, cmd, before, [3]bool{true, true})
	s3eAssertPublications(t, otherCmd, otherBefore, [3]bool{})
	before = s3eRevisions(cmd)
	if !otherCmd.MarkComponentChanged(otherFirst, s3eValueType) {
		t.Fatal("independent owner must mark its own component")
	}
	s3eAssertPublications(t, cmd, before, [3]bool{})
	entities := []EntityId{first}
	for i := 0; i < 65; i++ {
		entities = append(entities, cmd.AddEntity(s3eValue{i + 100}, s3eOther{i}))
	}
	app.FlushCommands()
	s3eAssertPublications(t, cmd, before, [3]bool{true, true})
	if s3eValueOf(t, cmd, first) != 1 {
		t.Fatal("growth must preserve original value")
	}
	for i, entity := range entities[1:] {
		if s3eValueOf(t, cmd, entity) != i+100 {
			t.Fatal("growth corrupted inserted value")
		}
	}
	before = s3eRevisions(cmd)
	for _, entity := range entities {
		cmd.RemoveEntity(entity)
	}
	app.FlushCommands()
	s3eAssertPublications(t, cmd, before, [3]bool{true, true})
	retired := s3eRevisions(cmd)
	reused := cmd.AddEntity(s3eValue{999}, s3eOther{888})
	app.FlushCommands()
	s3eAssertPublications(t, cmd, retired, [3]bool{true, true})
	if cmd.EntityExists(first) || s3eValueOf(t, cmd, reused) != 999 || s3eValueOf(t, otherCmd, otherFirst) != 10 {
		t.Fatal("reuse/independent owner must preserve exact live values")
	}
	if shared.ComponentRevision(s3eValueType) != cmd.ComponentRevision(s3eValueType) {
		t.Fatal("wrapper copied before growth must observe re-admission revision")
	}
}
