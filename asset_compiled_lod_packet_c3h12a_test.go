package gekko

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func c3h12aGeometry(v *volume.XBrickMap) map[[3]int]uint8 {
	return c3cGeometry(VoxelObjectSnapshotFromXBrickMap(v).Voxels)
}

func c3h12aIndependentStorage(t *testing.T, owners ...*volume.XBrickMap) {
	t.Helper()
	type span struct {
		start, end uintptr
		owner      int
	}
	var spans []span
	add := func(start, size uintptr, owner int) {
		if start == 0 || size == 0 {
			return
		}
		for _, prior := range spans {
			if prior.owner != owner && start < prior.end && prior.start < start+size {
				t.Fatalf("nested geometry storage shared by owners %d and %d", prior.owner, owner)
			}
		}
		spans = append(spans, span{start, start + size, owner})
	}
	for i, geometry := range owners {
		add(uintptr(unsafe.Pointer(geometry)), unsafe.Sizeof(*geometry), i)
		for _, table := range []any{geometry.Sectors, geometry.DirtySectors, geometry.DirtyBricks, geometry.SectorRevisions} {
			add(reflect.ValueOf(table).Pointer(), 1, i)
		}
		for _, sector := range geometry.Sectors {
			add(uintptr(unsafe.Pointer(sector)), unsafe.Sizeof(*sector), i)
			if cap(sector.PackedBricks) > 0 {
				add(uintptr(unsafe.Pointer(unsafe.SliceData(sector.PackedBricks))), uintptr(cap(sector.PackedBricks))*unsafe.Sizeof((*volume.Brick)(nil)), i)
			}
			for _, brick := range sector.PackedBricks {
				add(uintptr(unsafe.Pointer(brick)), unsafe.Sizeof(*brick), i)
				if cap(brick.PrecomputedAux) > 0 {
					add(uintptr(unsafe.Pointer(unsafe.SliceData(brick.PrecomputedAux))), uintptr(cap(brick.PrecomputedAux)), i)
				}
			}
		}
	}
}

func c3h12aPacket(t *testing.T, path string, loader *RuntimeContentLoader) *compiledAssetPacket {
	t.Helper()
	packet, err := prepareCompiledAssetPacket(path, loader, nil)
	if err != nil || packet == nil {
		t.Fatal("packet preparation failed", err)
	}
	t.Cleanup(packet.release)
	return packet
}

