package gekko

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func e2b1Part(resolution float32) content.AssetPartDef {
	return content.AssetPartDef{ID: "shape", ModelScale: 2, VoxelResolution: resolution, Source: content.AssetSourceDef{Kind: content.AssetSourceKindVoxelShape, VoxelShape: &content.AssetVoxelShapeDef{Voxels: []content.VoxelObjectVoxelDef{{X: -9, Value: 2}, {X: -9, Value: 0}, {X: -8, Y: 1, Value: 3}, {X: 1, Value: 4}}}}}
}

// Reconstruct from authored records independently of the cache being qualified.
func e2b1Canonical(t *testing.T, part content.AssetPartDef) (*content.VoxelObjectSnapshotDef, content.VoxelObjectLatticeDef, string) {
	t.Helper()
	base := XBrickMapFromVoxelObjectSnapshot(&content.VoxelObjectSnapshotDef{SchemaVersion: 1, Voxels: append([]content.VoxelObjectVoxelDef(nil), part.Source.VoxelShape.Voxels...)})
	base = base.Resample(part.ModelScale)
	snapshot := VoxelObjectSnapshotFromXBrickMap(base)
	lattice := content.VoxelObjectLatticeDef{VoxelResolution: VoxelResolutionOrDefault(&VoxelModelComponent{VoxelResolution: part.VoxelResolution}), RasterizationVersion: authoredVoxelShapeRasterizationVersion}
	identity, _, err := content.VoxelObjectBaseIdentity(snapshot, lattice, nil)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, lattice, identity
}

func e2b1Add(t *testing.T, f *s3cVoxelFixture, geometry AssetId, resolution float32) EntityId {
	t.Helper()
	model := f.voxelModel()
	model.VoxelModel = geometry
	model.VoxelResolution = resolution
	eid := f.cmd.AddEntity(s3cTransform(0), model)
	f.app.FlushCommands()
	p1dEnable(t, f, eid)
	f.app.FlushCommands()
	return eid
}

func e2b1Base(t *testing.T, f *s3cVoxelFixture, eid EntityId, identity string, lattice content.VoxelObjectLatticeDef) {
	t.Helper()
	got, gotLattice, ok := managedVoxelGeometryBase(f.cmd, f.server, eid)
	if !ok || got != identity || gotLattice != lattice {
		t.Fatalf("base=(%q,%+v,%v), want (%q,%+v,true)", got, gotLattice, ok, identity, lattice)
	}
}

func e2b1Fallback(t *testing.T, f *s3cVoxelFixture, eid EntityId) {
	t.Helper()
	identity, lattice, ok := managedVoxelGeometryBase(f.cmd, f.server, eid)
	if ok || identity != "" || lattice != (content.VoxelObjectLatticeDef{}) {
		t.Fatalf("full fallback returned provenance (%q,%+v,%v)", identity, lattice, ok)
	}
}

func TestE2b1ScaledAuthoredShapeBaseTwoLatticesAndTrackedIsolation(t *testing.T) {
	f := newS3cVoxelFixture(t)
	f.cmd.AddResources(f.server, f.state)
	part := e2b1Part(0.25)
	before := append([]content.VoxelObjectVoxelDef(nil), part.Source.VoxelShape.Voxels...)
	geometry, err := authoredVoxelShapeGeometry(f.server, part)
	if err != nil {
		t.Fatal(err)
	}
	_, lattice, identity := e2b1Canonical(t, part)
	first := e2b1Add(t, f, geometry, part.VoxelResolution)
	e2b1Base(t, f, first, identity, lattice)
	secondPart := part
	secondPart.VoxelResolution = 0.5
	warmGeometry, err := authoredVoxelShapeGeometry(f.server, secondPart)
	if err != nil || warmGeometry != geometry {
		t.Fatal("resolution changed existing authored geometry cache key", err)
	}
	_, secondLattice, secondIdentity := e2b1Canonical(t, secondPart)
	if secondIdentity == identity {
		t.Fatal("construction identity failed to bind resolution")
	}
	second := e2b1Add(t, f, geometry, secondPart.VoxelResolution)
	e2b1Base(t, f, second, secondIdentity, secondLattice)
	p1dApply(t, f, first, volume.VoxelWrite{X: 40, Value: 7})
	e2b1Base(t, f, first, identity, lattice)
	asset, _ := f.server.GetVoxelGeometry(geometry)
	asset.XBrickMap.SetVoxel(40, 0, 0, 9)
	current, _, _ := currentVoxelMapForEntity(f.cmd, first)
	p1dVoxel(t, current, 40, 7)
	e2b1Base(t, f, first, identity, lattice)
	e2b1Base(t, f, second, secondIdentity, secondLattice)
	vmc := s3cComponent[VoxelModelComponent](t, f.cmd, first)
	vmc.VoxelResolution = secondPart.VoxelResolution
	e2b1Fallback(t, f, first)
	vmc.VoxelResolution = part.VoxelResolution
	e2b1Base(t, f, first, identity, lattice)
	if !reflect.DeepEqual(part.Source.VoxelShape.Voxels, before) {
		t.Fatal("provenance construction mutated authored shape input")
	}
}

