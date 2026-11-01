package estimation

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kubev2v/assisted-migration-agent/internal/services/estimations"
	"github.com/kubev2v/assisted-migration-agent/internal/vmselection"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// Handler is shared by the Agent and Analyzer HTTP handlers. It uses the same
// request shape in both products without depending on either generated API.
type Handler struct {
	resolver *vmselection.Resolver
	service  *estimations.Service
}

func New(resolver *vmselection.Resolver, service *estimations.Service) *Handler {
	return &Handler{resolver: resolver, service: service}
}

type selectionRequest struct {
	Selection struct {
		Include struct {
			All      bool     `json:"all"`
			VMs      []string `json:"vms"`
			Groups   []string `json:"groups"`
			Clusters []string `json:"clusters"`
		} `json:"include"`
	} `json:"selection"`
}

func (r selectionRequest) selection() vmselection.VMSelection {
	return vmselection.VMSelection{Include: vmselection.VMSelectors{
		All:      r.Selection.Include.All,
		VMIDs:    r.Selection.Include.VMs,
		GroupIDs: r.Selection.Include.Groups,
		Clusters: r.Selection.Include.Clusters,
	}}
}

type timeRequest struct {
	selectionRequest
	EstimationSchema []string       `json:"estimationSchema"`
	Params           map[string]any `json:"params"`
}

func (r timeRequest) options() estimations.TimeOptions {
	return estimations.TimeOptions{Schemas: r.EstimationSchema, Params: r.Params}
}

func decode(c *gin.Context, target any) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, vmselection.ErrInvalidSelection),
		errors.Is(err, estimations.ErrInvalidOptions),
		errors.Is(err, estimations.ErrNoEligibleVMs):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case srvErrors.IsResourceNotFoundError(err):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

func (h *Handler) latestID(c *gin.Context) (string, bool) {
	id, err := h.resolver.LatestCollectionID()
	if err != nil {
		writeError(c, err)
		return "", false
	}
	return id, true
}

func (h *Handler) Complexity(c *gin.Context, collectionID string) {
	var request selectionRequest
	if err := decode(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	vms, err := h.resolver.Resolve(c.Request.Context(), collectionID, request.selection())
	if err != nil {
		writeError(c, err)
		return
	}
	response, err := h.service.EstimateComplexity(c.Request.Context(), vms)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *Handler) LatestComplexity(c *gin.Context) {
	if id, ok := h.latestID(c); ok {
		h.Complexity(c, id)
	}
}

func (h *Handler) Time(c *gin.Context, collectionID string) {
	h.time(c, collectionID, false)
}

func (h *Handler) LatestTime(c *gin.Context) {
	if id, ok := h.latestID(c); ok {
		h.Time(c, id)
	}
}

func (h *Handler) TimeByComplexity(c *gin.Context, collectionID string) {
	h.time(c, collectionID, true)
}

func (h *Handler) LatestTimeByComplexity(c *gin.Context) {
	if id, ok := h.latestID(c); ok {
		h.TimeByComplexity(c, id)
	}
}

func (h *Handler) time(c *gin.Context, collectionID string, byComplexity bool) {
	var request timeRequest
	if err := decode(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	vms, err := h.resolver.Resolve(c.Request.Context(), collectionID, request.selection())
	if err != nil {
		writeError(c, err)
		return
	}
	if byComplexity {
		response, err := h.service.EstimateTimeByComplexity(c.Request.Context(), vms, request.options())
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusOK, response)
		return
	}
	response, err := h.service.EstimateTime(c.Request.Context(), vms, request.options())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}
