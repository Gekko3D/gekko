package gekko

import (
	"strings"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestCellularVolumeComponentValidateAcceptsSupportedTypes(t *testing.T) {
	cases := []CellularType{CellularSmoke, CellularFire}
	for _, cellularType := range cases {
		cv := &CellularVolumeComponent{Type: cellularType}
		if err := cv.Validate(); err != nil {
			t.Fatalf("expected %s to validate, got %v", cellularType, err)
		}
	}
}

func TestCellularVolumeComponentValidateRejectsUnsupportedTypes(t *testing.T) {
	cases := []CellularType{CellularSand, CellularWater, CellularType(99)}
	for _, cellularType := range cases {
		cv := &CellularVolumeComponent{Type: cellularType}
		err := cv.Validate()
		if err == nil {
			t.Fatalf("expected %s to be rejected", cellularType)
		}
		if !strings.Contains(err.Error(), "only CellularSmoke and CellularFire are supported") {
			t.Fatalf("expected supported-types message, got %v", err)
		}
	}
}

func TestSpawnCellularVolumePanicsOnUnsupportedType(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected unsupported cellular volume type to panic")
		}
	}()

	spawnCellularVolume(cmd, CellularVolumeDef{
		Position: mgl32.Vec3{},
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
		Volume: CellularVolumeComponent{
			Type: CellularSand,
		},
	})
}

func TestCellularVolumeLifecycleHelpersKeepZeroIntensityVolumesManaged(t *testing.T) {
	cv := &CellularVolumeComponent{
		Type:         CellularFire,
		UseIntensity: true,
		Intensity:    0,
	}

	if !cv.UsesGPUVolume() {
		t.Fatal("expected supported fire volume to stay managed by the GPU path at zero intensity")
	}
	if cv.HasVisibleGPUVolume() {
		t.Fatal("expected zero-intensity fire volume to be invisible")
	}
	if cv.WantsGPUVolumeSimulation() {
		t.Fatal("expected zero-intensity fire volume to freeze simulation")
	}

	cv.Intensity = 1
	if !cv.HasVisibleGPUVolume() {
		t.Fatal("expected non-zero target intensity to make the volume visible")
	}
	if !cv.WantsGPUVolumeSimulation() {
		t.Fatal("expected non-zero target intensity to resume simulation")
	}
}

func TestCAStepSystemFreezesZeroIntensityGPUVolumesWithoutEvictingThem(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	eid := app.ecs.addEntity(
		&TransformComponent{Position: mgl32.Vec3{}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&CellularVolumeComponent{
			Type:         CellularFire,
			UseIntensity: true,
			Intensity:    0,
			TickRate:     1,
		},
	)

	getVolume := func() *CellularVolumeComponent {
		var found *CellularVolumeComponent
		MakeQuery1[CellularVolumeComponent](cmd).Map(func(foundEID EntityId, cv *CellularVolumeComponent) bool {
			if foundEID == eid {
				found = cv
				return false
			}
			return true
		})
		if found == nil {
			t.Fatal("expected to find CA volume in ECS")
		}
		return found
	}

	caStepSystem(&Time{Dt: 1.0}, cmd)
	if getVolume()._gpuStepsPending != 0 {
		t.Fatalf("expected inactive volume to keep zero pending GPU steps, got %d", getVolume()._gpuStepsPending)
	}

	getVolume().Intensity = 1
	caStepSystem(&Time{Dt: 1.0}, cmd)
	if getVolume()._gpuStepsPending == 0 {
		t.Fatal("expected active volume to queue GPU simulation steps")
	}
}
