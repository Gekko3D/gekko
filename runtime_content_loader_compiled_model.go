package gekko

import (
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

type runtimeCompiledAssetModelHeader struct {
	Definition *content.CompiledAssetModelHeaderDef
	Info       voxelcodec.Info
}

func (v *runtimeCompiledAssetModelHeader) runtimeContentDefinition() any { return v.Definition }

type runtimeCompiledAssetModel struct {
	Definition *content.CompiledAssetModelDef
	Info       voxelcodec.Info
}

func (v *runtimeCompiledAssetModel) runtimeContentDefinition() any { return v.Definition }

// LoadCompiledAssetModelHeader explicitly loads a bounded compiled header through the
// shared decoded-content owner. It does not follow references or publish geometry.
// Returned definitions are shared read-only within this owner and its scopes.
func (l *RuntimeContentLoader) LoadCompiledAssetModelHeader(path string) (*content.CompiledAssetModelHeaderDef, voxelcodec.Info, error) {
	if l == nil {
		return content.LoadCompiledAssetModelHeader(path, nil)
	}
	value, err := loadRuntimeContent(l, "compiled-asset-model-header", path, func(path string) (*runtimeCompiledAssetModelHeader, error) {
		definition, info, err := content.LoadCompiledAssetModelHeader(path, l.owner.compiledAssetCodec)
		if err != nil {
			return nil, err
		}
		return &runtimeCompiledAssetModelHeader{Definition: definition, Info: info}, nil
	})
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return value.Definition, value.Info, nil
}

// LoadCompiledAssetModel explicitly loads bounded canonical bricks through the
// shared decoded-content owner. It does not register runtime geometry.
// Returned definitions are shared read-only within this owner and its scopes.
func (l *RuntimeContentLoader) LoadCompiledAssetModel(path string) (*content.CompiledAssetModelDef, voxelcodec.Info, error) {
	if l == nil {
		return content.LoadCompiledAssetModel(path, nil)
	}
	value, err := loadRuntimeContent(l, "compiled-asset-model", path, func(path string) (*runtimeCompiledAssetModel, error) {
		definition, info, err := content.LoadCompiledAssetModel(path, l.owner.compiledAssetCodec)
		if err != nil {
			return nil, err
		}
		return &runtimeCompiledAssetModel{Definition: definition, Info: info}, nil
	})
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return value.Definition, value.Info, nil
}
