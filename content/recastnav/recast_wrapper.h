#pragma once

#ifdef __cplusplus
extern "C" {
#endif

typedef struct GkRecastConfig {
	float bmin[3];
	float bmax[3];
	float cellSize;
	float cellHeight;
	float walkableSlopeAngle;
	int walkableHeight;
	int walkableClimb;
	int walkableRadius;
	int borderSize;
	int maxEdgeLen;
	float maxSimplificationError;
	float detailSampleDist;
	float detailSampleMaxError;
	int minRegionArea;
	int mergeRegionArea;
	int maxVertsPerPoly;
} GkRecastConfig;

typedef struct GkRecastResult {
	int ok;
	char error[256];
	int nverts;
	int npolys;
	int nvp;
	float* verts;
	int* polys;
	int* neighbors;
	unsigned char* areas;
	int ncontours;
	int* contourVertCounts;
	float* contourVerts;
	unsigned char* contourAreas;
	int ndetailMeshes;
	int ndetailVerts;
	int ndetailTris;
	unsigned int* detailMeshes;
	float* detailVerts;
	unsigned char* detailTris;
} GkRecastResult;

GkRecastResult gk_recast_build(const float* verts, int nverts, const int* tris, const unsigned char* triAreaOverrides, int ntris, GkRecastConfig cfg);
void gk_recast_free(GkRecastResult* result);

#ifdef __cplusplus
}
#endif
