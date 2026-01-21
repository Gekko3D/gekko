package gekko

import (
	"github.com/go-gl/mathgl/mgl32"
)

// SceneDef defines the initial state of a scene.
type SceneDef struct {
	VoxelObjects     []VoxelObjectDef
	Lights           []LightDef
	ParticleEmitters []ParticleEmitterDef
	CellularVolumes  []CellularVolumeDef
	// Generic extensions can be added here if needed, or composed in higher level structs
}

// VoxelObjectDef defines a voxel model instantiation.
type VoxelObjectDef struct {
	ModelPath    string
	Position     mgl32.Vec3
	Scale        mgl32.Vec3
	Rotation     mgl32.Quat
	ModelScale   float32 // Scale applied during model creation (resolution)
	IsProcedural bool
	Procedural   ProceduralDef
	HasPhysics   bool
	Physics      PhysicsDef
}

type ProceduralDef struct {
	Type   string // "sphere", "cube", "cone", "pyramid"
	Params []float32
	Color  [4]uint8
	PBR    PBRDef
}

type PBRDef struct {
	Roughness    float32
	Metallic     float32
	Emissive     float32
	IoR          float32
	Transparency float32
}

type PhysicsDef struct {
	Mass          float32
	GravityScale  float32
	Friction      float32
	Restitution   float32
	IsStatic      bool
	CollisionOnly bool
	HalfExtents   mgl32.Vec3
}

// LightDef defines a light instantiation.
type LightDef struct {
	Type      LightType
	Position  mgl32.Vec3
	Color     [3]float32
	Intensity float32
	Range     float32
	ConeAngle float32
	Rotation  mgl32.Quat
	Orbit     *Orbiting
	Rotate    bool
}

// Rotating component for simple rotation behavior
type Rotating struct{}

// Orbiting component for simple orbiting behavior
type Orbiting struct {
	Center mgl32.Vec3
	Radius float32
	Speed  float32
	Angle  float32
}

// ParticleEmitterDef defines a particle emitter.
type ParticleEmitterDef struct {
	Position mgl32.Vec3
	Rotation mgl32.Quat
	Scale    mgl32.Vec3
	Emitter  ParticleEmitterComponent
}

// CellularVolumeDef defines a cellular automaton volume.
type CellularVolumeDef struct {
	Position mgl32.Vec3
	Rotation mgl32.Quat
	Scale    mgl32.Vec3
	Volume   CellularVolumeComponent
}

// LoadScene iterates through the SceneDef and spawns entities.
func LoadScene(cmd *Commands, assets *AssetServer, scene *SceneDef) {
	for _, obj := range scene.VoxelObjects {
		spawnVoxelObject(cmd, assets, obj)
	}

	for _, light := range scene.Lights {
		spawnLight(cmd, light)
	}

	for _, emitter := range scene.ParticleEmitters {
		spawnParticleEmitter(cmd, emitter)
	}

	for _, volume := range scene.CellularVolumes {
		spawnCellularVolume(cmd, volume)
	}
}

func spawnVoxelObject(cmd *Commands, assets *AssetServer, def VoxelObjectDef) {
	var model AssetId
	var palette AssetId

	if def.IsProcedural {
		switch def.Procedural.Type {
		case "sphere":
			// Params: [radius]
			radius := def.Procedural.Params[0]
			model = assets.CreateSphereModel(radius, def.ModelScale)
		case "cube":
			// Params: [w, h, d]
			w, h, d := def.Procedural.Params[0], def.Procedural.Params[1], def.Procedural.Params[2]
			model = assets.CreateCubeModel(w, h, d, def.ModelScale)
		case "cone":
			// Params: [radius, height]
			r, h := def.Procedural.Params[0], def.Procedural.Params[1]
			model = assets.CreateConeModel(r, h, def.ModelScale)
		case "pyramid":
			// Params: [base, height]
			b, h := def.Procedural.Params[0], def.Procedural.Params[1]
			model = assets.CreatePyramidModel(b, h, def.ModelScale)
		}

		palette = assets.CreatePBRPalette(
			def.Procedural.Color,
			def.Procedural.PBR.Roughness,
			def.Procedural.PBR.Metallic,
			def.Procedural.PBR.Emissive,
			def.Procedural.PBR.IoR,
		)
	} else {
		// Load from .vox file
		voxFile, err := LoadVoxFile(def.ModelPath)
		if err != nil {
			panic(err)
		}

		if len(voxFile.Models) == 1 {
			model = assets.CreateVoxelModel(voxFile.Models[0], def.ModelScale)
			palette = assets.CreateVoxelPalette(voxFile.Palette, voxFile.VoxMaterials)
		} else {
			combineFileAsset := assets.CreateVoxelFile(voxFile)
			rootEid := assets.SpawnHierarchicalVoxelModel(cmd, combineFileAsset, TransformComponent{
				Position: def.Position,
				Rotation: def.Rotation,
				Scale:    def.Scale,
			}, def.ModelScale)

			if def.HasPhysics {
				var physicsComps []any
				if !def.Physics.CollisionOnly {
					physicsComps = append(physicsComps, &RigidBodyComponent{
						Mass:         def.Physics.Mass,
						GravityScale: def.Physics.GravityScale,
						IsStatic:     def.Physics.IsStatic,
					})
				}
				physicsComps = append(physicsComps, &ColliderComponent{
					AABBHalfExtents: def.Physics.HalfExtents,
					Friction:        def.Physics.Friction,
					Restitution:     def.Physics.Restitution,
				})
				cmd.AddComponents(rootEid, physicsComps...)
			}
			return
		}
	}

	// Create Entity
	comps := []any{
		&TransformComponent{
			Position: def.Position,
			Scale:    def.Scale,
			Rotation: def.Rotation,
		},
		&VoxelModelComponent{
			VoxelModel:   model,
			VoxelPalette: palette,
		},
	}

	if def.HasPhysics {
		if !def.Physics.CollisionOnly {
			comps = append(comps, &RigidBodyComponent{
				Mass:         def.Physics.Mass,
				GravityScale: def.Physics.GravityScale,
				IsStatic:     def.Physics.IsStatic,
			})
		}
		comps = append(comps, &ColliderComponent{
			AABBHalfExtents: def.Physics.HalfExtents,
			Friction:        def.Physics.Friction,
			Restitution:     def.Physics.Restitution,
		})
	}

	cmd.AddEntity(comps...)
}

func spawnLight(cmd *Commands, def LightDef) {
	comps := []any{
		&TransformComponent{
			Position: def.Position,
			Rotation: def.Rotation,
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&LightComponent{
			Type:      def.Type,
			Color:     def.Color,
			Intensity: def.Intensity,
			Range:     def.Range,
			ConeAngle: def.ConeAngle,
		},
	}

	if def.Orbit != nil {
		comps = append(comps, def.Orbit)
	}
	if def.Rotate {
		comps = append(comps, &Rotating{})
	}

	cmd.AddEntity(comps...)
}

func spawnParticleEmitter(cmd *Commands, def ParticleEmitterDef) {
	cmd.AddEntity(
		&TransformComponent{
			Position: def.Position,
			Rotation: def.Rotation,
			Scale:    def.Scale,
		},
		&def.Emitter,
	)
}

func spawnCellularVolume(cmd *Commands, def CellularVolumeDef) {
	cmd.AddEntity(
		&TransformComponent{
			Position: def.Position,
			Rotation: def.Rotation,
			Scale:    def.Scale,
		},
		&def.Volume,
	)
}
