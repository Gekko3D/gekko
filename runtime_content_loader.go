package gekko

import (
	"fmt"
	"sync"

	"github.com/gekko3d/gekko/content"
)

type RuntimeContentLoader struct {
	mu               sync.RWMutex
	assets           map[string]*content.AssetDef
	levels           map[string]*content.LevelDef
	terrainManifests map[string]*content.TerrainChunkManifestDef
	terrainChunks    map[string]*content.TerrainChunkDef
	importedWorlds   map[string]*content.ImportedWorldDef
	importedChunks   map[string]*content.ImportedWorldChunkDef
}

func NewRuntimeContentLoader() *RuntimeContentLoader {
	return &RuntimeContentLoader{
		assets:           make(map[string]*content.AssetDef),
		levels:           make(map[string]*content.LevelDef),
		terrainManifests: make(map[string]*content.TerrainChunkManifestDef),
		terrainChunks:    make(map[string]*content.TerrainChunkDef),
		importedWorlds:   make(map[string]*content.ImportedWorldDef),
		importedChunks:   make(map[string]*content.ImportedWorldChunkDef),
	}
}

func (l *RuntimeContentLoader) LoadAsset(path string) (*content.AssetDef, error) {
	if l == nil {
		def, err := content.LoadAsset(path)
		if err != nil {
			return nil, err
		}
		return def, nil
	}
	return loadRuntimeContentCached(&l.mu, path, l.assets, content.LoadAsset)
}

func (l *RuntimeContentLoader) LoadLevel(path string) (*content.LevelDef, error) {
	if l == nil {
		def, err := content.LoadLevel(path)
		if err != nil {
			return nil, err
		}
		return def, nil
	}
	return loadRuntimeContentCached(&l.mu, path, l.levels, content.LoadLevel)
}

func (l *RuntimeContentLoader) LoadTerrainChunkManifest(path string) (*content.TerrainChunkManifestDef, error) {
	if l == nil {
		def, err := content.LoadTerrainChunkManifest(path)
		if err != nil {
			return nil, err
		}
		return def, nil
	}
	return loadRuntimeContentCached(&l.mu, path, l.terrainManifests, content.LoadTerrainChunkManifest)
}

func (l *RuntimeContentLoader) LoadTerrainChunk(path string) (*content.TerrainChunkDef, error) {
	if l == nil {
		def, err := content.LoadTerrainChunk(path)
		if err != nil {
			return nil, err
		}
		return def, nil
	}
	return loadRuntimeContentCached(&l.mu, path, l.terrainChunks, content.LoadTerrainChunk)
}

func (l *RuntimeContentLoader) LoadImportedWorld(path string) (*content.ImportedWorldDef, error) {
	if l == nil {
		def, err := content.LoadImportedWorld(path)
		if err != nil {
			return nil, err
		}
		return def, nil
	}
	return loadRuntimeContentCached(&l.mu, path, l.importedWorlds, content.LoadImportedWorld)
}

func (l *RuntimeContentLoader) LoadImportedWorldChunk(path string) (*content.ImportedWorldChunkDef, error) {
	if l == nil {
		def, err := content.LoadImportedWorldChunk(path)
		if err != nil {
			return nil, err
		}
		return def, nil
	}
	return loadRuntimeContentCached(&l.mu, path, l.importedChunks, content.LoadImportedWorldChunk)
}

func loadRuntimeContentCached[T any](mu *sync.RWMutex, path string, cache map[string]*T, load func(string) (*T, error)) (*T, error) {
	if path == "" {
		return nil, fmt.Errorf("content path is empty")
	}

	mu.RLock()
	cached := cache[path]
	mu.RUnlock()
	if cached != nil {
		return cached, nil
	}

	loaded, err := load(path)
	if err != nil {
		return nil, err
	}

	mu.Lock()
	defer mu.Unlock()
	if cached = cache[path]; cached != nil {
		return cached, nil
	}
	cache[path] = loaded
	return loaded, nil
}
