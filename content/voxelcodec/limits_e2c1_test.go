package voxelcodec_test

import (
	"errors"
	"github.com/gekko3d/gekko/content/voxelcodec"
	"reflect"
	"testing"
)

func TestE2c1CodecLimitsAreNormalizedOwnedAndReadableAfterClose(t *testing.T) {
	bounds := &voxelcodec.Bounds{Min: [3]int32{-3, -2, -1}, Max: [3]int32{3, 2, 1}}
	codec := c1aCodec(t, voxelcodec.Options{Limits: voxelcodec.Limits{MaxBricks: 2, Bounds: bounds}})
	want := voxelcodec.DefaultLimits()
	want.MaxBricks = 2
	want.Bounds = &voxelcodec.Bounds{Min: bounds.Min, Max: bounds.Max}
	first := codec.Limits()
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("normalized profile=%+v want=%+v", first, want)
	}
	bounds.Min[0] = -99
	first.MaxBricks = 1
	first.Bounds.Max[0] = 99
	if !reflect.DeepEqual(codec.Limits(), want) {
		t.Fatal("codec profile aliases constructor or accessor bounds")
	}
	frame, _, err := codec.Encode(voxelcodec.Document{Kind: "probe"})
	if err != nil {
		t.Fatal(err)
	}
	if err := codec.Close(); err != nil {
		t.Fatal(err)
	}
	after := codec.Limits()
	if !reflect.DeepEqual(after, want) {
		t.Fatal("closed codec lost immutable profile")
	}
	after.Bounds.Min[0] = -88
	if !reflect.DeepEqual(codec.Limits(), want) {
		t.Fatal("closed profile exposes internal bounds")
	}
	if _, _, err := codec.Encode(voxelcodec.Document{Kind: "probe"}); !errors.Is(err, voxelcodec.ErrClosed) {
		t.Fatal("profile accessor changed closed encode lifecycle", err)
	}
	if _, _, err := codec.Decode(frame); !errors.Is(err, voxelcodec.ErrClosed) {
		t.Fatal("profile accessor changed closed decode lifecycle", err)
	}
}