func TestC3h12aPacketOwnsVerifiedDerivativeAndIndependentProof(t *testing.T) {
	path, header := c3h7Fixture(t)
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
	caller := owner.NewScope()
	defer caller.Close()
	cachedLOD, _, err := caller.Loader().LoadCompiledAssetLOD(c3h7LODPath(path, header))
	if err != nil {
		t.Fatal(err)
	}
	cachedFull, _, err := caller.Loader().LoadCompiledAssetShape(filepath.Join(filepath.Dir(path), filepath.FromSlash(header.Shapes[0].Path)))
	if err != nil {
		t.Fatal(err)
	}
	before := owner.Stats()
	lodBytes, _ := json.Marshal(cachedLOD)
	fullBytes, _ := json.Marshal(cachedFull)
	packet := c3h12aPacket(t, path, caller.Loader())
	if len(packet.partLODs) != 2 || len(packet.lods) != 1 || packet.partLODs["shape"] != packet.partLODs["duplicate"] {
		t.Fatal("eligible duplicate parts did not share one derivative owner")
	}
	if _, exists := packet.partLODs["root"]; exists {
		t.Fatal("group acquired derivative")
	}
	lod := packet.lods[packet.partLODs["shape"]]
	full := packet.shapes[packet.parts["shape"]]
	if lod == nil || lod.source == nil || lod.registration == nil || lod.proof == nil || lod.proof.full == nil || lod.proof.coarse == nil {
		t.Fatal("incomplete derivative ownership")
	}
	if lod.contentID != header.LODs[0].ContentID || lod.sourceContentID != full.contentID || lod.value != cachedLOD.Value || lod.proof.contentID != lod.contentID || lod.proof.sourceContentID != full.contentID || lod.proof.lattice != cachedFull.Lattice || lod.proof.value != lod.value {
		t.Fatal("derivative proof lost authenticated identity/lattice/value")
	}
	// Fixed signed oracle is independent of runtime reduction and construction.
	wantCoarse := map[[3]int]uint8{{-1, 0, 0}: 3, {0, 0, 0}: 3}
	wantFull := c3cGeometry(c3cSnapshot(cachedFull).Voxels)
	fromFrame := c3cGeometry(c3cSnapshot(&content.CompiledAssetShapeDef{Bricks: cachedLOD.Bricks}).Voxels)
	if !reflect.DeepEqual(fromFrame, wantCoarse) || !reflect.DeepEqual(c3h12aGeometry(lod.source), fromFrame) || !reflect.DeepEqual(c3h12aGeometry(lod.proof.coarse), fromFrame) || !reflect.DeepEqual(c3h12aGeometry(lod.proof.full), wantFull) {
		t.Fatal("packet geometry differs from exact verified signed frames")
	}
	independent := []*volume.XBrickMap{full.source, full.registration.geometry, lod.source, lod.registration.geometry, lod.proof.full, lod.proof.coarse}
	for i, a := range independent {
		for j, b := range independent {
			if i != j && (a == b || reflect.ValueOf(a.Sectors).Pointer() == reflect.ValueOf(b.Sectors).Pointer()) {
				t.Fatal("packet owners share mutable map storage", i, j)
			}
		}
	}
	if count, ok := lod.registration.countFor(lod.source); !ok || count != len(wantCoarse) {
		t.Fatal("derivative registration is not bound to owned source")
	}
	second := c3h12aPacket(t, path, caller.Loader())
	other := second.lods[second.partLODs["shape"]]
	if other == lod || other.source == lod.source || other.registration == lod.registration || other.proof == lod.proof || other.proof.full == lod.proof.full || other.proof.coarse == lod.proof.coarse {
		t.Fatal("independent preparations shared ownership")
	}
	secondFull := second.shapes[second.parts["shape"]]
	c3h12aIndependentStorage(t, append(independent, secondFull.source, secondFull.registration.geometry, other.source, other.registration.geometry, other.proof.full, other.proof.coarse)...)
	// Mutate occupied voxels to detect nested sector/brick/value aliases too.
	full.source.SetVoxel(-2, 0, 0, 91)
	lod.source.SetVoxel(-1, 0, 0, 92)
	if !reflect.DeepEqual(c3h12aGeometry(lod.proof.full), wantFull) || !reflect.DeepEqual(c3h12aGeometry(lod.proof.coarse), wantCoarse) || !reflect.DeepEqual(c3h12aGeometry(other.source), wantCoarse) || !reflect.DeepEqual(c3h12aGeometry(other.proof.full), wantFull) {
		t.Fatal("ordinary source mutation changed proof or another packet")
	}
	registered, _, _, _, ok := lod.registration.take(lod.source)
	if !ok || !reflect.DeepEqual(c3h12aGeometry(registered.XBrickMap), wantCoarse) {
		t.Fatal("source aliases registration nested geometry")
	}
	fullRegistered, _, _, _, ok := full.registration.take(full.source)
	if !ok || !reflect.DeepEqual(c3h12aGeometry(fullRegistered.XBrickMap), wantFull) {
		t.Fatal("full source aliases registration or baseline")
	}
	afterLOD, _ := json.Marshal(cachedLOD)
	afterFull, _ := json.Marshal(cachedFull)
	if !bytes.Equal(lodBytes, afterLOD) || !bytes.Equal(fullBytes, afterFull) {
		t.Fatal("packet mutation leaked into decoded cache")
	}
	if after := owner.Stats(); after.Entries != before.Entries || after.Bytes != before.Bytes || after.PinnedBytes != before.PinnedBytes {
		t.Fatal("packet leaked child pins or revoked caller pins", after, before)
	}
	second.release()
	second.release()
	if other.registration.charge() != 0 {
		t.Fatal("idempotent release retained derivative registration")
	}
	if !reflect.DeepEqual(c3h12aGeometry(other.proof.coarse), wantCoarse) || !reflect.DeepEqual(c3h12aGeometry(other.source), wantCoarse) {
		t.Fatal("release erased retained immutable source/proof")
	}
	caller.Close()
	if s := owner.Stats(); s.Entries != 0 || s.Bytes != 0 || s.PinnedBytes != 0 {
		t.Fatal("packet kept decoded ownership", s)
	}
}

