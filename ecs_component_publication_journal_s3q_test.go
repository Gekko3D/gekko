package gekko

import (
	"reflect"
	"testing"
)

type s3qJournal interface {
	ComponentPublicationCursor() ComponentPublicationCursor
	ComponentPublicationsSince(ComponentPublicationCursor) ComponentPublicationBatch
}

func s3qAssertBatch(t *testing.T, journal s3qJournal, cursor ComponentPublicationCursor, want ...ComponentPublication) ComponentPublicationBatch {
	t.Helper()
	batch := journal.ComponentPublicationsSince(cursor)
	if batch.Resync || batch.Cursor != journal.ComponentPublicationCursor() {
		t.Fatalf("complete batch must carry current watermark: %+v", batch)
	}
	gotCounts := make(map[ComponentPublication]int)
	wantCounts := make(map[ComponentPublication]int)
	for _, publication := range batch.Publications {
		gotCounts[publication]++
	}
	for _, publication := range want {
		wantCounts[publication]++
	}
	if !reflect.DeepEqual(gotCounts, wantCounts) {
		t.Fatalf("publications=%v, want %v (type order unspecified)", batch.Publications, want)
	}
	if (batch.Cursor == cursor) != (len(want) == 0) {
		t.Fatalf("watermark must advance exactly when publications exist: %+v", batch)
	}
	return batch
}

func s3qAssertResync(t *testing.T, journal s3qJournal, cursor ComponentPublicationCursor) ComponentPublicationBatch {
	t.Helper()
	batch := journal.ComponentPublicationsSince(cursor)
	if !batch.Resync || len(batch.Publications) != 0 || batch.Cursor != journal.ComponentPublicationCursor() {
		t.Fatalf("resync must carry current watermark and no partial history: %+v", batch)
	}
	return batch
}

func TestS3qPublicationJournalSafeOwners(t *testing.T) {
	var nilEcs *Ecs
	var nilCommands *Commands
	active := MakeEcs()
	foreign := active.ComponentPublicationCursor()
	for name, journal := range map[string]s3qJournal{
		"nil ECS":              nilEcs,
		"zero ECS":             &Ecs{},
		"nil commands":         nilCommands,
		"zero commands":        &Commands{},
		"commands without ECS": &Commands{app: &App{}},
	} {
		t.Run(name, func(t *testing.T) {
			if journal.ComponentPublicationCursor() != (ComponentPublicationCursor{}) {
				t.Fatal("uninitialized owner must have zero cursor")
			}
			for _, cursor := range []ComponentPublicationCursor{{}, foreign} {
				batch := s3qAssertResync(t, journal, cursor)
				if batch.Cursor != (ComponentPublicationCursor{}) {
					t.Fatal("uninitialized resync must return zero cursor")
				}
			}
		})
	}
	if foreign == (ComponentPublicationCursor{}) {
		t.Fatal("initialized owner must have a valid empty watermark")
	}
	s3qAssertResync(t, &active, ComponentPublicationCursor{})
	s3qAssertBatch(t, &active, foreign)
}

func TestS3qPublicationJournalSharedOwnerReplayAndBatchCopies(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	shared := *app.ecs
	copiedCommands := *cmd
	cursor := cmd.ComponentPublicationCursor()
	entity := cmd.AddEntity(s3eValue{1}, s3eOther{2})
	app.FlushCommands()
	want := []ComponentPublication{{entity, s3eValueType}, {entity, s3eOtherType}}
	first := s3qAssertBatch(t, cmd, cursor, want...)
	second := s3qAssertBatch(t, &shared, cursor, want...)
	s3qAssertBatch(t, &copiedCommands, cursor, want...)
	if first.Cursor != second.Cursor || !reflect.DeepEqual(first.Publications, second.Publications) {
		t.Fatal("replay and independent consumers must observe the same owner history")
	}
	first.Publications[0] = ComponentPublication{InvalidEntityId, nil}
	s3qAssertBatch(t, cmd, cursor, want...)
	if !reflect.DeepEqual(second.Publications, cmd.ComponentPublicationsSince(cursor).Publications) {
		t.Fatal("returned batches must not alias each other or journal records")
	}
	s3qAssertBatch(t, cmd, second.Cursor)

	other := NewApp()
	otherCmd := other.Commands()
	otherEntity := otherCmd.AddEntity(s3eValue{10}, s3eOther{20})
	other.FlushCommands()
	if otherEntity != entity || otherCmd.ComponentRevision(s3eValueType) != cmd.ComponentRevision(s3eValueType) {
		t.Fatal("fixture requires equal entity IDs and publication counts across independent owners")
	}
	otherCursor := otherCmd.ComponentPublicationCursor()
	if otherCursor == second.Cursor {
		t.Fatal("independent owner cursors must differ despite matching counters")
	}
	s3qAssertResync(t, cmd, otherCursor)
	s3qAssertResync(t, otherCmd, second.Cursor)
	if !shared.MarkComponentChanged(entity, s3eValueType) || !copiedCommands.MarkComponentChanged(entity, s3eOtherType) {
		t.Fatal("copied wrappers must publish into shared owner")
	}
	batch := s3qAssertBatch(t, cmd, second.Cursor, want...)
	if !reflect.DeepEqual(batch.Publications, want) {
		t.Fatal("separate explicit marks must retain their publication order")
	}
	s3qAssertBatch(t, otherCmd, otherCursor)
}

