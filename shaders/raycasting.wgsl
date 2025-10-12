struct VertexOutput {
    @builtin(position) position: vec4f,
    @location(0) uv: vec2f,
};

struct RenderParameters {
    width: u32,
    height: u32,
    emptyBlockValue: u32
}

struct Camera {
    viewProjection: mat4x4f,
    invViewProjection: mat4x4f,
    position: vec4f
};

struct Transform {
    modelMatrix: mat4x4f,
    invModelMatrix: mat4x4f
};

struct VoxelInstance {
    size: vec3u,
    paletteId: u32,
    macroGrid: MacroGrid
};

struct Brick {
    position: vec3u,       // Position in macro grid
    dataOffset: u32        // Offset in brick pool
};

struct MacroGrid {
    size: vec3u,           // Dimensions in macro blocks
    brickSize: vec3u,      // Voxels per brick (e.g., 8,8,8)
    dataOffset: u32        // Offset in macroIndex
};

struct Voxel {
    colorId: u32,
    alpha: f32
};

struct VoxInstanceHit {
    instanceId: u32,
    entryDist: f32,
    exitDist: f32
};

struct DDA3DState {
    cell: vec3u,
    step: vec3i,
    tMax: vec3f,
    tDelta: vec3f,
    tEnd: f32,
};

struct RayHit {
    color: vec4<f32>,
    t: f32,
    valid: bool,
};

struct RayResult {
    color: vec4<f32>,
    depth: f32,
};

// Group 0: compute pipeline resources
@group(0) @binding(0)
var<uniform> renderParams: RenderParameters;
@group(0) @binding(1)
var<uniform> camera: Camera;
@group(0) @binding(2)
var<storage> instanceTransforms: array<Transform>;
@group(0) @binding(3)
var<storage> voxInstances: array<VoxelInstance>;
@group(0) @binding(4)
var<storage, read> macroIndexPool: array<u32>; // 3D grid of brick indices
@group(0) @binding(5)
var<storage, read> brickPool: array<Brick>;
@group(0) @binding(6)
var<storage, read> voxelPool: array<Voxel>;
@group(0) @binding(7)
var<storage, read> palettes: array<array<vec4f, 256>>;
// Output storage texture for compute
@group(0) @binding(8)
var outputTex: texture_storage_2d<rgba8unorm, write>;

/* Render (blit) pipeline resources: use group(0) with a distinct binding to avoid conflicts with compute.
   Compute uses group(0) bindings [0..8]. We'll use binding 9 for the blit texture. */
@group(0) @binding(9)
var blitTex: texture_2d<f32>;

const EMPTY_BRICK: u32 = 0xffffffffu;
const DIRECT_COLOR_FLAG: u32 = 0x80000000u;
const COLOR_MASK: u32 = 0x7fffffffu;
const MAX_INSTANCES: u32 = 64u;
const INF: f32 = 1e9;
const OPAQUE_THRESHOLD: f32 = 0.995;
const T_EPS: f32 = 1e-4;

