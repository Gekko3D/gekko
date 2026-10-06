package gekko

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func p5oPNG(t *testing.T, path string, width int, seed byte) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: seed, G: 120, B: 60, A: 128})
		}
	}
	img.SetNRGBA(width-1, 1, color.NRGBA{R: 8, G: 16, B: 32, A: 255})
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, img); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
func p5oAsset(t *testing.T, compiled bool, emitters, width int) (string, string, *content.AssetDef) {
	t.Helper()
	def := p5eShapeDef()
	def.Runtime.CollapseVoxelParts = false
	def.Parts = def.Parts[:1]
	def.Parts[0].ID = "body"
	def.Parts[0].Name = "body"
	input := p5mSave(t, def)
	texture := filepath.Join(filepath.Dir(input), "emitter.png")
	p5oPNG(t, texture, width, 240)
	for i := 0; i < emitters; i++ {
		def.Emitters = append(def.Emitters, content.AssetEmitterDef{ID: fmt.Sprintf("emitter-%d", i), Name: fmt.Sprintf("Emitter %d", i), ParentID: "body", Transform: def.Parts[0].Transform, Emitter: content.EmitterDef{Enabled: i%2 == 0, MaxParticles: 16, SpawnRate: 3, LifetimeRange: content.Range2{2, 4}, StartSpeedRange: content.Range2{1, 3}, StartSizeRange: content.Range2{.5, 1}, StartColorMin: content.Vec4{.1, .2, .3, .4}, StartColorMax: content.Vec4{.5, .6, .7, .8}, Gravity: 2, Drag: .25, ConeAngleDegrees: 12, SpriteIndex: 2, AtlasCols: 3, AtlasRows: 2, TexturePath: texture, AlphaMode: content.AssetAlphaModeTexture}})
	}
	if err := content.SaveAsset(input, def); err != nil {
		t.Fatal(err)
	}
	if !compiled {
		return input, texture, def
	}
	path := filepath.Join(t.TempDir(), "asset.gkassetc")
	if _, err := CompileAuthoredAsset(input, path, nil); err != nil {
		t.Fatal(err)
	}
	h, _, err := content.LoadCompiledAssetHeader(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	texture = content.ResolveDocumentPath(h.Asset.Emitters[0].Emitter.TexturePath, path)
	return path, texture, def
}
func p5oPrepare(t *testing.T, f *s1gFixture) streamedPreparedChunk {
	t.Helper()
	textures, models, palettes := len(f.assets.textures), len(f.assets.voxModels), len(f.assets.voxPalettes)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	p := <-f.runtime.PreparedLoads
	if p.Err != nil {
		p.release()
		t.Fatal(p.Err)
	}
	if len(f.assets.textures) != textures || len(f.assets.voxModels) != models || len(f.assets.voxPalettes) != palettes || len(f.hooks) != 0 {
		p.release()
		t.Fatal("texture worker published assets or ECS callbacks")
	}
	return p
}
func p5oRun(t *testing.T, run func()) {
	t.Helper()
	defer func() {
		if value := recover(); value != nil {
			t.Fatalf("prepared texture path panicked or performed main source IO: %v", value)
		}
	}()
	run()
}
func p5oTexture(t *testing.T, f *s1gFixture, results map[string]AuthoredAssetSpawnResult, placement, emitter string) (AssetId, TextureAsset) {
	t.Helper()
	r, ok := results[placement]
	if !ok || f.hooks[placement] != 1 {
		t.Fatalf("missing completed placement callback: %s", placement)
	}
	entity := r.EntitiesByAssetID[emitter]
	if entity == 0 {
		t.Fatal("emitter entity missing")
	}
	component := s3cComponent[ParticleEmitterComponent](t, f.cmd, entity)
	texture, ok := f.assets.textures[component.Texture]
	if !ok {
		t.Fatal("spawned emitter missing texture asset")
	}
	return component.Texture, texture
}
func p5oPixelsEqual(t *testing.T, got, want TextureAsset) {
	t.Helper()
	if got.Version != want.Version || got.Width != want.Width || got.Height != want.Height || got.Depth != want.Depth || got.Dimension != want.Dimension || got.Format != want.Format || !bytes.Equal(got.Texels, want.Texels) {
		t.Fatalf("worker PNG conversion changed texture metadata or premultiplied pixels: got=%+v want=%+v", got, want)
	}
}

func TestP5oStreamedEmitterTexturesSourceFreeAndIndependentPerSpawn(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		for _, cpu := range []bool{false, true} {
			t.Run(fmt.Sprintf("compiled=%v/cpu=%v", compiled, cpu), func(t *testing.T) {
				path, pngPath, def := p5oAsset(t, compiled, 2, 3)
				oracle := newSpawnTestAssetServer()
				oracle.ensureTextureStorage()
				want := oracle.textures[oracle.CreateTexture(pngPath)]
				f, results := p5mRuntime(t, path, []int{2}, cpu)
				p := p5oPrepare(t, f)
				defer p.release()
				if err := os.Remove(pngPath); err != nil {
					t.Fatal(err)
				}
				p5oRun(t, func() { p5mCommit(t, f, p) })
				for _, emitter := range def.Emitters {
					entity := results[s1gID(0, 0)].EntitiesByAssetID[emitter.ID]
					got := *s3cComponent[ParticleEmitterComponent](t, f.cmd, entity)
					expected, err := ParticleEmitterFromContent(emitter.Emitter, nil)
					if err != nil {
						t.Fatal(err)
					}
					got.Texture = AssetId{}
					if !reflect.DeepEqual(got, expected) {
						t.Fatal("prepared texture route changed emitter settings")
					}
					parent := s3cComponent[Parent](t, f.cmd, entity)
					if parent.Entity != results[s1gID(0, 0)].EntitiesByAssetID["body"] {
						t.Fatal("prepared emitter lost authored parent")
					}
					ref := s3cComponent[AuthoredLevelItemRefComponent](t, f.cmd, entity)
					if ref.ItemID != emitter.ID || ref.PlacementID != s1gID(0, 0) {
						t.Fatal("prepared emitter lost authored level identity")
					}
				}
				var ids []AssetId
				var pixels [][]byte
				for _, emitter := range []string{"emitter-0", "emitter-1"} {
					id, texture := p5oTexture(t, f, results, s1gID(0, 0), emitter)
					p5oPixelsEqual(t, texture, want)
					ids = append(ids, id)
					pixels = append(pixels, texture.Texels)
				}
				// Disabled authored emitters still own decoded textures, and each spawn owns
				// distinct pixel storage. Later placement publication cannot share an edit.
				pixels[0][0] = 99
				_, disabled := p5oTexture(t, f, results, s1gID(0, 0), "emitter-1")
				p5oPixelsEqual(t, disabled, want)
				p5oRun(t, func() { f.commitStage() })
				for _, emitter := range []string{"emitter-0", "emitter-1"} {
					id, texture := p5oTexture(t, f, results, s1gID(0, 1), emitter)
					p5oPixelsEqual(t, texture, want)
					for _, old := range ids {
						if id == old {
							t.Fatal("separate emitter spawn reused texture ID")
						}
					}
					ids = append(ids, id)
				}
				if ids[0] == ids[1] {
					t.Fatal("same-path emitters shared mutable texture ID")
				}
				if f.runtime.Metrics.PendingPreparedBytes != 0 {
					t.Fatal("completed texture packet retained pending charge")
				}
				if err := StopStreamedLevelRuntime(f.cmd); err != nil {
					t.Fatal(err)
				}
				for _, id := range ids {
					if _, ok := f.assets.textures[id]; !ok {
						t.Fatal("Stop deleted ordinary global emitter texture")
					}
				}
			})
		}
	}
}
func TestP5oWorkerPublicationCopiesTransferAndConsumedHandlesDoNotResurrect(t *testing.T) {
	path, pngPath, _ := p5oAsset(t, false, 1, 2)
	f, results := p5mRuntime(t, path, []int{2}, false)
	p := p5oPrepare(t, f)
	defer p.release()
	handle := p.emitterTextures[s1gID(0, 0)].emitters["emitter-0"]
	publication := handle.texture.Texels
	if len(publication) == 0 {
		t.Fatal("worker omitted publication pixels")
	}
	var source []byte
	for _, entry := range p.emitterTextureSources {
		source = entry.texture.Texels
	}
	if len(source) == 0 || &source[0] == &publication[0] {
		t.Fatal("source/publication pixels are not independent")
	}
	before := streamedPreparedChunkCharge(p)
	if err := os.Remove(pngPath); err != nil {
		t.Fatal(err)
	}
	p5oRun(t, func() { p5mCommit(t, f, p) })
	_, texture := p5oTexture(t, f, results, s1gID(0, 0), "emitter-0")
	if &texture.Texels[0] != &publication[0] {
		t.Fatal("main copied pixels instead of transferring worker publication")
	}
	texture.Texels[0] = 99
	if source[0] == 99 {
		t.Fatal("adopted texture edited immutable worker source")
	}
	if len(handle.texture.Texels) != 0 {
		t.Fatal("consumed handle retained publication pixels")
	}
	if after := streamedPreparedChunkCharge(p); after <= 0 || after >= before {
		t.Fatal("consumed texture publication still charged")
	}
	// Replaying the exact tuple must fail as consumed, rather than decode source
	// or silently create a second publication from retained immutable pixels.
	_, err := handle.adopt(f.assets)
	if err == nil {
		t.Fatal("consumed texture handle silently replayed")
	}
	p.release()
	if len(texture.Texels) == 0 || texture.Texels[0] != 99 {
		t.Fatal("release cleared transferred global pixels")
	}
}
func TestP5oWorkerPNGFailurePrecedesPublicationAndAlphaPrecedesIO(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		for _, failure := range []string{"missing", "corrupt", "alpha-before-missing"} {
			t.Run(fmt.Sprintf("%v/%s", compiled, failure), func(t *testing.T) {
				path, pngPath, def := p5oAsset(t, compiled, 1, 2)
				if failure == "corrupt" {
					if err := os.WriteFile(pngPath, []byte("not a PNG"), 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Remove(pngPath); err != nil {
						t.Fatal(err)
					}
				}
				if failure == "alpha-before-missing" {
					if compiled {
						h, _, err := content.LoadCompiledAssetHeader(path, nil)
						if err != nil {
							t.Fatal(err)
						}
						h.Asset.Emitters[0].Emitter.AlphaMode = "unsupported"
						if _, err := content.SaveCompiledAssetHeader(path, h, nil); err != nil {
							t.Fatal(err)
						}
					} else {
						def.Emitters[0].Emitter.AlphaMode = "unsupported"
						if err := content.SaveAsset(path, def); err != nil {
							t.Fatal(err)
						}
					}
				}
				f, _ := p5mRuntime(t, path, []int{1}, true)
				s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
				p := <-f.runtime.PreparedLoads
				defer p.release()
				if p.Err == nil {
					t.Fatal("texture worker accepted invalid PNG or alpha")
				}
				if failure == "alpha-before-missing" && !strings.Contains(p.Err.Error(), "unsupported asset alpha mode") {
					t.Fatalf("PNG error displaced alpha validation: %v", p.Err)
				}
				p.release()
				refreshStreamedRuntimeMetricsCounts(f.runtime)
				if len(f.assets.textures) != 0 || len(f.assets.voxModels) != 0 || len(f.assets.voxPalettes) != 0 || len(f.hooks) != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
					t.Fatal("failed texture worker published state or retained envelope")
				}
			})
		}
	}
}
func TestP5oDirectPreparationAndSelectedPartIgnoreUnusedMissingPNG(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		t.Run(fmt.Sprint(compiled), func(t *testing.T) {
			path, pngPath, _ := p5oAsset(t, compiled, 1, 2)
			if err := os.Remove(pngPath); err != nil {
				t.Fatal(err)
			}
			assets := newSpawnTestAssetServer()
			p, err := LoadAndPrepareAuthoredAsset(path, assets, NewRuntimeContentLoader())
			if err != nil || p == nil {
				t.Fatal("public preparation gained unused emitter PNG IO", err)
			}
			if len(assets.textures) != 0 {
				t.Fatal("public preparation eagerly registered unused emitter texture")
			}
			part, _, err := loadAuthoredLevelVoxelPart(assets, NewRuntimeContentLoader(), path)
			if err != nil || part.model == (AssetId{}) {
				t.Fatal("selected-part consumer gained unused emitter PNG IO", err)
			}
		})
	}
}
func TestP5oEmptyEmitterPathKeepsUntexturedSpawn(t *testing.T) {
	path, _, def := p5oAsset(t, false, 1, 2)
	def.Emitters[0].Emitter.TexturePath = ""
	if err := content.SaveAsset(path, def); err != nil {
		t.Fatal(err)
	}
	f, results := p5mRuntime(t, path, []int{1}, true)
	p := p5oPrepare(t, f)
	defer p.release()
	p5oRun(t, func() { p5mCommit(t, f, p) })
	entity := results[s1gID(0, 0)].EntitiesByAssetID["emitter-0"]
	if entity == 0 || s3cComponent[ParticleEmitterComponent](t, f.cmd, entity).Texture != (AssetId{}) || len(f.assets.textures) != 0 {
		t.Fatal("empty texture path acquired texture ownership")
	}
}

