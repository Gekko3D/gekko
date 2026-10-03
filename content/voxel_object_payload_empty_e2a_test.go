package content_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestE2aDeltaExplicitCompleteRemovalRoundTripStaysEmpty(t *testing.T) {
	base := e2aBase()
	identity, _, err := content.VoxelObjectBaseIdentity(base, e2aLattice(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, removal := range []bool{false, true} {
		name := "empty-delta-preserves-base"
		if removal {
			name = "explicit-zero-removes-base"
		}
		t.Run(name, func(t *testing.T) {
			payload := e2aPayload(content.VoxelObjectPayloadBaseDelta)
			payload.BaseIdentity = identity
			if removal {
				for _, voxel := range base.Voxels {
					voxel.Value = 0
					payload.Voxels = append(payload.Voxels, voxel)
				}
			}
			data, _, err := content.EncodeVoxelObjectPayload(payload, nil)
			if err != nil {
				t.Fatal(err)
			}
			decoded, _, err := content.DecodeVoxelObjectPayload(data, nil)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "empty-override.gkvox")
			if _, err := content.SaveVoxelObjectPayload(path, payload, nil); err != nil {
				t.Fatal(err)
			}
			loaded, _, err := content.LoadVoxelObjectPayload(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, roundTrip := range []*content.VoxelObjectPayloadDef{decoded, loaded} {
				if len(roundTrip.Voxels) != len(payload.Voxels) {
					t.Fatal("round trip dropped explicit zero assignments")
				}
				resolved, err := content.ResolveVoxelObjectPayload(roundTrip, base, e2aLattice(), "placement", "item", nil)
				if err != nil {
					t.Fatal(err)
				}
				if removal {
					if len(resolved.Voxels) != 0 {
						t.Fatal("complete removal resurrected construction base")
					}
				} else if !reflect.DeepEqual(e2aValues(resolved.Voxels), e2aValues(base.Voxels)) {
					t.Fatal("empty delta failed to preserve construction base")
				}
			}
			if !reflect.DeepEqual(base, e2aBase()) {
				t.Fatal("resolution mutated construction base")
			}
		})
	}
}