func TestC3h12aPartMembershipAndAbsentDerivativeTables(t *testing.T) {
	for _, kind := range []string{"one-eligible", "legacy", "v2-empty", "group-only"} {
		t.Run(kind, func(t *testing.T) {
			input, output, asset := c3h4Fixture(t)
			output = strings.TrimSuffix(output, ".gkasset") + ".gkassetc"
			if kind == "one-eligible" {
				for i := range asset.Parts {
					if asset.Parts[i].ID == "duplicate" {
						asset.Parts[i].Source.VoxelShape.Palette[0].MaterialID = "unused"
					}
				}
			}
			if kind == "group-only" {
				asset.Parts = asset.Parts[1:2]
				asset.Markers = nil
				asset.Emitters = nil
				asset.AnimationSetPaths = asset.AnimationSetPaths[:1]
			}
			c3cWrite(t, input, asset)
			if _, err := CompileAuthoredAssetWithOptions(input, output, nil, CompiledAssetCompileOptions{EnableLOD2: kind == "one-eligible"}); err != nil {
				t.Fatal(err)
			}
			if kind == "v2-empty" {
				h, _, err := content.LoadCompiledAssetHeader(output, nil)
				if err != nil {
					t.Fatal(err)
				}
				h.SchemaVersion = 2
				h.CompilerVersion = content.CompiledAssetLODHeaderCompilerVersion
				c3h7SaveHeader(t, output, h)
			}
			owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
			packet := c3h12aPacket(t, output, owner)
			if kind == "one-eligible" {
				if len(packet.partLODs) != 1 || len(packet.lods) != 1 || packet.partLODs["shape"] == "" || packet.partLODs["duplicate"] != "" {
					t.Fatal("per-part eligibility was widened through full source sharing")
				}
				if packet.parts["shape"] != packet.parts["duplicate"] || packet.shapes[packet.parts["shape"]].source.GetVoxelCount() != 4 {
					t.Fatal("full-detail part authority changed")
				}
			} else if packet.partLODs != nil || packet.lods != nil {
				t.Fatal("nonLOD packet allocated derivative tables/baselines")
			}
			if s := owner.Stats(); s.Entries != 0 || s.Bytes != 0 || s.PinnedBytes != 0 {
				t.Fatal("packet retained provisional decode scope", s)
			}
		})
	}
}

