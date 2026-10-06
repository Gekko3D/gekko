package gekko

import (
	"fmt"
	"path/filepath"
	"sync"
	"unsafe"

	"github.com/gekko3d/gekko/content"
)

// Sources belong to one pending chunk, never to the global AssetServer cache.
type streamedEmitterTextureSource struct {
	mu      sync.Mutex
	texture TextureAsset
}

func (s *streamedEmitterTextureSource) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.texture.Texels = nil
}

type streamedEmitterTextureRegistration struct {
	mu                              sync.Mutex
	source                          *streamedEmitterTextureSource
	texturePath                     string
	texture                         TextureAsset
	prepared, transferred, released bool
}

func (r *streamedEmitterTextureRegistration) release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.texture.Texels = nil
	r.source = nil
	r.released = true
}

// Adoption transfers owned pixels without copying. Global textures outlive the chunk.
func (r *streamedEmitterTextureRegistration) adopt(assets *AssetServer) (AssetId, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if assets == nil || r.released || r.transferred || !r.prepared {
		return AssetId{}, fmt.Errorf("emitter texture publication unavailable")
	}
	id := makeAssetId()
	assets.mu.Lock()
	if assets.textures == nil {
		assets.textures = make(map[AssetId]TextureAsset)
	}
	assets.textures[id] = r.texture
	assets.mu.Unlock()
	r.texture.Texels = nil
	r.transferred = true
	return id, nil
}

type streamedPreparedEmitterTextures struct {
	def       *content.AssetDef // Borrowed from the packet, charged by its owner.
	selection string
	emitters  map[string]*streamedEmitterTextureRegistration
}

func streamedEmitterTextureSelection(assetPath, levelPath string) (string, error) {
	resolved := content.ResolveDocumentPath(assetPath, levelPath)
	if isCompiledAssetPath(resolved) {
		key, err := compiledAssetPacketKey(assetPath, levelPath)
		return "compiled:" + key, err
	}
	key, err := legacyAssetPacketKey(assetPath, levelPath)
	return "legacy:" + key, err
}
func prepareStreamedEmitterTextureSources(p *streamedPreparedChunk, job streamedChunkLoadJob) error {
	if !job.prepareEmitterTextures {
		return nil
	}
	for _, placement := range job.Placements {
		if err := checkCompiledAssetWork(job.Loader, func() bool { return streamedPreparationCancelled(job.prepareCancel) }); err != nil {
			return err
		}
		var selection string
		var def *content.AssetDef
		resolved := content.ResolveDocumentPath(placement.AssetPath, job.LevelPath)
		if isCompiledAssetPath(resolved) {
			key, e := compiledAssetPacketKey(placement.AssetPath, job.LevelPath)
			if e != nil {
				return e
			}
			if packet := p.compiledAssets[key]; packet != nil {
				def = packet.def
				selection = "compiled:" + key
			}
		} else {
			key, e := legacyAssetPacketKey(placement.AssetPath, job.LevelPath)
			if e != nil {
				return e
			}
			if packet := p.legacyAssets[key]; packet != nil {
				def = packet.def
				selection = "legacy:" + key
			}
		}
		if def == nil || len(def.Emitters) == 0 {
			continue
		}
		bundle := &streamedPreparedEmitterTextures{def: def, selection: selection, emitters: make(map[string]*streamedEmitterTextureRegistration)}
		if p.emitterTextures == nil {
			p.emitterTextures = make(map[string]*streamedPreparedEmitterTextures)
		}
		p.emitterTextures[placement.PlacementID] = bundle
		for _, emitter := range def.Emitters {
			if err := checkCompiledAssetWork(job.Loader, func() bool { return streamedPreparationCancelled(job.prepareCancel) }); err != nil {
				return err
			}
			// Match per-emitter validation order before accessing its PNG, even when disabled.
			if _, err := ParticleEmitterFromContent(emitter.Emitter, nil); err != nil {
				return err
			}
			path := emitter.Emitter.TexturePath
			if path == "" {
				continue
			}
			absolute, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			key := filepath.Clean(absolute)
			source := p.emitterTextureSources[key]
			if source == nil {
				texture, err := decodeTexturePNG(path)
				if err != nil {
					return fmt.Errorf("emitter texture %s: %w", path, err)
				}
				source = &streamedEmitterTextureSource{texture: texture}
				if p.emitterTextureSources == nil {
					p.emitterTextureSources = make(map[string]*streamedEmitterTextureSource)
				}
				p.emitterTextureSources[key] = source
			}
			bundle.emitters[emitter.ID] = &streamedEmitterTextureRegistration{source: source, texturePath: path}
		}
	}
	return checkCompiledAssetWork(job.Loader, func() bool { return streamedPreparationCancelled(job.prepareCancel) })
}
func prepareStreamedEmitterTextureCopies(p *streamedPreparedChunk, job streamedChunkLoadJob) error {
	for _, bundle := range p.emitterTextures {
		for _, r := range bundle.emitters {
			if err := checkCompiledAssetWork(job.Loader, func() bool { return streamedPreparationCancelled(job.prepareCancel) }); err != nil {
				return err
			}
			r.mu.Lock()
			r.source.mu.Lock()
			r.texture = r.source.texture
			r.texture.Texels = make([]byte, len(r.source.texture.Texels))
			copy(r.texture.Texels, r.source.texture.Texels)
			r.source.mu.Unlock()
			r.prepared = true
			r.mu.Unlock()
		}
	}
	return checkCompiledAssetWork(job.Loader, func() bool { return streamedPreparationCancelled(job.prepareCancel) })
}

