package analyzer

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/api/analyzer/v1"
)

// GetVersion returns the analyzer version information.
// (GET /version)
func (h *Handler) GetVersion(c *gin.Context) {
	c.JSON(http.StatusOK, v1.VersionInfo{
		Version:   h.cfg.Version,
		GitCommit: h.cfg.GitCommit,
	})
}
