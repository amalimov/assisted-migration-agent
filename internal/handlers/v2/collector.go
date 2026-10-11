package v2

import (
	"github.com/gin-gonic/gin"
	v2 "github.com/kubev2v/rvtools-analyser/api/v2"
	"net/http"
)

func (h *Handler) GetCollectorStatus(c *gin.Context) {
	c.JSON(http.StatusOK, v2.NewCollectorStatus(h.svc.GetCollectorStatus()))
}
func (h *Handler) StopCollector(c *gin.Context) {
	if err := h.svc.StopCollecting(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
