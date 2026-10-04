package gekko

// Declared instance intent is independent of shared geometry availability.
// The pair remains construction-relative through overrides; the render owner
// must qualify current effective geometry before using it. No asset lease or
// display selection is owned by this ephemeral ECS component.
type compiledAssetLODComponent struct {
	fullID, coarseID AssetId
}

func compiledAssetLODIntent(part preparedAuthoredPart) *compiledAssetLODComponent {
	if part.model == (AssetId{}) || part.compiledLOD == (AssetId{}) || part.model == part.compiledLOD {
		return nil
	}
	return &compiledAssetLODComponent{fullID: part.model, coarseID: part.compiledLOD}
}
