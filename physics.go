package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

type ColliderShape int

const (
	ShapeBox ColliderShape = iota
	ShapeSphere
)

type RigidBodyComponent struct {
	Velocity        mgl32.Vec3
	AngularVelocity mgl32.Vec3
	Mass            float32
	GravityScale    float32
	IsStatic        bool
	Sleeping        bool
	IdleTime        float32

	// Physics Properties
	CoM         mgl32.Vec3 // Local Center of Mass offset from Transform
	Inertia     mgl32.Mat3 // Local Inertia Tensor
	InvInertia  mgl32.Mat3 // Local Inverse Inertia Tensor
	Initialized bool
}

func (rb *RigidBodyComponent) Wake() {
	rb.Sleeping = false
	rb.IdleTime = 0
}

func (rb *RigidBodyComponent) ApplyLinearImpulse(impulse mgl32.Vec3) {
	rb.Wake()
	if rb.IsStatic {
		return
	}
	if rb.Mass > 0 {
		rb.Velocity = rb.Velocity.Add(impulse.Mul(1.0 / rb.Mass))
	} else {
		rb.Velocity = rb.Velocity.Add(impulse)
	}
}

func (rb *RigidBodyComponent) ApplyImpulse(impulse mgl32.Vec3, point mgl32.Vec3, worldCoM mgl32.Vec3, rotation mgl32.Quat) {
	rb.Wake()
	if rb.IsStatic {
		return
	}

	// Linear Impulse
	rb.ApplyLinearImpulse(impulse)

	// Angular Impulse: L = r x J
	r := point.Sub(worldCoM)
	torque := r.Cross(impulse)

	// Transform local InvInertia to world space
	// I_world^-1 = R * I_local^-1 * R^T
	R := QuatToMat3(rotation)
	worldInvInertia := R.Mul3(rb.InvInertia).Mul3(R.Transpose())

	dOmega := worldInvInertia.Mul3x1(torque)
	rb.AngularVelocity = rb.AngularVelocity.Add(dOmega)
}

type ColliderComponent struct {
	Shape           ColliderShape
	HalfExtents     mgl32.Vec3 // For Box
	Radius          float32    // For Sphere
	Friction        float32
	Restitution     float32
	AABBHalfExtents mgl32.Vec3 // Cached or calculated total half extents
}

type PhysicsWorld struct {
	Gravity        mgl32.Vec3
	VoxelSize      float32
	SleepThreshold float32
	SleepTime      float32
}

func NewPhysicsWorld() *PhysicsWorld {
	return &PhysicsWorld{
		Gravity:        mgl32.Vec3{0, -9.81, 0},
		VoxelSize:      0.1,
		SleepThreshold: -1.0, // Disable sleeping for debugging
		SleepTime:      1.0,
	}
}

type PhysicsModule struct{}

func (m PhysicsModule) Install(app *App, cmd *Commands) {
	cmd.AddResources(NewPhysicsWorld())

	app.UseSystem(
		System(PhysicsSystem).
			InStage(Update).
			RunAlways(),
	)
}

type BodyInfo struct {
	Eid           EntityId
	Tr            *TransformComponent
	Rb            *RigidBodyComponent
	Col           *ColliderComponent
	ScaledExtents mgl32.Vec3
	Model         *VoxModel
	PhysicsData   *VoxPhysicsData

	// Derived World State
	WorldCoM        mgl32.Vec3
	WorldInvInertia mgl32.Mat3
}

type Contact struct {
	BodyA  *BodyInfo
	BodyB  *BodyInfo // Can be nil for World Collision
	Point  mgl32.Vec3
	Normal mgl32.Vec3 // Points from B to A (or World to A)
	Depth  float32
}

func GetComponent[T any](cmd *Commands, eid EntityId) *T {
	comps := cmd.GetAllComponents(eid)
	for _, c := range comps {
		if t, ok := c.(T); ok {
			copy := t
			return &copy
		}
		if t, ok := c.(*T); ok {
			return t
		}
	}
	return nil
}

func GetVoxelModelComponent(cmd *Commands, eid EntityId) *VoxelModelComponent {
	return GetComponent[VoxelModelComponent](cmd, eid)
}

