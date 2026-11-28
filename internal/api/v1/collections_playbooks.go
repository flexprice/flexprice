package v1

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

type PlaybookHandler struct {
	service service.PlaybookService
	log     *logger.Logger
}

func NewPlaybookHandler(service service.PlaybookService, log *logger.Logger) *PlaybookHandler {
	return &PlaybookHandler{service: service, log: log}
}

func bindPlaybookJSON(c *gin.Context, req any) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request body").Mark(ierr.ErrValidation))
		return false
	}
	return true
}

// bindOptionalPlaybookJSON binds a body that may be empty, even one of unknown length.
func bindOptionalPlaybookJSON(c *gin.Context, req any) bool {
	if c.Request.ContentLength == 0 {
		return true
	}
	if err := c.ShouldBindJSON(req); err != nil && !errors.Is(err, io.EOF) {
		c.Error(ierr.WithError(err).WithHint("Invalid request body").Mark(ierr.ErrValidation))
		return false
	}
	return true
}

func bindPlaybookQuery(c *gin.Context, req any) bool {
	if err := c.ShouldBindQuery(req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid query parameters").Mark(ierr.ErrValidation))
		return false
	}
	return true
}

func respondPlaybook[T any](c *gin.Context, status int, resp T, err error) {
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(status, resp)
}

// ListPlaybookTemplates godoc
// @Summary List playbook templates
// @ID listPlaybookTemplates
// @Description Use when you need the starter playbooks a new playbook can be created from. Each template carries its trigger, audience and steps; creating from one copies them.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Success 200 {object} dto.ListPlaybookTemplatesResponse
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/playbook-templates [get]
func (h *PlaybookHandler) ListTemplates(c *gin.Context) {
	resp, err := h.service.ListTemplates(c.Request.Context())
	respondPlaybook(c, http.StatusOK, resp, err)
}

// ListPlaybooks godoc
// @Summary List playbooks
// @ID listPlaybooks
// @Description Use when you need the environment's collection playbooks in priority order (1 is checked first), each with its readiness problems and its stats for the last 30 days.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param state query string false "Comma-separated draft, live, paused; empty lists every playbook"
// @Success 200 {object} dto.ListPlaybooksResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/playbooks [get]
func (h *PlaybookHandler) ListPlaybooks(c *gin.Context) {
	var req dto.ListPlaybooksRequest
	if !bindPlaybookQuery(c, &req) {
		return
	}
	resp, err := h.service.List(c.Request.Context(), &req)
	respondPlaybook(c, http.StatusOK, resp, err)
}

// CreatePlaybook godoc
// @Summary Create playbook
// @ID createPlaybook
// @Description Use when you want a new draft playbook, blank (one friendly reminder on day 3) or copied from a template. It is placed last in priority order. Drafts run nothing until activated.
// @Tags Collections
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param request body dto.CreatePlaybookRequest true "Name and optional template"
// @Success 201 {object} dto.PlaybookResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request or unknown template"
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/playbooks [post]
func (h *PlaybookHandler) CreatePlaybook(c *gin.Context) {
	var req dto.CreatePlaybookRequest
	if !bindPlaybookJSON(c, &req) {
		return
	}
	resp, err := h.service.Create(c.Request.Context(), &req)
	respondPlaybook(c, http.StatusCreated, resp, err)
}

// GetPlaybook godoc
// @Summary Get playbook
// @ID getPlaybook
// @Description Use when you need one playbook with its steps (ordered by day), readiness problems, version and stats.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Playbook ID"
// @Success 200 {object} dto.PlaybookResponse
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/playbooks/{id} [get]
func (h *PlaybookHandler) GetPlaybook(c *gin.Context) {
	resp, err := h.service.Get(c.Request.Context(), c.Param("id"))
	respondPlaybook(c, http.StatusOK, resp, err)
}

