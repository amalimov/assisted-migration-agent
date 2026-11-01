package analyzer

import "github.com/gin-gonic/gin"

func (h *Handler) CalculateMigrationComplexity(c *gin.Context, id string) {
	h.estimation.Complexity(c, id)
}

func (h *Handler) CalculateLatestMigrationComplexity(c *gin.Context) {
	h.estimation.LatestComplexity(c)
}

func (h *Handler) CalculateMigrationEstimation(c *gin.Context, id string) {
	h.estimation.Time(c, id)
}

func (h *Handler) CalculateLatestMigrationEstimation(c *gin.Context) {
	h.estimation.LatestTime(c)
}

func (h *Handler) CalculateMigrationEstimationByComplexity(c *gin.Context, id string) {
	h.estimation.TimeByComplexity(c, id)
}

func (h *Handler) CalculateLatestMigrationEstimationByComplexity(c *gin.Context) {
	h.estimation.LatestTimeByComplexity(c)
}
