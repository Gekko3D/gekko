package gekko

import "reflect"

// ComponentPublicationHistoryLimit bounds journal-owned metadata globally
// across all component types and entities in one ECS storage owner.
const ComponentPublicationHistoryLimit = 1024

// ComponentPublication invalidates an entity/type pair. Resolve current
// committed state when processing it; removals can leave no live component.
// Records contain no component values or replayable mutation operations.
type ComponentPublication struct {
	Entity        EntityId
	ComponentType reflect.Type
}

// ComponentPublicationCursor is an opaque comparable journal watermark.
// It retains only an identity token, never the ECS storage or component data.
type ComponentPublicationCursor struct {
	owner    *componentPublicationOwner
	sequence uint64
}

// ComponentPublicationBatch owns its Publications slice. Cursor is the read-time
// watermark. Resync means history is unavailable and Publications is empty.
// Acknowledge Cursor only after successful processing or a committed rescan;
// publications during that rescan remain replayable from this watermark.
type ComponentPublicationBatch struct {
	Cursor       ComponentPublicationCursor
	Publications []ComponentPublication
	Resync       bool
}

// Nonzero size ensures simultaneously live owner tokens have distinct identity.
type componentPublicationOwner struct{ marker byte }

type componentPublicationJournal struct {
	owner    *componentPublicationOwner
	sequence uint64
	records  *[ComponentPublicationHistoryLimit]ComponentPublication
	next     int
	count    int
}

// ComponentPublicationCursor reads committed publication state on the main
// thread without allocation, type registration or a command flush.
func (ecs *Ecs) ComponentPublicationCursor() ComponentPublicationCursor {
	if ecs == nil || ecs.storage == nil {
		return ComponentPublicationCursor{}
	}
	journal := &ecs.storage.componentPublications
	return ComponentPublicationCursor{owner: journal.owner, sequence: journal.sequence}
}

// ComponentPublicationsSince returns retained invalidations in publication
// order on the main thread. Reads neither drain history nor acknowledge work.
// Zero/unbound, foreign, future and expired cursors request resync with the
// current watermark and no partial suffix. Independent readers own their copies.
func (ecs *Ecs) ComponentPublicationsSince(cursor ComponentPublicationCursor) ComponentPublicationBatch {
	current := ecs.ComponentPublicationCursor()
	batch := ComponentPublicationBatch{Cursor: current}
	if current.owner == nil || cursor.owner != current.owner || cursor.sequence > current.sequence {
		batch.Resync = true
		return batch
	}
	// Validate owner and sequence before subtraction to avoid underflow.
	journal := &ecs.storage.componentPublications
	delta := current.sequence - cursor.sequence
	if delta > uint64(journal.count) {
		batch.Resync = true
		return batch
	}
	if delta == 0 {
		return batch
	}
	batch.Publications = make([]ComponentPublication, int(delta))
	start := (journal.next - int(delta) + ComponentPublicationHistoryLimit) % ComponentPublicationHistoryLimit
	n := copy(batch.Publications, journal.records[start:])
	copy(batch.Publications[n:], journal.records[:])
	return batch
}

func (journal *componentPublicationJournal) append(publication ComponentPublication) {
	if journal.sequence == ^uint64(0) {
		// A new token era invalidates every old cursor, even matching counters.
		journal.owner = &componentPublicationOwner{}
		journal.sequence = 0
		clear(journal.records[:])
		journal.next, journal.count = 0, 0
	}
	if journal.records == nil {
		journal.records = new([ComponentPublicationHistoryLimit]ComponentPublication)
	}
	journal.records[journal.next] = publication
	journal.next = (journal.next + 1) % ComponentPublicationHistoryLimit
	if journal.count < ComponentPublicationHistoryLimit {
		journal.count++
	}
	journal.sequence++
}
