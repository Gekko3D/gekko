package gekko

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// Wrapping a built-in classifier exercises the observable custom-provider path.
type e1bBackingWrapper struct {
	VoxelBackingProvider
	observe func([3]int)
}

func (p *e1bBackingWrapper) VoxelValue(global [3]int) uint8 {
	if p.observe != nil {
		p.observe(global)
	}
	return p.VoxelBackingProvider.VoxelValue(global)
}

func e1bBackingMap() *volume.XBrickMap {
	x := volume.NewXBrickMap()
	x.SetVoxel(7, 0, 2, 9)
	x.SetVoxel(8, 0, 2, 3)
	x.SetVoxel(15, 0, 2, 6)
	x.ComputeAABB()
	x.ClearDirty()
	x.DirtyBricks[[6]int{9, 9, 9, 0, 0, 0}] = true
	for _, s := range x.Sectors {
		for _, b := range s.PackedBricks {
			b.PrecomputedAux = []byte{1, 2}
		}
	}
	return x
}

func TestE1bBuiltInBackingMatchesSequentialClassifier(t *testing.T) {
	def := testPlaneTreeBackingDef() // No thin-surface support: generic classification is equivalent.
	def.PlaneTree.Leaves[1].Solid = true
	plane, err := NewPlaneTreeVoxelBacking(def)
	if err != nil {
		t.Fatal(err)
	}
	var columns []content.TerrainChunkColumnDef
	for x := 0; x < 16; x++ {
		columns = append(columns, content.TerrainChunkColumnDef{X: x, Z: 2, FilledVoxels: 4})
	}
	terrain := NewTerrainColumnVoxelBacking(&content.TerrainChunkDef{ChunkSize: 16, SolidValue: 7, Columns: columns})
	for _, provider := range []VoxelBackingProvider{plane, terrain} {
		kind := content.VoxelBackingOwnerImportedWorld
		if provider == terrain {
			kind = content.VoxelBackingOwnerTerrain
		}
		got, want := e1bBackingMap(), e1bBackingMap()
		builtIn := NewVoxelBackingComponent(kind, "owner", "source", [3]int{}, 16, provider, nil)
		custom := NewVoxelBackingComponent(kind, "owner", "source", [3]int{}, 16, &e1bBackingWrapper{VoxelBackingProvider: provider}, nil)
		for _, center := range []mgl32.Vec3{{7.5, 0.5, 2.5}, {15.5, 0.5, 2.5}, {7.5, 0.5, 2.5}} {
			gc := builtIn.materializeSphere(got, center, 0.1, 11)
			wc := custom.materializeSphere(want, center, 0.1, 11)
			volume.Sphere(got, center, 0.1, 0)
			volume.Sphere(want, center, 0.1, 0)
			if gc != wc || !reflect.DeepEqual(builtIn.RemovalDef(), custom.RemovalDef()) || builtIn.Dirty != custom.Dirty || !reflect.DeepEqual(builtIn.surfaceSupportSeeds, custom.surfaceSupportSeeds) {
				t.Fatal("backing edit changed removal/material/support history")
			}
			p5hMapParity(t, got, want)
		}
		removal := builtIn.RemovalDef()
		bg := NewVoxelBackingComponent(kind, "owner", "source", [3]int{}, 16, provider, &removal)
		cw := NewVoxelBackingComponent(kind, "owner", "source", [3]int{}, 16, &e1bBackingWrapper{VoxelBackingProvider: provider}, &removal)
		g, w := e1bBackingMap(), e1bBackingMap()
		bg.ApplyRemovals(g)
		cw.ApplyRemovals(w)
		p5hMapParity(t, g, w)
		if found, _ := g.GetVoxel(7, 0, 2); found {
			t.Fatal("restored backing removals must not resurrect carved matter")
		}
	}
}

func TestE1bCustomBackingObservesFinalizedPriorEdits(t *testing.T) {
	provider, err := NewPlaneTreeVoxelBacking(testPlaneTreeBackingDef())
	if err != nil {
		t.Fatal(err)
	}
	x := e1bBackingMap()
	visits, reentered := 0, false
	wrapper := &e1bBackingWrapper{VoxelBackingProvider: provider, observe: func(_ [3]int) {
		visits++
		for _, sector := range x.Sectors {
			for _, brick := range sector.PackedBricks {
				final := brick.Copy()
				final.RefreshMaterialFlags()
				if brick.Flags != final.Flags || brick.AtlasOffset != final.AtlasOffset {
					t.Fatal("custom classifier observed deferred material finalization from a prior edit")
				}
			}
		}
		if !reentered {
			reentered = true
			x.SetVoxel(40, 0, 0, 12)
		}
	}}
	component := NewVoxelBackingComponent(content.VoxelBackingOwnerImportedWorld, "owner", "source", [3]int{}, 16, wrapper, nil)
	component.materializeSphere(x, mgl32.Vec3{5.5, 0.5, 2.5}, 1.2, 7)
	if visits < 2 || !reentered {
		t.Fatal("fixture must exercise observable custom-provider callbacks")
	}
	if _, value := x.GetVoxel(40, 0, 0); value != 12 {
		t.Fatal("custom classifier callback edit was lost")
	}
}
