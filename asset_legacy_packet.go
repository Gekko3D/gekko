package gekko

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"

	"github.com/gekko3d/gekko/content"
)

// Legacy packets retain the ordinary cache namespaces and global asset lifetime.
// Their sources and single-use publication copies never borrow decoded content.
type legacyAssetPacket struct {
	def          *content.AssetDef
	documentPath string
	animations   *content.ResolvedAssetAnimations
	parts        map[string]string
	partPalettes map[string]string
	geometries   map[string]*legacyAssetGeometry
	palettes     map[string]*compiledAssetPacketPalette
	collapse     *authoredCollapseCandidate
}
type legacyAssetGeometry struct {
	source       VoxelGeometryAsset
	bases        map[content.VoxelObjectLatticeDef]string
	registration *legacyGeometryRegistration
}
type legacyGeometryRegistration struct {
	mu    sync.Mutex
	asset *VoxelGeometryAsset
	key   string
}

// AssetDef is an acyclic exported DTO graph. Clone it before validation and
// normalization without the nil/empty changes caused by JSON omitempty.
func cloneLegacyContent(value reflect.Value) reflect.Value {
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.New(value.Type().Elem())
		out.Elem().Set(cloneLegacyContent(value.Elem()))
		return out
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			out.Index(i).Set(cloneLegacyContent(value.Index(i)))
		}
		return out
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), cloneLegacyContent(iter.Value()))
		}
		return out
	case reflect.Struct:
		out := reflect.New(value.Type()).Elem()
		for i := 0; i < value.NumField(); i++ {
			out.Field(i).Set(cloneLegacyContent(value.Field(i)))
		}
		return out
	default:
		return value
	}
}
func prepareLegacyGeometryRegistration(key string, source VoxelGeometryAsset) *legacyGeometryRegistration {
	copy := source
	copy.VoxModel.Voxels = cloneCompiledPaletteSlice(source.VoxModel.Voxels)
	copy.XBrickMap = source.XBrickMap.Copy()
	return &legacyGeometryRegistration{asset: &copy, key: key}
}
func (r *legacyGeometryRegistration) release() {
	if r != nil {
		r.mu.Lock()
		r.asset = nil
		r.mu.Unlock()
	}
}
func (p *legacyAssetPacket) release() {
	if p == nil {
		return
	}
	for _, g := range p.geometries {
		g.registration.release()
	}
	for _, v := range p.palettes {
		v.registration.release()
	}
	if p.collapse != nil {
		p.collapse.geometry.registration.release()
		p.collapse.palette.registration.release()
	}
}

