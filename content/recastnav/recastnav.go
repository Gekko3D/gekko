package recastnav

/*
#cgo CXXFLAGS: -std=c++11 -I${SRCDIR}/../../third_party/recastnavigation/Recast/Include
#cgo LDFLAGS: -lstdc++
#include <stdlib.h>
#include "recast_wrapper.h"
*/
import "C"

import (
	"fmt"
	"math"
	"unsafe"
)

type Config struct {
	BoundsMin              [3]float32
	BoundsMax              [3]float32
	CellSize               float32
	CellHeight             float32
	WalkableSlopeAngle     float32
	WalkableHeight         int
	WalkableClimb          int
	WalkableRadius         int
	BorderSize             int
	MaxEdgeLen             int
	MaxSimplificationError float32
	DetailSampleDist       float32
	DetailSampleMaxError   float32
	MinRegionArea          int
	MergeRegionArea        int
	MaxVertsPerPoly        int
}

type Mesh struct {
	Vertices   [][3]float32
	Polys      [][]int
	Neighbors  [][]int
	Areas      []uint8
	Contours   []Contour
	DetailMesh DetailMesh
}

type Contour struct {
	Vertices [][3]float32
	Area     uint8
}

type DetailMesh struct {
	SubMeshes []DetailSubMesh
	Vertices  [][3]float32
	Triangles [][]int
}

type DetailSubMesh struct {
	VertexBase    int
	VertexCount   int
	TriangleBase  int
	TriangleCount int
}

const (
	AreaNull     uint8 = 0
	AreaWalkable uint8 = 63
	AreaAuto     uint8 = 255
)

func Build(vertices []float32, triangles []int32, cfg Config) (Mesh, error) {
	return BuildWithTriangleAreas(vertices, triangles, nil, cfg)
}

