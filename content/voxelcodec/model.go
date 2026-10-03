// Package voxelcodec owns bounded, lossless independent compiled voxel frames.
package voxelcodec

import (
	"errors"
	"fmt"
	"math"
)

var ErrClosed = errors.New("voxel codec is closed")

const frameHeaderBytes = 96
const version = 1
const frameMagic = "GKBRCK1\n"

type Brick struct {
	Coord     [3]int32
	Occupancy [8]uint64
	Values    []uint8
	Materials []uint8
	Aux       []byte
}

type Document struct {
	Kind              string
	Metadata          []byte
	NormalBakeVersion string
	Bricks            []Brick
}

type Dictionary struct {
	ID    uint32
	Bytes []byte
}
type Bounds struct{ Min, Max [3]int32 }
type Limits struct {
	MaxEncodedBytes, MaxDecodedBytes                                                                           int64
	MaxBricks, MaxVoxels, MaxMetadataBytes, MaxAuxBytes, MaxDictionaryBytes, MaxKindBytes, MaxBakeVersionBytes int
	Bounds                                                                                                     *Bounds
}
type Options struct {
	Limits     Limits
	Dictionary *Dictionary
}
type Info struct {
	ContentID                  string
	EncodedBytes, DecodedBytes int64
	DictionaryID               uint32
	DictionaryHash             string
}

func normalizedLimits(l Limits) (Limits, error) {
	defaults := Limits{MaxEncodedBytes: 32 << 20, MaxDecodedBytes: 32 << 20, MaxBricks: 16384, MaxVoxels: 1048576, MaxMetadataBytes: 1 << 20, MaxAuxBytes: 1 << 20, MaxDictionaryBytes: 64 << 10, MaxKindBytes: 64, MaxBakeVersionBytes: 128}
	maxInt := int64(int(^uint(0) >> 1))
	for _, p := range []struct {
		v *int64
		d int64
	}{{&l.MaxEncodedBytes, defaults.MaxEncodedBytes}, {&l.MaxDecodedBytes, defaults.MaxDecodedBytes}} {
		if *p.v < 0 || *p.v > maxInt {
			return Limits{}, fmt.Errorf("unrepresentable byte limit")
		}
		if *p.v == 0 {
			*p.v = p.d
		}
	}
	for _, p := range []struct {
		v   *int
		d   int
		max int64
	}{{&l.MaxBricks, defaults.MaxBricks, math.MaxUint32}, {&l.MaxVoxels, defaults.MaxVoxels, maxInt}, {&l.MaxMetadataBytes, defaults.MaxMetadataBytes, math.MaxUint32}, {&l.MaxAuxBytes, defaults.MaxAuxBytes, math.MaxUint32}, {&l.MaxDictionaryBytes, defaults.MaxDictionaryBytes, maxInt}, {&l.MaxKindBytes, defaults.MaxKindBytes, math.MaxUint16}, {&l.MaxBakeVersionBytes, defaults.MaxBakeVersionBytes, math.MaxUint16}} {
		if *p.v < 0 || int64(*p.v) > p.max {
			return Limits{}, fmt.Errorf("unrepresentable document limit")
		}
		if *p.v == 0 {
			*p.v = p.d
		}
	}
	if l.Bounds != nil {
		b := *l.Bounds
		for axis := 0; axis < 3; axis++ {
			if b.Min[axis] > b.Max[axis] {
				return Limits{}, fmt.Errorf("invalid brick bounds")
			}
		}
		l.Bounds = &b
	}
	return l, nil
}

func (l Limits) inBounds(coord [3]int32) bool {
	if l.Bounds == nil {
		return true
	}
	for axis := 0; axis < 3; axis++ {
		if coord[axis] < l.Bounds.Min[axis] || coord[axis] > l.Bounds.Max[axis] {
			return false
		}
	}
	return true
}

func coordLess(a, b [3]int32) bool {
	for axis := 0; axis < 3; axis++ {
		if a[axis] != b[axis] {
			return a[axis] < b[axis]
		}
	}
	return false
}