// Charge only owned storage; borrowed definitions, source pointers and mutexes
// are deliberately projected out of metadata traversal.
func streamedEmitterTexturesCharge(p streamedPreparedChunk) int64 {
	if p.emitterTextures == nil && p.emitterTextureSources == nil {
		return 0
	}
	// Preserve actual inline layouts, including owner locks and pointer slots,
	// while stopping traversal at borrowed definitions and source ownership edges.
	type sourceMetadata struct {
		Mutex   [unsafe.Sizeof(sync.Mutex{})]byte
		Texture TextureAsset
	}
	type registrationMetadata struct {
		Mutex                           [unsafe.Sizeof(sync.Mutex{})]byte
		Source                          uintptr
		TexturePath                     string
		Texture                         TextureAsset
		Prepared, Transferred, Released bool
	}
	type bundleMetadata struct {
		Definition uintptr
		Selection  string
		Emitters   map[string]*registrationMetadata
	}
	sources := make(map[string]*sourceMetadata, len(p.emitterTextureSources))
	sourceOwners := make(map[*streamedEmitterTextureSource]*sourceMetadata)
	for key, source := range p.emitterTextureSources {
		metadata := sourceOwners[source]
		if metadata == nil {
			source.mu.Lock()
			metadata = &sourceMetadata{Texture: source.texture}
			source.mu.Unlock()
			sourceOwners[source] = metadata
		}
		sources[key] = metadata
	}
	bundles := make(map[string]*bundleMetadata, len(p.emitterTextures))
	bundleOwners := make(map[*streamedPreparedEmitterTextures]*bundleMetadata)
	registrationOwners := make(map[*streamedEmitterTextureRegistration]*registrationMetadata)
	for key, bundle := range p.emitterTextures {
		metadata := bundleOwners[bundle]
		if metadata == nil {
			metadata = &bundleMetadata{Selection: bundle.selection, Emitters: make(map[string]*registrationMetadata, len(bundle.emitters))}
			for id, r := range bundle.emitters {
				registration := registrationOwners[r]
				if registration == nil {
					r.mu.Lock()
					registration = &registrationMetadata{TexturePath: r.texturePath, Texture: r.texture, Prepared: r.prepared, Transferred: r.transferred, Released: r.released}
					r.mu.Unlock()
					registrationOwners[r] = registration
				}
				metadata.Emitters[id] = registration
			}
			bundleOwners[bundle] = metadata
		}
		bundles[key] = metadata
	}
	return runtimeContentGraphCharge(sources, bundles)
}

func streamedEmitterTextureFutureCopies(p streamedPreparedChunk, m *streamedGeometryBoundMath) int64 {
	var total int64
	for _, b := range p.emitterTextures {
		for _, r := range b.emitters {
			r.mu.Lock()
			if !r.prepared && !r.transferred && !r.released && r.source != nil {
				r.source.mu.Lock()
				total = m.add(total, int64(len(r.source.texture.Texels)))
				r.source.mu.Unlock()
			}
			r.mu.Unlock()
		}
	}
	return total
}