func BuildWithTriangleAreas(vertices []float32, triangles []int32, triangleAreas []uint8, cfg Config) (Mesh, error) {
	if len(vertices)%3 != 0 {
		return Mesh{}, fmt.Errorf("recast vertices length must be a multiple of 3")
	}
	if len(triangles)%3 != 0 {
		return Mesh{}, fmt.Errorf("recast triangles length must be a multiple of 3")
	}
	if len(triangleAreas) != 0 && len(triangleAreas) != len(triangles)/3 {
		return Mesh{}, fmt.Errorf("recast triangle area length must be zero or match triangle count")
	}
	if len(vertices) == 0 || len(triangles) == 0 {
		return Mesh{}, nil
	}
	cVerts := (*C.float)(unsafe.Pointer(&vertices[0]))
	cTris := (*C.int)(unsafe.Pointer(&triangles[0]))
	var cAreas *C.uchar
	if len(triangleAreas) > 0 {
		cAreas = (*C.uchar)(unsafe.Pointer(&triangleAreas[0]))
	}
	cCfg := C.GkRecastConfig{
		cellSize:               C.float(cfg.CellSize),
		cellHeight:             C.float(cfg.CellHeight),
		walkableSlopeAngle:     C.float(cfg.WalkableSlopeAngle),
		walkableHeight:         C.int(cfg.WalkableHeight),
		walkableClimb:          C.int(cfg.WalkableClimb),
		walkableRadius:         C.int(cfg.WalkableRadius),
		borderSize:             C.int(cfg.BorderSize),
		maxEdgeLen:             C.int(cfg.MaxEdgeLen),
		maxSimplificationError: C.float(cfg.MaxSimplificationError),
		detailSampleDist:       C.float(cfg.DetailSampleDist),
		detailSampleMaxError:   C.float(cfg.DetailSampleMaxError),
		minRegionArea:          C.int(cfg.MinRegionArea),
		mergeRegionArea:        C.int(cfg.MergeRegionArea),
		maxVertsPerPoly:        C.int(cfg.MaxVertsPerPoly),
	}
	for i := 0; i < 3; i++ {
		cCfg.bmin[i] = C.float(cfg.BoundsMin[i])
		cCfg.bmax[i] = C.float(cfg.BoundsMax[i])
	}
	result := C.gk_recast_build(cVerts, C.int(len(vertices)/3), cTris, cAreas, C.int(len(triangles)/3), cCfg)
	defer C.gk_recast_free(&result)
	if result.ok == 0 {
		return Mesh{}, fmt.Errorf("recast build failed: %s", C.GoString(&result.error[0]))
	}
	mesh := Mesh{
		Vertices:  make([][3]float32, int(result.nverts)),
		Polys:     make([][]int, 0, int(result.npolys)),
		Neighbors: make([][]int, 0, int(result.npolys)),
		Areas:     make([]uint8, 0, int(result.npolys)),
	}
	if result.nverts > 0 && result.verts != nil {
		values := unsafe.Slice((*C.float)(result.verts), int(result.nverts)*3)
		for i := 0; i < int(result.nverts); i++ {
			mesh.Vertices[i] = [3]float32{float32(values[i*3]), float32(values[i*3+1]), float32(values[i*3+2])}
		}
	}
	rawToMesh := make([]int, int(result.npolys))
	for i := range rawToMesh {
		rawToMesh[i] = -1
	}
	if result.npolys > 0 && result.polys != nil {
		values := unsafe.Slice((*C.int)(result.polys), int(result.npolys)*int(result.nvp))
		areas := []C.uchar(nil)
		if result.areas != nil {
			areas = unsafe.Slice((*C.uchar)(result.areas), int(result.npolys))
		}
		for i := 0; i < int(result.npolys); i++ {
			poly := make([]int, 0, int(result.nvp))
			for j := 0; j < int(result.nvp); j++ {
				index := int(values[i*int(result.nvp)+j])
				if index < 0 || index >= len(mesh.Vertices) {
					continue
				}
				poly = append(poly, index)
			}
			if len(poly) >= 3 {
				rawToMesh[i] = len(mesh.Polys)
				mesh.Polys = append(mesh.Polys, poly)
				mesh.Neighbors = append(mesh.Neighbors, nil)
				if areas != nil {
					mesh.Areas = append(mesh.Areas, uint8(areas[i]))
				} else {
					mesh.Areas = append(mesh.Areas, 0)
				}
			}
		}
	}
	if result.npolys > 0 && result.neighbors != nil {
		values := unsafe.Slice((*C.int)(result.neighbors), int(result.npolys)*int(result.nvp))
		for i := 0; i < int(result.npolys); i++ {
			meshIndex := rawToMesh[i]
			if meshIndex < 0 {
				continue
			}
			for j := 0; j < int(result.nvp); j++ {
				rawNeighbor := int(values[i*int(result.nvp)+j])
				if rawNeighbor < 0 || rawNeighbor >= len(rawToMesh) {
					continue
				}
				neighborIndex := rawToMesh[rawNeighbor]
				if neighborIndex >= 0 && neighborIndex != meshIndex {
					mesh.Neighbors[meshIndex] = appendUniqueInt(mesh.Neighbors[meshIndex], neighborIndex)
				}
			}
		}
	}
	if result.ncontours > 0 && result.contourVertCounts != nil {
		counts := unsafe.Slice((*C.int)(result.contourVertCounts), int(result.ncontours))
		var vertices []C.float
		totalVertices := 0
		for _, count := range counts {
			totalVertices += int(count)
		}
		if totalVertices > 0 && result.contourVerts != nil {
			vertices = unsafe.Slice((*C.float)(result.contourVerts), totalVertices*3)
		}
		var areas []C.uchar
		if result.contourAreas != nil {
			areas = unsafe.Slice((*C.uchar)(result.contourAreas), int(result.ncontours))
		}
		mesh.Contours = make([]Contour, 0, int(result.ncontours))
		offset := 0
		for i, count := range counts {
			contour := Contour{Vertices: make([][3]float32, 0, int(count))}
			if areas != nil {
				contour.Area = uint8(areas[i])
			}
			for j := 0; j < int(count); j++ {
				if vertices != nil {
					index := (offset + j) * 3
					contour.Vertices = append(contour.Vertices, [3]float32{float32(vertices[index]), float32(vertices[index+1]), float32(vertices[index+2])})
				}
			}
			offset += int(count)
			mesh.Contours = append(mesh.Contours, contour)
		}
	}
	if result.ndetailMeshes > 0 && result.detailMeshes != nil {
		values := unsafe.Slice((*C.uint)(result.detailMeshes), int(result.ndetailMeshes)*4)
		mesh.DetailMesh.SubMeshes = make([]DetailSubMesh, int(result.ndetailMeshes))
		for i := 0; i < int(result.ndetailMeshes); i++ {
			mesh.DetailMesh.SubMeshes[i] = DetailSubMesh{
				VertexBase:    int(values[i*4]),
				VertexCount:   int(values[i*4+1]),
				TriangleBase:  int(values[i*4+2]),
				TriangleCount: int(values[i*4+3]),
			}
		}
	}
	if result.ndetailVerts > 0 && result.detailVerts != nil {
		values := unsafe.Slice((*C.float)(result.detailVerts), int(result.ndetailVerts)*3)
		mesh.DetailMesh.Vertices = make([][3]float32, int(result.ndetailVerts))
		for i := 0; i < int(result.ndetailVerts); i++ {
			mesh.DetailMesh.Vertices[i] = [3]float32{float32(values[i*3]), float32(values[i*3+1]), float32(values[i*3+2])}
		}
	}
	if result.ndetailTris > 0 && result.detailTris != nil {
		values := unsafe.Slice((*C.uchar)(result.detailTris), int(result.ndetailTris)*4)
		mesh.DetailMesh.Triangles = make([][]int, int(result.ndetailTris))
		for i := 0; i < int(result.ndetailTris); i++ {
			mesh.DetailMesh.Triangles[i] = []int{int(values[i*4]), int(values[i*4+1]), int(values[i*4+2])}
		}
	}
	return mesh, nil
}

func appendUniqueInt(values []int, value int) []int {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func CellCount(worldUnits, cellSize float32) int {
	if cellSize <= 0 {
		return 0
	}
	return int(math.Ceil(float64(worldUnits / cellSize)))
}
