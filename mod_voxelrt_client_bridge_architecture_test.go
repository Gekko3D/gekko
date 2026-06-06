package gekko

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestVoxelRtBridgeRendererInternalsTouchpointsAreAudited(t *testing.T) {
	files, err := filepath.Glob("mod_voxelrt_client*.go")
	if err != nil {
		t.Fatalf("glob voxelrt bridge files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected voxelrt bridge files")
	}

	allowedBufferManagerLines := map[string]string{
		"if state.RtApp.BufferManager != nil {":  "frame batch boundary guard",
		"state.RtApp.BufferManager.BeginBatch()": "frame batch boundary",
		"state.RtApp.BufferManager.EndBatch()":   "frame batch boundary",
		"if texAsset, ok := spriteAtlasTexture(server, batch.AtlasKey); ok && state.RtApp.BufferManager != nil {": "transitional sprite atlas upload",
		"state.RtApp.BufferManager.SetSpriteAtlas(":                                                               "transitional sprite atlas upload",
	}
	gpuHostRef := regexp.MustCompile(`\bgpu_rt\.[A-Za-z0-9_]*Host\b`)

	var violations []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for lineNo, raw := range strings.Split(string(data), "\n") {
			line := strings.TrimSpace(raw)
			if strings.Contains(line, "BufferManager") {
				if _, ok := allowedBufferManagerLines[line]; !ok {
					violations = append(violations, fmt.Sprintf("%s:%d unexpected BufferManager touchpoint: %s", file, lineNo+1, line))
				}
			}
			if gpuHostRef.MatchString(line) {
				violations = append(violations, fmt.Sprintf("%s:%d bridge should use typed app inputs instead of GPU host records: %s", file, lineNo+1, line))
			}
		}
	}

	if len(violations) > 0 {
		t.Fatalf("voxelrt bridge renderer-internal touchpoints must go through typed App input APIs or be explicitly audited:\n%s", strings.Join(violations, "\n"))
	}
}
