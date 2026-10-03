package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestE2b3ReviewActualStreamMembershipLeasesOverridesWithoutUsableAuthoredRef(t *testing.T) {
	for _, change := range []string{"missing-ref", "changed-ids"} {
		t.Run(change, func(t *testing.T) {
			f, _, eid := e2b3Pristine(t)
			source := s3cComponent[VoxelModelComponent](t, f.cmd, eid).GeometryAsset()
			if change == "missing-ref" {
				f.cmd.RemoveComponents(eid, AuthoredLevelItemRefComponent{})
			} else {
				ref := *s3cComponent[AuthoredLevelItemRefComponent](t, f.cmd, eid)
				ref.PlacementID, ref.ItemID = "different-placement", "different-item"
				f.cmd.AddComponents(eid, ref)
			}
			f.app.FlushCommands()
			override := e2b3Enable(t, f, eid)
			e2b3Fallback(t, f, f.runtime, eid, s1gID(0, 0), "body")
			if err := ApplyManagedVoxelWrites(f.cmd, f.assets, eid, p1dWrites(volume.VoxelWrite{X: -9, Value: 7})); err != nil {
				t.Fatal("missing authored binding disabled ordinary editing", err)
			}
			f.app.FlushCommands()
			if lease := f.runtime.snapshotGeometryAssets[eid]; lease.ID != override || lease.Server != f.assets {
				t.Fatal("actual stream-owned override lost lease with authored reference")
			}
			if err := unloadStreamedChunk(f.cmd, f.runtime, ChunkCoord{}); err != nil {
				t.Fatal(err)
			}
			f.app.FlushCommands()
			p5cAssetPresent(t, f, override, false)
			p5cAssetPresent(t, f, source, true)
		})
	}
}
