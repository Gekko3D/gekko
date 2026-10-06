package gpu

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
	"testing"
)

func TestSectorLookupSameCoordinatePublicationInvalidatesOnlyDependentShadow(t *testing.T) {
	m, b, s, p := s1mFixture(t, 1)
	q := s1l14ProducerFor(s, [3]int{})
	p.o.Transform.Position = mgl32.Vec3{40, 0, 0}
	p.o.Transform.Scale = mgl32.Vec3{.02, .02, .02}
	p.o.Transform.Dirty = true
	q.o.Transform.Position = mgl32.Vec3{100, 0, 0}
	q.o.Transform.Scale = mgl32.Vec3{.02, .02, .02}
	q.o.Transform.Dirty = true
	s.Lights = []core.Light{r2aLight(core.LightTypeSpot, 40), r2aLight(core.LightTypeSpot, 100)}
	camera := core.NewCameraState()
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) && s1mPublished(m, q) })
	before := m.SectorLookupFrameStats().CurrentGeneration
	budget := m.SectorLookupFrameBudget()
	budget.MaxUploadBytes = 0
	m.SetSectorLookupFrameBudget(budget)
	s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 3})
	uploaded := false
	for frame := 0; frame < 64; frame++ {
		s1mFrame(t, m, b, s)
		uploaded = uploaded || m.VoxelSectorsUploaded > 0
	}
	if !uploaded {
		t.Fatal("fixture did not upload same-coordinate replacement before lookup publication")
	}
	if m.SectorLookupFrameStats().CurrentGeneration != before {
		t.Fatal("paused lookup published before warming the old readable shadow")
	}
	// Warm after content upload/transfer, while lookup still addresses the old
	// physical edge. Publishing the replacement must invalidate this cache again.
	r2aCommit(m, s, camera)
	r2eWarm(t, m, s, camera)
	r2eIdle(t, m, s, camera, 1000)
	m.SetSectorLookupFrameBudget(DefaultSectorLookupFrameBudget())
	committed := false
	for frame := 0; frame < 2048; frame++ {
		s1mFrame(t, m, b, s)
		if m.SectorLookupFrameStats().CurrentGeneration > before && s1mPublished(m, p) {
			committed = true
			break
		}
	}
	if !committed {
		t.Fatal("same-coordinate lookup replacement did not commit")
	}
	r2aCommit(m, s, camera)
	updates := r2aAssert(t, m, s, camera, 1001, []int{0})
	if len(updates) == 0 {
		t.Fatal("physical lookup replacement reused a shadow warmed against the old edge")
	}
	m.RecordShadowUpdates(updates, 1001, s.ShadowRevision())
	r2eIdle(t, m, s, camera, 1002)
}