func TestP5oLegacyTextureSpellingAndBlockingCommitRemainSourceFree(t *testing.T) {
	path, pngPath, def := p5oAsset(t, false, 1, 2)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, pngPath)
	if err != nil {
		t.Fatal(err)
	}
	def.Emitters[0].Emitter.TexturePath = relative
	if err := content.SaveAsset(path, def); err != nil {
		t.Fatal(err)
	}
	f, results := p5mRuntime(t, path, []int{1}, true)
	p := p5oPrepare(t, f)
	defer p.release()
	if err := os.Remove(pngPath); err != nil {
		t.Fatal(err)
	}
	p5oRun(t, func() {
		if _, err := commitPreparedStreamedChunk(f.cmd, f.assets, f.runtime, p); err != nil {
			t.Fatal(err)
		}
		f.app.FlushCommands()
	})
	_, texture := p5oTexture(t, f, results, s1gID(0, 0), "emitter-0")
	if texture.Width != 2 || texture.Depth != 1 {
		t.Fatal("legacy cwd-spelled emitter changed texture")
	}
}
func TestP5oWrongSelectedAssetRejectsCapturedEmitterBinding(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		t.Run(fmt.Sprint(compiled), func(t *testing.T) {
			old, oldPNG, _ := p5oAsset(t, compiled, 1, 2)
			selected, selectedPNG, _ := p5oAsset(t, compiled, 1, 2)
			p5oPNG(t, selectedPNG, 2, 80)
			oracle := newSpawnTestAssetServer()
			oracle.ensureTextureStorage()
			want := oracle.textures[oracle.CreateTexture(selectedPNG)]
			f, results := p5mRuntime(t, old, []int{1}, true)
			f.assets.ensureTextureStorage()
			p := p5oPrepare(t, f)
			defer p.release()
			p.PlacementItems[0].AssetPath = selected
			if err := os.Remove(oldPNG); err != nil {
				t.Fatal(err)
			}
			p5oRun(t, func() { p5mCommit(t, f, p) })
			_, texture := p5oTexture(t, f, results, s1gID(0, 0), "emitter-0")
			p5oPixelsEqual(t, texture, want)
		})
	}
}
func TestP5oStartWithoutAssetServerSkipsPNGAndLaterServerUsesMainPath(t *testing.T) {
	for _, lateServer := range []bool{false, true} {
		t.Run(fmt.Sprint(lateServer), func(t *testing.T) {
			path, pngPath, _ := p5oAsset(t, false, 1, 2)
			f, results := p5mRuntime(t, path, []int{1}, true)
			cfg := f.runtime.Config
			if err := StopStreamedLevelRuntime(f.cmd); err != nil {
				t.Fatal(err)
			}
			if err := StartStreamedLevelRuntime(f.cmd, nil, cfg); err != nil {
				t.Fatal(err)
			}
			for coord, items := range f.runtime.PlacementsByChunk {
				for i := range items {
					items[i].AssetPath = path
				}
				f.runtime.PlacementsByChunk[coord] = items
			}
			f.app.FlushCommands()
			updateStreamedObserverSelection(f.cmd, f.runtime)
			if err := os.Remove(pngPath); err != nil {
				t.Fatal(err)
			}
			p := p5oPrepare(t, f)
			defer p.release()
			if lateServer {
				p5oPNG(t, pngPath, 2, 80)
				f.assets.ensureTextureStorage()
				p5oRun(t, func() { p5mCommit(t, f, p) })
				_, texture := p5oTexture(t, f, results, s1gID(0, 0), "emitter-0")
				if texture.Texels[0] != 40 {
					t.Fatal("nil-at-Start job captured stale pixels instead of main fallback")
				}
			} else {
				p5oRun(t, func() {
					if _, err := commitPreparedStreamedChunk(f.cmd, nil, f.runtime, p); err != nil {
						t.Fatal(err)
					}
					f.app.FlushCommands()
				})
				entity := results[s1gID(0, 0)].EntitiesByAssetID["emitter-0"]
				if entity == 0 || s3cComponent[ParticleEmitterComponent](t, f.cmd, entity).Texture != (AssetId{}) {
					t.Fatal("nil-server emitter acquired texture")
				}
			}
		})
	}
}
func TestP5oPendingChargeCountsOneDecodedSourceAndEveryPublication(t *testing.T) {
	const widthSmall, widthLarge = 2, 258
	var growth []int64
	for _, placements := range []int{1, 3} {
		var charges []int64
		for _, width := range []int{widthSmall, widthLarge} {
			path, _, _ := p5oAsset(t, false, 2, width)
			f, _ := p5mRuntime(t, path, []int{placements}, true)
			p := p5oPrepare(t, f)
			defer p.release()
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			charges = append(charges, f.runtime.Metrics.PendingPreparedBytes)
			if len(p.emitterTextureSources) != 1 {
				t.Fatal("same actual PNG path decoded into multiple retained sources")
			}
			copies := 0
			for _, bundle := range p.emitterTextures {
				copies += len(bundle.emitters)
			}
			if copies != placements*2 {
				t.Fatal("worker did not create independent publication per placement/emitter")
			}
			alias := p
			p.release()
			alias.release()
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			if f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatal("aliased texture envelope retained charge")
			}
		}
		growth = append(growth, charges[1]-charges[0])
	}
	const deltaPixels = int64((widthLarge - widthSmall) * 2 * 4)
	if growth[0] < 3*deltaPixels || growth[1] < 7*deltaPixels {
		t.Fatalf("texture pending charge omitted source or copies: growth=%v", growth)
	}
	// Adding two placements adds four publications, while the one decoded source
	// stays deduplicated. Fixed metadata does not grow with image dimensions.
	if difference := growth[1] - growth[0]; difference != 4*deltaPixels {
		t.Fatalf("pixel growth charged source per placement or omitted publications: difference=%d want=%d", difference, 4*deltaPixels)
	}
}
func TestP5oPressureRetryCancellationAndPartialStopDrainTextureOwnership(t *testing.T) {
	path, _, _ := p5oAsset(t, false, 1, 3)
	f, results := p5mRuntime(t, path, []int{2}, false)
	job := buildStreamedChunkLoadJob(f.runtime, ChunkCoord{})
	owner := newStreamedPendingPreparedOwner(1)
	hold, ok := owner.reserve(1)
	if !ok {
		t.Fatal("pressure fixture")
	}
	defer hold.release()
	job.pendingOwner = owner
	pinned := f.runtime.Loader.Stats().PinnedBytes
	denied := prepareStreamedChunkLoad(job)
	defer denied.release()
	if denied.Err != nil || denied.retryCost <= 0 || owner.snapshot().Bytes != 1 || f.runtime.Loader.Stats().PinnedBytes != pinned || len(f.assets.textures) != 0 {
		t.Fatal("texture pressure failed to retry/drain before publication")
	}
	hold.release()
	cancel := make(chan struct{})
	close(cancel)
	job.prepareCancel = cancel
	p := prepareStreamedChunkLoad(job)
	defer p.release()
	p.release()
	if owner.snapshot().Bytes != 0 || len(f.assets.textures) != 0 {
		t.Fatal("cancelled texture preparation retained ownership")
	}
	prepared := p5oPrepare(t, f)
	defer prepared.release()
	p5oRun(t, func() { p5mCommit(t, f, prepared) })
	id, texture := p5oTexture(t, f, results, s1gID(0, 0), "emitter-0")
	if f.hooks[s1gID(0, 1)] != 0 {
		t.Fatal("partial placement cap ignored")
	}
	future := prepared.emitterTextures[s1gID(0, 1)].emitters["emitter-0"]
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	if len(future.texture.Texels) != 0 {
		t.Fatal("partial Stop retained future publication pixels")
	}
	if _, err := future.adopt(f.assets); err == nil {
		t.Fatal("released future publication silently resurrected")
	}
	if current, ok := f.assets.textures[id]; !ok || &current.Texels[0] != &texture.Texels[0] {
		t.Fatal("partial Stop retired transferred ordinary texture")
	}
	refreshStreamedRuntimeMetricsCounts(f.runtime)
	if f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("Stop retained texture pending envelope")
	}
}
func TestP5oPublicTextureAPIsKeepPanicAndBorrowedPixelContracts(t *testing.T) {
	assets := newSpawnTestAssetServer()
	assets.ensureTextureStorage()
	texels := []byte{1, 2, 3, 4}
	id := assets.CreateTextureFromTexels(texels, 1, 1, 1, TextureDimension2D, TextureFormatRGBA8Unorm)
	texels[0] = 99
	if assets.textures[id].Texels[0] != 99 {
		t.Fatal("CreateTextureFromTexels stopped borrowing caller pixels")
	}
	for _, kind := range []string{"missing", "corrupt"} {
		path := filepath.Join(t.TempDir(), kind+".png")
		if kind == "corrupt" {
			if err := os.WriteFile(path, []byte("invalid PNG"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		panicked := false
		func() { defer func() { panicked = recover() != nil }(); assets.CreateTexture(path) }()
		if !panicked {
			t.Fatalf("public CreateTexture %s panic contract changed", kind)
		}
	}
}

func TestP5oSamePathReplacementDefinitionRejectsCapturedEmitterBinding(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		t.Run(fmt.Sprint(compiled), func(t *testing.T) {
			path, pngPath, _ := p5oAsset(t, compiled, 1, 2)
			f, results := p5mRuntime(t, path, []int{1}, true)
			f.assets.ensureTextureStorage()
			p := p5oPrepare(t, f)
			defer p.release()
			// Removing packet selection exercises the real same-path loader fallback.
			// It returns a different definition than the one captured in the bundle.
			if compiled {
				for _, packet := range p.compiledAssets {
					defer packet.release()
				}
				p.compiledAssets = nil
			} else {
				for _, packet := range p.legacyAssets {
					defer packet.release()
				}
				p.legacyAssets = nil
			}
			p5oPNG(t, pngPath, 2, 80)
			p5oRun(t, func() { p5mCommit(t, f, p) })
			_, texture := p5oTexture(t, f, results, s1gID(0, 0), "emitter-0")
			if texture.Texels[0] != 40 {
				t.Fatal("replacement definition adopted pixels sealed to different packet identity")
			}
		})
	}
}
func TestP5oStaleAndCancelledPublishedEnvelopesDrainUnusedPixels(t *testing.T) {
	for _, terminal := range []string{"stale", "cancel"} {
		t.Run(terminal, func(t *testing.T) {
			path, _, _ := p5oAsset(t, false, 1, 2)
			f, _ := p5mRuntime(t, path, []int{1}, true)
			p := p5oPrepare(t, f)
			defer p.release()
			handle := p.emitterTextures[s1gID(0, 0)].emitters["emitter-0"]
			if len(handle.texture.Texels) == 0 {
				t.Fatal("missing pending pixels")
			}
			var sources []*streamedEmitterTextureSource
			for _, source := range p.emitterTextureSources {
				sources = append(sources, source)
			}
			if terminal == "stale" {
				p.Generation--
			} else {
				cancelStreamedPreparation(f.runtime.chunkPrepareCancels[ChunkCoord{}])
			}
			f.runtime.PreparedLoads <- p
			f.commitStage()
			for _, source := range sources {
				if len(source.texture.Texels) != 0 {
					t.Fatal("terminal texture owner retained decoded source pixels")
				}
			}
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			if len(handle.texture.Texels) != 0 || len(f.assets.textures) != 0 || len(f.hooks) != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatal("stale/cancelled prepared texture published or retained unused pixels")
			}
		})
	}
}

func TestP5oEmitterFailuresFollowAuthoredOrderAndDrainEarlierSources(t *testing.T) {
	for _, compiled := range []bool{false, true} {
		for _, failure := range []string{"first-PNG-before-later-alpha", "later-PNG"} {
			t.Run(fmt.Sprintf("%v/%s", compiled, failure), func(t *testing.T) {
				path, pngPath, def := p5oAsset(t, compiled, 2, 2)
				missing := filepath.Join(t.TempDir(), "missing-later.png")
				if compiled {
					missing = "missing-later.png"
				}
				change := func(a *content.AssetDef) {
					if failure == "first-PNG-before-later-alpha" {
						a.Emitters[1].Emitter.AlphaMode = "unsupported"
					} else {
						a.Emitters[1].Emitter.TexturePath = missing
					}
				}
				if compiled {
					h, _, err := content.LoadCompiledAssetHeader(path, nil)
					if err != nil {
						t.Fatal(err)
					}
					change(h.Asset)
					if _, err := content.SaveCompiledAssetHeader(path, h, nil); err != nil {
						t.Fatal(err)
					}
				} else {
					change(def)
					if err := content.SaveAsset(path, def); err != nil {
						t.Fatal(err)
					}
				}
				if failure == "first-PNG-before-later-alpha" {
					if err := os.Remove(pngPath); err != nil {
						t.Fatal(err)
					}
				}
				f, _ := p5mRuntime(t, path, []int{1}, true)
				pinned := f.runtime.Loader.Stats().PinnedBytes
				s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
				p := <-f.runtime.PreparedLoads
				defer p.release()
				if p.Err == nil {
					t.Fatal("worker ignored ordered emitter PNG failure")
				}
				if strings.Contains(p.Err.Error(), "unsupported asset alpha mode") {
					t.Fatalf("later emitter alpha error displaced earlier PNG access: %v", p.Err)
				}
				if failure == "later-PNG" && !strings.Contains(p.Err.Error(), "missing-later.png") {
					t.Fatalf("later PNG failure missing selected filename: %v", p.Err)
				}
				p.release()
				if f.runtime.Loader.Stats().PinnedBytes != pinned {
					t.Fatal("failed emitter preparation retained decoded pins")
				}
				refreshStreamedRuntimeMetricsCounts(f.runtime)
				if f.runtime.Metrics.PendingPreparedBytes != 0 || len(f.assets.textures) != 0 || len(f.assets.voxModels) != 0 || len(f.hooks) != 0 {
					t.Fatal("failed later emitter retained earlier source or published assets")
				}
			})
		}
	}
}
func TestP5oChangedEmitterPathRejectsCapturedPublication(t *testing.T) {
	path, _, _ := p5oAsset(t, false, 1, 2)
	newPNG := filepath.Join(t.TempDir(), "current.png")
	p5oPNG(t, newPNG, 2, 80)
	f, results := p5mRuntime(t, path, []int{1}, true)
	f.assets.ensureTextureStorage()
	p := p5oPrepare(t, f)
	defer p.release()
	for _, packet := range p.legacyAssets {
		packet.def.Emitters[0].Emitter.TexturePath = newPNG
	}
	p5oRun(t, func() { p5mCommit(t, f, p) })
	_, texture := p5oTexture(t, f, results, s1gID(0, 0), "emitter-0")
	if texture.Texels[0] != 40 {
		t.Fatal("changed emitter path adopted old bound pixels")
	}
}
