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

type CollapsedAuthoredVoxelPartsComponent struct {
	PartIDs []string
}

type AuthoredMarkerComponent struct {
	Name string
	Kind string
	Tags []string
}

type AuthoredLevelRootComponent struct {
	LevelID string
}

type AuthoredLevelPlacementRefComponent struct {
	LevelID     string
	PlacementID string
	AssetPath   string
	VolumeID    string
	Tags        []string
}

type AuthoredLevelItemRefComponent struct {
	LevelID     string
	PlacementID string
	ItemID      string
	AssetID     string
	AssetPath   string
	VolumeID    string
	Tags        []string
}

type AuthoredTerrainChunkRefComponent struct {
	LevelID    string
	TerrainID  string
	ChunkCoord [3]int
}

type AuthoredImportedWorldChunkRefComponent struct {
	LevelID    string
	WorldID    string
	ChunkCoord [3]int
}

type StreamedDestructionResidentComponent struct {
	LevelID    string
	WorldID    string
	ChunkCoord [3]int
}

type VoxelPersistenceDirtyComponent struct{}

type VoxelRenderHiddenComponent struct{}

type AuthoredLevelMarkerRefComponent struct {
	LevelID  string
	MarkerID string
	Name     string
	Kind     string
}

type AuthoredLevelWaterBodyRefComponent struct {
	LevelID     string
	WaterBodyID string
	Name        string
}

type AuthoredLevelLadderVolumeRefComponent struct {
	LevelID        string
	LadderVolumeID string
	Name           string
}

type AuthoredLevelMovingBrushRefComponent struct {
	LevelID       string
	MovingBrushID string
	Name          string
}

type AuthoredLevelPathNodeRefComponent struct {
	LevelID    string
	PathNodeID string
	Name       string
}

type AuthoredLevelUseTriggerRefComponent struct {
	LevelID      string
	UseTriggerID string
	Name         string
}

type AuthoredLevelTriggerVolumeRefComponent struct {
	LevelID         string
	TriggerVolumeID string
	Name            string
}

type AuthoredLevelDamageVolumeRefComponent struct {
	LevelID        string
	DamageVolumeID string
	Name           string
}

type AuthoredLevelChangeLevelRefComponent struct {
	LevelID       string
	ChangeLevelID string
	Name          string
}

type AuthoredLevelChargerRefComponent struct {
	LevelID   string
	ChargerID string
	Name      string
}

type AuthoredLevelMultiTargetRefComponent struct {
	LevelID       string
	MultiTargetID string
	Name          string
}

type AuthoredLevelTargetRelayRefComponent struct {
	LevelID       string
	TargetRelayID string
	Name          string
}

type AuthoredLevelBreakableRefComponent struct {
	LevelID     string
	BreakableID string
	Name        string
}

type AuthoredLevelPickupRefComponent struct {
	LevelID  string
	PickupID string
	Name     string
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

func IsAuthoredLevelRootEntity(cmd *Commands, eid EntityId) bool {
	if cmd == nil {
		return false
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		if _, ok := comp.(*AuthoredLevelRootComponent); ok {
			return true
		}
		if _, ok := comp.(AuthoredLevelRootComponent); ok {
			return true
		}
	}
	return false
}

func AuthoredLevelPlacementRefForEntity(cmd *Commands, eid EntityId) (AuthoredLevelPlacementRefComponent, bool) {
	if cmd == nil {
		return AuthoredLevelPlacementRefComponent{}, false
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		if ref, ok := comp.(*AuthoredLevelPlacementRefComponent); ok {
			return *ref, true
		}
		if ref, ok := comp.(AuthoredLevelPlacementRefComponent); ok {
			return ref, true
		}
	}
	return AuthoredLevelPlacementRefComponent{}, false
}

func AuthoredTerrainChunkRefForEntity(cmd *Commands, eid EntityId) (AuthoredTerrainChunkRefComponent, bool) {
	if cmd == nil {
		return AuthoredTerrainChunkRefComponent{}, false
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		if ref, ok := comp.(*AuthoredTerrainChunkRefComponent); ok {
			return *ref, true
		}
		if ref, ok := comp.(AuthoredTerrainChunkRefComponent); ok {
			return ref, true
		}
	}
	return AuthoredTerrainChunkRefComponent{}, false
}

func AuthoredImportedWorldChunkRefForEntity(cmd *Commands, eid EntityId) (AuthoredImportedWorldChunkRefComponent, bool) {
	if cmd == nil {
		return AuthoredImportedWorldChunkRefComponent{}, false
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		if ref, ok := comp.(*AuthoredImportedWorldChunkRefComponent); ok {
			return *ref, true
		}
		if ref, ok := comp.(AuthoredImportedWorldChunkRefComponent); ok {
			return ref, true
		}
	}
	return AuthoredImportedWorldChunkRefComponent{}, false
}

func MarkVoxelEntityPersistenceDirty(cmd *Commands, eid EntityId) {
	if cmd == nil || eid == 0 {
		return
	}
	cmd.AddComponents(eid, &VoxelPersistenceDirtyComponent{})
}

func VoxelEntityPersistenceDirty(cmd *Commands, eid EntityId) bool {
	if cmd == nil || eid == 0 {
		return false
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		switch comp.(type) {
		case *VoxelPersistenceDirtyComponent, VoxelPersistenceDirtyComponent:
			return true
		}
	}
	return false
}

func VoxelEntityRenderHidden(cmd *Commands, eid EntityId) bool {
	if cmd == nil || eid == 0 {
		return false
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		switch comp.(type) {
		case *VoxelRenderHiddenComponent, VoxelRenderHiddenComponent:
			return true
		}
	}
	return false
}

func AuthoredLevelItemRefForEntity(cmd *Commands, eid EntityId) (AuthoredLevelItemRefComponent, bool) {
	if cmd == nil {
		return AuthoredLevelItemRefComponent{}, false
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		if ref, ok := comp.(*AuthoredLevelItemRefComponent); ok {
			return *ref, true
		}
		if ref, ok := comp.(AuthoredLevelItemRefComponent); ok {
			return ref, true
		}
	}
	return AuthoredLevelItemRefComponent{}, false
}

func AuthoredLevelMarkerRefForEntity(cmd *Commands, eid EntityId) (AuthoredLevelMarkerRefComponent, bool) {
	if cmd == nil {
		return AuthoredLevelMarkerRefComponent{}, false
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		if ref, ok := comp.(*AuthoredLevelMarkerRefComponent); ok {
			return *ref, true
		}
		if ref, ok := comp.(AuthoredLevelMarkerRefComponent); ok {
			return ref, true
		}
	}
	return AuthoredLevelMarkerRefComponent{}, false
}