func TestS3qPublicationJournalCommittedAttribution(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cursor := cmd.ComponentPublicationCursor()
	var first, second, readmitted EntityId
	steps := []struct {
		name    string
		enqueue func()
		want    func() []ComponentPublication
	}{
		{"duplicate insertion", func() { first = cmd.AddEntity(s3eValue{1}, &s3eValue{2}, s3eOther{3}) }, func() []ComponentPublication {
			return []ComponentPublication{{first, s3eValueType}, {first, s3eOtherType}}
		}},
		{"same archetype insertion", func() { second = cmd.AddEntity(s3eValue{4}, s3eOther{5}) }, func() []ComponentPublication {
			return []ComponentPublication{{second, s3eValueType}, {second, s3eOtherType}}
		}},
		{"add while copying unchanged types", func() { cmd.AddComponents(first, s3eUnseen{}, &s3eUnseen{}) }, func() []ComponentPublication {
			return []ComponentPublication{{first, s3eUnseenType}}
		}},
		{"duplicate replacement", func() { cmd.AddComponents(first, s3eValue{6}, &s3eValue{7}) }, func() []ComponentPublication {
			return []ComponentPublication{{first, s3eValueType}}
		}},
		{"equal replacement", func() { cmd.AddComponents(first, s3eValue{7}) }, func() []ComponentPublication {
			return []ComponentPublication{{first, s3eValueType}}
		}},
		{"duplicate and absent removal", func() { cmd.RemoveComponents(second, s3eValue{}, &s3eValue{}, s3eUnseen{}) }, func() []ComponentPublication {
			return []ComponentPublication{{second, s3eValueType}}
		}},
		{"removed component re-admission", func() { cmd.AddComponents(second, s3eValue{8}) }, func() []ComponentPublication {
			return []ComponentPublication{{second, s3eValueType}}
		}},
		{"multiple buffered publications", func() {
			cmd.AddComponents(second, s3eValue{9})
			cmd.RemoveComponents(second, s3eValue{})
			cmd.AddComponents(second, s3eValue{10})
		}, func() []ComponentPublication {
			return []ComponentPublication{{second, s3eValueType}, {second, s3eValueType}, {second, s3eValueType}}
		}},
		{"entity removal", func() { cmd.RemoveEntity(first) }, func() []ComponentPublication {
			return []ComponentPublication{{first, s3eValueType}, {first, s3eOtherType}, {first, s3eUnseenType}}
		}},
		{"final type removal", func() { cmd.RemoveEntity(second) }, func() []ComponentPublication {
			return []ComponentPublication{{second, s3eValueType}, {second, s3eOtherType}}
		}},
		{"type re-admission", func() { readmitted = cmd.AddEntity(&s3eValue{9}) }, func() []ComponentPublication {
			return []ComponentPublication{{readmitted, s3eValueType}}
		}},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			before := s3eRevisions(cmd)
			step.enqueue()
			s3qAssertBatch(t, cmd, cursor)
			if cmd.ComponentPublicationCursor() != cursor {
				t.Fatal("cursor reads must not flush pending mutations")
			}
			app.FlushCommands()
			want := step.want()
			cursor = s3qAssertBatch(t, cmd, cursor, want...).Cursor
			if step.name == "multiple buffered publications" && (s3eValueOf(t, cmd, second) != 10 || cmd.GetComponent(second, s3eOtherType).(*s3eOther).Value != 5) {
				t.Fatal("invalidations must resolve final committed state after buffered removal and additions")
			}
			after := s3eRevisions(cmd)
			for i, typ := range []reflect.Type{s3eValueType, s3eOtherType, s3eUnseenType} {
				count := uint64(0)
				for _, publication := range want {
					if publication.ComponentType == typ {
						count++
					}
				}
				if after[i] != before[i]+count {
					t.Fatal("journal must preserve aggregate scalar publication semantics")
				}
			}
		})
	}
	if cmd.EntityExists(first) || cmd.EntityExists(second) || s3eValueOf(t, cmd, readmitted) != 9 {
		t.Fatal("journal must preserve committed membership and final supplied values")
	}
}

