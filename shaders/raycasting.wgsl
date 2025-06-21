struct VertexOutput {
    @builtin(position) position: vec4f,
    @location(0) uv: vec2f,
};

struct Camera {
    viewProjection: mat4x4f,
    invViewProjection: mat4x4f,
    position: vec3f,
    width: f32,
    height: f32,
};

struct VoxelModel {
    modelMatrix: mat4x4f,
    invModelMatrix: mat4x4f,
    size: vec3f,          // Size of the volume in world units
    resolution: vec3f,    // Voxel grid resolution (e.g., 256x256x256)
    paletteIndex: f32,
    voxelPoolOffset: f32,
};

struct Voxel {
    colorIndex: f32,
    alpha: f32,
};

struct AabbRayHit {
    isHit: bool,
    tMin: f32,
    tMax: f32,
}

@group(0) @binding(0)
var<uniform> camera: Camera;
@group(0) @binding(1)
var<storage> voxelModels: array<VoxelModel>;
@group(0) @binding(2)
var<storage, read> voxelPool: array<Voxel>;
@group(0) @binding(3)
var<storage, read> voxPalettes: array<array<vec4f, 256>>;

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

@fragment
fn fs_main(vertex: VertexOutput) -> @location(0) vec4f {
    let res = vec2f(camera.width, camera.height);

    let uv = (vertex.position.xy / res) * 2.0 - 1.0;
    var clip = vec4f(uv, 1.0, 1.0);
    var world = camera.invViewProjection * clip;
    world /= world.w;
    // Transform ray to world space
    let rayWorld  = normalize(world.xyz - camera.position);

    let numOfModels = arrayLength(&voxelModels);

    // First pass: Find all intersecting volumes and their tMin distances
    var intersectingVoxModels: array<u32, 32>; // Adjust size as needed
    var voxModelCount = 0u;
    var voxModelDistances: array<f32, 32>;

    // Find all intersecting volumes
    for (var i = 0u; i < numOfModels; i++) {
        let voxelModel = voxelModels[i];
        let rayOrigin = (voxelModel.invModelMatrix * vec4f(camera.position, 1.0)).xyz;
        let rayDir = normalize((voxelModel.invModelMatrix * vec4f(rayWorld, 0.0)).xyz);

        // AABB intersection test
        let rayHit = aabbRayIntersection(rayOrigin, rayDir, voxelModel);

        if (rayHit.isHit) {
            // Store volume index and distance
            intersectingVoxModels[voxModelCount] = i;
            voxModelDistances[voxModelCount] = rayHit.tMin;
            voxModelCount++;
        }
    }

    // Sort volumes by distance (back to front)
    for (var i = 0u; i < voxModelCount; i++) {
        for (var j = i + 1u; j < voxModelCount; j++) {
            if (voxModelDistances[i] > voxModelDistances[j]) {
                // Swap distances
                let tempDist = voxModelDistances[i];
                voxModelDistances[i] = voxModelDistances[j];
                voxModelDistances[j] = tempDist;

                // Swap volume indices
                let tempVol = intersectingVoxModels[i];
                intersectingVoxModels[i] = intersectingVoxModels[j];
                intersectingVoxModels[j] = tempVol;
            }
        }
    }

    var finalColor = vec4f(0.0);
    var accumulatedAlpha = 0.0;
    // Process each volume instance
    for (var i = 0u; i < voxModelCount; i++) {
        if (accumulatedAlpha >= 0.99) {
            break;
        }

        let voxelModel = voxelModels[intersectingVoxModels[i]];
        let rayOrigin = (voxelModel.invModelMatrix * vec4f(camera.position, 1.0)).xyz;
        let rayDir = normalize((voxelModel.invModelMatrix * vec4f(rayWorld, 0.0)).xyz);

        // AABB intersection
        let rayHit = aabbRayIntersection(rayOrigin, rayDir, voxelModel);

        // DDA setup
        var rayPos = rayOrigin + rayDir * rayHit.tMin;
        let voxelSize = voxelModel.size / voxelModel.resolution;
        var voxel = vec3i(floor(rayPos / voxelSize));
        let step = vec3i(sign(rayDir));
        let delta = voxelSize / abs(rayDir);

        var next = (vec3f(voxel) + max(vec3f(step), vec3f(0.5))) * voxelSize;
        var tMaxXYZ = (next - rayPos) / rayDir;

        // Volume-specific traversal
        var t = rayHit.tMin;
        while (accumulatedAlpha < 0.99 && t < rayHit.tMax) {
            if (all(voxel >= vec3i(0)) && all(voxel < vec3i(voxelModel.resolution))) {
                // Calculate buffer index with volume offset
                let voxelIndex = getVoxelPoolIndex(voxelModel, voxel);
                let voxelData = voxelPool[voxelIndex];

                if (voxelData.alpha > 0.0) {
                    // Get color from palette
                    let baseColor = voxPalettes[u32(voxelModel.paletteIndex)][u32(voxelData.colorIndex)];

                    // Compute normal and shade
                    let normal = computeNormal(voxel, vec3i(voxelModel.resolution), u32(voxelModel.voxelPoolOffset));
                    var shadedColor = shadeVoxel(baseColor, normal, rayDir);
                    shadedColor.a *= voxelData.alpha;

                    // Front-to-back blending
                    let weight = shadedColor.a * (1.0 - accumulatedAlpha);

                    let tmp = finalColor.rgb + shadedColor.rgb * weight;
                    finalColor = vec4f(tmp.rgb, finalColor.a);

                    accumulatedAlpha += weight;
                }
            }

            // Move to next voxel
            if (tMaxXYZ.x < tMaxXYZ.y && tMaxXYZ.x < tMaxXYZ.z) {
                voxel.x += step.x;
                t = tMaxXYZ.x;
                tMaxXYZ.x += delta.x;
            } else if (tMaxXYZ.y < tMaxXYZ.z) {
                voxel.y += step.y;
                t = tMaxXYZ.y;
                tMaxXYZ.y += delta.y;
            } else {
                voxel.z += step.z;
                t = tMaxXYZ.z;
                tMaxXYZ.z += delta.z;
            }
        }
    }

    return vec4f(finalColor.rgb, accumulatedAlpha);
}

