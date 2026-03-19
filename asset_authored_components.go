package gekko

type AuthoredItemKind string

const (
	AuthoredItemKindPart    AuthoredItemKind = "part"
	AuthoredItemKindLight   AuthoredItemKind = "light"
	AuthoredItemKindEmitter AuthoredItemKind = "emitter"
	AuthoredItemKindMarker  AuthoredItemKind = "marker"
)

type AuthoredAssetRootComponent struct {
	AssetID string
}

type AuthoredAssetRefComponent struct {
	AssetID string
	ItemID  string
	Kind    AuthoredItemKind
}

type AuthoredMarkerComponent struct {
	Kind string
	Tags []string
}

func IsAuthoredAssetRootEntity(cmd *Commands, eid EntityId) bool {
	if cmd == nil {
		return false
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		if _, ok := comp.(*AuthoredAssetRootComponent); ok {
			return true
		}
		if _, ok := comp.(AuthoredAssetRootComponent); ok {
			return true
		}
	}
	return false
}

func AuthoredAssetRefForEntity(cmd *Commands, eid EntityId) (AuthoredAssetRefComponent, bool) {
	if cmd == nil {
		return AuthoredAssetRefComponent{}, false
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		if ref, ok := comp.(*AuthoredAssetRefComponent); ok {
			return *ref, true
		}
		if ref, ok := comp.(AuthoredAssetRefComponent); ok {
			return ref, true
		}
	}
	return AuthoredAssetRefComponent{}, false
}

func AuthoredMarkerForEntity(cmd *Commands, eid EntityId) (AuthoredMarkerComponent, bool) {
	if cmd == nil {
		return AuthoredMarkerComponent{}, false
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		if marker, ok := comp.(*AuthoredMarkerComponent); ok {
			return *marker, true
		}
		if marker, ok := comp.(AuthoredMarkerComponent); ok {
			return marker, true
		}
	}
	return AuthoredMarkerComponent{}, false
}