func TestS3qPublicationJournalDirectWritesMarksAndStructuralOnlyChanges(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cursor := cmd.ComponentPublicationCursor()
	entity := cmd.AddEntity(s3eValue{1})
	if cmd.MarkComponentChanged(entity, s3eValueType) {
		t.Fatal("uncommitted insertion must reject marks")
	}
	s3qAssertBatch(t, cmd, cursor)
	app.FlushCommands()
	cursor = s3qAssertBatch(t, cmd, cursor, ComponentPublication{entity, s3eValueType}).Cursor
	structural := cmd.StructuralRevision()
	MakeQuery1[s3eValue](cmd).Map(func(_ EntityId, value *s3eValue) bool {
		value.Value = 9
		return true
	})
	cmd.GetAllComponents(entity)
	app.FlushCommands()
	s3qAssertBatch(t, cmd, cursor)
	if s3eValueOf(t, cmd, entity) != 9 || cmd.StructuralRevision() != structural {
		t.Fatal("direct unmarked writes remain live without structural publication")
	}
	for _, typ := range []reflect.Type{nil, reflect.TypeOf(0), reflect.TypeOf((**s3eValue)(nil)), s3eUnseenType} {
		if cmd.MarkComponentChanged(entity, typ) {
			t.Fatalf("invalid or absent type %v must reject marks", typ)
		}
	}
	if cmd.MarkComponentChanged(InvalidEntityId, s3eValueType) {
		t.Fatal("invalid entity must reject marks")
	}
	s3qAssertBatch(t, cmd, cursor)
	for _, typ := range []reflect.Type{reflect.TypeOf((*s3eValue)(nil)), s3eValueType} {
		if !cmd.MarkComponentChanged(entity, typ) {
			t.Fatal("committed direct writes and equal values must accept explicit marks")
		}
	}
	cursor = s3qAssertBatch(t, cmd, cursor, ComponentPublication{entity, s3eValueType}, ComponentPublication{entity, s3eValueType}).Cursor
	if cmd.StructuralRevision() != structural || !cmd.EntityExists(entity) || s3eValueOf(t, cmd, entity) != 9 {
		t.Fatal("explicit marks must preserve membership, values and structural revision")
	}
	var empty EntityId
	for _, mutate := range []func(){
		func() { cmd.RemoveComponents(entity, s3eUnseen{}) },
		func() { empty = cmd.AddEntity() },
		func() { cmd.RemoveEntity(empty) },
	} {
		before := cmd.StructuralRevision()
		mutate()
		app.FlushCommands()
		if cmd.StructuralRevision() <= before {
			t.Fatal("structural-only mutations must retain structural invalidation")
		}
		s3qAssertBatch(t, cmd, cursor)
	}
}

func TestS3qPublicationJournalExactCapacityOverflowAndRecovery(t *testing.T) {
	if ComponentPublicationHistoryLimit != 1024 {
		t.Fatalf("history limit=%d, want 1024", ComponentPublicationHistoryLimit)
	}
	app := NewApp()
	cmd := app.Commands()
	first := cmd.AddEntity(s3eValue{1}, s3eOther{2})
	second := cmd.AddEntity(s3eValue{3}, s3eOther{4})
	app.FlushCommands()
	cursor := cmd.ComponentPublicationCursor()
	cycle := []ComponentPublication{{first, s3eValueType}, {second, s3eOtherType}, {first, s3eOtherType}, {second, s3eValueType}}
	want := make([]ComponentPublication, 0, ComponentPublicationHistoryLimit)
	for i := 0; i < ComponentPublicationHistoryLimit; i++ {
		publication := cycle[i%len(cycle)]
		if !cmd.MarkComponentChanged(publication.Entity, publication.ComponentType) {
			t.Fatal("fixture mark must succeed")
		}
		want = append(want, publication)
	}
	exact := s3qAssertBatch(t, cmd, cursor, want...)
	if !reflect.DeepEqual(exact.Publications, want) {
		t.Fatal("exact-capacity history must retain all separate marks in order")
	}
	extra := cycle[0]
	if !cmd.MarkComponentChanged(extra.Entity, extra.ComponentType) {
		t.Fatal("overflow mark must succeed")
	}
	recovery := s3qAssertResync(t, cmd, cursor)
	s3qAssertBatch(t, cmd, exact.Cursor, extra)
	s3qAssertBatch(t, cmd, recovery.Cursor)
	if !cmd.MarkComponentChanged(cycle[1].Entity, cycle[1].ComponentType) {
		t.Fatal("post-resync mark must succeed")
	}
	s3qAssertBatch(t, cmd, recovery.Cursor, cycle[1])
}

func TestS3qPublicationJournalResyncWatermarkPrecedesSnapshot(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	entity := cmd.AddEntity(s3eValue{1})
	app.FlushCommands()
	recovery := s3qAssertResync(t, cmd, ComponentPublicationCursor{})
	// A consumer rescans committed state after receiving the resync watermark.
	MakeQuery1[s3eValue](cmd).Map(func(id EntityId, value *s3eValue) bool {
		value.Value = 2
		if !cmd.MarkComponentChanged(id, s3eValueType) {
			t.Fatal("publication during committed rescan must succeed")
		}
		return true
	})
	if s3eValueOf(t, cmd, entity) != 2 {
		t.Fatal("consumer must resolve current committed values")
	}
	s3qAssertBatch(t, cmd, recovery.Cursor, ComponentPublication{entity, s3eValueType})
}