func TestC3h12aLateDerivativeRejectionPreservesUnrelatedPins(t *testing.T) {
	for _, kind := range []string{"cancel", "origin-close", "closed-origin", "terminal-cancel", "terminal-origin-close"} {
		t.Run(kind, func(t *testing.T) {
			path, _ := c3h7Fixture(t)
			owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
			guard := owner.NewScope()
			defer guard.Close()
			_, unrelated, _, _ := c3d1Paths(t, nil)
			held, _, err := guard.Loader().LoadCompiledAssetShape(unrelated)
			if err != nil {
				t.Fatal(err)
			}
			baseline := owner.Stats()
			caller := owner.NewScope()
			defer caller.Close()
			if kind == "closed-origin" {
				caller.Close()
			}
			terminalCalls := 0
			if strings.HasPrefix(kind, "terminal-") {
				probe, err := prepareCompiledAssetPacket(path, caller.Loader(), func() bool { terminalCalls++; return false })
				if err != nil || probe == nil {
					t.Fatal("terminal boundary probe failed", err)
				}
				if len(probe.lods) != 1 || probe.lods[probe.partLODs["shape"]].registration.charge() <= 0 {
					t.Fatal("terminal probe lacks completed derivative construction")
				}
				probe.release()
			}
			calls := 0
			observedLOD := false
			cancel := func() bool {
				calls++
				// The last cooperative boundary is after all CPU packet construction.
				// Derive its position from a successful preparation of the same closure.
				if terminalCalls > 0 {
					if calls == terminalCalls {
						observedLOD = true
						if kind == "terminal-origin-close" {
							caller.Close()
							return false
						}
						return true
					}
					return false
				}
				if owner.Stats().Entries >= baseline.Entries+3 {
					observedLOD = true
					if kind == "origin-close" {
						caller.Close()
						return false
					}
					return true
				}
				return false
			}
			packet, err := prepareCompiledAssetPacket(path, caller.Loader(), cancel)
			if packet != nil {
				packet.release()
			}
			if err == nil || packet != nil || (kind != "closed-origin" && !observedLOD) {
				t.Fatal("derivative rejection returned partial output or missed late boundary", err)
			}
			if after := owner.Stats(); after.Entries != baseline.Entries || after.Bytes != baseline.Bytes || after.PinnedBytes != baseline.PinnedBytes {
				t.Fatal("rejection leaked ownership or revoked unrelated pin", after, baseline)
			}
			if got, _, err := guard.Loader().LoadCompiledAssetShape(unrelated); err != nil || got != held {
				t.Fatal("rejection revoked unrelated caller", err)
			}
		})
	}
}

// Independent accounting oracle: expose only the scalar graph to the generic
// estimator and explicitly sum each unique volume and pending handle once.
func c3h12aChargeOracle(packet *compiledAssetPacket) int64 {
	metadata := *packet
	if packet.lods != nil {
		metadata.lods = make(map[string]*compiledAssetPacketLOD, len(packet.lods))
	}
	seenLODs := map[*compiledAssetPacketLOD]*compiledAssetPacketLOD{}
	seenProofs := map[*compiledAssetLODProof]*compiledAssetLODProof{}
	sources := map[*volume.XBrickMap]bool{}
	registrations := map[*streamedGeometryRegistration]bool{}
	var storage int64
	addSource := func(source *volume.XBrickMap) {
		if !sources[source] {
			sources[source] = true
			storage += streamedPendingGeometryCharge(source)
		}
	}
	metadata.shapes = nil
	if packet.shapes != nil {
		metadata.shapes = make(map[string]*compiledAssetPacketShape, len(packet.shapes))
		for key, shape := range packet.shapes {
			if shape == nil {
				metadata.shapes[key] = nil
				continue
			}
			copy := *shape
			copy.source = nil
			copy.registration = nil
			metadata.shapes[key] = &copy
			addSource(shape.source)
			if shape.registration != nil && !registrations[shape.registration] {
				registrations[shape.registration] = true
				storage += int64(unsafe.Sizeof(streamedGeometryRegistration{})) + shape.registration.charge()
			}
		}
	}

	for key, lod := range packet.lods {
		if lod == nil {
			metadata.lods[key] = nil
			continue
		}
		if cloned, ok := seenLODs[lod]; ok {
			metadata.lods[key] = cloned
			continue
		}
		clone := *lod
		clone.source = nil
		clone.registration = nil
		seenLODs[lod] = &clone
		metadata.lods[key] = &clone
		addSource(lod.source)
		if lod.registration != nil && !registrations[lod.registration] {
			registrations[lod.registration] = true
			storage += int64(unsafe.Sizeof(streamedGeometryRegistration{})) + lod.registration.charge()
		}
		if lod.proof != nil {
			if copied, ok := seenProofs[lod.proof]; ok {
				clone.proof = copied
			} else {
				proof := *lod.proof
				proof.full = nil
				proof.coarse = nil
				clone.proof = &proof
				seenProofs[lod.proof] = &proof
				addSource(lod.proof.full)
				addSource(lod.proof.coarse)
			}
		}
	}
	return storage + runtimeContentGraphCharge(&metadata) + runtimeContentGraphCharge(map[string]*compiledAssetPacket{"packet": nil})
}

