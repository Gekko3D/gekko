package gekko

import (
	"bytes"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func p5aRuntime(t *testing.T, proxy, auxiliary bool) (*streamedRenderHarness, EntityId, []byte) {
	t.Helper()
	root := t.TempDir()
	auxBytes := make([]byte, volume.VoxelAuxRecordBytes)
	auxBytes[len(auxBytes)-1] = 42
	cmd, state, assets := s1dRuntime(t, []s1dChunkSpec{{coord: ChunkCoord{}, proxy: proxy}, {coord: ChunkCoord{X: 1}}}, 1, 1, 1, func(world *content.ImportedWorldDef) {
		for index := range world.Entries {
			entry := &world.Entries[index]
			chunk := &content.ImportedWorldChunkDef{
				WorldID: world.WorldID, Coord: entry.Coord, SchemaVersion: content.CurrentImportedWorldChunkSchemaVersion,
				ChunkSize: 16, VoxelResolution: 1,
			}
			for _, x := range []int{1, 9} {
				for _, y := range []int{2, 10} {
					for _, z := range []int{3, 11} {
						chunk.Voxels = append(chunk.Voxels, content.ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
					}
				}
			}
			chunk.NonEmptyVoxelCount = len(chunk.Voxels)
			path := filepath.Join(root, fmt.Sprintf("chunk_%d.gkchunk", index))
			if err := content.SaveImportedWorldChunk(path, chunk); err != nil {
				t.Fatal(err)
			}
			entry.ChunkPath, entry.NonEmptyVoxelCount = path, chunk.NonEmptyVoxelCount
			entry.PayloadHash, entry.PayloadSizeBytes = chunk.PayloadHash, chunk.PayloadSizeBytes
			if auxiliary {
				aux := &content.ImportedWorldChunkAuxDef{
					WorldID: world.WorldID, Coord: entry.Coord, ChunkSize: 16, VoxelResolution: 1,
					SourcePayloadHash: chunk.PayloadHash, SourcePayloadSizeBytes: chunk.PayloadSizeBytes,
					Records: []content.ImportedWorldBrickAuxDef{{Origin: [3]int{}, Bytes: append([]byte(nil), auxBytes...)}},
				}
				auxPath := filepath.Join(root, fmt.Sprintf("chunk_%d.gkaux", index))
				if err := content.SaveImportedWorldChunkAux(auxPath, aux); err != nil {
					t.Fatal(err)
				}
				entry.Aux = content.ImportedWorldChunkAuxRef(auxPath, aux)
			}
			sector := &world.Sectors[index]
			sector.NonEmptyVoxelCount = chunk.NonEmptyVoxelCount
			if len(sector.LODs) != 0 {
				lod := &sector.LODs[0]
				lod.ChunkPath, lod.NonEmptyVoxelCount = path, chunk.NonEmptyVoxelCount
				lod.PayloadHash, lod.PayloadSizeBytes, lod.Aux = chunk.PayloadHash, chunk.PayloadSizeBytes, entry.Aux
			}
		}
	})
	observer := s3aObserver(cmd, mgl32.Vec3{1, 1, 1}, StreamedLevelObserverComponent{})
	cmd.app.FlushCommands()
	updateStreamedObserverSelection(cmd, state)
	return &streamedRenderHarness{t: t, app: cmd.app, cmd: cmd, assets: assets, runtime: state}, observer, auxBytes
}

func p5aAdoptions(t *testing.T, state *StreamedLevelRuntimeState) int {
	t.Helper()
	refreshStreamedRuntimeMetricsCounts(state)
	field := reflect.ValueOf(state.Metrics).FieldByName("PreparedGeometryAssetAdoptions")
	if !field.IsValid() || field.Kind() != reflect.Int {
		t.Fatal("public metric PreparedGeometryAssetAdoptions must have type int")
	}
	return int(field.Int())
}

func p5aGeometry(t *testing.T, geometry *volume.XBrickMap, aux []byte) {
	t.Helper()
	if geometry == nil || geometry.GetVoxelCount() != 8 {
		t.Fatal("registered/prepared geometry lost authored occupancy")
	}
	for _, point := range [][3]int{{1, 2, 3}, {9, 10, 11}} {
		if occupied, value := geometry.GetVoxel(point[0], point[1], point[2]); !occupied || value != 1 {
			t.Fatalf("authored voxel %v changed: occupied=%t value=%d", point, occupied, value)
		}
	}
	if aux != nil {
		brick := geometry.Sectors[[3]int{}].GetBrick(0, 0, 0)
		if brick == nil || !bytes.Equal(brick.PrecomputedAux, aux) {
			t.Fatal("registration changed deep auxiliary payload")
		}
	}
}

func p5aAsset(t *testing.T, f *streamedRenderHarness, proxy bool) (AssetId, VoxelGeometryAsset) {
	t.Helper()
	var entity EntityId
	if proxy {
		loaded := f.runtime.LoadedSectorProxies[ChunkCoord{}]
		if loaded == nil {
			t.Fatal("real proxy did not commit")
		}
		entity = loaded.Entity
	} else {
		entity = s1eImported(t, f, ChunkCoord{})
	}
	model := mustVoxelModelComponentForLevelTest(t, f.cmd, entity)
	id := model.GeometryAsset()
	asset, ok := f.assets.GetVoxelGeometry(id)
	if !ok || asset.LocalMin != (mgl32.Vec3{1, 2, 3}) || asset.LocalMax != (mgl32.Vec3{10, 11, 12}) {
		t.Fatalf("registered asset lost worker-computed bounds: present=%t min=%v max=%v", ok, asset.LocalMin, asset.LocalMax)
	}
	return id, asset
}

func TestP5aWorkerRegistrationAdoptsFullAndProxyPreservingIsolationAndWarmReuse(t *testing.T) {
	f, _, auxiliary := p5aRuntime(t, true, true)
	s1fPrepared(t, f, ChunkCoord{}, false)
	s1fPrepared(t, f, ChunkCoord{}, true)
	full := <-f.runtime.PreparedLoads
	proxy := <-f.runtime.PreparedProxyLoads
	f.runtime.PreparedLoads <- full
	f.runtime.PreparedProxyLoads <- proxy
	fullSource, proxySource := full.PreparedImportedWorldGeometry, proxy.PreparedGeometry
	p5aGeometry(t, fullSource, auxiliary)
	p5aGeometry(t, proxySource, auxiliary)
	f.commitStage()
	f.commitStage()
	fullID, fullAsset := p5aAsset(t, f, false)
	proxyID, proxyAsset := p5aAsset(t, f, true)
	p5aGeometry(t, fullAsset.XBrickMap, auxiliary)
	p5aGeometry(t, proxyAsset.XBrickMap, auxiliary)
	if p5aAdoptions(t, f.runtime) != 2 || f.runtime.Metrics.PreparedGeometryAssetRegisters != 2 || f.runtime.Metrics.PreparedGeometryAssetReuses != 0 {
		t.Fatal("first full/proxy registrations did not adopt exactly two worker payloads")
	}
	removeStreamedChunk(f.cmd, f.runtime, ChunkCoord{})
	unloadStreamedSectorProxy(f.cmd, f.runtime, ChunkCoord{})
	f.app.FlushCommands()
	s1fPrepared(t, f, ChunkCoord{}, false)
	s1fPrepared(t, f, ChunkCoord{}, true)
	f.commitStage()
	f.commitStage()
	reusedFull, _ := p5aAsset(t, f, false)
	reusedProxy, _ := p5aAsset(t, f, true)
	if reusedFull != fullID || reusedProxy != proxyID || p5aAdoptions(t, f.runtime) != 2 ||
		f.runtime.Metrics.PreparedGeometryAssetRegisters != 2 || f.runtime.Metrics.PreparedGeometryAssetReuses != 2 {
		t.Fatal("warm full/proxy reuse changed asset identity or adopted an unused worker payload")
	}
	if f.runtime.Metrics.PendingPreparedBytes != 0 || f.runtime.Metrics.PreparedQueueDepth != 0 {
		t.Fatal("warm reuse retained unused prepared registration ownership")
	}
	// Renderer auxiliary updates can mutate registered geometry. Both worker
	// source maps, including the independently prepared other result, stay intact.
	fullAsset.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux[7] ^= 0x80
	p5aGeometry(t, fullSource, auxiliary)
	p5aGeometry(t, proxySource, auxiliary)
	p5aGeometry(t, proxyAsset.XBrickMap, auxiliary)
	proxyAsset.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux[7] ^= 0x40
	p5aGeometry(t, fullSource, auxiliary)
	p5aGeometry(t, proxySource, auxiliary)
}

func TestP5aDeferredRegistrationCopyRetainsPendingChargeUntilCancellationOrStop(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "stop"}[stop], func(t *testing.T) {
			f, observer, _ := p5aRuntime(t, false, false)
			for _, coord := range []ChunkCoord{{}, {X: 1}} {
				s1fPrepared(t, f, coord, false)
			}
			first, deferred := <-f.runtime.PreparedLoads, <-f.runtime.PreparedLoads
			f.runtime.PreparedLoads <- first
			f.runtime.PreparedLoads <- deferred
			source := deferred.PreparedImportedWorldGeometry
			// A conservative lower bound measures real map storage. Envelope and
			// decoded metadata add further bytes; no packet layout is assumed.
			copy := source.Copy()
			copy.ClearDirty()
			minimumCopyBytes := min(s2aCharge(t, source), s2aCharge(t, copy))
			f.commitStage()
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			retained := f.runtime.Metrics.PendingPreparedBytes
			if f.runtime.Metrics.PreparedChunkQueueDepth != 1 || retained < 2*minimumCopyBytes {
				t.Fatalf("deferred result did not retain independent source and registration storage: pending=%d minimum=%d", retained, 2*minimumCopyBytes)
			}
			s1eWork(t, f.runtime, 1, 32, 0, 0)
			if stop {
				if err := StopStreamedLevelRuntime(f.cmd); err != nil {
					t.Fatal(err)
				}
			} else {
				s3aMove(f.cmd, observer, mgl32.Vec3{1600, 1, 1})
				f.observerStage()
				if f.runtime.Metrics.PendingPreparedBytes != retained {
					t.Fatal("cancellation released deferred registration storage before acknowledgement")
				}
				f.commitStage()
				if f.runtime.Metrics.PrepareCancelledCount != 1 {
					t.Fatal("cancelled deferred result did not reach normal acknowledgement")
				}
			}
			if f.runtime.Metrics.PendingPreparedBytes != 0 || f.runtime.Metrics.PreparedQueueDepth != 0 {
				t.Fatal("terminal consumption retained registration copy ownership")
			}
			s1eWork(t, f.runtime, 0, 32, 0, 0)
		})
	}
}

func TestP5aPublicSharedRegistrationRetainsDefensiveCopy(t *testing.T) {
	assets := newSpawnTestAssetServer()
	source := volume.NewXBrickMap()
	source.SetVoxel(1, 2, 3, 1)
	aux := make([]byte, volume.VoxelAuxRecordBytes)
	aux[7] = 42
	source.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux = aux
	id := assets.RegisterSharedVoxelGeometry(source, "mutable-caller")
	source.SetVoxel(1, 2, 3, 2)
	aux[7] = 99
	registered, ok := assets.GetVoxelGeometry(id)
	if !ok || registered.XBrickMap == nil {
		t.Fatal("public registration did not publish usable geometry")
	}
	occupied, value := registered.XBrickMap.GetVoxel(1, 2, 3)
	if !occupied || value != 1 || registered.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux[7] != 42 {
		t.Fatal("mutable caller changed public registration's defensive geometry/aux copy")
	}
}
