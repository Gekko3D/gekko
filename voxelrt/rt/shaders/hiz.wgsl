// voxelrt/shaders/hiz.wgsl

@group(0) @binding(0) var sourceTexture: texture_2d<f32>; // Use texture_2d<f32> for sampled depth, or texture_depth_2d if using comparison
// Since we are reading raw values in compute, we likely bind it as a sampled texture (float) or use textureLoad.
// If the source is the depth buffer, it's usually texture_depth_2d.
// However, WebGPU doesn't allow binding texture_depth_2d as storage. We use textureLoad.

@group(0) @binding(1) var destTexture: texture_storage_2d<r32float, write>;

// Simple 2x2 reduction
@compute @workgroup_size(8, 8, 1)
fn main(@builtin(global_invocation_id) global_id: vec3<u32>) {
    let destSize = textureDimensions(destTexture);
    if (global_id.x >= destSize.x || global_id.y >= destSize.y) {
        return;
    }

    // Source coords
    let srcBase = global_id.xy * 2u;
    
    // Clamp to source dimensions
    let srcSize = textureDimensions(sourceTexture);
    
    // We want the MAX depth (furthest) for culling if we treat depth as standard 0..1 (0=near, 1=far)?
    // Wait, typical Occlusion Culling uses Hierarchical Z Buffer.
    // If we want to safely cull, we need to know the *nearest* visible surface in the tile.
    // So we want the MIN depth (closest to camera) for standard Z-buffer?
    // Let's verify standard Hi-Z:
    // To cull an object, we check if its closest point is FURTHER than the furthest thing in the occlusion buffer.
    // Wait.
    // The Occlusion Buffer represents "What is definitely occluding stuff".
    // So it stores the *furthest* depth of the *closest* visible surfaces?
    // No.
    // The Hi-Z buffer stores the *furthest* depth value in a region of the *current depth buffer*.
    // If an object's *nearest* point is *further* than the Hi-Z *furthest* value, it is occluded.
    // Wait. If Hi-Z value is 0.5 (mid-range), and object is at 0.8.
    // That means EVERYTHING in that tile is closer than 0.5.
    // So 0.8 is definitely behind 0.5. Occluded.
    // So Hi-Z should store the MAX depth (furthest value) of the screen depth buffer.
    // Wait. If depth buffer is 0=near, 1=far.
    // If a pixel is 0.2, another is 0.3.
    // The tile max is 0.3. 
    // If an object is at 0.4. Is it occluded?
    // No, 0.3 is the furthest thing seen. So something exists at 0.3.
    // But maybe there is a hole at 0.2?
    // Hi-Z Conservative Culling:
    // Any pixel in the tile might be an occluder.
    // We want to know: Is the *entire* tile covered by something closer than the object?
    // If we store MAX depth of a tile, say 0.9.
    // And object is at 0.95.
    // We can't say it's occluded, because 0.9 means *something* is at 0.9, but maybe something else is at 0.1?
    // Actually, we want to know the *furthest possible depth* within that tile. 
    // If the object's *closest* point is > `max(tile_depths)`, then the object is fully behind the "deepest" thing in the tile? 
    // No, that doesn't prove full occlusion.
    
    // Let's reverse.
    // If we want to ensure an object is HIDDEN by the depth buffer.
    // The depth buffer represents visible surfaces.
    // For an object to be hidden, *every* pixel covered by the object's AABB must have a depth < object's depth.
    // So, in a tile of the depth buffer, we need the *maximum* value (furthest point).
    // If object_min_z > tile_max_z, then object is further than the furthest thing in that tile.
    // Correct.
    // Example: Tile depths = [0.1, 0.2, 0.1, 0.2]. Max = 0.2.
    // Object Z = 0.5.
    // Since Max(tile) = 0.2, ALL pixels in tile are <= 0.2.
    // Object is > 0.2.
    // Therefore Object is behind ALL pixels.
    // Occluded.
    
    // So Hi-Z MIPs should use MAX reduction.
    
    var d: f32 = 0.0;
    
    // Texture load (0 level)
    // Note: If this is the *first* pass (SceneDepth -> Mip0), we might read from depth texture.
    // If subsequent passes (MipN -> MipN+1), we read from previous mip.
    // Let's assume we dispatch separate passes or handle inputs appropriately.
    // Usually pass 0: Input=Depth, Output=Mip0.
    // Pass N: Input=MipN-1, Output=MipN.
    
    // Fetch 4 samples
    let s00 = textureLoad(sourceTexture, srcBase + vec2<u32>(0, 0), 0).x;
    let s10 = textureLoad(sourceTexture, srcBase + vec2<u32>(1, 0), 0).x;
    let s01 = textureLoad(sourceTexture, srcBase + vec2<u32>(0, 1), 0).x;
    let s11 = textureLoad(sourceTexture, srcBase + vec2<u32>(1, 1), 0).x;
    
    // Bounds check? TextureLoad handles OOB differently (undefined or clamped?) or we should check.
    // It's safer to check. But for power-of-two reduction loop it works well.
    // If srcSize is not multiple of 2, we should be careful.
    
    // Assuming we handle boundary by taking max of available?
    // Or initialize with 0.0 (near) so it doesn't contribute to Max?
    // Max reduction: if we sample OOB, we should return 0.0 (near) so we don't accidentally say "it's 1.0 (far)" which would cull things falsely?
    // Wait. If tile max is 0.0, and object is 0.5. 0.5 > 0.0. Object occluded? YES.
    // So if we read OOB (empty space), we should return 1.0 (FAR) so that 'max' becomes 1.0.
    // If tile max is 1.0, object > 1.0 is impossible (usually). Or object 0.5 > 1.0 is FALSE. Visible.
    // So OOB should be 1.0 (FAR). Not 0.0.
    
    // But textureLoad clamps coords or returns 0?
    // In WGSL textureLoad on valid texture with coords >= dimension is undefined or clamps?
    // Usually safe to check.
    
    var maxDepend = 0.0;
    
    // Let's just use strict logic.
    let sx = srcBase.x;
    let sy = srcBase.y;
    
    var val00 = 60000.0; if (sx < srcSize.x && sy < srcSize.y) { val00 = textureLoad(sourceTexture, vec2<u32>(sx, sy), 0).x; }
    var val10 = 60000.0; if (sx + 1u < srcSize.x && sy < srcSize.y) { val10 = textureLoad(sourceTexture, vec2<u32>(sx + 1u, sy), 0).x; }
    var val01 = 60000.0; if (sx < srcSize.x && sy + 1u < srcSize.y) { val01 = textureLoad(sourceTexture, vec2<u32>(sx, sy + 1u), 0).x; }
    var val11 = 60000.0; if (sx + 1u < srcSize.x && sy + 1u < srcSize.y) { val11 = textureLoad(sourceTexture, vec2<u32>(sx + 1u, sy + 1u), 0).x; }
    
    // For Standard Depth (0=Near, 1=Far):
    // Reduction is MAX.
    d = max(max(val00, val10), max(val01, val11));
    
    // NOTE: G-Buffer Depth output is R32F with "t" (distance). 
    // It is NOT 0..1 depth buffer. It is WORLD DISTANCE.
    // gbuffer.wgsl: textureStore(out_depth, ... vec4<f32>(hit_res.t, ...))
    // Empty space is 60000.0.
    // So 0 is near, 60000 is far.
    // Logic holds: Max(t) works.
    // If tile has [10, 20, 10, 15]. Max=20.
    // Object at 30. 30 > 20. Occluded.
    // OOB should be 60000.0 (Far).
    
    // Wait. If we are using G-Buffer linear depth "t", we must project AABB to "t" distance from camera, not "Z" clip space.
    // That's fine, easier actually.
    // Distance from camera plane? Or distance from camera point?
    // G-Buffer "t" is ray distance (Euclidean or View Plane Z?).
    // Raytrace shader: t is distance along ray.
    // So it's Euclidean distance from eye.
    // When checking AABB, we should use minimum distance from eye to AABB.
    // If min_dist(AABB) > max_dist(Tile), then occluded.
    
    textureStore(destTexture, global_id.xy, vec4<f32>(d, 0.0, 0.0, 0.0));
}
