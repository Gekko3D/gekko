package gekko

import (
	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

const DefaultLadderClimbSpeed = content.DefaultNavLadderClimbSpeed

type LadderVolumeComponent struct {
	BoundsCenter      mgl32.Vec3
	BoundsHalfExtents mgl32.Vec3
	ClimbSpeed        float32
	SourceTag         string
	Tags              []string
}

func (l LadderVolumeComponent) NormalizedClimbSpeed() float32 {
	if l.ClimbSpeed <= 0 {
		return DefaultLadderClimbSpeed
	}
	return l.ClimbSpeed
}