func TestE2b1WarmNewLatticeUsesAuthoredBaseRatherThanPoisonedCache(t *testing.T) {
	f := newS3cVoxelFixture(t)
	f.cmd.AddResources(f.server, f.state)
	part := e2b1Part(0.25)
	geometry, err := authoredVoxelShapeGeometry(f.server, part)
	if err != nil {
		t.Fatal(err)
	}
	base, _, _ := e2b1Canonical(t, part)
	cell := base.Voxels[0]
	asset, _ := f.server.GetVoxelGeometry(geometry)
	asset.XBrickMap.SetVoxel(cell.X, cell.Y, cell.Z, 9)
	part.VoxelResolution = 0.5
	warm, err := authoredVoxelShapeGeometry(f.server, part)
	if err != nil || warm != geometry {
		t.Fatal("warm lattice changed geometry cache identity", err)
	}
	poisoned := e2b1Add(t, f, geometry, part.VoxelResolution)
	e2b1Fallback(t, f, poisoned)
	p1dChanges(t, f, poisoned)
	// Restoring the source must qualify against freshly reconstructed authored
	// geometry, proving a warm metadata miss did not hash the poisoned cache.
	asset.XBrickMap.SetVoxel(cell.X, cell.Y, cell.Z, cell.Value)
	_, lattice, identity := e2b1Canonical(t, part)
	restored := e2b1Add(t, f, geometry, part.VoxelResolution)
	e2b1Base(t, f, restored, identity, lattice)
}

func TestE2b1ExposureForeignResealingAndUnboundSourcesDoNotInheritBase(t *testing.T) {
	f := newS3cVoxelFixture(t)
	f.cmd.AddResources(f.server, f.state)
	part := e2b1Part(0.25)
	geometry, err := authoredVoxelShapeGeometry(f.server, part)
	if err != nil {
		t.Fatal(err)
	}
	owner := e2b1Add(t, f, geometry, part.VoxelResolution)
	ownerOverride := s3cComponent[VoxelModelComponent](t, f.cmd, owner).OverrideGeometry
	model := f.voxelModel()
	model.OverrideGeometry, model.VoxelResolution = ownerOverride, part.VoxelResolution
	foreign := f.cmd.AddEntity(s3cTransform(20), model)
	f.app.FlushCommands()
	p1dEnable(t, f, foreign)
	f.app.FlushCommands()
	e2b1Fallback(t, f, foreign)
	_, _ = f.server.GetVoxelGeometry(ownerOverride)
	e2b1Fallback(t, f, owner)
	if err := EnableManagedVoxelGeometry(f.cmd, f.server, owner); err != nil {
		t.Fatal(err)
	}
	e2b1Fallback(t, f, owner)
	canonical, _, _ := e2b1Canonical(t, part)
	for _, kind := range []string{"managed", "snapshot", "exposed-managed"} {
		var unbound AssetId
		source := XBrickMapFromVoxelObjectSnapshot(canonical)
		if kind == "snapshot" {
			unbound = f.server.RegisterSharedVoxelGeometry(source, "unbound")
		} else {
			unbound = f.server.RegisterManagedVoxelGeometry(source, "unbound")
			if kind == "exposed-managed" {
				_, _ = f.server.GetVoxelGeometry(unbound)
			}
		}
		eid := e2b1Add(t, f, unbound, part.VoxelResolution)
		e2b1Fallback(t, f, eid)
	}
}

