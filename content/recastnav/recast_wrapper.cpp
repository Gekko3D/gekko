#include "recast_wrapper.h"

#include <algorithm>
#include <cmath>
#include <cstdlib>
#include <cstring>

#include "Recast.h"

static void gk_recast_set_error(GkRecastResult* result, const char* message)
{
	if (!result || !message)
		return;
	std::strncpy(result->error, message, sizeof(result->error) - 1);
	result->error[sizeof(result->error) - 1] = '\0';
}

static int gk_recast_ceil_to_int(float value)
{
	return static_cast<int>(std::ceil(value));
}

static void gk_recast_cleanup(rcHeightfield* solid, rcCompactHeightfield* chf, rcContourSet* cset, rcPolyMesh* pmesh, rcPolyMeshDetail* dmesh)
{
	rcFreeHeightField(solid);
	rcFreeCompactHeightfield(chf);
	rcFreeContourSet(cset);
	rcFreePolyMesh(pmesh);
	rcFreePolyMeshDetail(dmesh);
}

static const unsigned char GK_RECAST_AREA_AUTO = 255;

GkRecastResult gk_recast_build(const float* verts, int nverts, const int* tris, const unsigned char* triAreaOverrides, int ntris, GkRecastConfig in)
{
	GkRecastResult result;
	std::memset(&result, 0, sizeof(result));
	if (!verts || !tris || nverts <= 0 || ntris <= 0) {
		gk_recast_set_error(&result, "empty Recast input mesh");
		return result;
	}
	if (in.cellSize <= 0 || in.cellHeight <= 0) {
		gk_recast_set_error(&result, "invalid Recast cell size");
		return result;
	}

	rcContext ctx(false);
	rcConfig cfg;
	std::memset(&cfg, 0, sizeof(cfg));
	rcVcopy(cfg.bmin, in.bmin);
	rcVcopy(cfg.bmax, in.bmax);
	cfg.cs = in.cellSize;
	cfg.ch = in.cellHeight;
	cfg.walkableSlopeAngle = in.walkableSlopeAngle;
	cfg.walkableHeight = std::max(3, in.walkableHeight);
	cfg.walkableClimb = std::max(0, in.walkableClimb);
	cfg.walkableRadius = std::max(0, in.walkableRadius);
	cfg.borderSize = std::max(0, in.borderSize);
	cfg.maxEdgeLen = std::max(0, in.maxEdgeLen);
	cfg.maxSimplificationError = std::max(0.0f, in.maxSimplificationError);
	cfg.detailSampleDist = std::max(0.0f, in.detailSampleDist);
	cfg.detailSampleMaxError = std::max(0.0f, in.detailSampleMaxError);
	cfg.minRegionArea = std::max(0, in.minRegionArea);
	cfg.mergeRegionArea = std::max(0, in.mergeRegionArea);
	cfg.maxVertsPerPoly = std::max(3, in.maxVertsPerPoly);
	rcCalcGridSize(cfg.bmin, cfg.bmax, cfg.cs, &cfg.width, &cfg.height);
	if (cfg.width <= 0 || cfg.height <= 0) {
		gk_recast_set_error(&result, "empty Recast build bounds");
		return result;
	}

	unsigned char* triAreas = static_cast<unsigned char*>(std::malloc(sizeof(unsigned char) * ntris));
	if (!triAreas) {
		gk_recast_set_error(&result, "failed to allocate Recast triangle areas");
		return result;
	}
	std::memset(triAreas, 0, sizeof(unsigned char) * ntris);
	rcMarkWalkableTriangles(&ctx, cfg.walkableSlopeAngle, verts, nverts, tris, ntris, triAreas);
	if (triAreaOverrides) {
		for (int i = 0; i < ntris; ++i) {
			if (triAreaOverrides[i] != GK_RECAST_AREA_AUTO) {
				triAreas[i] = triAreaOverrides[i];
			}
		}
	}

	rcHeightfield* solid = rcAllocHeightfield();
	rcCompactHeightfield* chf = 0;
	rcContourSet* cset = 0;
	rcPolyMesh* pmesh = 0;
	rcPolyMeshDetail* dmesh = 0;
	if (!solid || !rcCreateHeightfield(&ctx, *solid, cfg.width, cfg.height, cfg.bmin, cfg.bmax, cfg.cs, cfg.ch)) {
		std::free(triAreas);
		gk_recast_cleanup(solid, chf, cset, pmesh, dmesh);
		gk_recast_set_error(&result, "failed to create Recast heightfield");
		return result;
	}
	if (!rcRasterizeTriangles(&ctx, verts, nverts, tris, triAreas, ntris, *solid, cfg.walkableClimb)) {
		std::free(triAreas);
		gk_recast_cleanup(solid, chf, cset, pmesh, dmesh);
		gk_recast_set_error(&result, "failed to rasterize Recast triangles");
		return result;
	}
	std::free(triAreas);

	rcFilterLowHangingWalkableObstacles(&ctx, cfg.walkableClimb, *solid);
	rcFilterLedgeSpans(&ctx, cfg.walkableHeight, cfg.walkableClimb, *solid);
	rcFilterWalkableLowHeightSpans(&ctx, cfg.walkableHeight, *solid);

	chf = rcAllocCompactHeightfield();
	if (!chf || !rcBuildCompactHeightfield(&ctx, cfg.walkableHeight, cfg.walkableClimb, *solid, *chf)) {
		gk_recast_cleanup(solid, chf, cset, pmesh, dmesh);
		gk_recast_set_error(&result, "failed to build Recast compact heightfield");
		return result;
	}
	if (cfg.walkableRadius > 0 && !rcErodeWalkableArea(&ctx, cfg.walkableRadius, *chf)) {
		gk_recast_cleanup(solid, chf, cset, pmesh, dmesh);
		gk_recast_set_error(&result, "failed to erode Recast walkable area");
		return result;
	}
	if (!rcBuildDistanceField(&ctx, *chf)) {
		gk_recast_cleanup(solid, chf, cset, pmesh, dmesh);
		gk_recast_set_error(&result, "failed to build Recast distance field");
		return result;
	}
	if (!rcBuildRegions(&ctx, *chf, cfg.borderSize, cfg.minRegionArea, cfg.mergeRegionArea)) {
		gk_recast_cleanup(solid, chf, cset, pmesh, dmesh);
		gk_recast_set_error(&result, "failed to build Recast regions");
		return result;
	}
	cset = rcAllocContourSet();
	if (!cset || !rcBuildContours(&ctx, *chf, cfg.maxSimplificationError, cfg.maxEdgeLen, *cset)) {
		gk_recast_cleanup(solid, chf, cset, pmesh, dmesh);
		gk_recast_set_error(&result, "failed to build Recast contours");
		return result;
	}
	pmesh = rcAllocPolyMesh();
	if (!pmesh || !rcBuildPolyMesh(&ctx, *cset, cfg.maxVertsPerPoly, *pmesh)) {
		gk_recast_cleanup(solid, chf, cset, pmesh, dmesh);
		gk_recast_set_error(&result, "failed to build Recast poly mesh");
		return result;
	}
	dmesh = rcAllocPolyMeshDetail();
	if (!dmesh || !rcBuildPolyMeshDetail(&ctx, *pmesh, *chf, cfg.detailSampleDist, cfg.detailSampleMaxError, *dmesh)) {
		gk_recast_cleanup(solid, chf, cset, pmesh, dmesh);
		gk_recast_set_error(&result, "failed to build Recast detail mesh");
		return result;
	}

	result.nverts = pmesh->nverts;
	result.npolys = pmesh->npolys;
	result.nvp = pmesh->nvp;
	result.ncontours = cset ? cset->nconts : 0;
	result.ndetailMeshes = dmesh ? dmesh->nmeshes : 0;
	result.ndetailVerts = dmesh ? dmesh->nverts : 0;
	result.ndetailTris = dmesh ? dmesh->ntris : 0;
	if (result.nverts > 0) {
		result.verts = static_cast<float*>(std::malloc(sizeof(float) * result.nverts * 3));
	}
	if (result.npolys > 0 && result.nvp > 0) {
		result.polys = static_cast<int*>(std::malloc(sizeof(int) * result.npolys * result.nvp));
		result.neighbors = static_cast<int*>(std::malloc(sizeof(int) * result.npolys * result.nvp));
		result.areas = static_cast<unsigned char*>(std::malloc(sizeof(unsigned char) * result.npolys));
	}
	int contourVertTotal = 0;
	for (int i = 0; cset && i < cset->nconts; ++i) {
		contourVertTotal += cset->conts[i].nverts;
	}
	if (result.ncontours > 0) {
		result.contourVertCounts = static_cast<int*>(std::malloc(sizeof(int) * result.ncontours));
		result.contourAreas = static_cast<unsigned char*>(std::malloc(sizeof(unsigned char) * result.ncontours));
	}
	if (contourVertTotal > 0) {
		result.contourVerts = static_cast<float*>(std::malloc(sizeof(float) * contourVertTotal * 3));
	}
	if (result.ndetailMeshes > 0) {
		result.detailMeshes = static_cast<unsigned int*>(std::malloc(sizeof(unsigned int) * result.ndetailMeshes * 4));
	}
	if (result.ndetailVerts > 0) {
		result.detailVerts = static_cast<float*>(std::malloc(sizeof(float) * result.ndetailVerts * 3));
	}
	if (result.ndetailTris > 0) {
		result.detailTris = static_cast<unsigned char*>(std::malloc(sizeof(unsigned char) * result.ndetailTris * 4));
	}
	if ((result.nverts > 0 && !result.verts) ||
		(result.npolys > 0 && (!result.polys || !result.neighbors || !result.areas)) ||
		(result.ncontours > 0 && (!result.contourVertCounts || !result.contourAreas)) ||
		(contourVertTotal > 0 && !result.contourVerts) ||
		(result.ndetailMeshes > 0 && !result.detailMeshes) ||
		(result.ndetailVerts > 0 && !result.detailVerts) ||
		(result.ndetailTris > 0 && !result.detailTris)) {
		gk_recast_free(&result);
		gk_recast_cleanup(solid, chf, cset, pmesh, dmesh);
		gk_recast_set_error(&result, "failed to copy Recast result mesh");
		return result;
	}
	for (int i = 0; i < pmesh->nverts; ++i) {
		const unsigned short* v = &pmesh->verts[i * 3];
		result.verts[i * 3 + 0] = pmesh->bmin[0] + static_cast<float>(v[0]) * pmesh->cs;
		result.verts[i * 3 + 1] = pmesh->bmin[1] + static_cast<float>(v[1]) * pmesh->ch;
		result.verts[i * 3 + 2] = pmesh->bmin[2] + static_cast<float>(v[2]) * pmesh->cs;
	}
	for (int i = 0; i < pmesh->npolys; ++i) {
		const unsigned short* poly = &pmesh->polys[i * 2 * pmesh->nvp];
		for (int j = 0; j < pmesh->nvp; ++j) {
			unsigned short index = poly[j];
			result.polys[i * pmesh->nvp + j] = index == RC_MESH_NULL_IDX ? -1 : static_cast<int>(index);
			unsigned short neighbor = poly[pmesh->nvp + j];
			if (neighbor == RC_MESH_NULL_IDX || (neighbor & 0x8000)) {
				result.neighbors[i * pmesh->nvp + j] = -1;
			} else {
				result.neighbors[i * pmesh->nvp + j] = static_cast<int>(neighbor);
			}
		}
		result.areas[i] = pmesh->areas ? pmesh->areas[i] : 0;
	}
	int contourVertexOffset = 0;
	for (int i = 0; cset && i < cset->nconts; ++i) {
		const rcContour& contour = cset->conts[i];
		result.contourVertCounts[i] = contour.nverts;
		result.contourAreas[i] = contour.area;
		for (int j = 0; j < contour.nverts; ++j) {
			const int* v = &contour.verts[j * 4];
			const int out = (contourVertexOffset + j) * 3;
			result.contourVerts[out + 0] = cset->bmin[0] + static_cast<float>(v[0]) * cset->cs;
			result.contourVerts[out + 1] = cset->bmin[1] + static_cast<float>(v[1]) * cset->ch;
			result.contourVerts[out + 2] = cset->bmin[2] + static_cast<float>(v[2]) * cset->cs;
		}
		contourVertexOffset += contour.nverts;
	}
	if (dmesh) {
		if (result.detailMeshes) {
			std::memcpy(result.detailMeshes, dmesh->meshes, sizeof(unsigned int) * result.ndetailMeshes * 4);
		}
		if (result.detailVerts) {
			std::memcpy(result.detailVerts, dmesh->verts, sizeof(float) * result.ndetailVerts * 3);
		}
		if (result.detailTris) {
			std::memcpy(result.detailTris, dmesh->tris, sizeof(unsigned char) * result.ndetailTris * 4);
		}
	}
	result.ok = 1;
	gk_recast_cleanup(solid, chf, cset, pmesh, dmesh);
	return result;
}

void gk_recast_free(GkRecastResult* result)
{
	if (!result)
		return;
	std::free(result->verts);
	std::free(result->polys);
	std::free(result->neighbors);
	std::free(result->areas);
	std::free(result->contourVertCounts);
	std::free(result->contourVerts);
	std::free(result->contourAreas);
	std::free(result->detailMeshes);
	std::free(result->detailVerts);
	std::free(result->detailTris);
	result->verts = 0;
	result->polys = 0;
	result->neighbors = 0;
	result->areas = 0;
	result->contourVertCounts = 0;
	result->contourVerts = 0;
	result->contourAreas = 0;
	result->detailMeshes = 0;
	result->detailVerts = 0;
	result->detailTris = 0;
	result->nverts = 0;
	result->npolys = 0;
	result->nvp = 0;
	result->ncontours = 0;
	result->ndetailMeshes = 0;
	result->ndetailVerts = 0;
	result->ndetailTris = 0;
}
