package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type pelicanTestRequest struct {
	AccountID int64  `json:"account_id"`
	Model     string `json:"model"`
	Prompt    string `json:"prompt"`
}

// RunPelicanTest runs one Pelican IQ test against a single account and stores
// the generated HTML/SVG.
func (h *AccountHandler) RunPelicanTest(c *gin.Context) {
	if h.pelicanTests == nil {
		response.Error(c, http.StatusServiceUnavailable, "pelican test service unavailable")
		return
	}
	var req pelicanTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid pelican test request: "+err.Error())
		return
	}
	if req.AccountID <= 0 {
		response.BadRequest(c, "account_id is required")
		return
	}
	record, err := h.pelicanTests.Run(c.Request.Context(), service.PelicanTestInput{
		AccountID: req.AccountID,
		Model:     req.Model,
		Prompt:    req.Prompt,
	})
	if err != nil {
		if errors.Is(err, service.ErrAccountNotFound) {
			response.Error(c, http.StatusNotFound, "account not found")
			return
		}
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, record)
}

// ListPelicanTests returns the paginated Pelican test history.
func (h *AccountHandler) ListPelicanTests(c *gin.Context) {
	if h.pelicanTests == nil {
		response.Error(c, http.StatusServiceUnavailable, "pelican test service unavailable")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	result, err := h.pelicanTests.List(c.Request.Context(), page, pageSize)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, result)
}

// GetPelicanTest returns one stored work including its HTML/SVG output.
func (h *AccountHandler) GetPelicanTest(c *gin.Context) {
	if h.pelicanTests == nil {
		response.Error(c, http.StatusServiceUnavailable, "pelican test service unavailable")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid pelican test id")
		return
	}
	record, err := h.pelicanTests.Get(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
	response.Success(c, record)
}