func TestE2b1DeleteAuthoredSourceReleasesProvenanceSidecar(t *testing.T) {
	f := newS3cVoxelFixture(t)
	part := e2b1Part(0.25)
	geometry, err := authoredVoxelShapeGeometry(f.server, part)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.server.authoredVoxelBases[geometry]) == 0 {
		t.Fatal("authored source failed to record construction provenance")
	}
	owner := e2b1Add(t, f, geometry, part.VoxelResolution)
	_, lattice, identity := e2b1Canonical(t, part)
	ownerOverride := s3cComponent[VoxelModelComponent](t, f.cmd, owner).OverrideGeometry
	e2b1Base(t, f, owner, identity, lattice)
	if !f.server.DeleteVoxelGeometry(geometry) {
		t.Fatal("authored geometry deletion failed")
	}
	if _, exists := f.server.authoredVoxelBases[geometry]; exists {
		t.Fatal("deleted geometry retained provenance sidecar")
	}
	e2b1Base(t, f, owner, identity, lattice)
	p1dApply(t, f, owner, volume.VoxelWrite{X: 40, Value: 7})
	e2b1Base(t, f, owner, identity, lattice)
	p1dChanges(t, f, owner, volume.VoxelWrite{X: 40, Value: 7})
	if !f.server.DeleteVoxelGeometry(ownerOverride) {
		t.Fatal("independently owned override deletion failed")
	}
	e2b1Fallback(t, f, owner)
	replacement, err := authoredVoxelShapeGeometry(f.server, part)
	if err != nil || replacement == geometry {
		t.Fatal("deleted cache identity was reattached", err)
	}
	eid := e2b1Add(t, f, replacement, part.VoxelResolution)
	e2b1Base(t, f, eid, identity, lattice)
}

func TestE2b1WarmCacheWithoutAnyProvenanceReconstructsAuthoredBase(t *testing.T) {
	f := newS3cVoxelFixture(t)
	part := e2b1Part(0.25)
	base, lattice, identity := e2b1Canonical(t, part)
	key, err := json.Marshal(struct {
		Scale float32                     `json:"scale"`
		Shape *content.AssetVoxelShapeDef `json:"shape"`
	}{Scale: part.ModelScale, Shape: part.Source.VoxelShape})
	if err != nil {
		t.Fatal(err)
	}
	geometry := f.server.RegisterSharedVoxelGeometryWithCacheKey(string(key), XBrickMapFromVoxelObjectSnapshot(base), "legacy-cache")
	if len(f.server.authoredVoxelBases[geometry]) != 0 {
		t.Fatal("fixture unexpectedly has authored provenance")
	}
	asset, _ := f.server.GetVoxelGeometry(geometry)
	cell := base.Voxels[0]
	asset.XBrickMap.SetVoxel(cell.X, cell.Y, cell.Z, 9)
	warm, err := authoredVoxelShapeGeometry(f.server, part)
	if err != nil || warm != geometry {
		t.Fatal("unqualified legacy warm cache changed identity", err)
	}
	poisoned := e2b1Add(t, f, geometry, part.VoxelResolution)
	e2b1Fallback(t, f, poisoned)
	asset.XBrickMap.SetVoxel(cell.X, cell.Y, cell.Z, cell.Value)
	restored := e2b1Add(t, f, geometry, part.VoxelResolution)
	e2b1Base(t, f, restored, identity, lattice)
}

func TestE2b1DefaultLatticeAndQualificationLoss(t *testing.T) {
	f := newS3cVoxelFixture(t)
	part := e2b1Part(0)
	geometry, err := authoredVoxelShapeGeometry(f.server, part)
	if err != nil {
		t.Fatal(err)
	}
	_, lattice, identity := e2b1Canonical(t, part)
	if lattice.VoxelResolution != 0.1 {
		t.Fatal("default fixture did not use existing effective resolution")
	}
	eid := e2b1Add(t, f, geometry, 0)
	e2b1Base(t, f, eid, identity, lattice)
	vmc := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
	vmc.VoxelResolution = float32(math.NaN())
	e2b1Base(t, f, eid, identity, lattice)
	vmc.VoxelResolution = float32(math.Inf(1))
	e2b1Fallback(t, f, eid)
	part.VoxelResolution = float32(math.Inf(1))
	if warm, err := authoredVoxelShapeGeometry(f.server, part); err != nil || warm != geometry {
		t.Fatal("best-effort identity failure changed legacy geometry construction", err)
	}
	vmc.VoxelResolution = 0
	vmc.IsTerrainChunk = true
	e2b1Fallback(t, f, eid)
	vmc.IsTerrainChunk = false
	e2b1Base(t, f, eid, identity, lattice)
	model := *vmc
	model.OverrideGeometry = f.server.RegisterManagedVoxelGeometry(volume.NewXBrickMap(), "unbound-replacement")
	f.cmd.AddComponents(eid, model)
	e2b1Fallback(t, f, eid)
}
