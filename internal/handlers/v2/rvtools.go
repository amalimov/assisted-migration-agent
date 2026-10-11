package v2

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/rvtools-analyser/api/v2"
	srvErrors "github.com/kubev2v/rvtools-analyser/pkg/errors"
)

// StartRvtoolsCollector accepts RVTools Excel files and starts a collection pipeline.
// StartRvtoolsCollector is available only in rvtools mode
func (h *Handler) StartRvtoolsCollector(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid multipart form"})
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one file is required"})
		return
	}

	var savedPaths []string
	for _, fh := range files {
		filename := filepath.Base(strings.ReplaceAll(fh.Filename, "\\", "/"))
		destPath := filepath.Join(h.cfg.Agent.DataFolder, fmt.Sprintf("rvtools_%d_%s", time.Now().UnixNano(), filename))
		if err := c.SaveUploadedFile(fh, destPath); err != nil {
			for _, p := range savedPaths {
				_ = os.Remove(p)
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save file %s: %v", fh.Filename, err)})
			return
		}
		savedPaths = append(savedPaths, destPath)
	}

	status, err := h.svc.StartRVToolsCollecting(savedPaths)
	if err != nil {
		for _, p := range savedPaths {
			_ = os.Remove(p)
		}
		if srvErrors.IsOperationInProgressError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, v2.NewCollectorStatus(status))
}