func TestC3h12aDerivativeAccountingExactAliasesAndConsumption(t *testing.T) {
	path, _ := c3h7Fixture(t)
	real := c3h12aPacket(t, path, nil)
	lod := real.lods[real.partLODs["shape"]]
	// Isolate the new owner graph from already-covered ordinary palettes/shapes.
	packet := &compiledAssetPacket{partLODs: real.partLODs, lods: real.lods}
	charge := func() int64 {
		return streamedCompiledAssetPacketsCharge(map[string]*compiledAssetPacket{"packet": packet})
	}
	if got, want := charge(), c3h12aChargeOracle(packet); got != want {
		t.Fatalf("derivative charge=%d independent owned-storage oracle=%d", got, want)
	}
	before := charge()
	// Exact pointer aliases add scalar table entries, never extra volume storage.
	packet.lods["alias"] = lod
	second := *lod
	packet.lods["shared-proof"] = &second
	if got, want := charge(), c3h12aChargeOracle(packet); got != want {
		t.Fatalf("source/registration/proof aliases charged twice: got=%d want=%d", got, want)
	}
	oneKey := runtimeContentGraphCharge(map[string]*compiledAssetPacket{"packet": nil})
	twoKeys := runtimeContentGraphCharge(map[string]*compiledAssetPacket{"packet": nil, "alias": nil})
	if got := streamedCompiledAssetPacketsCharge(map[string]*compiledAssetPacket{"packet": packet, "alias": packet}); got != charge()+twoKeys-oneKey {
		t.Fatal("aliased packet duplicated owner storage")
	}
	// Also exercise exact volume aliases between baseline and derivative source.
	second.proof = &compiledAssetLODProof{sourceContentID: lod.sourceContentID, contentID: lod.contentID, lattice: lod.proof.lattice, value: lod.value, full: lod.proof.full, coarse: lod.source}
	if got, want := charge(), c3h12aChargeOracle(packet); got != want {
		t.Fatalf("cross-proof volume aliases charged twice: got=%d want=%d", got, want)
	}
	packet.shapes = map[string]*compiledAssetPacketShape{"proof-alias": {source: lod.proof.full}, "source-alias": {source: lod.source, registration: lod.registration}}
	if got, want := charge(), c3h12aChargeOracle(packet); got != want {
		t.Fatalf("full/derivative/proof cross-owner aliases charged twice: got=%d want=%d", got, want)
	}
	pending := lod.registration.charge()
	if pending <= 0 || before <= pending {
		t.Fatal("fixture lacks retained source/proof and pending charge")
	}
	charged := charge()
	_, _, _, _, ok := lod.registration.take(lod.source)
	if !ok || charge() != charged-pending || charge() != c3h12aChargeOracle(packet) {
		t.Fatal("consuming derivative drained source/proof charge or retained pending storage")
	}
	packet.release()
	packet.release()
	if lod.registration.charge() != 0 || charge() != charged-pending {
		t.Fatal("release changed immutable baseline or revived consumed charge")
	}
}

func TestC3h12aFullHandleMetadataRetainedAfterRelease(t *testing.T) {
	source := volume.NewXBrickMap()
	registration := prepareStreamedGeometryRegistration(source)
	defer registration.release()
	shape := &compiledAssetPacketShape{source: source, registration: registration}
	packet := &compiledAssetPacket{shapes: map[string]*compiledAssetPacketShape{"empty": shape}}
	charge := func() int64 {
		return streamedCompiledAssetPacketsCharge(map[string]*compiledAssetPacket{"packet": packet})
	}
	pending := registration.charge()
	before := charge()
	if got, want := before, c3h12aChargeOracle(packet); got != want {
		t.Fatalf("full handle metadata omitted: got=%d want=%d", got, want)
	}
	registration.release()
	if got, want := charge(), before-pending; got != want || got != c3h12aChargeOracle(packet) {
		t.Fatalf("release removed retained fixed handle metadata: got=%d want=%d", got, want)
	}
	shape.registration = nil
	if got := charge(); got != before-pending-int64(unsafe.Sizeof(streamedGeometryRegistration{})) {
		t.Fatal("consumed handle fixed metadata not charged exactly once")
	}
}