func PhysicsSystem(cmd *Commands, time *Time, physics *PhysicsWorld, vrs *VoxelRtState, assets *AssetServer) {
	dt := float32(time.Dt)
	if dt <= 0 || dt > 0.5 { // Safety cap for dt
		return
	}

	// 1. Sync Voxel Science from RtState resource
	if vrs != nil && vrs.RtApp != nil && vrs.RtApp.Scene != nil {
		physics.VoxelSize = vrs.RtApp.Scene.TargetVoxelSize
	}

	// Find world component for collision
	var world *WorldComponent
	MakeQuery1[WorldComponent](cmd).Map(func(eid EntityId, w *WorldComponent) bool {
		world = w
		return false
	})

	// 2. Collection
	var bodies []BodyInfo
	dynamicCount := 0
	// We query for Transform and Collider. RigidBody is optional.
	MakeQuery3[TransformComponent, RigidBodyComponent, ColliderComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, rb *RigidBodyComponent, col *ColliderComponent) bool {
		// If no RigidBody component (CollisionOnly), create a virtual static one for the solver
		if rb == nil {
			rb = &RigidBodyComponent{
				IsStatic:    true,
				Initialized: false,
				Mass:        1.0,
			}
		} else if !rb.IsStatic {
			dynamicCount++
		}
		scaledHalfExtents := mgl32.Vec3{
			col.AABBHalfExtents.X() * tr.Scale.X(),
			col.AABBHalfExtents.Y() * tr.Scale.Y(),
			col.AABBHalfExtents.Z() * tr.Scale.Z(),
		}
		// Validate extents
		if scaledHalfExtents.X() < 0.001 {
			scaledHalfExtents[0] = 0.001
		}
		if scaledHalfExtents.Y() < 0.001 {
			scaledHalfExtents[1] = 0.001
		}
		if scaledHalfExtents.Z() < 0.001 {
			scaledHalfExtents[2] = 0.001
		}

		var model *VoxModel
		var physicsData *VoxPhysicsData

		// Try to find VoxelModel if it exists
		vmc := GetVoxelModelComponent(cmd, eid)

		if vmc != nil && assets != nil {
			if vmc.CustomPhysicsData != nil {
				physicsData = vmc.CustomPhysicsData
				if vmAsset, ok := assets.voxModels[vmc.VoxelModel]; ok {
					model = &vmAsset.VoxModel
				}
			} else if vmAsset, ok := assets.voxModels[vmc.VoxelModel]; ok {
				model = &vmAsset.VoxModel
				physicsData = model.PhysicsData
			}
		}

		// Initialize Physics Properties if needed
		if !rb.Initialized {
			if rb.Mass < 0.0001 {
				rb.Mass = 1.0
			}

			if physicsData != nil && model != nil {
				// CoM Offset: PhysicsData.CoM is in 0..Size space. Transform is at Center (Size/2).
				size := mgl32.Vec3{float32(model.SizeX), float32(model.SizeY), float32(model.SizeZ)}.Mul(physics.VoxelSize)
				rb.CoM = physicsData.CenterOfMass.Sub(size.Mul(0.5))

				rb.Inertia = physicsData.InertiaTensor
				// Avoid singular matrix
				if rb.Inertia.Det() < 0.0001 {
					// Fallback to box inertia
					width, height, depth := size.X(), size.Y(), size.Z()
					m := rb.Mass
					ix := (1.0 / 12.0) * m * (height*height + depth*depth)
					iy := (1.0 / 12.0) * m * (width*width + depth*depth)
					iz := (1.0 / 12.0) * m * (width*width + height*height)
					rb.Inertia = mgl32.Mat3{ix, 0, 0, 0, iy, 0, 0, 0, iz}
				}
			} else {
				// Fallback box properties
				he := col.AABBHalfExtents
				width, height, depth := he.X()*2, he.Y()*2, he.Z()*2
				m := rb.Mass
				ix := (1.0 / 12.0) * m * (height*height + depth*depth)
				iy := (1.0 / 12.0) * m * (width*width + depth*depth)
				iz := (1.0 / 12.0) * m * (width*width + height*height)
				rb.Inertia = mgl32.Mat3{ix, 0, 0, 0, iy, 0, 0, 0, iz}
				rb.CoM = mgl32.Vec3{0, 0, 0}
			}
			rb.InvInertia = rb.Inertia.Inv()
			rb.Initialized = true
		}

		// Calculate World State for initial body structure
		rot := tr.Rotation
		scale := tr.Scale.X() // Assume uniform scale for inertia

		// Scaled CoM and World CoM
		worldCoM := tr.Position.Add(rot.Rotate(rb.CoM.Mul(scale)))

		// Local InvInertia scales by 1/s^2 when radius scales by s
		localInvInertia := rb.InvInertia.Mul(1.0 / (scale * scale))

		R := QuatToMat3(rot)
		worldInvInertia := R.Mul3(localInvInertia).Mul3(R.Transpose())

		bodies = append(bodies, BodyInfo{
			Eid:             eid,
			Tr:              tr,
			Rb:              rb,
			Col:             col,
			ScaledExtents:   scaledHalfExtents,
			Model:           model,
			PhysicsData:     physicsData,
			WorldCoM:        worldCoM,
			WorldInvInertia: worldInvInertia,
		})
		return true
	}, RigidBodyComponent{})

	if dynamicCount > 0 && time.FrameCount%60 == 0 {
		var sleepCount int
		for i := range bodies {
			if !bodies[i].Rb.IsStatic && bodies[i].Rb.Sleeping {
				sleepCount++
			}
		}
		cmd.app.Logger().Debugf("Physics - Total: %d, Dyn: %d, Sleep: %d", len(bodies), dynamicCount, sleepCount)
		count := 0
		for i := range bodies {
			if !bodies[i].Rb.IsStatic {
				if count < 4 { // Log first few dynamic bodies
					cmd.app.Logger().Debugf("  Body[%d] Eid:%d Pos:%.2f %.2f %.2f Vel:%.2f", i, bodies[i].Eid, bodies[i].Tr.Position.X(), bodies[i].Tr.Position.Y(), bodies[i].Tr.Position.Z(), bodies[i].Rb.Velocity.Len())
				}
				count++
			}
		}
	}

	// 3. Solver & Integration with Sub-stepping
	const subSteps = 4
	dtSub := dt / float32(subSteps)

	for s := 0; s < subSteps; s++ {
		// Calculate World State for this sub-step
		for i := range bodies {
			b := &bodies[i]
			if b.Rb.IsStatic {
				continue
			}

			// Damping (scaled per sub-step)
			damp := float32(math.Pow(0.98, float64(1.0/float32(subSteps))))
			b.Rb.Velocity = b.Rb.Velocity.Mul(damp)
			b.Rb.AngularVelocity = b.Rb.AngularVelocity.Mul(damp)

			// World CoM and InvInertia (Update per sub-step as body moves)
			rot := b.Tr.Rotation
			scale := b.Tr.Scale.X()

			b.WorldCoM = b.Tr.Position.Add(rot.Rotate(b.Rb.CoM.Mul(scale)))

			localInvInertia := b.Rb.InvInertia.Mul(1.0 / (scale * scale))
			R := QuatToMat3(rot)
			b.WorldInvInertia = R.Mul3(localInvInertia).Mul3(R.Transpose())

			// Apply Gravity
			if !b.Rb.Sleeping && b.Rb.GravityScale != 0 {
				b.Rb.Velocity = b.Rb.Velocity.Add(physics.Gravity.Mul(b.Rb.GravityScale * dtSub))
			}
		}

		// Collision Detection
		var contacts []Contact
		contacts = append(contacts, FindWorldContacts(world, bodies, physics.VoxelSize)...)
		contacts = append(contacts, FindBodyContacts(bodies)...)

		// Solve Constraints
		for iter := 0; iter < 4; iter++ {
			for _, c := range contacts {
				ResolveContact(c, dtSub)
			}
		}

		// Integrate
		for i := range bodies {
			b := &bodies[i]
			if b.Rb.IsStatic || b.Rb.Sleeping {
				continue
			}

			// Integrate Position
			b.Tr.Position = b.Tr.Position.Add(b.Rb.Velocity.Mul(dtSub))

			// Integrate Rotation
			omega := b.Rb.AngularVelocity
			if omega.Len() > 0.001 {
				angle := omega.Len() * dtSub
				axis := omega.Normalize()
				rotChange := mgl32.QuatRotate(angle, axis)
				b.Tr.Rotation = rotChange.Mul(b.Tr.Rotation).Normalize()
			}
		}
	}

	// 4. Sleeping Check (Post-step)
	for i := range bodies {
		b := &bodies[i]
		if b.Rb.IsStatic {
			continue
		}
		if b.Rb.Velocity.Len() < physics.SleepThreshold && b.Rb.AngularVelocity.Len() < physics.SleepThreshold {
			b.Rb.IdleTime += dt
			if b.Rb.IdleTime > physics.SleepTime {
				b.Rb.Sleeping = true
				b.Rb.Velocity = mgl32.Vec3{}
				b.Rb.AngularVelocity = mgl32.Vec3{}
			}
		} else {
			b.Rb.IdleTime = 0
			b.Rb.Sleeping = false
		}
	}
}