// UpdatePlaybook godoc
// @Summary Update playbook
// @ID updatePlaybook
// @Description Use when saving the whole playbook from the builder. Send the version you loaded; a playbook saved by someone else since is a 409. A draft may be incomplete; a live playbook must stay ready (400 invalid_operation with details.problems). Invoices already in a live playbook follow the edit for steps that have not run yet: removed steps are skipped, moved steps rescheduled (never into the past), new steps scheduled when their day is still ahead.
// @Tags Collections
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Playbook ID"
// @Param request body dto.UpdatePlaybookRequest true "The whole playbook"
// @Success 200 {object} dto.PlaybookResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid playbook, or a live playbook left incomplete"
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 409 {object} ierr.ErrorResponse "Saved by someone else since it was loaded"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/playbooks/{id} [put]
func (h *PlaybookHandler) UpdatePlaybook(c *gin.Context) {
	var req dto.UpdatePlaybookRequest
	if !bindPlaybookJSON(c, &req) {
		return
	}
	resp, err := h.service.Update(c.Request.Context(), c.Param("id"), &req)
	respondPlaybook(c, http.StatusOK, resp, err)
}

// DeletePlaybook godoc
// @Summary Delete playbook
// @ID deletePlaybook
// @Description Use when removing a playbook. Invoices in it leave it (their pending steps are skipped) and the scheduler offers them to the next live playbook they match. Reminders already written are not changed. Remaining playbooks are renumbered.
// @Tags Collections
// @Security ApiKeyAuth
// @x-scope "delete"
// @Param id path string true "Playbook ID"
// @Success 204
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/playbooks/{id} [delete]
func (h *PlaybookHandler) DeletePlaybook(c *gin.Context) {
	if err := h.service.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

// DuplicatePlaybook godoc
// @Summary Duplicate playbook
// @ID duplicatePlaybook
// @Description Use when starting a new playbook from an existing one. The copy is a draft named "<name> (copy)", placed last, with no stats.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Playbook ID"
// @Success 201 {object} dto.PlaybookResponse
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/playbooks/{id}/duplicate [post]
func (h *PlaybookHandler) DuplicatePlaybook(c *gin.Context) {
	resp, err := h.service.Duplicate(c.Request.Context(), c.Param("id"))
	respondPlaybook(c, http.StatusCreated, resp, err)
}

// ActivatePlaybook godoc
// @Summary Activate playbook
// @ID activatePlaybook
// @Description Use when a draft or paused playbook should go live. Readiness is checked: a name, at least one reminder email, every step complete, before-due emails sent without review. A playbook that is not ready is 400 invalid_operation with details.problems ([{step_id, field, message}]). From the next scheduler pass it enrols matching invoices, scheduling only steps whose day is today or later.
// @Tags Collections
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Playbook ID"
// @Param request body dto.PlaybookVersionRequest true "The version you loaded"
// @Success 200 {object} dto.PlaybookResponse
// @Failure 400 {object} ierr.ErrorResponse "Not ready (details.problems)"
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 409 {object} ierr.ErrorResponse "Changed since loaded, or already live"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/playbooks/{id}/activate [post]
func (h *PlaybookHandler) ActivatePlaybook(c *gin.Context) {
	var req dto.PlaybookVersionRequest
	if !bindPlaybookJSON(c, &req) {
		return
	}
	resp, err := h.service.Activate(c.Request.Context(), c.Param("id"), &req)
	respondPlaybook(c, http.StatusOK, resp, err)
}

// PausePlaybook godoc
// @Summary Pause playbook
// @ID pausePlaybook
// @Description Use when a live playbook should stop running steps and enrolling invoices. Invoices keep their place; on resume only steps due on the latest day run, never a burst of missed ones.
// @Tags Collections
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Playbook ID"
// @Param request body dto.PlaybookVersionRequest true "The version you loaded"
// @Success 200 {object} dto.PlaybookResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 409 {object} ierr.ErrorResponse "Changed since loaded, or not live"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/playbooks/{id}/pause [post]
func (h *PlaybookHandler) PausePlaybook(c *gin.Context) {
	var req dto.PlaybookVersionRequest
	if !bindPlaybookJSON(c, &req) {
		return
	}
	resp, err := h.service.Pause(c.Request.Context(), c.Param("id"), &req)
	respondPlaybook(c, http.StatusOK, resp, err)
}

// ResumePlaybook godoc
// @Summary Resume playbook
// @ID resumePlaybook
// @Description Use when a paused playbook should go live again. Readiness is checked as on activate. Steps that fell due while paused are skipped except those on each invoice's latest due day.
// @Tags Collections
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Playbook ID"
// @Param request body dto.PlaybookVersionRequest true "The version you loaded"
// @Success 200 {object} dto.PlaybookResponse
// @Failure 400 {object} ierr.ErrorResponse "Not ready (details.problems)"
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 409 {object} ierr.ErrorResponse "Changed since loaded, or not paused"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/playbooks/{id}/resume [post]
func (h *PlaybookHandler) ResumePlaybook(c *gin.Context) {
	var req dto.PlaybookVersionRequest
	if !bindPlaybookJSON(c, &req) {
		return
	}
	resp, err := h.service.Resume(c.Request.Context(), c.Param("id"), &req)
	respondPlaybook(c, http.StatusOK, resp, err)
}

// ReorderPlaybooks godoc
// @Summary Reorder playbooks
// @ID reorderPlaybooks
// @Description Use when changing which playbook is checked first. Send every playbook id of the environment once, highest priority first. New enrolments follow the new order; invoices already in a playbook stay in it.
// @Tags Collections
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param request body dto.ReorderPlaybooksRequest true "Every playbook id, highest priority first"
// @Success 200 {object} dto.ListPlaybooksResponse
// @Failure 400 {object} ierr.ErrorResponse "Ids missing, unknown or repeated"
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/playbooks/reorder [put]
func (h *PlaybookHandler) ReorderPlaybooks(c *gin.Context) {
	var req dto.ReorderPlaybooksRequest
	if !bindPlaybookJSON(c, &req) {
		return
	}
	resp, err := h.service.Reorder(c.Request.Context(), &req)
	respondPlaybook(c, http.StatusOK, resp, err)
}

// PreviewPlaybook godoc
// @Summary Preview playbook
// @ID previewPlaybook
// @Description Use before activating: which open invoices the saved playbook would enrol today, how many a higher-priority live playbook takes first, and the first step each would get. Read-only.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Playbook ID"
// @Success 200 {object} dto.PlaybookPreviewResponse
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/playbooks/{id}/preview [get]
func (h *PlaybookHandler) PreviewPlaybook(c *gin.Context) {
	resp, err := h.service.Preview(c.Request.Context(), c.Param("id"))
	respondPlaybook(c, http.StatusOK, resp, err)
}

// ListPlaybookRuns godoc
// @Summary List a playbook's invoices
// @ID listPlaybookRuns
// @Description Use when you need the invoices in a playbook, oldest due date first, each with its next pending step.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Playbook ID"
// @Param state query string false "Comma-separated active, paused, stopped, completed (default active,paused)"
// @Param limit query int false "Page size (1-100, default 25)"
// @Param offset query int false "Offset (default 0)"
// @Success 200 {object} dto.ListPlaybookRunsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/playbooks/{id}/runs [get]
func (h *PlaybookHandler) ListPlaybookRuns(c *gin.Context) {
	var req dto.ListPlaybookRunsRequest
	if !bindPlaybookQuery(c, &req) {
		return
	}
	resp, err := h.service.ListRuns(c.Request.Context(), c.Param("id"), &req)
	respondPlaybook(c, http.StatusOK, resp, err)
}

// PausePlaybookRun godoc
// @Summary Pause an invoice's playbook
// @ID pausePlaybookRun
// @Description Use when outreach for one invoice should stop for now. Only allowed when the playbook has the manually_paused stop condition. Its steps wait until resumed.
// @Tags Collections
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Playbook run ID"
// @Param request body dto.PausePlaybookRunRequest false "Optional reason"
// @Success 200 {object} dto.PlaybookRunResponse
// @Failure 400 {object} ierr.ErrorResponse "Pausing is off for this playbook"
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 409 {object} ierr.ErrorResponse "Run is not active"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/playbook-runs/{id}/pause [post]
func (h *PlaybookHandler) PausePlaybookRun(c *gin.Context) {
	var req dto.PausePlaybookRunRequest
	if !bindOptionalPlaybookJSON(c, &req) {
		return
	}
	resp, err := h.service.PauseRun(c.Request.Context(), c.Param("id"), &req)
	respondPlaybook(c, http.StatusOK, resp, err)
}

// ResumePlaybookRun godoc
// @Summary Resume an invoice's playbook
// @ID resumePlaybookRun
// @Description Use when outreach for a paused invoice should continue. Steps missed while paused are skipped except those on the latest due day.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Playbook run ID"
// @Success 200 {object} dto.PlaybookRunResponse
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 409 {object} ierr.ErrorResponse "Run is not paused"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/playbook-runs/{id}/resume [post]
func (h *PlaybookHandler) ResumePlaybookRun(c *gin.Context) {
	resp, err := h.service.ResumeRun(c.Request.Context(), c.Param("id"))
	respondPlaybook(c, http.StatusOK, resp, err)
}

// PauseCustomerCollections godoc
// @Summary Pause a customer's playbooks
// @ID pauseCustomerCollections
// @Description Use when outreach to a customer should stop for now: pauses every active playbook run of the customer. Invoices that enrol later are not paused.
// @Tags Collections
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Customer ID"
// @Param request body dto.PausePlaybookRunRequest false "Optional reason"
// @Success 200 {object} dto.CustomerPlaybookRunsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 404 {object} ierr.ErrorResponse "Customer not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/customers/{id}/pause [post]
func (h *PlaybookHandler) PauseCustomerCollections(c *gin.Context) {
	var req dto.PausePlaybookRunRequest
	if !bindOptionalPlaybookJSON(c, &req) {
		return
	}
	resp, err := h.service.PauseCustomer(c.Request.Context(), c.Param("id"), &req)
	respondPlaybook(c, http.StatusOK, resp, err)
}

// ResumeCustomerCollections godoc
// @Summary Resume a customer's playbooks
// @ID resumeCustomerCollections
// @Description Use when outreach to a customer should continue: resumes every paused playbook run of the customer.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Customer ID"
// @Success 200 {object} dto.CustomerPlaybookRunsResponse
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 404 {object} ierr.ErrorResponse "Customer not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/customers/{id}/resume [post]
func (h *PlaybookHandler) ResumeCustomerCollections(c *gin.Context) {
	resp, err := h.service.ResumeCustomer(c.Request.Context(), c.Param("id"))
	respondPlaybook(c, http.StatusOK, resp, err)
}

// ListCollectionTasks godoc
// @Summary List collection tasks
// @ID listCollectionTasks
// @Description Use when you need the to-dos playbook task steps created, newest first.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param invoice_id query string false "Invoice ID"
// @Param customer_id query string false "Customer ID"
// @Param assignee_user_id query string false "Assignee user ID"
// @Param state query string false "open or done"
// @Param limit query int false "Page size (1-100, default 25)"
// @Param offset query int false "Offset (default 0)"
// @Success 200 {object} dto.ListCollectionTasksResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/tasks [get]
func (h *PlaybookHandler) ListCollectionTasks(c *gin.Context) {
	var req dto.ListCollectionTasksRequest
	if !bindPlaybookQuery(c, &req) {
		return
	}
	resp, err := h.service.ListTasks(c.Request.Context(), &req)
	respondPlaybook(c, http.StatusOK, resp, err)
}

// CompleteCollectionTask godoc
// @Summary Complete collection task
// @ID completeCollectionTask
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Task ID"
// @Success 200 {object} dto.CollectionTaskResponse
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 409 {object} ierr.ErrorResponse "Already done"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/tasks/{id}/complete [post]
func (h *PlaybookHandler) CompleteCollectionTask(c *gin.Context) {
	resp, err := h.service.CompleteTask(c.Request.Context(), c.Param("id"))
	respondPlaybook(c, http.StatusOK, resp, err)
}

// ReopenCollectionTask godoc
// @Summary Reopen collection task
// @ID reopenCollectionTask
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Task ID"
// @Success 200 {object} dto.CollectionTaskResponse
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 409 {object} ierr.ErrorResponse "Already open"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/tasks/{id}/reopen [post]
func (h *PlaybookHandler) ReopenCollectionTask(c *gin.Context) {
	resp, err := h.service.ReopenTask(c.Request.Context(), c.Param("id"))
	respondPlaybook(c, http.StatusOK, resp, err)
}

// RunPlaybooks godoc
// @Summary Run playbooks now
// @ID runPlaybooks
// @Description Use to run the playbook scheduler for this environment now instead of waiting for the daily pass (09:00 UTC): stops runs whose invoice was paid or voided, enrols matching invoices and runs due steps. Safe to repeat: a step runs at most once per invoice.
// @Tags Collections
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Success 200 {object} dto.RunPlaybooksResponse
// @Failure 403 {object} ierr.ErrorResponse "Collections is not enabled for this environment"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /collections/playbooks/run [post]
func (h *PlaybookHandler) RunPlaybooks(c *gin.Context) {
	resp, err := h.service.RunEnvironment(c.Request.Context())
	respondPlaybook(c, http.StatusOK, resp, err)
}
