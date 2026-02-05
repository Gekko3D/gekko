package gekko

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-gl/mathgl/mgl32"
)

type EntityData struct {
	ID          EntityId   `json:"id"`
	Position    mgl32.Vec3 `json:"position"`
	Rotation    mgl32.Quat `json:"rotation"`
	Scale       mgl32.Vec3 `json:"scale"`
	HasLocal    bool       `json:"has_local"`
	LocalPos    mgl32.Vec3 `json:"local_position,omitempty"`
	LocalRot    mgl32.Quat `json:"local_rotation,omitempty"`
	LocalScale  mgl32.Vec3 `json:"local_scale,omitempty"`
	HasParent   bool       `json:"has_parent"`
	ParentID    EntityId   `json:"parent_id"`
	ModelPath   string     `json:"model_path,omitempty"`
	PalettePath string     `json:"palette_path,omitempty"`
}

type PresetData struct {
	Entities []EntityData `json:"entities"`
}

func SavePreset(cmd *Commands, server *AssetServer, filename string) error {
	var entities []EntityData

	// Map all entities that have a TransformComponent
	MakeQuery1[TransformComponent](cmd).Map(func(eid EntityId, tr *TransformComponent) bool {
		// Skip gizmos and UI
		isInternal := false
		allComps := cmd.GetAllComponents(eid)
		for _, c := range allComps {
			if _, ok := c.(EditorGizmoTag); ok {
				isInternal = true
				break
			}
			if _, ok := c.(EditorUiTag); ok {
				isInternal = true
				break
			}
		}
		if isInternal {
			return true
		}

		data := EntityData{
			ID:       eid,
			Position: tr.Position,
			Rotation: tr.Rotation,
			Scale:    tr.Scale,
		}

		// Check other components directly from allComps
		for _, c := range allComps {
			switch comp := c.(type) {
			case LocalTransformComponent:
				data.HasLocal = true
				data.LocalPos = comp.Position
				data.LocalRot = comp.Rotation
				data.LocalScale = comp.Scale
			case *LocalTransformComponent:
				data.HasLocal = true
				data.LocalPos = comp.Position
				data.LocalRot = comp.Rotation
				data.LocalScale = comp.Scale
			case Parent:
				data.HasParent = true
				data.ParentID = comp.Entity
			case *Parent:
				data.HasParent = true
				data.ParentID = comp.Entity
			case VoxelModelComponent:
				if model, ok := server.voxModels[comp.VoxelModel]; ok {
					data.ModelPath = model.SourcePath
				}
				if palette, ok := server.voxPalettes[comp.VoxelPalette]; ok {
					data.PalettePath = palette.SourcePath
				}
			case *VoxelModelComponent:
				if model, ok := server.voxModels[comp.VoxelModel]; ok {
					data.ModelPath = model.SourcePath
				}
				if palette, ok := server.voxPalettes[comp.VoxelPalette]; ok {
					data.PalettePath = palette.SourcePath
				}
			}
		}

		entities = append(entities, data)
		return true
	})

	preset := PresetData{Entities: entities}
	bytes, err := json.MarshalIndent(preset, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, bytes, 0644)
}

func LoadPreset(cmd *Commands, server *AssetServer, filename string) ([]EntityId, error) {
	bytes, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var preset PresetData
	if err := json.Unmarshal(bytes, &preset); err != nil {
		return nil, err
	}

	// Map old IDs to new IDs
	idMap := make(map[EntityId]EntityId)
	var newEntities []EntityId

	// First pass: Create entities and load assets
	for _, data := range preset.Entities {
		var components []any

		// Transform
		components = append(components, &TransformComponent{
			Position: data.Position,
			Rotation: data.Rotation,
			Scale:    data.Scale,
		})

		// Local Transform
		if data.HasLocal {
			components = append(components, &LocalTransformComponent{
				Position: data.LocalPos,
				Rotation: data.LocalRot,
				Scale:    data.LocalScale,
			})
		}

		// Voxel Model
		if data.ModelPath != "" {
			// This is a bit simplified - we assume the asset server can reload from path
			// In a real scenario, we might want to check if it's already loaded
			voxFile, err := LoadVoxFile(data.ModelPath)
			if err == nil {
				modelId := server.CreateVoxelModelFromSource(voxFile.Models[0], 1.0, data.ModelPath)
				paletteId := server.CreateVoxelPaletteFromSource(voxFile.Palette, voxFile.VoxMaterials, data.PalettePath)
				components = append(components, &VoxelModelComponent{
					VoxelModel:   modelId,
					VoxelPalette: paletteId,
				})
			} else {
				fmt.Printf("Warning: Could not load vox file %s: %v\n", data.ModelPath, err)
			}
		}

		newEid := cmd.AddEntity(components...)
		idMap[data.ID] = newEid
		newEntities = append(newEntities, newEid)
	}

	// Second pass: Restore hierarchy
	// fmt.Printf("Restoring hierarchy for %d entities...\n", len(preset.Entities)) // Debug print removed
	for _, data := range preset.Entities {
		if data.HasParent {
			if newChild, okC := idMap[data.ID]; okC {
				if newParent, okP := idMap[data.ParentID]; okP {
					cmd.AddComponents(newChild, &Parent{Entity: newParent})
					// fmt.Printf("  -> Component Added: Parent{Entity: %d} to Entity %d\n", newParent, newChild) // Debug print removed
				}
			}
		}
	}

	return newEntities, nil
}