// ============================================================
// Generic 3D DDA
// ============================================================
fn dda3dInit(origin: vec3<f32>, dir: vec3<f32>, gridSize: vec3<u32>, cellSize: vec3<f32>, tEntry: f32, tExit: f32) -> DDA3DState {
    let pos = origin + dir * tEntry;

    // compute initial cell indices
    var cell = vec3<u32>(
        clamp(u32(floor(pos.x / cellSize.x)), 0u, gridSize.x - 1u),
        clamp(u32(floor(pos.y / cellSize.y)), 0u, gridSize.y - 1u),
        clamp(u32(floor(pos.z / cellSize.z)), 0u, gridSize.z - 1u)
    );

    var step = vec3<i32>(0, 0, 0);
    if (dir.x > 0.0) { step.x = 1; } else { step.x = -1; }
    if (dir.y > 0.0) { step.y = 1; } else { step.y = -1; }
    if (dir.z > 0.0) { step.z = 1; } else { step.z = -1; }

    // calculate tMax and tDelta for each axis
    var boundary = vec3<f32>(0.0, 0.0, 0.0);
    if (dir.x > 0.0) { boundary.x = f32(cell.x + 1u) * cellSize.x; }
    else { boundary.x = f32(cell.x) * cellSize.x; }

    if (dir.y > 0.0) { boundary.y = f32(cell.y + 1u) * cellSize.y; }
    else { boundary.y = f32(cell.y) * cellSize.y; }

    if (dir.z > 0.0) { boundary.z = f32(cell.z + 1u) * cellSize.z; }
    else { boundary.z = f32(cell.z) * cellSize.z; }

    // tMax must be absolute parametric distances (from the original ray origin), not relative to tEntry.
    // Add tEntry so comparisons against tEnd (tExit) are correct and returned hits are absolute.
    var tMax = vec3<f32>(INF, INF, INF);
    if (abs(dir.x) > 1e-8) { tMax.x = tEntry + (boundary.x - pos.x) / dir.x; }
    if (abs(dir.y) > 1e-8) { tMax.y = tEntry + (boundary.y - pos.y) / dir.y; }
    if (abs(dir.z) > 1e-8) { tMax.z = tEntry + (boundary.z - pos.z) / dir.z; }

    var tDelta = vec3<f32>(INF, INF, INF);
    if (abs(dir.x) > 1e-8) { tDelta.x = cellSize.x / abs(dir.x); }
    if (abs(dir.y) > 1e-8) { tDelta.y = cellSize.y / abs(dir.y); }
    if (abs(dir.z) > 1e-8) { tDelta.z = cellSize.z / abs(dir.z); }

    var s: DDA3DState;
    s.cell = cell;
    s.step = step;
    s.tMax = tMax;
    s.tDelta = tDelta;
    s.tEnd = tExit;
    return s;
}

fn dda3dAdvance(state: DDA3DState) -> DDA3DState {
    var s = state;

    if (s.tMax.x < s.tMax.y) {
        if (s.tMax.x < s.tMax.z) {
            s.tMax.x += s.tDelta.x;
            s.cell.x = u32(i32(s.cell.x) + s.step.x);
        } else {
            s.tMax.z += s.tDelta.z;
            s.cell.z = u32(i32(s.cell.z) + s.step.z);
        }
    } else {
        if (s.tMax.y < s.tMax.z) {
            s.tMax.y += s.tDelta.y;
            s.cell.y = u32(i32(s.cell.y) + s.step.y);
        } else {
            s.tMax.z += s.tDelta.z;
            s.cell.z = u32(i32(s.cell.z) + s.step.z);
        }
    }

    return s;
}

// ============================================================
// Helpers
// ============================================================
fn intersectAABB(origin: vec3<f32>, dir: vec3<f32>, bmin: vec3<f32>, bmax: vec3<f32>) -> vec2<f32> {
    var tmin = -INF;
    var tmax = INF;

    for (var i = 0; i < 3; i = i + 1) {
        let o = origin[i];
        let d = dir[i];
        let minB = bmin[i];
        let maxB = bmax[i];

        if (abs(d) < 1e-8) {
            if (o < minB || o > maxB) { return vec2<f32>(1.0, -1.0); }
        } else {
            let t1 = (minB - o) / d;
            let t2 = (maxB - o) / d;
            let tminA = min(t1, t2);
            let tmaxA = max(t1, t2);
            tmin = max(tmin, tminA);
            tmax = min(tmax, tmaxA);
            if (tmin > tmax) { return vec2<f32>(1.0, -1.0); }
        }
    }

    return vec2<f32>(tmin, tmax);
}

fn voxelIndexInBrick(size: vec3<u32>, x: u32, y: u32, z: u32) -> u32 {
    return x + size.x * (y + size.y * z);
}

fn samplePalette(paletteId: u32, colorId: u32) -> vec4<f32> {
    let idx = min(colorId, 255u);
    return palettes[paletteId][idx];
}


fn blendFrontToBack(accum: vec4<f32>, src: vec4<f32>) -> vec4<f32> {
    let a = accum.a;
    let outColor = accum.rgb + src.rgb * src.a * (1.0 - a);
    let outAlpha = a + src.a * (1.0 - a);
    return vec4<f32>(outColor, outAlpha);
}

