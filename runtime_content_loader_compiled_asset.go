package gekko

import (
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

// Keep frame information in the same retained graph as its decoded definition.
// The definition accessor lets selective scope release identify public borrowers.
type runtimeCompiledAssetHeader struct {
	Definition *content.CompiledAssetHeaderDef
	Info       voxelcodec.Info
}

func (v *runtimeCompiledAssetHeader) runtimeContentDefinition() any { return v.Definition }

type runtimeCompiledAssetShape struct {
	Definition *content.CompiledAssetShapeDef
	Info       voxelcodec.Info
}

func (v *runtimeCompiledAssetShape) runtimeContentDefinition() any { return v.Definition }

type runtimeCompiledAssetLOD struct {
	Definition *content.CompiledAssetLODDef
	Info       voxelcodec.Info
}

func (v *runtimeCompiledAssetLOD) runtimeContentDefinition() any { return v.Definition }

// LoadCompiledAssetHeader explicitly loads a bounded compiled header through the
// shared decoded-content owner. It does not follow references or publish geometry.
// Returned definitions are shared read-only within this owner and its scopes.
func (l *RuntimeContentLoader) LoadCompiledAssetHeader(path string) (*content.CompiledAssetHeaderDef, voxelcodec.Info, error) {
	if l == nil {
		return content.LoadCompiledAssetHeader(path, nil)
	}
	value, err := loadRuntimeContent(l, "compiled-asset-header", path, func(path string) (*runtimeCompiledAssetHeader, error) {
		definition, info, err := content.LoadCompiledAssetHeader(path, l.owner.compiledAssetCodec)
		if err != nil {
			return nil, err
		}
		return &runtimeCompiledAssetHeader{Definition: definition, Info: info}, nil
	})
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return value.Definition, value.Info, nil
}

// LoadCompiledAssetShape explicitly loads bounded canonical bricks through the
// shared decoded-content owner. It does not register runtime geometry.
// Returned definitions are shared read-only within this owner and its scopes.
func (l *RuntimeContentLoader) LoadCompiledAssetShape(path string) (*content.CompiledAssetShapeDef, voxelcodec.Info, error) {
	if l == nil {
		return content.LoadCompiledAssetShape(path, nil)
	}
	value, err := loadRuntimeContent(l, "compiled-asset-shape", path, func(path string) (*runtimeCompiledAssetShape, error) {
		definition, info, err := content.LoadCompiledAssetShape(path, l.owner.compiledAssetCodec)
		if err != nil {
			return nil, err
		}
		return &runtimeCompiledAssetShape{Definition: definition, Info: info}, nil
	})
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return value.Definition, value.Info, nil
}

// LoadCompiledAssetLOD loads bounded derivative bricks through the shared
// decoded-content owner. It does not follow source references or publish geometry.
// Returned definitions are shared read-only within this owner and its scopes.
func (l *RuntimeContentLoader) LoadCompiledAssetLOD(path string) (*content.CompiledAssetLODDef, voxelcodec.Info, error) {
	if l == nil {
		return content.LoadCompiledAssetLOD(path, nil)
	}
	value, err := loadRuntimeContent(l, "compiled-asset-lod", path, func(path string) (*runtimeCompiledAssetLOD, error) {
		definition, info, err := content.LoadCompiledAssetLOD(path, l.owner.compiledAssetCodec)
		if err != nil {
			return nil, err
		}
		return &runtimeCompiledAssetLOD{Definition: definition, Info: info}, nil
	})
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return value.Definition, value.Info, nil
}