func FindWorldContacts(world *WorldComponent, bodies []BodyInfo, vSize float32) []Contact {
	var contacts []Contact
	if world == nil {
		return contacts
	}

	for i := range bodies {
		b := &bodies[i]
		if b.Rb.IsStatic || b.Rb.Sleeping {
			continue
		}

		// Check World Collision
		if b.PhysicsData != nil && len(b.PhysicsData.Corners) > 0 {
			// Complex Voxel-to-Voxel collision
			sizeOffset := mgl32.Vec3{float32(b.Model.SizeX), float32(b.Model.SizeY), float32(b.Model.SizeZ)}.Mul(vSize * 0.5)

			for _, v := range b.PhysicsData.Corners {
				localPos := mgl32.Vec3{float32(v.X), float32(v.Y), float32(v.Z)}.Mul(vSize).Sub(sizeOffset)
				localPos = mgl32.Vec3{localPos.X() * b.Tr.Scale.X(), localPos.Y() * b.Tr.Scale.Y(), localPos.Z() * b.Tr.Scale.Z()}
				worldPos := b.Tr.Position.Add(b.Tr.Rotation.Rotate(localPos))

				imX, imY, imZ := int(math.Floor(float64(worldPos.X()/vSize))), int(math.Floor(float64(worldPos.Y()/vSize))), int(math.Floor(float64(worldPos.Z()/vSize)))
				if hit, _ := world.MainXBM.GetVoxel(imX, imY, imZ); hit {
					contacts = append(contacts, generateWorldContact(world, b, worldPos, imX, imY, imZ, vSize, 0.1))
				}
			}
		} else {
			// Simple AABB-to-Voxel collision
			min := b.Tr.Position.Sub(b.ScaledExtents)
			max := b.Tr.Position.Add(b.ScaledExtents)

			minX, minY, minZ := int(math.Floor(float64(min.X()/vSize))), int(math.Floor(float64(min.Y()/vSize))), int(math.Floor(float64(min.Z()/vSize)))
			maxX, maxY, maxZ := int(math.Floor(float64(max.X()/vSize))), int(math.Floor(float64(max.Y()/vSize))), int(math.Floor(float64(max.Z()/vSize)))

			for gx := minX; gx <= maxX; gx++ {
				for gy := minY; gy <= maxY; gy++ {
					for gz := minZ; gz <= maxZ; gz++ {
						if hit, _ := world.MainXBM.GetVoxel(gx, gy, gz); hit {
							// Determine a contact point (closest point on world voxel to entity center?)
							wvMin := mgl32.Vec3{float32(gx) * vSize, float32(gy) * vSize, float32(gz) * vSize}
							wvMax := wvMin.Add(mgl32.Vec3{vSize, vSize, vSize})

							cp := mgl32.Vec3{
								clamp(b.Tr.Position.X(), wvMin.X(), wvMax.X()),
								clamp(b.Tr.Position.Y(), wvMin.Y(), wvMax.Y()),
								clamp(b.Tr.Position.Z(), wvMin.Z(), wvMax.Z()),
							}

							// Penetration depth check
							// Use distance from voxel center to object AABB face
							distY := math.Abs(float64(b.Tr.Position.Y() - cp.Y()))
							depth := b.ScaledExtents.Y() - float32(distY) + vSize*0.5
							if depth < 0 {
								continue
							}
							if depth > vSize {
								depth = vSize
							}

							contacts = append(contacts, generateWorldContact(world, b, cp, gx, gy, gz, vSize, depth))
						}
					}
				}
			}
		}

		if len(contacts) > 100 {
			break
		}
	}
	return contacts
}