// Include selected file identity and logical spelling: VOX keys expose spelling.
func legacyAssetPacketKey(path, levelPath string) (string, error) {
	resolved := content.ResolveDocumentPath(path, levelPath)
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute) + "\x00" + resolved, nil
}
func prepareLegacyAssetPacket(path string, loader *RuntimeContentLoader, cancelled func() bool) (*legacyAssetPacket, error) {
	if err := checkCompiledAssetWork(loader, cancelled); err != nil {
		return nil, err
	}
	cached, err := loader.LoadAsset(path)
	if err != nil {
		return nil, err
	}
	def := cloneLegacyContent(reflect.ValueOf(cached)).Interface().(*content.AssetDef)
	if err := checkCompiledAssetWork(loader, cancelled); err != nil {
		return nil, err
	}
	if validation := content.ValidateAsset(def, content.AssetValidationOptions{DocumentPath: path}); validation.HasErrors() {
		return nil, fmt.Errorf("asset validation failed: %s", validation.Error())
	}
	if err := checkCompiledAssetWork(loader, cancelled); err != nil {
		return nil, err
	}
	animations, err := content.ResolveAssetAnimations(def, path)
	if err != nil {
		return nil, err
	}
	content.NormalizeAssetDef(def)
	if err := ValidateAssetHierarchy(def); err != nil {
		return nil, err
	}
	p := &legacyAssetPacket{def: def, documentPath: path, animations: animations, parts: map[string]string{}, partPalettes: map[string]string{}, geometries: map[string]*legacyAssetGeometry{}, palettes: map[string]*compiledAssetPacketPalette{}}
	success := false
	defer func() {
		if !success {
			p.release()
		}
	}()
	files := map[string]*VoxFile{}
	for _, part := range def.Parts {
		if err := checkCompiledAssetWork(loader, cancelled); err != nil {
			return nil, err
		}
		var geometry VoxelGeometryAsset
		var palette VoxelPaletteAsset
		var key, paletteKey string
		sourcePath := content.ResolveDocumentPath(part.Source.Path, path)
		switch part.Source.Kind {
		case content.AssetSourceKindGroup:
			continue
		case content.AssetSourceKindVoxelShape:
			payload, e := json.Marshal(struct {
				Scale float32                     `json:"scale"`
				Shape *content.AssetVoxelShapeDef `json:"shape"`
			}{part.ModelScale, part.Source.VoxelShape})
			if e != nil {
				return nil, e
			}
			key = string(payload)
			if existing := p.geometries[key]; existing != nil {
				geometry = existing.source
			} else {
				source := buildAuthoredVoxelShapeMap(part)
				geometry = VoxelGeometryAsset{XBrickMap: source, LocalMin: source.GetAABBMin(), LocalMax: source.GetAABBMax(), BrickSize: [3]uint32{8, 8, 8}, SourcePath: key, RuntimeOwned: true}
			}
			palette, err = buildAuthoredVoxelShapePalette(def, part)
		case content.AssetSourceKindProceduralPrimitive:
			var model VoxModel
			model, err = buildProceduralPrimitiveModel(part.Source.Primitive, part.Source.Params, part.ModelScale)
			if err != nil {
				return nil, err
			}
			key = voxelGeometryCacheKey(model, "")
			geometry = buildVoxelGeometryAsset(model, "")
			palette, err = buildAuthoredProceduralPalette(def, part)
		case content.AssetSourceKindVoxModel, content.AssetSourceKindVoxSceneNode:
			file := files[sourcePath]
			if file == nil {
				file, err = LoadVoxFile(sourcePath)
				if err != nil {
					return nil, err
				}
				files[sourcePath] = file
			}
			if err := checkCompiledAssetWork(loader, cancelled); err != nil {
				return nil, err
			}
			index := part.Source.ModelIndex
			if part.Source.Kind == content.AssetSourceKindVoxSceneNode {
				resolved, e := ResolveVoxSceneNodeModel(InspectVoxScene(file, 1), part.Source.NodeName, index)
				if e != nil {
					return nil, e
				}
				index = resolved.ModelIndex
			}
			if index < 0 || index >= len(file.Models) {
				return nil, fmt.Errorf("model index %d out of range for %s", index, sourcePath)
			}
			original := file.Models[index]
			model := ScaleVoxModel(original, part.ModelScale)
			model.Voxels = cloneCompiledPaletteSlice(model.Voxels)
			key = voxelGeometryCacheKey(model, sourcePath)
			geometry = buildVoxelGeometryAsset(model, sourcePath)
			palette, err = buildAuthoredVoxFilePalette(def, part, file.Palette, file.VoxMaterials, original, sourcePath)
			if part.Source.MaterialID == "" {
				paletteKey = voxelPaletteCacheKey(palette.VoxPalette, palette.Materials, sourcePath)
			}
		default:
			return nil, fmt.Errorf("unsupported asset source kind %q", part.Source.Kind)
		}
		if err != nil {
			return nil, err
		}
		if err := checkCompiledAssetWork(loader, cancelled); err != nil {
			return nil, err
		}
		g := p.geometries[key]
		if g == nil {
			g = &legacyAssetGeometry{source: geometry, bases: map[content.VoxelObjectLatticeDef]string{}, registration: prepareLegacyGeometryRegistration(key, geometry)}
			p.geometries[key] = g
		}
		if part.Source.Kind == content.AssetSourceKindVoxelShape {
			lattice := authoredVoxelShapeLattice(part.VoxelResolution)
			if validAuthoredVoxelShapeLattice(lattice) && g.bases[lattice] == "" {
				identity, _, e := content.VoxelObjectBaseIdentity(VoxelObjectSnapshotFromXBrickMap(g.source.XBrickMap), lattice, nil)
				if e == nil {
					g.bases[lattice] = identity
				}
			}
		}
		// First clone owns borrowed VOX maps; second is the publication copy.
		source, e := clonePalettePublication(&palette)
		if e != nil {
			return nil, e
		}
		if paletteKey == "" {
			paletteKey = voxelPaletteAssetCacheKey(*source)
		}
		if p.palettes[paletteKey] == nil {
			registration, e := preparePaletteRegistrationWithKey(source, paletteKey)
			if e != nil {
				return nil, e
			}
			p.palettes[paletteKey] = &compiledAssetPacketPalette{source: source, registration: registration}
		}
		p.parts[part.ID] = key
		p.partPalettes[part.ID] = paletteKey
	}
	if err := checkCompiledAssetWork(loader, cancelled); err != nil {
		return nil, err
	}
	if def.Runtime != nil && def.Runtime.CollapseVoxelParts && len(def.AnimationSetPaths) == 0 {
		// Ineligibility is automatic expanded fallback, not a preparation error.
		p.collapse, err = prepareLegacyCollapseCandidate(p, loader, cancelled)
		if err != nil {
			return nil, err
		}
	}
	if err := checkCompiledAssetWork(loader, cancelled); err != nil {
		return nil, err
	}
	success = true
	return p, nil
}
func (assets *AssetServer) adoptLegacyGeometry(key string, g *legacyAssetGeometry, r *legacyGeometryRegistration) (AssetId, bool) {
	assets.ensureVoxelStorage()
	assets.mu.Lock()
	defer assets.mu.Unlock()
	id, ok, _ := assets.adoptLegacyGeometryLocked(key, g, r)
	return id, ok
}

