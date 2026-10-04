package gekko

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func c3h7Fixture(t *testing.T) (string, *content.CompiledAssetHeaderDef) {
	t.Helper()
	input, output, _ := c3h4Fixture(t)
	output = strings.TrimSuffix(output, ".gkasset") + ".gkassetc"
	if _, err := CompileAuthoredAssetWithOptions(input, output, nil, CompiledAssetCompileOptions{EnableLOD2: true}); err != nil {
		t.Fatal(err)
	}
	h, _, err := content.LoadCompiledAssetHeader(output, nil)
	if err != nil {
		t.Fatal(err)
	}
	return output, h
}
func c3h7SaveHeader(t *testing.T, path string, h *content.CompiledAssetHeaderDef) {
	t.Helper()
	if _, err := content.SaveCompiledAssetHeader(path, h, nil); err != nil {
		t.Fatal("typed header fixture", err)
	}
}
func c3h7LODPath(path string, h *content.CompiledAssetHeaderDef) string {
	return filepath.Join(filepath.Dir(path), filepath.FromSlash(h.LODs[0].Path))
}

func TestC3h7SessionBorrowsVerifiedLODsAndConsumersKeepLevelZero(t *testing.T) {
	path, h := c3h7Fixture(t)
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
	caller := owner.NewScope()
	defer caller.Close()
	cached, ci, err := caller.Loader().LoadCompiledAssetLOD(c3h7LODPath(path, h))
	if err != nil {
		t.Fatal(err)
	}
	cachedShape, si, err := caller.Loader().LoadCompiledAssetShape(filepath.Join(filepath.Dir(path), filepath.FromSlash(h.Shapes[0].Path)))
	if err != nil {
		t.Fatal(err)
	}
	before := owner.Stats()
	session, err := verifyCompiledAssetInput(path, caller.Loader(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.lods) != 2 || len(session.shapes) != 2 {
		t.Fatal("missing verified per-part definitions")
	}
	for _, ref := range h.LODs {
		v, ok := session.lods[ref.PartID]
		if !ok || v.definition != cached || v.contentID != ci.ContentID || session.shapes[ref.PartID].definition != cachedShape || session.shapes[ref.PartID].contentID != si.ContentID {
			t.Fatal("session didn't borrow existing typed cache/proof")
		}
	}
	session.close()
	if after := owner.Stats(); after.Entries != before.Entries || after.Bytes != before.Bytes || after.PinnedBytes != before.PinnedBytes {
		t.Fatal("session close revoked caller pins or retained child entries", after)
	}
	assets := c3d3Server()
	prepared, err := LoadAndPrepareAuthoredAsset(path, assets, caller.Loader())
	if err != nil {
		t.Fatal(err)
	}
	id, ok := PreparedAuthoredAssetPartGeometry(prepared, "shape")
	geometry, found := assets.GetVoxelGeometry(id)
	if !ok || !found || geometry.XBrickMap.GetVoxelCount() != 4 {
		t.Fatal("direct preparation substituted coarse geometry")
	}
	firstAssets := c3d3Server()
	first, _, _, err := loadAuthoredLevelVoxelModel(firstAssets, caller.Loader(), path)
	if err != nil {
		t.Fatal(err)
	}
	g, ok := firstAssets.GetVoxelGeometry(first)
	if !ok || g.XBrickMap.GetVoxelCount() != 4 {
		t.Fatal("first-part consumer substituted coarse geometry")
	}
	packet, err := prepareCompiledAssetPacket(path, caller.Loader(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer packet.release()
	for _, shape := range packet.shapes {
		if shape.source.GetVoxelCount() != 4 {
			t.Fatal("worker packet substituted coarse geometry")
		}
	}
	if metadata, err := LoadAndPrepareAuthoredAsset(path, nil, caller.Loader()); err != nil || metadata == nil {
		t.Fatal("nil asset-server preparation failed valid closure", err)
	}
	if base := assets.authoredVoxelBaseIdentity(id, cachedShape.Lattice); base != h.Shapes[0].BaseIdentity {
		t.Fatal("level-zero original base proof changed")
	}
}

func TestC3h7DeclaredLODFailuresPrecedeEveryConsumerPublication(t *testing.T) {
	cases := []string{"missing", "unused-part-missing", "later-alias-encoded-size", "later-alias-missing", "corrupt", "wrong-kind", "content-id", "encoded-size", "decoded-size", "source-binding", "wrong-or"}
	for _, kind := range cases {
		t.Run(kind, func(t *testing.T) {
			path, h := c3h7Fixture(t)
			lodPath := c3h7LODPath(path, h)
			switch kind {
			case "missing":
				if err := os.Remove(lodPath); err != nil {
					t.Fatal(err)
				}
			case "unused-part-missing":
				for i := range h.LODs {
					if h.LODs[i].PartID == "duplicate" {
						h.LODs[i].Path = "lods/missing-unused.gklod"
					}
				}
				c3h7SaveHeader(t, path, h)
			case "later-alias-encoded-size", "later-alias-missing":
				// Canonical duplicate ref succeeds first; source/LOD logical pair
				// memoization must still verify the later physical alias.
				alias := "lods/later-alias.gklod"
				if kind == "later-alias-encoded-size" {
					if err := os.WriteFile(filepath.Join(filepath.Dir(path), filepath.FromSlash(alias)), c3cRead(t, lodPath), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if len(h.LODs) != 2 || h.LODs[0].PartID != "duplicate" || h.LODs[1].PartID != "shape" {
					t.Fatal("fixture refs lack expected canonical order")
				}
				h.LODs[1].Path = alias
				if kind == "later-alias-encoded-size" {
					h.LODs[1].EncodedBytes++
				}
				c3h7SaveHeader(t, path, h)
			case "corrupt":
				raw := c3cRead(t, lodPath)
				if err := os.WriteFile(lodPath, raw[:len(raw)-1], 0600); err != nil {
					t.Fatal(err)
				}
			case "wrong-kind":
				if err := os.WriteFile(lodPath, c3cRead(t, filepath.Join(filepath.Dir(path), filepath.FromSlash(h.Shapes[0].Path))), 0600); err != nil {
					t.Fatal(err)
				}
			case "content-id", "encoded-size", "decoded-size":
				for i := range h.LODs {
					if kind == "content-id" {
						h.LODs[i].ContentID = strings.Repeat("a", 64)
					}
					if kind == "encoded-size" {
						h.LODs[i].EncodedBytes++
					}
					if kind == "decoded-size" {
						h.LODs[i].DecodedBytes++
					}
				}
				c3h7SaveHeader(t, path, h)
			case "source-binding":
				lod, _, err := content.LoadCompiledAssetLOD(lodPath, nil)
				if err != nil {
					t.Fatal(err)
				}
				lod.SourceContentID = strings.Repeat("a", 64)
				info, err := content.SaveCompiledAssetLOD(lodPath, lod, nil)
				if err != nil {
					t.Fatal(err)
				}
				for i := range h.LODs {
					h.LODs[i].ContentID = info.ContentID
					h.LODs[i].EncodedBytes = info.EncodedBytes
					h.LODs[i].DecodedBytes = info.DecodedBytes
				}
				c3h7SaveHeader(t, path, h)
			case "wrong-or":
				// Independent typed fixture: source columns1,3,5 reduce to0,1,2;
				// omitting interior1 keeps extrema/count-capacity structurally valid.
				input, out, a := c3h4Fixture(t)
				for i := range a.Parts {
					if a.Parts[i].Source.VoxelShape != nil {
						var voxels []content.VoxelObjectVoxelDef
						for _, p := range c3h5Columns(1, 3, 5) {
							voxels = append(voxels, content.VoxelObjectVoxelDef{X: int(p[0]), Y: int(p[1]), Z: int(p[2]), Value: 3})
						}
						a.Parts[i].Source.VoxelShape.Voxels = voxels
					}
				}
				c3cWrite(t, input, a)
				out = strings.TrimSuffix(out, ".gkasset") + ".gkassetc"
				if _, err := CompileAuthoredAssetWithOptions(input, out, nil, CompiledAssetCompileOptions{EnableLOD2: true}); err != nil {
					t.Fatal(err)
				}
				path = out
				var err error
				h, _, err = content.LoadCompiledAssetHeader(path, nil)
				if err != nil {
					t.Fatal(err)
				}
				lodPath = c3h7LODPath(path, h)
				lod, _, err := content.LoadCompiledAssetLOD(lodPath, nil)
				if err != nil {
					t.Fatal(err)
				}
				wrong := c3h1Shape([3]int64{0, 0, 0}, [3]int64{2, 0, 0})
				for i := range wrong.Bricks {
					for j := range wrong.Bricks[i].Values {
						wrong.Bricks[i].Values[j] = 3
					}
				}
				lod.Bricks = wrong.Bricks
				info, err := content.SaveCompiledAssetLOD(lodPath, lod, nil)
				if err != nil {
					t.Fatal("wrong OR fixture must pass typed validator", err)
				}
				if _, _, err := content.LoadCompiledAssetLOD(lodPath, nil); err != nil {
					t.Fatal(err)
				}
				for i := range h.LODs {
					h.LODs[i].ContentID = info.ContentID
					h.LODs[i].EncodedBytes = info.EncodedBytes
					h.LODs[i].DecodedBytes = info.DecodedBytes
				}
				c3h7SaveHeader(t, path, h)
			}
			// Header alone intentionally does not follow derivative references.
			if _, _, err := content.LoadCompiledAssetHeader(path, nil); err != nil {
				t.Fatal("typed header fixture rejected", err)
			}
			for _, consumer := range []string{"direct", "nil-server", "packet", "first-part"} {
				t.Run(consumer, func(t *testing.T) {
					assets := c3d3Server()
					loader := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
					var err error
					switch consumer {
					case "direct":
						var result *PreparedAuthoredAsset
						result, err = LoadAndPrepareAuthoredAsset(path, assets, loader)
						if result != nil {
							t.Fatal("failure returned prepared asset")
						}
					case "nil-server":
						var result *PreparedAuthoredAsset
						result, err = LoadAndPrepareAuthoredAsset(path, nil, loader)
						if result != nil {
							t.Fatal("nil-server failure returned metadata")
						}
					case "packet":
						var packet *compiledAssetPacket
						packet, err = prepareCompiledAssetPacket(path, loader, nil)
						if packet != nil {
							packet.release()
							t.Fatal("bad closure returned worker packet")
						}
					case "first-part":
						id, palette, _, e := loadAuthoredLevelVoxelModel(assets, loader, path)
						err = e
						if id != (AssetId{}) || palette != (AssetId{}) {
							t.Fatal("failure returned global IDs")
						}
					}
					if err == nil {
						t.Fatal("declared derivative failure ignored")
					}
					var missing *authoredAssetInputLoadError
					if errors.As(err, &missing) {
						t.Fatal("present-header dependency failure classified as missing selected input")
					}
					if len(assets.voxModels) != 0 || len(assets.voxPalettes) != 0 {
						t.Fatal("bad declared derivative published global geometry/palettes")
					}
					if s := loader.Stats(); s.Entries != 0 || s.Bytes != 0 || s.PinnedBytes != 0 {
						t.Fatal("failed verification retained provisional decoded ownership", s)
					}
				})
			}
		})
	}
}

func TestC3h7LateLODChecksCancelAndOriginClosePreserveOtherPins(t *testing.T) {
	for _, kind := range []string{"cancel", "origin-close"} {
		t.Run(kind, func(t *testing.T) {
			path, h := c3h7Fixture(t)
			h.LODs = h.LODs[:1]
			c3h7SaveHeader(t, path, h)
			owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
			guard := owner.NewScope()
			defer guard.Close()
			_, unrelated, _, _ := c3d1Paths(t, nil)
			held, _, err := guard.Loader().LoadCompiledAssetShape(unrelated)
			if err != nil {
				t.Fatal(err)
			}
			guardStats := owner.Stats()
			caller := owner.NewScope()
			defer caller.Close()
			header, _, err := caller.Loader().LoadCompiledAssetHeader(path)
			if err != nil {
				t.Fatal(err)
			}
			baseline := owner.Stats()
			observedLOD := false
			session, err := verifyCompiledAssetInput(path, caller.Loader(), func() bool {
				if owner.Stats().Entries >= baseline.Entries+2 {
					observedLOD = true
					if kind == "origin-close" {
						caller.Close()
						return false
					}
					return true
				}
				return false
			})
			if err == nil || session != nil || !observedLOD {
				t.Fatal("last LOD load/proof lacks cancellation/origin boundary", err)
			}
			want := baseline
			if kind == "origin-close" {
				want = guardStats
			}
			after := owner.Stats()
			if after.Entries != want.Entries || after.Bytes != want.Bytes || after.PinnedBytes != want.PinnedBytes {
				t.Fatal("late rejection leaked pins or revoked other scope", after, want)
			}
			if hit, _, err := guard.Loader().LoadCompiledAssetShape(unrelated); err != nil || hit != held {
				t.Fatal("unrelated caller pin revoked", err)
			}
			if kind == "cancel" {
				if hit, _, err := caller.Loader().LoadCompiledAssetHeader(path); err != nil || hit != header {
					t.Fatal("cancel revoked caller header", err)
				}
			}
		})
	}
}

func TestC3h7LegacyAndEmptyLODHeaderVerification(t *testing.T) {
	for _, kind := range []string{"legacy", "v2-empty"} {
		input, path, _ := c3h4Fixture(t)
		path = strings.TrimSuffix(path, ".gkasset") + ".gkassetc"
		if _, err := CompileAuthoredAsset(input, path, nil); err != nil {
			t.Fatal(err)
		}
		h, _, err := content.LoadCompiledAssetHeader(path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if kind == "v2-empty" {
			h.SchemaVersion = 2
			h.CompilerVersion = content.CompiledAssetLODHeaderCompilerVersion
			c3h7SaveHeader(t, path, h)
		}
		raw := c3cRead(t, path)
		session, err := verifyCompiledAssetInput(path, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(session.lods) != 0 || len(session.shapes) != 2 {
			t.Fatal("legacy/empty header unexpectedly adds derivative")
		}
		for _, ref := range h.Shapes {
			if session.shapes[ref.PartID].baseIdentity != ref.BaseIdentity {
				t.Fatal("legacy base proof changed")
			}
		}
		session.close()
		if !bytes.Equal(raw, c3cRead(t, path)) {
			t.Fatal("verification changed header bytes")
		}
	}
	// Only the selected header load failure retains the input-load classification.
	_, err := verifyCompiledAssetInput(filepath.Join(t.TempDir(), "missing.gkassetc"), nil, nil)
	var inputError *authoredAssetInputLoadError
	if !errors.As(err, &inputError) {
		t.Fatal("missing selected input classification changed", err)
	}
}