func generateWorldContact(world *WorldComponent, b *BodyInfo, p mgl32.Vec3, gx, gy, gz int, vSize float32, depth float32) Contact {
	normal := mgl32.Vec3{0, 1, 0} // Default up
	var accumNormal mgl32.Vec3
	cnt := 0
	neighbors := [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
	for _, n := range neighbors {
		if h, _ := world.MainXBM.GetVoxel(gx+n[0], gy+n[1], gz+n[2]); !h {
			accumNormal = accumNormal.Add(mgl32.Vec3{float32(n[0]), float32(n[1]), float32(n[2])})
			cnt++
		}
	}
	if cnt > 0 && accumNormal.Len() > 0.001 {
		normal = accumNormal.Normalize()
	} else if b.Rb.Velocity.Len() > 0.001 {
		normal = b.Rb.Velocity.Normalize().Mul(-1)
	}

	return Contact{
		BodyA:  b,
		BodyB:  nil,
		Point:  p,
		Normal: normal,
		Depth:  depth,
	}
}

func clamp(v, min, max float32) float32 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func FindBodyContacts(bodies []BodyInfo) []Contact {
	var contacts []Contact
	for i := 0; i < len(bodies); i++ {
		for j := i + 1; j < len(bodies); j++ {
			bA := &bodies[i]
			bB := &bodies[j]

			if bA.Rb.IsStatic && bB.Rb.IsStatic {
				continue
			}
			if bA.Rb.Sleeping && bB.Rb.Sleeping {
				continue
			}

			// AABB Check
			posA := bA.Tr.Position
			posB := bB.Tr.Position
			extA := bA.ScaledExtents
			extB := bB.ScaledExtents

			diff := posA.Sub(posB)
			overlapX := float64(extA.X()+extB.X()) - math.Abs(float64(diff.X()))
			overlapY := float64(extA.Y()+extB.Y()) - math.Abs(float64(diff.Y()))
			overlapZ := float64(extA.Z()+extB.Z()) - math.Abs(float64(diff.Z()))

			if overlapX > 0 && overlapY > 0 && overlapZ > 0 {
				// Collision! Find normal (axis of least penetration)
				normal := mgl32.Vec3{0, 1, 0}
				depth := float32(overlapY)

				if overlapX < overlapY && overlapX < overlapZ {
					depth = float32(overlapX)
					if diff.X() > 0 {
						normal = mgl32.Vec3{1, 0, 0}
					} else {
						normal = mgl32.Vec3{-1, 0, 0}
					}
				} else if overlapZ < overlapX && overlapZ < overlapY {
					depth = float32(overlapZ)
					if diff.Z() > 0 {
						normal = mgl32.Vec3{0, 0, 1}
					} else {
						normal = mgl32.Vec3{0, 0, -1}
					}
				} else {
					if diff.Y() > 0 {
						normal = mgl32.Vec3{0, 1, 0}
					} else {
						normal = mgl32.Vec3{0, -1, 0}
					}
				}

				// Point: Find closer point on the overlap surface
				point := posA.Add(posB).Mul(0.5)
				// Offset point towards the surface along the normal
				point = point.Add(normal.Mul(depth * 0.5))

				contacts = append(contacts, Contact{
					BodyA:  bA,
					BodyB:  bB,
					Point:  point,
					Normal: normal,
					Depth:  depth,
				})
			}
		}
	}
	return contacts
}

func ResolveContact(c Contact, dt float32) {
	bA := c.BodyA
	bB := c.BodyB // Can be nil

	restitution := float32(0.2)
	friction := float32(0.5)

	if bA.Col != nil {
		restitution = bA.Col.Restitution
		friction = bA.Col.Friction
	}

	nA := c.Normal

	// R vectors
	rA := c.Point.Sub(bA.WorldCoM) // Vector from CoM to contact
	var rB mgl32.Vec3
	if bB != nil {
		rB = c.Point.Sub(bB.WorldCoM)
	}

	// Velocities at contact point
	// v = v_cm + w x r
	vA := bA.Rb.Velocity.Add(bA.Rb.AngularVelocity.Cross(rA))
	var vB mgl32.Vec3
	if bB != nil {
		vB = bB.Rb.Velocity.Add(bB.Rb.AngularVelocity.Cross(rB))
	}

	// Relative velocity
	vRel := vA.Sub(vB)

	// Check if separating
	velAlongNormal := vRel.Dot(nA)
	if velAlongNormal > 0 {
		return
	}

	// Compute Impulse Scalar j
	// j = -(1+e) * vRel.n / (invM_A + invM_B + (I_A^-1 (rA x n) x rA + ...).n)

	invMassA := float32(0.0)
	if !bA.Rb.IsStatic {
		invMassA = 1.0 / bA.Rb.Mass
	}
	invMassB := float32(0.0)
	if bB != nil && !bB.Rb.IsStatic {
		invMassB = 1.0 / bB.Rb.Mass
	}

	// Angular part A
	var angA float32 = 0
	if !bA.Rb.IsStatic {
		// (I^-1 * (r x n)) x r
		raxn := rA.Cross(nA)
		ia_raxn := bA.WorldInvInertia.Mul3x1(raxn)
		term := ia_raxn.Cross(rA)
		angA = term.Dot(nA)
	}

	// Angular part B
	var angB float32 = 0
	if bB != nil && !bB.Rb.IsStatic {
		rbxnB := rB.Cross(nA)
		ib_rbxn := bB.WorldInvInertia.Mul3x1(rbxnB)
		term := ib_rbxn.Cross(rB)
		angB = term.Dot(nA)
	}

	denominator := invMassA + invMassB + angA + angB
	if denominator == 0 {
		return
	}

	j := -(1.0 + restitution) * velAlongNormal / denominator

	// Baumgarte Stabilization
	beta := float32(0.02) // Further reduced to prevent high-frequency jitter in sub-stepping
	slop := float32(0.01)
	bias := (beta / dt) * float32(math.Max(0, float64(c.Depth-slop)))
	j += bias / denominator

	// Apply Impulse
	impulse := nA.Mul(j)

	if !bA.Rb.IsStatic {
		bA.Rb.Velocity = bA.Rb.Velocity.Add(impulse.Mul(invMassA))
		// w += I^-1 (r x P)
		rxp := rA.Cross(impulse)
		bA.Rb.AngularVelocity = bA.Rb.AngularVelocity.Add(bA.WorldInvInertia.Mul3x1(rxp))
		bA.Rb.Wake()
	}

	if bB != nil && !bB.Rb.IsStatic {
		impulseB := impulse.Mul(-1)
		bB.Rb.Velocity = bB.Rb.Velocity.Add(impulseB.Mul(invMassB))
		rxp := rB.Cross(impulseB)
		bB.Rb.AngularVelocity = bB.Rb.AngularVelocity.Add(bB.WorldInvInertia.Mul3x1(rxp))
		bB.Rb.Wake()
	}

	// Friction (Tangential Impulse)
	// Tangent direction
	tangent := vRel.Sub(nA.Mul(velAlongNormal))
	if tangent.Len() > 0.001 {
		tangent = tangent.Normalize()

		// Solve for jt
		// Same denominator except different directions
		// For approximation, re-use denominator? No, angular part changes.

		// Ang A Tangent
		var angAT float32 = 0
		if !bA.Rb.IsStatic {
			raxt := rA.Cross(tangent)
			ia_raxt := bA.WorldInvInertia.Mul3x1(raxt)
			angAT = ia_raxt.Cross(rA).Dot(tangent)
		}
		var angBT float32 = 0
		if bB != nil && !bB.Rb.IsStatic {
			rbxt := rB.Cross(tangent)
			ib_rbxt := bB.WorldInvInertia.Mul3x1(rbxt)
			angBT = ib_rbxt.Cross(rB).Dot(tangent)
		}

		denomT := invMassA + invMassB + angAT + angBT
		if denomT > 0 {
			jt := -vRel.Dot(tangent) / denomT

			// Coulomb limit
			if math.Abs(float64(jt)) > float64(j*friction) {
				jt = j * friction * float32(math.Copysign(1, float64(jt)))
			}

			impulseT := tangent.Mul(jt)

			if !bA.Rb.IsStatic {
				bA.Rb.Velocity = bA.Rb.Velocity.Add(impulseT.Mul(invMassA))
				rxp := rA.Cross(impulseT)
				bA.Rb.AngularVelocity = bA.Rb.AngularVelocity.Add(bA.WorldInvInertia.Mul3x1(rxp))
			}
			if bB != nil && !bB.Rb.IsStatic {
				impulseTB := impulseT.Mul(-1)
				bB.Rb.Velocity = bB.Rb.Velocity.Add(impulseTB.Mul(invMassB))
				rxp := rB.Cross(impulseTB)
				bB.Rb.AngularVelocity = bB.Rb.AngularVelocity.Add(bB.WorldInvInertia.Mul3x1(rxp))
			}
		}
	}
}

func QuatToMat3(q mgl32.Quat) mgl32.Mat3 {
	m4 := q.Mat4()
	return mgl32.Mat3{
		m4[0], m4[1], m4[2],
		m4[4], m4[5], m4[6],
		m4[8], m4[9], m4[10],
	}
}

func PhysicsResolveAxis(world *WorldComponent, bodies []BodyInfo, self *BodyInfo, pos, vel, displacement mgl32.Vec3, axis int, vSize, friction, restitution float32) (mgl32.Vec3, mgl32.Vec3) {
	newPos := pos
	dist := displacement[axis]
	if math.Abs(float64(dist)) < 0.0001 {
		return pos, vel
	}

	stepSize := float32(0.1)
	if dist < 0 {
		stepSize = -0.1
	}

	remaining := float32(math.Abs(float64(dist)))
	if remaining > 10.0 { // Safety cap: max 10 meters per frame resolution
		remaining = 10.0
	}

	iterations := 0
	for remaining > 0 && iterations < 200 { // Max 200 steps
		iterations++
		move := stepSize
		if remaining < float32(math.Abs(float64(stepSize))) {
			move = dist / float32(math.Abs(float64(dist))) * remaining
		}

		testPos := newPos
		testPos[axis] += move

		if PhysicsCheckCollision(world, bodies, self, testPos, self.ScaledExtents, vSize) {
			// Apply restitution
			vel[axis] = -vel[axis] * restitution
			if math.Abs(float64(vel[axis])) < 0.1 {
				vel[axis] = 0
			}

			// Apply friction to tangential axes
			for a := 0; a < 3; a++ {
				if a != axis {
					vel[a] *= (1.0 - friction)
					if math.Abs(float64(vel[a])) < 0.01 {
						vel[a] = 0
					}
				}
			}
			break
		}
		newPos = testPos
		remaining -= float32(math.Abs(float64(move)))
	}

	return newPos, vel
}

func PhysicsCheckCollision(world *WorldComponent, bodies []BodyInfo, self *BodyInfo, pos mgl32.Vec3, halfExtents mgl32.Vec3, vSize float32) bool {
	if halfExtents.X() < 0.001 || halfExtents.Y() < 0.001 || halfExtents.Z() < 0.001 {
		return false
	}

	// 1. World Voxel Collision
	if world != nil {
		if vSize <= 0 {
			vSize = 0.1
		}

		min := pos.Sub(halfExtents)
		max := pos.Add(halfExtents)

		// Integer bounds for grid iteration
		minX, minY, minZ := int(math.Floor(float64(min.X()/vSize))), int(math.Floor(float64(min.Y()/vSize))), int(math.Floor(float64(min.Z()/vSize)))
		maxX, maxY, maxZ := int(math.Floor(float64(max.X()/vSize))), int(math.Floor(float64(max.Y()/vSize))), int(math.Floor(float64(max.Z()/vSize)))

		// Iterate over all potential world voxels intersecting the AABB
		for gx := minX; gx <= maxX; gx++ {
			for gy := minY; gy <= maxY; gy++ {
				for gz := minZ; gz <= maxZ; gz++ {
					// Check if there is a voxel at this world position
					if hit, _ := world.MainXBM.GetVoxel(gx, gy, gz); hit {
						// World voxel exists. Check collision with Entity.

						// If entity has precise model, check if the world voxel VOLUMETRICALLY overlaps any solid model voxel.
						if self != nil && self.Tr != nil && self.Model != nil {
							// Determine World Voxel AABB corners
							wvMin := mgl32.Vec3{float32(gx) * vSize, float32(gy) * vSize, float32(gz) * vSize}
							wvMax := wvMin.Add(mgl32.Vec3{vSize, vSize, vSize})

							corners := [8]mgl32.Vec3{
								{wvMin.X(), wvMin.Y(), wvMin.Z()},
								{wvMin.X(), wvMin.Y(), wvMax.Z()},
								{wvMin.X(), wvMax.Y(), wvMin.Z()},
								{wvMin.X(), wvMax.Y(), wvMax.Z()},
								{wvMax.X(), wvMin.Y(), wvMin.Z()},
								{wvMax.X(), wvMin.Y(), wvMax.Z()},
								{wvMax.X(), wvMax.Y(), wvMin.Z()},
								{wvMax.X(), wvMax.Y(), wvMax.Z()},
							}

							// Transform corners to Local Space to find Local AABB of the World Voxel
							var lMin, lMax mgl32.Vec3
							first := true

							invRot := self.Tr.Rotation.Conjugate()
							pos := self.Tr.Position

							const VoxelUnitSize = 0.1
							sx := self.Tr.Scale.X()
							if sx < 0.001 {
								sx = 1
							}
							sy := self.Tr.Scale.Y()
							if sy < 0.001 {
								sy = 1
							}
							sz := self.Tr.Scale.Z()
							if sz < 0.001 {
								sz = 1
							}

							offX := float32(self.Model.SizeX) / 2.0
							offY := float32(self.Model.SizeY) / 2.0
							offZ := float32(self.Model.SizeZ) / 2.0

							for _, c := range corners {
								rel := c.Sub(pos)
								loc := invRot.Rotate(rel)

								// Convert to "Voxel Index Space" coordinates
								vx := (loc.X() / (VoxelUnitSize * sx)) + offX
								vy := (loc.Y() / (VoxelUnitSize * sy)) + offY
								vz := (loc.Z() / (VoxelUnitSize * sz)) + offZ

								if first {
									lMin = mgl32.Vec3{vx, vy, vz}
									lMax = mgl32.Vec3{vx, vy, vz}
									first = false
								} else {
									lMin = mgl32.Vec3{float32(math.Min(float64(lMin.X()), float64(vx))), float32(math.Min(float64(lMin.Y()), float64(vy))), float32(math.Min(float64(lMin.Z()), float64(vz)))}
									lMax = mgl32.Vec3{float32(math.Max(float64(lMax.X()), float64(vx))), float32(math.Max(float64(lMax.Y()), float64(vy))), float32(math.Max(float64(lMax.Z()), float64(vz)))}
								}
							}

							// Check if any Model Voxel overlaps this Local AABB.
							hitModel := false

							if self.PhysicsData != nil {
								for _, v := range self.PhysicsData.Corners {
									ix, iy, iz := float32(v.X), float32(v.Y), float32(v.Z)
									if lMax.X() > ix && lMin.X() < ix+1 &&
										lMax.Y() > iy && lMin.Y() < iy+1 &&
										lMax.Z() > iz && lMin.Z() < iz+1 {
										hitModel = true
										break
									}
								}
								if !hitModel {
									for _, v := range self.PhysicsData.Edges {
										ix, iy, iz := float32(v.X), float32(v.Y), float32(v.Z)
										if lMax.X() > ix && lMin.X() < ix+1 &&
											lMax.Y() > iy && lMin.Y() < iy+1 &&
											lMax.Z() > iz && lMin.Z() < iz+1 {
											hitModel = true
											break
										}
									}
								}
							} else if self.Model != nil {
								for _, v := range self.Model.Voxels {
									ix, iy, iz := float32(v.X), float32(v.Y), float32(v.Z)
									if lMax.X() > ix && lMin.X() < ix+1 &&
										lMax.Y() > iy && lMin.Y() < iy+1 &&
										lMax.Z() > iz && lMin.Z() < iz+1 {
										hitModel = true
										break
									}
								}
							}

							if !hitModel {
								continue // Miss
							}
						}
						return true
					}
				}
			}
		}
	}

	// 2. Entity-vs-Entity AABB Collision
	for _, other := range bodies {
		if self != nil && other.Eid == self.Eid {
			continue
		}

		// AABB Check
		otherPos := other.Tr.Position
		otherExt := other.ScaledExtents

		if math.Abs(float64(pos.X()-otherPos.X())) < float64(halfExtents.X()+otherExt.X()) &&
			math.Abs(float64(pos.Y()-otherPos.Y())) < float64(halfExtents.Y()+otherExt.Y()) &&
			math.Abs(float64(pos.Z()-otherPos.Z())) < float64(halfExtents.Z()+otherExt.Z()) {
			return true
		}
	}

	return false
}
