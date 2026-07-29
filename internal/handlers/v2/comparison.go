package v2

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const (
	comparisonDefaultPageSize = 20
	comparisonMaxPageSize     = 100
)

// CompareCollections returns aggregate stats and diffs for two collections.
// (GET /collections/{aId}/compare/{bId})
func (h *Handler) CompareCollections(c *gin.Context, aId string, bId string) {
	if aId == bId {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot compare a collection with itself"})
		return
	}

	svc, err := h.svc.ComparisonService(aId, bId)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	summary, err := svc.Summary(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v2.NewCollectionComparisonSummaryFromModel(summary))
}

// CompareCollectionsDiff returns paginated VM IDs that differ between two collections.
// (GET /collections/{aId}/compare/{bId}/{dimension})
func (h *Handler) CompareCollectionsDiff(c *gin.Context, aId string, bId string, dimension v2.CompareCollectionsDiffParamsDimension, params v2.CompareCollectionsDiffParams) {
	dim := models.ComparisonDimension(dimension)
	if !dim.IsValid() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dimension: must be one of total, migratable, non-migratable"})
		return
	}

	if aId == bId {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot compare a collection with itself"})
		return
	}

	svc, err := h.svc.ComparisonService(aId, bId)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	page := 1
	if params.Page != nil && *params.Page > 0 {
		page = *params.Page
	}
	pageSize := comparisonDefaultPageSize
	if params.PageSize != nil && *params.PageSize > 0 {
		pageSize = min(*params.PageSize, comparisonMaxPageSize)
	}

	diff, err := svc.Diff(c.Request.Context(), dim, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v2.NewCollectionComparisonDiffFromModel(diff))
}
