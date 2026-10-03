package voxelcodec_test

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/gekko3d/gekko/content/voxelcodec"
)

func TestC1cIdentityCanonicalProjectionAndLimits(t *testing.T) {
	doc := c1aDocument()
	before := c1aDocument()
	plain := c1aCodec(t, voxelcodec.Options{})
	dict := c1aCodec(t, voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 29, Bytes: []byte("canonical identity dictionary history")}})
	_, info, err := plain.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []*voxelcodec.Codec{plain, dict, c1aCodec(t, voxelcodec.Options{Limits: voxelcodec.Limits{MaxEncodedBytes: 96}})} {
		hash, size, err := c.Identity(doc)
		if err != nil || hash != info.ContentID || size != info.DecodedBytes {
			t.Fatalf("identity differs from canonical encoded body: %s %d %v", hash, size, err)
		}
	}
	reordered := c1aDocument()
	slices.Reverse(reordered.Bricks)
	if hash, size, err := plain.Identity(reordered); err != nil || hash != info.ContentID || size != info.DecodedBytes {
		t.Fatal("brick order changed canonical identity")
	}
	limited := c1aCodec(t, voxelcodec.Options{Limits: voxelcodec.Limits{MaxAuxBytes: 1087}})
	if hash, size, err := limited.Identity(doc); err == nil || hash != "" || size != 0 {
		t.Fatal("identity bypassed logical auxiliary limit")
	}
	if !reflect.DeepEqual(doc, before) {
		t.Fatal("identity mutated input")
	}
	doc.Bricks[1].Aux[64] ^= 1
	hash, _, err := plain.Identity(doc)
	if err != nil || hash == info.ContentID {
		t.Fatal("unoccupied normal bytes missing from identity")
	}
	doc = c1aDocument()
	doc.Bricks[0].Materials[0] = 255
	hash, _, err = plain.Identity(doc)
	if err != nil || hash == info.ContentID {
		t.Fatal("raw secondary bytes missing from identity")
	}
	doc.Bricks[0].Values = nil
	if hash, size, err := plain.Identity(doc); err == nil || hash != "" || size != 0 {
		t.Fatal("invalid identity published partial results")
	}
	plain.Close()
	if hash, size, err := plain.Identity(c1aDocument()); !errors.Is(err, voxelcodec.ErrClosed) || hash != "" || size != 0 {
		t.Fatal("closed identity is not terminal")
	}
}
