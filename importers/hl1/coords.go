package hl1

import importcommon "github.com/gekko3d/gekko/importers/common"

const HammerUnitMeters = 0.0254

func HammerToGekko(v importcommon.Vec3) importcommon.Vec3 {
	return importcommon.Vec3{
		X: v.X * HammerUnitMeters,
		Y: v.Z * HammerUnitMeters,
		Z: -v.Y * HammerUnitMeters,
	}
}

func GekkoToHammer(v importcommon.Vec3) importcommon.Vec3 {
	return importcommon.Vec3{
		X: v.X / HammerUnitMeters,
		Y: -v.Z / HammerUnitMeters,
		Z: v.Y / HammerUnitMeters,
	}
}

func HammerBoundsToGekko(minV, maxV importcommon.Vec3) importcommon.Bounds {
	corners := []importcommon.Vec3{
		{X: minV.X, Y: minV.Y, Z: minV.Z},
		{X: minV.X, Y: minV.Y, Z: maxV.Z},
		{X: minV.X, Y: maxV.Y, Z: minV.Z},
		{X: minV.X, Y: maxV.Y, Z: maxV.Z},
		{X: maxV.X, Y: minV.Y, Z: minV.Z},
		{X: maxV.X, Y: minV.Y, Z: maxV.Z},
		{X: maxV.X, Y: maxV.Y, Z: minV.Z},
		{X: maxV.X, Y: maxV.Y, Z: maxV.Z},
	}
	outMin := HammerToGekko(corners[0])
	outMax := outMin
	for _, corner := range corners[1:] {
		converted := HammerToGekko(corner)
		outMin = minVec3(outMin, converted)
		outMax = maxVec3(outMax, converted)
	}
	return importcommon.Bounds{Min: outMin, Max: outMax}
}

func minVec3(a, b importcommon.Vec3) importcommon.Vec3 {
	if b.X < a.X {
		a.X = b.X
	}
	if b.Y < a.Y {
		a.Y = b.Y
	}
	if b.Z < a.Z {
		a.Z = b.Z
	}
	return a
}

func maxVec3(a, b importcommon.Vec3) importcommon.Vec3 {
	if b.X > a.X {
		a.X = b.X
	}
	if b.Y > a.Y {
		a.Y = b.Y
	}
	if b.Z > a.Z {
		a.Z = b.Z
	}
	return a
}
