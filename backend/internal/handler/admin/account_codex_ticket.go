package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// SetCodexTicketService attaches the optional Codex ticket service.
func (h *AccountHandler) SetCodexTicketService(tickets *service.CodexTicketService) {
	h.codexTickets = tickets
}

// GetCodexTicketOverview returns ticket settings, per-account status and the
// site-wide shared pool. Ticket state blobs are never returned.
func (h *AccountHandler) GetCodexTicketOverview(c *gin.Context) {
	if h.codexTickets == nil {
		response.Error(c, http.StatusServiceUnavailable, "codex ticket service unavailable")
		return
	}
	overview, err := h.codexTickets.Overview(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, overview)
}

// UpdateCodexTicketSettings persists the Codex ticket switches.
func (h *AccountHandler) UpdateCodexTicketSettings(c *gin.Context) {
	if h.codexTickets == nil {
		response.Error(c, http.StatusServiceUnavailable, "codex ticket service unavailable")
		return
	}
	var req service.CodexTicketSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid codex ticket settings: "+err.Error())
		return
	}
	settings, err := h.codexTickets.UpdateSettings(c.Request.Context(), req)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, settings)
}

type codexTicketHarvestRequest struct {
	AccountID int64    `json:"account_id"`
	Models    []string `json:"models"`
}

// HarvestCodexTickets runs a manual harvest. With account_id it targets one
// account; otherwise it runs one bounded background-style round.
func (h *AccountHandler) HarvestCodexTickets(c *gin.Context) {
	if h.codexTickets == nil {
		response.Error(c, http.StatusServiceUnavailable, "codex ticket service unavailable")
		return
	}
	var req codexTicketHarvestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid codex ticket harvest request: "+err.Error())
		return
	}
	ctx := c.Request.Context()
	var (
		summary any
		err     error
	)
	if req.AccountID > 0 {
		summary, err = h.codexTickets.HarvestAccount(ctx, req.AccountID, req.Models)
	} else {
		summary, err = h.codexTickets.HarvestRound(ctx)
	}
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, summary)
}

// HarvestAccountCodexTickets runs a manual harvest for the path account.
func (h *AccountHandler) HarvestAccountCodexTickets(c *gin.Context) {
	if h.codexTickets == nil {
		response.Error(c, http.StatusServiceUnavailable, "codex ticket service unavailable")
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "invalid account id")
		return
	}
	var req codexTicketHarvestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid codex ticket harvest request: "+err.Error())
		return
	}
	summary, err := h.codexTickets.HarvestAccount(c.Request.Context(), accountID, req.Models)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, summary)
}

// ListCodexTickets returns a paginated ticket inventory for the ticket
// management page. Ticket state blobs are never returned.
func (h *AccountHandler) ListCodexTickets(c *gin.Context) {
	if h.codexTickets == nil {
		response.Error(c, http.StatusServiceUnavailable, "codex ticket service unavailable")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	accountID, _ := strconv.ParseInt(c.DefaultQuery("account_id", "0"), 10, 64)
	filter := service.CodexTicketListFilter{
		AccountID: accountID,
		Model:     c.Query("model"),
		Status:    c.Query("status"),
		Page:      page,
		PageSize:  pageSize,
	}
	result, err := h.codexTickets.ListTickets(c.Request.Context(), filter)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, result)
}

// DeleteCodexTicket removes a ticket; the next request will harvest a new one
// when 打票 is enabled.
func (h *AccountHandler) DeleteCodexTicket(c *gin.Context) {
	if h.codexTickets == nil {
		response.Error(c, http.StatusServiceUnavailable, "codex ticket service unavailable")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid ticket id")
		return
	}
	if err := h.codexTickets.DeleteTicket(c.Request.Context(), id); err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}
	response.Success(c, gin.H{"message": "ticket deleted"})
}

// RefreshCodexTicket force-harvests the account/model behind one ticket.
func (h *AccountHandler) RefreshCodexTicket(c *gin.Context) {
	if h.codexTickets == nil {
		response.Error(c, http.StatusServiceUnavailable, "codex ticket service unavailable")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid ticket id")
		return
	}
	summary, err := h.codexTickets.RefreshTicket(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, summary)
}

type codexIQTestRequest struct {
	Model string `json:"model"`
}

// RunCodexIQTest runs the two-shot ticket state probe for one account.
func (h *AccountHandler) RunCodexIQTest(c *gin.Context) {
	if h.codexTickets == nil {
		response.Error(c, http.StatusServiceUnavailable, "codex ticket service unavailable")
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "invalid account id")
		return
	}
	var req codexIQTestRequest
	_ = c.ShouldBindJSON(&req)
	result, err := h.codexTickets.ProbeIQ(c.Request.Context(), accountID, req.Model)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAccountNotFound):
			response.Error(c, http.StatusNotFound, "account not found")
		case errors.Is(err, service.ErrCodexIQProbeBusy):
			response.Error(c, http.StatusConflict, "该账号已有一轮智商测试在进行，请稍后再试")
		default:
			response.BadRequest(c, err.Error())
		}
		return
	}
	response.Success(c, result)
}

// GetCodexIQTest returns the last in-process IQ result for one account.
func (h *AccountHandler) GetCodexIQTest(c *gin.Context) {
	if h.codexTickets == nil {
		response.Error(c, http.StatusServiceUnavailable, "codex ticket service unavailable")
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "invalid account id")
		return
	}
	result := h.codexTickets.LastIQResult(accountID)
	if result == nil {
		response.Error(c, http.StatusNotFound, "no IQ test result yet")
		return
	}
	response.Success(c, result)
}

// GetAccountCodexTickets returns one account's ticket readiness per model.
func (h *AccountHandler) GetAccountCodexTickets(c *gin.Context) {
	if h.codexTickets == nil {
		response.Error(c, http.StatusServiceUnavailable, "codex ticket service unavailable")
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "invalid account id")
		return
	}
	status, err := h.codexTickets.StatusForAccountID(c.Request.Context(), accountID)
	if err != nil {
		if errors.Is(err, service.ErrAccountNotFound) {
			response.Error(c, http.StatusNotFound, "account not found")
			return
		}
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, status)
}