func (assets *AssetServer) adoptLegacyGeometryLocked(key string, g *legacyAssetGeometry, r *legacyGeometryRegistration) (AssetId, bool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.key != key {
		return AssetId{}, false, false
	}
	id, warm := assets.voxModelKeys[key]
	if warm {
		if _, ok := assets.voxModels[id]; !ok {
			return AssetId{}, false, false
		}
		r.asset = nil
	} else {
		if r.asset == nil {
			return AssetId{}, false, false
		}
		id = makeAssetId()
		assets.voxModels[id] = *r.asset
		assets.voxModelKeys[key] = id
		r.asset = nil
	}
	for lattice, identity := range g.bases {
		if assets.authoredVoxelBases[id][lattice] == "" {
			assets.recordVerifiedAuthoredVoxelBaseLocked(id, lattice, identity)
		}
	}
	return id, true, !warm
}
func publishLegacyAssetPacket(p *legacyAssetPacket, assets *AssetServer, loader *RuntimeContentLoader) (*PreparedAuthoredAsset, error) {
	if err := checkCompiledAssetWork(loader, nil); err != nil {
		return nil, err
	}
	prepared := &PreparedAuthoredAsset{def: p.def, documentPath: p.documentPath, animations: p.animations, parts: map[string]preparedAuthoredPart{}}
	models := map[string]AssetId{}
	palettes := map[string]AssetId{}
	if assets != nil {
		assets.ensureVoxelStorage()
		assets.mu.Lock()
		defer assets.mu.Unlock()
		allCold := true
		for key, g := range p.geometries {
			id, ok, cold := assets.adoptLegacyGeometryLocked(key, g, g.registration)
			if !ok {
				if _, warm := assets.voxModelKeys[key]; warm {
					return nil, fmt.Errorf("legacy geometry adoption rejected")
				}
				fresh := prepareLegacyGeometryRegistration(key, g.source)
				id, ok, cold = assets.adoptLegacyGeometryLocked(key, g, fresh)
				fresh.release()
				if !ok {
					return nil, fmt.Errorf("legacy geometry rebuild rejected")
				}
			}
			models[key] = id
			allCold = allCold && cold
		}
		for key, v := range p.palettes {
			id, ok, cold := assets.adoptCompiledAssetPaletteLocked(key, v.source, v.registration)
			if !ok {
				_, warm := assets.voxPaletteKeys[key]
				if warm {
					return nil, fmt.Errorf("legacy palette adoption rejected")
				}
				fresh, err := preparePaletteRegistrationWithKey(v.source, key)
				if err != nil {
					return nil, err
				}
				id, ok, cold = assets.adoptCompiledAssetPaletteLocked(key, v.source, fresh)
				fresh.release()
				if !ok {
					return nil, fmt.Errorf("legacy palette rebuild rejected")
				}
			}
			palettes[key] = id
			allCold = allCold && cold
		}
		if p.def.Runtime != nil && p.def.Runtime.CollapseVoxelParts {
			prepared.legacyCollapse = &legacyPreparedCollapse{}
			if allCold && p.collapse != nil {
				prepared.legacyCollapse.adopted = assets.adoptAuthoredCollapseCandidateLocked(p.collapse)
			}
		}
	}
	for _, part := range p.def.Parts {
		prepared.parts[part.ID] = preparedAuthoredPart{model: models[p.parts[part.ID]], palette: palettes[p.partPalettes[part.ID]]}
	}
	if err := checkCompiledAssetWork(loader, nil); err != nil {
		return nil, err
	}
	return prepared, nil
}

// Dense storage is accounted separately from scalar metadata and live mutexes.
func legacyGeometrySourceCharge(source VoxelGeometryAsset) int64 {
	geometry := source.XBrickMap
	source.XBrickMap = nil
	return runtimeContentChargeSum(runtimeContentGraphCharge(source), streamedPendingGeometryCharge(geometry))
}
