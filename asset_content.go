package gekko

import (
	"fmt"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func AssetTransformFromDef(def content.AssetTransformDef) TransformComponent {
	return TransformComponent{
		Position: mgl32.Vec3{def.Position[0], def.Position[1], def.Position[2]},
		Rotation: mgl32.Quat{W: def.Rotation[3], V: mgl32.Vec3{def.Rotation[0], def.Rotation[1], def.Rotation[2]}},
		Scale:    mgl32.Vec3{def.Scale[0], def.Scale[1], def.Scale[2]},
		Pivot:    mgl32.Vec3{def.Pivot[0], def.Pivot[1], def.Pivot[2]},
	}
}

func AssetTransformDefFromComponent(tr TransformComponent) content.AssetTransformDef {
	return content.AssetTransformDef{
		Position: content.Vec3{tr.Position.X(), tr.Position.Y(), tr.Position.Z()},
		Rotation: content.Quat{tr.Rotation.V.X(), tr.Rotation.V.Y(), tr.Rotation.V.Z(), tr.Rotation.W},
		Scale:    content.Vec3{tr.Scale.X(), tr.Scale.Y(), tr.Scale.Z()},
		Pivot:    content.Vec3{tr.Pivot.X(), tr.Pivot.Y(), tr.Pivot.Z()},
	}
}

func AssetLightTypeToEngine(lightType content.AssetLightType) (LightType, error) {
	switch lightType {
	case content.AssetLightTypePoint:
		return LightTypePoint, nil
	case content.AssetLightTypeDirectional:
		return LightTypeDirectional, nil
	case content.AssetLightTypeSpot:
		return LightTypeSpot, nil
	case content.AssetLightTypeAmbient:
		return LightTypeAmbient, nil
	default:
		return LightTypePoint, fmt.Errorf("unsupported asset light type %q", lightType)
	}
}

func AssetLightTypeFromEngine(lightType LightType) content.AssetLightType {
	switch lightType {
	case LightTypeDirectional:
		return content.AssetLightTypeDirectional
	case LightTypeSpot:
		return content.AssetLightTypeSpot
	case LightTypeAmbient:
		return content.AssetLightTypeAmbient
	default:
		return content.AssetLightTypePoint
	}
}

func AssetAlphaModeToEngine(alphaMode content.AssetAlphaMode) (SpriteAlphaMode, error) {
	switch alphaMode {
	case "", content.AssetAlphaModeTexture:
		return SpriteAlphaTexture, nil
	case content.AssetAlphaModeLuminance:
		return SpriteAlphaLuminance, nil
	default:
		return SpriteAlphaTexture, fmt.Errorf("unsupported asset alpha mode %q", alphaMode)
	}
}

func AssetAlphaModeFromEngine(alphaMode SpriteAlphaMode) content.AssetAlphaMode {
	switch alphaMode {
	case SpriteAlphaLuminance:
		return content.AssetAlphaModeLuminance
	default:
		return content.AssetAlphaModeTexture
	}
}

func ParticleEmitterFromContent(def content.EmitterDef, assets *AssetServer) (ParticleEmitterComponent, error) {
	alphaMode, err := AssetAlphaModeToEngine(def.AlphaMode)
	if err != nil {
		return ParticleEmitterComponent{}, err
	}

	component := ParticleEmitterComponent{
		Enabled:          def.Enabled,
		MaxParticles:     def.MaxParticles,
		SpawnRate:        def.SpawnRate,
		LifetimeRange:    [2]float32{def.LifetimeRange[0], def.LifetimeRange[1]},
		StartSpeedRange:  [2]float32{def.StartSpeedRange[0], def.StartSpeedRange[1]},
		StartSizeRange:   [2]float32{def.StartSizeRange[0], def.StartSizeRange[1]},
		StartColorMin:    [4]float32{def.StartColorMin[0], def.StartColorMin[1], def.StartColorMin[2], def.StartColorMin[3]},
		StartColorMax:    [4]float32{def.StartColorMax[0], def.StartColorMax[1], def.StartColorMax[2], def.StartColorMax[3]},
		Gravity:          def.Gravity,
		Drag:             def.Drag,
		ConeAngleDegrees: def.ConeAngleDegrees,
		SpriteIndex:      def.SpriteIndex,
		AtlasCols:        def.AtlasCols,
		AtlasRows:        def.AtlasRows,
		AlphaMode:        alphaMode,
	}
	if assets != nil && def.TexturePath != "" {
		component.Texture = assets.CreateTexture(def.TexturePath)
	}
	return component, nil
}

func ContentEmitterFromComponent(component ParticleEmitterComponent, texturePath string) content.EmitterDef {
	return content.EmitterDef{
		Enabled:          component.Enabled,
		MaxParticles:     component.MaxParticles,
		SpawnRate:        component.SpawnRate,
		LifetimeRange:    content.Range2{component.LifetimeRange[0], component.LifetimeRange[1]},
		StartSpeedRange:  content.Range2{component.StartSpeedRange[0], component.StartSpeedRange[1]},
		StartSizeRange:   content.Range2{component.StartSizeRange[0], component.StartSizeRange[1]},
		StartColorMin:    content.Vec4{component.StartColorMin[0], component.StartColorMin[1], component.StartColorMin[2], component.StartColorMin[3]},
		StartColorMax:    content.Vec4{component.StartColorMax[0], component.StartColorMax[1], component.StartColorMax[2], component.StartColorMax[3]},
		Gravity:          component.Gravity,
		Drag:             component.Drag,
		ConeAngleDegrees: component.ConeAngleDegrees,
		SpriteIndex:      component.SpriteIndex,
		AtlasCols:        component.AtlasCols,
		AtlasRows:        component.AtlasRows,
		TexturePath:      texturePath,
		AlphaMode:        AssetAlphaModeFromEngine(component.AlphaMode),
	}
}
