# GPU and CPU Struct Alignment

When Go structs back WebGPU buffers, alignment mistakes usually fail as silent corruption rather than obvious crashes. Use this note as the minimum layout checklist.

## Main Rule

Do not put `vec3` fields inside WGSL structs that are stored in buffers.

`vec3<f32>` and `vec3<i32>` consume 12 bytes of payload but still require 16-byte alignment. That mismatch is a common reason CPU-packed data no longer lines up with GPU reads.

## Safe Patterns

- Use explicit scalars instead of `vec3`.
- If you need three packed components, promote the field to `vec4` and ignore `.w`.
- Keep total struct size a multiple of the largest required alignment, usually 16 bytes.

## Failure Example

Go:

```go
type ProbeEntry struct {
    Coords  [4]int32
    Index   int32
    Padding [3]int32
}
```

WGSL:

```wgsl
struct ProbeEntry {
    coords: vec4<i32>,
    index: i32,
    pad: vec3<i32>,
};
```

The Go struct is packed as 32 bytes. The WGSL struct becomes 48 bytes because `pad` is forced to a 16-byte boundary. Every element after the first is now misaligned.

## Practical Checklist

1. Search WGSL buffer structs for `vec3`.
2. Confirm CPU and GPU structs agree on both field offsets and total stride.
3. When debugging corrupted data, inspect the first buffer entries on the CPU and write raw fields to a debug output on the GPU.
4. Check Naga validation output when available; it often exposes layout assumptions early.
