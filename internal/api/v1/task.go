package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type TaskHandler struct {
	taskService service.TaskService
	logger      *logger.Logger
}

func NewTaskHandler(taskService service.TaskService, logger *logger.Logger) *TaskHandler {
	return &TaskHandler{
		taskService: taskService,
		logger:      logger,
	}
}

// CreateTask godoc
// @Summary Create a new task
// @Description Create a new import/export task
// @Tags Tasks
// @Accept json
// @Produce json
// @Param task body dto.CreateTaskRequest true "Task details"
// @Success 201 {object} dto.TaskResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /tasks [post]
func (h *TaskHandler) CreateTask(c *gin.Context) {
	var req dto.CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("failed to bind request", "error", err)
		NewErrorResponse(c, http.StatusBadRequest, "invalid request", err)
		return
	}

	task, err := h.taskService.CreateTask(c.Request.Context(), req)
	if err != nil {
		h.logger.Error("failed to create task", "error", err)
		NewErrorResponse(c, http.StatusInternalServerError, "failed to create task", err)
		return
	}

	c.JSON(http.StatusCreated, task)
}

// GetTask godoc
// @Summary Get a task by ID
// @Description Get detailed information about a task
// @Tags Tasks
// @Accept json
// @Produce json
// @Param id path string true "Task ID"
// @Success 200 {object} dto.TaskResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /tasks/{id} [get]
func (h *TaskHandler) GetTask(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		NewErrorResponse(c, http.StatusBadRequest, "invalid task id", nil)
		return
	}

	task, err := h.taskService.GetTask(c.Request.Context(), id)
	if err != nil {
		if errors.IsNotFound(err) {
			NewErrorResponse(c, http.StatusNotFound, "task not found", err)
			return
		}
		h.logger.Error("failed to get task", "error", err)
		NewErrorResponse(c, http.StatusInternalServerError, "failed to get task", err)
		return
	}

	c.JSON(http.StatusOK, task)
}

// ListTasks godoc
// @Summary List tasks
// @Description List tasks with optional filtering
// @Tags Tasks
// @Accept json
// @Produce json
// @Param filter query types.TaskFilter false "Filter"
// @Success 200 {object} dto.ListTasksResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /tasks [get]
func (h *TaskHandler) ListTasks(c *gin.Context) {
	var filter types.TaskFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		h.logger.Error("failed to bind query parameters", "error", err)
		NewErrorResponse(c, http.StatusBadRequest, "invalid filter", err)
		return
	}

	if err := filter.Validate(); err != nil {
		NewErrorResponse(c, http.StatusBadRequest, "invalid filter", err)
		return
	}

	resp, err := h.taskService.ListTasks(c.Request.Context(), &filter)
	if err != nil {
		h.logger.Error("failed to list tasks", "error", err)
		NewErrorResponse(c, http.StatusInternalServerError, "failed to list tasks", err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// UpdateTaskStatus godoc
// @Summary Update task status
// @Description Update the status of a task
// @Tags Tasks
// @Accept json
// @Produce json
// @Param id path string true "Task ID"
// @Param status body dto.UpdateTaskStatusRequest true "New status"
// @Success 200 {object} dto.TaskResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /tasks/{id}/status [put]
func (h *TaskHandler) UpdateTaskStatus(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		NewErrorResponse(c, http.StatusBadRequest, "invalid task id", nil)
		return
	}

	var req dto.UpdateTaskStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("failed to bind request", "error", err)
		NewErrorResponse(c, http.StatusBadRequest, "invalid request", err)
		return
	}

	if err := req.Validate(); err != nil {
		NewErrorResponse(c, http.StatusBadRequest, "invalid request", err)
		return
	}

	if err := h.taskService.UpdateTaskStatus(c.Request.Context(), id, req.TaskStatus); err != nil {
		if errors.IsNotFound(err) {
			NewErrorResponse(c, http.StatusNotFound, "task not found", err)
			return
		}
		h.logger.Error("failed to update task status", "error", err)
		NewErrorResponse(c, http.StatusInternalServerError, "failed to update task status", err)
		return
	}

	// Get updated task
	task, err := h.taskService.GetTask(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("failed to get updated task", "error", err)
		NewErrorResponse(c, http.StatusInternalServerError, "failed to get updated task", err)
		return
	}

	c.JSON(http.StatusOK, task)
}

// ProcessTask godoc
// @Summary Process a task
// @Description Start processing a task
// @Tags Tasks
// @Accept json
// @Produce json
// @Param id path string true "Task ID"
// @Success 202 {object} gin.H
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /tasks/{id}/process [post]
func (h *TaskHandler) ProcessTask(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		NewErrorResponse(c, http.StatusBadRequest, "invalid task id", nil)
		return
	}

	// Start processing in a goroutine
	go func() {
		if err := h.taskService.ProcessTask(c.Request.Context(), id); err != nil {
			h.logger.Error("failed to process task", "error", err, "task_id", id)
		}
	}()

	c.JSON(http.StatusAccepted, gin.H{"message": "task processing started"})
}