fn aabbRayIntersection(rayOrigin: vec3f, rayDir: vec3f, voxelModel: VoxelModel) -> AabbRayHit {
    // AABB is from (0,0,0) to volume.size in model space
    let aabbMin = vec3f(0.0);
    let aabbMax = voxelModel.size;

    // Compute intersection with volume AABB (slab method)
    var hit: AabbRayHit;
    hit.isHit = false;
    hit.tMin = 0.0;
    hit.tMax = 1000.0; // Large value for far plane

    for (var i = 0; i < 3; i++) {
        if (abs(rayDir[i]) < 0.0001) {
            // Ray is parallel to slab
            if (rayOrigin[i] < aabbMin[i] || rayOrigin[i] > aabbMax[i]) {
                // No intersection
                break;
            }
        } else {
            let invDir = 1.0 / rayDir[i];
            var t1 = (aabbMin[i] - rayOrigin[i]) * invDir;
            var t2 = (aabbMax[i] - rayOrigin[i]) * invDir;

            if (t1 > t2) {
                let temp = t1;
                t1 = t2;
                t2 = temp;
            }

            hit.tMin = max(hit.tMin, t1);
            hit.tMax = min(hit.tMax, t2);

            if (hit.tMin > hit.tMax) {
                // No intersection
                break;
            } else {
                hit.isHit = true;
            }
        }
    }
    return hit;
}

fn getVoxelPoolIndex(voxelModel: VoxelModel, voxel: vec3i) -> u32 {
    return u32(voxelModel.voxelPoolOffset) +
            u32(voxel.x + voxel.y * i32(voxelModel.resolution.x) +
                voxel.z * i32(voxelModel.resolution.x) * i32(voxelModel.resolution.y));
}

fn computeNormal(voxel: vec3i, resolution: vec3i, voxelPoolOffset: u32) -> vec3f {
    // Calculate neighbor indices
    let xp = min(voxel.x + 1, resolution.x - 1);
    let xn = max(voxel.x - 1, 0);
    let yp = min(voxel.y + 1, resolution.y - 1);
    let yn = max(voxel.y - 1, 0);
    let zp = min(voxel.z + 1, resolution.z - 1);
    let zn = max(voxel.z - 1, 0);

    // Get alpha values from neighbors
    let idx_xp = voxelPoolOffset + u32(xp + voxel.y * resolution.x + voxel.z * resolution.x * resolution.y);
    let idx_xn = voxelPoolOffset + u32(xn + voxel.y * resolution.x + voxel.z * resolution.x * resolution.y);
    let idx_yp = voxelPoolOffset + u32(voxel.x + yp * resolution.x + voxel.z * resolution.x * resolution.y);
    let idx_yn = voxelPoolOffset + u32(voxel.x + yn * resolution.x + voxel.z * resolution.x * resolution.y);
    let idx_zp = voxelPoolOffset + u32(voxel.x + voxel.y * resolution.x + zp * resolution.x * resolution.y);
    let idx_zn = voxelPoolOffset + u32(voxel.x + voxel.y * resolution.x + zn * resolution.x * resolution.y);

    let x1 = voxelPool[idx_xp].alpha;
    let x2 = voxelPool[idx_xn].alpha;
    let y1 = voxelPool[idx_yp].alpha;
    let y2 = voxelPool[idx_yn].alpha;
    let z1 = voxelPool[idx_zp].alpha;
    let z2 = voxelPool[idx_zn].alpha;

    return normalize(vec3f(x2 - x1, y2 - y1, z2 - z1));
}

fn shadeVoxel(baseColor: vec4f, normal: vec3f, rayDir: vec3f) -> vec4f {
    let ambient = 0.2;
    let diffuse = max(0.0, dot(normal, -rayDir)) * 0.8;
    let intensity = ambient + diffuse;
//    return vec4f(baseColor.rgb * intensity, baseColor.a);
    return baseColor;
}