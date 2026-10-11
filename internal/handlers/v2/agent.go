package v2

import (
	"github.com/gin-gonic/gin"
	v2 "github.com/kubev2v/rvtools-analyser/api/v2"
	"net/http"
)

// GetAgentStatus reports the local inventory source.
func (h *Handler) GetAgentStatus(c *gin.Context) {
	c.JSON(http.StatusOK, v2.AgentStatus{Status: v2.Ready, Source: v2.Rvtools})
}