// ============================================================
// Brick-level DDA
// ============================================================
fn traceBrick(
    origin: vec3<f32>,
    dir: vec3<f32>,
    brickBase: vec3<f32>,
    brickSize: vec3<u32>,
    brick: Brick,
    paletteId: u32
) -> RayHit {
    let localOrig = origin - brickBase;
    let bmin = vec3<f32>(0.0);
    let bmax = vec3<f32>(f32(brickSize.x), f32(brickSize.y), f32(brickSize.z));
    let tints = intersectAABB(localOrig, dir, bmin, bmax);
    if (tints.x > tints.y) {
        return RayHit(vec4<f32>(0.0), INF, false);
    }

    var state = dda3dInit(localOrig, dir, brickSize, vec3<f32>(1.0), tints.x, tints.y);
    var iter: u32 = 0u;
    let maxIter = brickSize.x * brickSize.y * brickSize.z + 8u;
    var tCurr = tints.x;

    loop {
        if (iter > maxIter) { break; }
        if (state.cell.x >= brickSize.x || state.cell.y >= brickSize.y || state.cell.z >= brickSize.z) { break; }
        if (tCurr > state.tEnd) { break; }

        let idx = voxelIndexInBrick(brickSize, state.cell.x, state.cell.y, state.cell.z);
        let voxel = voxelPool[brick.dataOffset + idx];

        if (voxel.alpha > 0.0) {
            let col = samplePalette(paletteId, voxel.colorId);
            // Shade at voxel entry to avoid tie/z-fight across overlapping instances
            let tHit = tCurr;
            return RayHit(vec4<f32>(col.rgb, voxel.alpha), tHit, false);
        }

        let tNext = min(min(state.tMax.x, state.tMax.y), state.tMax.z);
        state = dda3dAdvance(state);
        tCurr = tNext;
        iter = iter + 1u;
    }

    return RayHit(vec4<f32>(0.0), INF, false);
}

// ============================================================
// Macro-level DDA
// ============================================================
fn traceTwoLevelDDA(rayOrigin: vec3<f32>, rayDir: vec3<f32>, inst: VoxelInstance, transform: Transform) -> RayHit {
    // Transform ray to object space
    let objRayOrigin = (transform.invModelMatrix * vec4f(rayOrigin, 1.0)).xyz;
    let objRayDir = normalize((transform.invModelMatrix * vec4f(rayDir, 0.0)).xyz);
    let mg = inst.macroGrid;
    let cellSize = vec3<f32>(f32(mg.brickSize.x), f32(mg.brickSize.y), f32(mg.brickSize.z));
    let bmin = vec3<f32>(0.0);
    let bmax = cellSize * vec3<f32>(f32(mg.size.x), f32(mg.size.y), f32(mg.size.z));
    let tints = intersectAABB(objRayOrigin, objRayDir, bmin, bmax);
    if (tints.x > tints.y) {
        return RayHit(vec4<f32>(0.0), INF, false);
    }

    var state = dda3dInit(objRayOrigin, objRayDir, mg.size, cellSize, tints.x, tints.y);
    var iter: u32 = 0u;
    let maxIter = mg.size.x * mg.size.y * mg.size.z + 8u;
    var tCurr = tints.x;

    loop {
        if (iter > maxIter) { break; }
        if (state.cell.x >= mg.size.x || state.cell.y >= mg.size.y || state.cell.z >= mg.size.z) { break; }
        if (tCurr > state.tEnd) { break; }

        let macroIdx = mg.dataOffset + state.cell.x + mg.size.x * (state.cell.y + mg.size.y * state.cell.z);
        let bIndex = macroIndexPool[macroIdx];

        if (bIndex != EMPTY_BRICK) {
            // Direct-color macro cell: encode color index in high-bit tagged entry
            if ((bIndex & DIRECT_COLOR_FLAG) != 0u) {
                let colorId = (bIndex & COLOR_MASK);
                let col = samplePalette(inst.paletteId, colorId);
                // Entry point of current macro cell is tCurr; convert to world-space distance
                let objHitPos = objRayOrigin + objRayDir * tCurr;
                let worldHitPos = (transform.modelMatrix * vec4f(objHitPos, 1.0)).xyz;
                let tWorld = max(0.0, dot(worldHitPos - rayOrigin, rayDir));
                return RayHit(vec4<f32>(col.rgb, 1.0), tWorld, false);
            } else {
                let brick = brickPool[bIndex];
                let brickBase = vec3<f32>(
                    f32(state.cell.x) * cellSize.x,
                    f32(state.cell.y) * cellSize.y,
                    f32(state.cell.z) * cellSize.z
                );

                let hit = traceBrick(objRayOrigin, objRayDir, brickBase, mg.brickSize, brick, inst.paletteId);
                if (hit.t < INF) {
                    // Convert object-space parametric distance to world-space ray distance for correct cross-instance sorting
                    let objHitPos = objRayOrigin + objRayDir * hit.t;
                    let worldHitPos = (transform.modelMatrix * vec4f(objHitPos, 1.0)).xyz;
                    // Project to ray to get consistent world-space distance, clamp to >= 0
                    let tWorld = max(0.0, dot(worldHitPos - rayOrigin, rayDir)); // rayDir is normalized
                    return RayHit(hit.color, tWorld, false);
                }
            }
        }

        let tNext = min(min(state.tMax.x, state.tMax.y), state.tMax.z);
        state = dda3dAdvance(state);
        tCurr = tNext;
        iter = iter + 1u;
    }

    return RayHit(vec4<f32>(0.0), INF, false);
}

