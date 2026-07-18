package hl1

import (
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestApplyHL1ExportProfileRustyVoxelRTInterop(t *testing.T) {
	opts, err := ApplyHL1ExportProfile(ImportOptions{
		ChunkPayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1,
		ExportProfile:    HL1ExportProfileRustyVoxelRTInteropV1,
	})
	if err != nil {
		t.Fatalf("ApplyHL1ExportProfile failed: %v", err)
	}
	if opts.ChunkPayloadKind != content.ImportedWorldChunkPayloadSparseJSONV1 {
		t.Fatalf("expected sparse JSON chunks, got %q", opts.ChunkPayloadKind)
	}
	if !opts.EmitGameAssets {
		t.Fatal("expected Rust interop profile to enable generated game assets")
	}
}

func TestNormalizeHL1ExportProfileAliases(t *testing.T) {
	profile, err := NormalizeHL1ExportProfile("rusty-voxelrt-interop-v1")
	if err != nil {
		t.Fatalf("NormalizeHL1ExportProfile failed: %v", err)
	}
	if profile != HL1ExportProfileRustyVoxelRTInteropV1 {
		t.Fatalf("unexpected profile %q", profile)
	}
}
