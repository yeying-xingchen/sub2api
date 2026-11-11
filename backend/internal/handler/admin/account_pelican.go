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

type pelicanPlanRequest struct {
	AccountID      int64  `json:"account_id"`
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	CronExpression string `json:"cron_expression"`
	Enabled        bool   `json:"enabled"`
}

// ListPelicanTestPlans returns all scheduled Pelican plans.
func (h *AccountHandler) ListPelicanTestPlans(c *gin.Context) {
	if h.pelicanTests == nil {
		response.Error(c, http.StatusServiceUnavailable, "pelican test service unavailable")
		return
	}
	plans, err := h.pelicanTests.ListPlans(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, plans)
}

// CreatePelicanTestPlan creates a scheduled Pelican plan.
func (h *AccountHandler) CreatePelicanTestPlan(c *gin.Context) {
	if h.pelicanTests == nil {
		response.Error(c, http.StatusServiceUnavailable, "pelican test service unavailable")
		return
	}
	var req pelicanPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid pelican plan: "+err.Error())
		return
	}
	plan, err := h.pelicanTests.CreatePlan(c.Request.Context(), service.PelicanTestPlanInput{
		AccountID: req.AccountID, Model: req.Model, Prompt: req.Prompt,
		CronExpression: req.CronExpression, Enabled: req.Enabled,
	})
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, plan)
}

// UpdatePelicanTestPlan updates a scheduled Pelican plan.
func (h *AccountHandler) UpdatePelicanTestPlan(c *gin.Context) {
	if h.pelicanTests == nil {
		response.Error(c, http.StatusServiceUnavailable, "pelican test service unavailable")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid pelican plan id")
		return
	}
	var req pelicanPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid pelican plan: "+err.Error())
		return
	}
	plan, err := h.pelicanTests.UpdatePlan(c.Request.Context(), id, service.PelicanTestPlanInput{
		AccountID: req.AccountID, Model: req.Model, Prompt: req.Prompt,
		CronExpression: req.CronExpression, Enabled: req.Enabled,
	})
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, plan)
}

// DeletePelicanTestPlan removes a scheduled plan.
func (h *AccountHandler) DeletePelicanTestPlan(c *gin.Context) {
	if h.pelicanTests == nil {
		response.Error(c, http.StatusServiceUnavailable, "pelican test service unavailable")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid pelican plan id")
		return
	}
	if err := h.pelicanTests.DeletePlan(c.Request.Context(), id); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, gin.H{"message": "plan deleted"})
}

// RunPelicanTestPlanNow runs a plan immediately.
func (h *AccountHandler) RunPelicanTestPlanNow(c *gin.Context) {
	if h.pelicanTests == nil {
		response.Error(c, http.StatusServiceUnavailable, "pelican test service unavailable")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid pelican plan id")
		return
	}
	record, err := h.pelicanTests.RunPlanNow(c.Request.Context(), id)
	if err != nil {
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