// Generate ray for current pixel
fn generateRay(coord: vec2f) -> vec3f {
    let res = vec2f(f32(renderParams.width), f32(renderParams.height));
    let uv = (coord / res) * 2.0 - 1.0;
    var clip = vec4f(uv, 1.0, 1.0);
    var world = camera.invViewProjection * clip;
    world /= world.w;
    // Transform ray to world space
    let rayWorld = normalize(world.xyz - camera.position.xyz);
    return rayWorld;
}

// Main raytracing function that tests all instances
fn traceScene(rayOrigin: vec3f, rayDir: vec3f) -> RayResult {
    var accum = vec4<f32>(0.0);
    var hits: array<RayHit, MAX_INSTANCES>;
    var hitCount: u32 = 0u;
    var minDepth: f32 = INF;

    // --- Collect hits from all instances ---
    for (var i = 0u; i < arrayLength(&voxInstances) && i < MAX_INSTANCES; i = i + 1u) {
        let hit = traceTwoLevelDDA(rayOrigin, rayDir, voxInstances[i], instanceTransforms[i]);
        if (hit.t < INF) {
            hits[hitCount] = RayHit(hit.color, hit.t, true);
            hitCount = hitCount + 1u;
        }
    }

    // --- Blend hits front-to-back by distance ---
    loop {
        var closestIdx: u32 = 0xffffffffu;
        var closestT: f32 = INF;

        // find nearest remaining hit (epsilon-aware, prefer higher opacity on ties)
        for (var i = 0u; i < hitCount; i = i + 1u) {
            if (hits[i].valid) {
                let ti = hits[i].t;
                if (closestIdx == 0xffffffffu ||
                    ti + T_EPS < closestT ||
                    (abs(ti - closestT) <= T_EPS && hits[i].color.a > hits[closestIdx].color.a)) {
                    closestT = ti;
                    closestIdx = i;
                }
            }
        }

        if (closestIdx == 0xffffffffu) {
            break;
        }

        let h = hits[closestIdx];
        accum = blendFrontToBack(accum, h.color);

        // record nearest visible surface
        if (accum.a > 0.0 && h.t < minDepth) {
            minDepth = h.t;
        }

        hits[closestIdx].valid = false;

        if (accum.a >= OPAQUE_THRESHOLD) {
            break;
        }
    }

    // Default background
    if (accum.a == 0.0) {
        return RayResult(vec4<f32>(0.0), INF);
    }

    return RayResult(accum, minDepth);
}

@vertex
fn vs_main(
    @location(0) position: vec4f,
    @location(1) uv: vec2f,
) -> VertexOutput {
    var result: VertexOutput;
    result.position = position;
    result.uv = uv;
    return result;
}

// Fragment now only blits the compute output texture
@fragment
fn fs_main(vertex: VertexOutput) -> @location(0) vec4f {
    let px = i32(floor(vertex.position.x));
    let py = i32(floor(vertex.position.y));
    return textureLoad(blitTex, vec2i(px, py), 0);
}

// Compute entry: raycast per pixel into storage texture
@compute @workgroup_size(8, 8, 1)
fn cs_main(@builtin(global_invocation_id) gid: vec3u) {
    if (gid.x >= renderParams.width || gid.y >= renderParams.height) {
        return;
    }
    let rayWorld = generateRay(vec2f(f32(gid.x), f32(gid.y)));
    let rayResult = traceScene(camera.position.xyz, rayWorld);
    textureStore(outputTex, vec2u(gid.xy), rayResult.color);
}
