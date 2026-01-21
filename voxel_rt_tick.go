package gekko


// voxelRtTick finalizes the per-frame update:
// - Ends the GPU batch
// - Updates particle buffers/bind groups
// - Processes debug rays and advances the renderer update
func voxelRtTick(state *VoxelRtState, time *Time, cmd *Commands) {
	if state == nil || state.RtApp == nil {
		return
	}

	// End batching and process all accumulated updates
	state.RtApp.Profiler.BeginScope("GPU Batch")
	state.RtApp.BufferManager.EndBatch()
	state.RtApp.Profiler.EndScope("GPU Batch")

	// CPU-simulate and upload particle instances
	instances := particlesCollect(state, time, cmd)
	pRecreated := state.RtApp.BufferManager.UpdateParticles(instances)
	if pRecreated || state.RtApp.BufferManager.ParticlesBindGroup0 == nil {
		state.RtApp.BufferManager.CreateParticlesBindGroups(state.RtApp.ParticlesPipeline)
	}

	state.RtApp.Profiler.BeginScope("RT Update")

	// Process debug rays BEFORE Update() so DrawText is captured
	dt := float32(time.Dt)
	if dt <= 0 {
		dt = 1.0 / 60.0
	}
	remainingRays := state.debugRays[:0]
	for _, ray := range state.debugRays {
		// Calculate hit point for visualization
		hit := state.Raycast(ray.Origin.Add(ray.Dir.Mul(0.1)), ray.Dir, 1000.0)
		dist := float32(100.0)
		if hit.Hit {
			dist = hit.T + 0.1
			// Draw marker at hit
			if x, y, ok := state.Project(ray.Origin.Add(ray.Dir.Mul(dist))); ok {
				state.RtApp.DrawText("*", x-8, y-16, 2.0, ray.Color)
			}
		}

		// Draw path
		steps := 50
		for i := 1; i <= steps; i++ {
			t := (dist / float32(steps)) * float32(i)
			if x, y, ok := state.Project(ray.Origin.Add(ray.Dir.Mul(t))); ok {
				// Fade alpha based on distance for cooler look
				alpha := 1.0 - (t/dist)*0.8
				color := ray.Color
				color[3] *= alpha
				state.RtApp.DrawText(".", x-6, y-12, 1.4, color)
			}
		}

		ray.Duration -= dt
		if ray.Duration > 0 {
			remainingRays = append(remainingRays, ray)
		}
	}
	state.debugRays = remainingRays

	state.RtApp.Update()
	state.RtApp.Profiler.EndScope("RT Update")
}