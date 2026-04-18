package gekko

import "strings"

type AuthoredAssetMarkerLookup struct {
	Entity         EntityId
	Ref            AuthoredAssetRefComponent
	Marker         AuthoredMarkerComponent
	Transform      TransformComponent
	LocalTransform LocalTransformComponent
}

func AuthoredAssetMarkerLookupForEntity(cmd *Commands, eid EntityId) (AuthoredAssetMarkerLookup, bool) {
	if cmd == nil || eid == 0 {
		return AuthoredAssetMarkerLookup{}, false
	}

	lookup := AuthoredAssetMarkerLookup{Entity: eid}
	hasRef := false
	hasMarker := false
	hasTransform := false
	hasLocal := false

	for _, comp := range cmd.GetAllComponents(eid) {
		switch typed := comp.(type) {
		case *AuthoredAssetRefComponent:
			lookup.Ref = *typed
			hasRef = true
		case AuthoredAssetRefComponent:
			lookup.Ref = typed
			hasRef = true
		case *AuthoredMarkerComponent:
			lookup.Marker = *typed
			hasMarker = true
		case AuthoredMarkerComponent:
			lookup.Marker = typed
			hasMarker = true
		case *TransformComponent:
			lookup.Transform = *typed
			hasTransform = true
		case TransformComponent:
			lookup.Transform = typed
			hasTransform = true
		case *LocalTransformComponent:
			lookup.LocalTransform = *typed
			hasLocal = true
		case LocalTransformComponent:
			lookup.LocalTransform = typed
			hasLocal = true
		}
	}

	if !hasRef || !hasMarker || lookup.Ref.Kind != AuthoredItemKindMarker || !hasTransform || !hasLocal {
		return AuthoredAssetMarkerLookup{}, false
	}
	return lookup, true
}

func FindAuthoredAssetMarkerByName(cmd *Commands, root EntityId, name string) (AuthoredAssetMarkerLookup, bool) {
	if cmd == nil || strings.TrimSpace(name) == "" {
		return AuthoredAssetMarkerLookup{}, false
	}
	want := strings.TrimSpace(name)

	var found AuthoredAssetMarkerLookup
	ok := false
	MakeQuery1[TransformComponent](cmd).Map(func(eid EntityId, _ *TransformComponent) bool {
		lookup, exists := AuthoredAssetMarkerLookupForEntity(cmd, eid)
		if !exists || strings.TrimSpace(lookup.Marker.Name) != want || !isEntityOrDescendantOf(cmd, eid, root) {
			return true
		}
		found = lookup
		ok = true
		return false
	})
	return found, ok
}

func FindFirstAuthoredAssetMarkerByKind(cmd *Commands, root EntityId, kind string) (AuthoredAssetMarkerLookup, bool) {
	if cmd == nil || strings.TrimSpace(kind) == "" {
		return AuthoredAssetMarkerLookup{}, false
	}
	markers := FindAuthoredAssetMarkersByKind(cmd, root, kind)
	if len(markers) == 0 {
		return AuthoredAssetMarkerLookup{}, false
	}
	return markers[0], true
}

func FindAuthoredAssetMarkersByKind(cmd *Commands, root EntityId, kind string) []AuthoredAssetMarkerLookup {
	if cmd == nil || strings.TrimSpace(kind) == "" {
		return nil
	}
	want := strings.TrimSpace(kind)
	markers := make([]AuthoredAssetMarkerLookup, 0)
	MakeQuery1[TransformComponent](cmd).Map(func(eid EntityId, _ *TransformComponent) bool {
		lookup, ok := AuthoredAssetMarkerLookupForEntity(cmd, eid)
		if !ok || strings.TrimSpace(lookup.Marker.Kind) != want || !isEntityOrDescendantOf(cmd, eid, root) {
			return true
		}
		markers = append(markers, lookup)
		return true
	})
	return markers
}

func isEntityOrDescendantOf(cmd *Commands, entity EntityId, root EntityId) bool {
	if cmd == nil || entity == 0 && root != 0 {
		return false
	}
	current := entity
	for depth := 0; depth < 64; depth++ {
		if current == root {
			return true
		}
		parent, ok := parentForEntity(cmd, current)
		if !ok {
			return false
		}
		current = parent.Entity
	}
	return false
}

func parentForEntity(cmd *Commands, eid EntityId) (Parent, bool) {
	if cmd == nil || eid == 0 {
		return Parent{}, false
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		if parent, ok := comp.(*Parent); ok {
			return *parent, true
		}
		if parent, ok := comp.(Parent); ok {
			return parent, true
		}
	}
	return Parent{}, false
}
