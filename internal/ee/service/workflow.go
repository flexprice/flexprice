package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/workflowexecution"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/queries"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// WorkflowService provides workflow list and query operations for the API.
type WorkflowService interface {
	ListWorkflows(ctx context.Context, filter *types.WorkflowExecutionFilter) (*dto.ListWorkflowsResponse, error)
	GetWorkflowSummary(ctx context.Context, workflowID, runID string) (*dto.WorkflowSummaryResponse, error)
	GetWorkflowDetails(ctx context.Context, workflowID, runID string) (*dto.WorkflowDetailsResponse, error)
	GetWorkflowTimeline(ctx context.Context, workflowID, runID string) (*dto.WorkflowTimelineResponse, error)
	GetWorkflowsBatch(ctx context.Context, req *dto.BatchWorkflowsRequest) (*dto.BatchWorkflowsResponse, error)
}

type workflowService struct {
	workflowExec *WorkflowExecutionService
	querier      *queries.WorkflowQuerier
	log          *logger.Logger
}

// NewWorkflowService creates a new WorkflowService instance.
func NewWorkflowService(
	workflowExec *WorkflowExecutionService,
	querier *queries.WorkflowQuerier,
	log *logger.Logger,
) WorkflowService {
	return &workflowService{
		workflowExec: workflowExec,
		querier:      querier,
		log:          log,
	}
}

func (s *workflowService) ListWorkflows(ctx context.Context, filter *types.WorkflowExecutionFilter) (*dto.ListWorkflowsResponse, error) {
	if types.GetTenantID(ctx) == "" {
		return nil, ierr.NewError("tenant_id is required").
			WithHint("Tenant ID must be present in context").
			Mark(ierr.ErrValidation)
	}
	if filter == nil {
		filter = types.NewDefaultWorkflowExecutionFilter()
	}
	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}
	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(50)
	}
	executions, total, err := s.workflowExec.ListWorkflowExecutions(ctx, filter)
	if err != nil {
		return nil, err
	}
	items := make([]*dto.WorkflowExecutionDTO, len(executions))
	for i, exec := range executions {
		items[i] = workflowExecutionToDTO(exec)
	}
	return &dto.ListWorkflowsResponse{
		Items: items,
		Pagination: types.NewPaginationResponse(
			int(total),
			filter.GetLimit(),
			filter.GetOffset(),
		),
	}, nil
}

// authorizeWorkflowAccess resolves a workflow execution through the tenant- and
// environment-scoped repository before any Temporal query is issued. Temporal's
// DescribeWorkflow has no notion of tenancy, so this lookup is the only thing
// standing between a caller and another tenant's workflow history — which embeds
// third-party error payloads. A workflow belonging to a different tenant is
// filtered out by the repository and is reported here as not found, so the
// response cannot be used to probe for the existence of foreign workflow IDs.
//
// Both tenant and environment must be present. The repository's environment
// filter is skipped when the environment is absent from the context, so a
// request carrying only a tenant would authorize against any environment of
// that tenant and then read Temporal history, which is not environment-scoped
// at all. Requiring the environment here keeps the check as narrow as the data
// it protects.
func (s *workflowService) authorizeWorkflowAccess(ctx context.Context, workflowID, runID string) (*workflowexecution.WorkflowExecution, error) {
	if err := requireTenantAndEnvironment(ctx); err != nil {
		return nil, err
	}

	exec, err := s.workflowExec.GetWorkflowExecution(ctx, workflowID, runID)
	if err != nil {
		// A genuine lookup failure is reported as such. Only absence is
		// translated to not-found, so a database outage is not reported as a
		// missing workflow.
		if !ierr.IsNotFound(err) {
			return nil, err
		}
		return nil, ierr.NewError("workflow not found").
			WithHint("Workflow not found").
			Mark(ierr.ErrNotFound)
	}
	if exec == nil {
		return nil, ierr.NewError("workflow not found").
			WithHint("Workflow not found").
			Mark(ierr.ErrNotFound)
	}
	return exec, nil
}

// requireTenantAndEnvironment rejects a request that is missing either scope.
// The repository applies each filter only when the corresponding value is
// present, so an absent environment silently widens the lookup to the whole
// tenant.
func requireTenantAndEnvironment(ctx context.Context) error {
	if types.GetTenantID(ctx) == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID must be present in context").
			Mark(ierr.ErrValidation)
	}
	if types.GetEnvironmentID(ctx) == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID must be present in context").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// authorizeWorkflowsBatch authorizes several workflows in a single scoped
// query, so batch size does not translate into one database round trip per
// item. It applies the same rule as authorizeWorkflowAccess to every entry: if
// any requested workflow falls outside the caller's tenant and environment the
// whole request is refused rather than quietly reduced, so a batch cannot be
// used to test which workflow IDs exist.
func (s *workflowService) authorizeWorkflowsBatch(ctx context.Context, refs []workflowexecution.WorkflowRef) error {
	if err := requireTenantAndEnvironment(ctx); err != nil {
		return err
	}

	execs, err := s.workflowExec.GetWorkflowExecutions(ctx, refs)
	if err != nil {
		return err
	}

	found := make(map[workflowexecution.WorkflowRef]struct{}, len(execs))
	for _, exec := range execs {
		if exec == nil {
			continue
		}
		found[workflowexecution.NewWorkflowRef(exec.WorkflowID, exec.RunID)] = struct{}{}
	}

	for _, ref := range refs {
		if _, ok := found[ref]; !ok {
			return ierr.NewError("workflow not found").
				WithHint("Workflow not found").
				Mark(ierr.ErrNotFound)
		}
	}

	return nil
}

func workflowExecutionToDTO(exec *workflowexecution.WorkflowExecution) *dto.WorkflowExecutionDTO {
	if exec == nil {
		return nil
	}
	return &dto.WorkflowExecutionDTO{
		WorkflowID:   exec.WorkflowID,
		RunID:        exec.RunID,
		WorkflowType: exec.WorkflowType,
		TaskQueue:    exec.TaskQueue,
		Status:       string(exec.WorkflowStatus),
		Entity:       exec.Entity,
		EntityID:     exec.EntityID,
		StartTime:    exec.StartTime,
		CloseTime:    exec.EndTime,
		DurationMs:   exec.DurationMs,
		CreatedBy:    exec.CreatedBy,
	}
}

func (s *workflowService) GetWorkflowSummary(ctx context.Context, workflowID, runID string) (*dto.WorkflowSummaryResponse, error) {
	if workflowID == "" || runID == "" {
		return nil, ierr.NewError("workflow_id and run_id are required").
			WithHint("Both workflow ID and run ID must be provided").
			Mark(ierr.ErrValidation)
	}
	if _, err := s.authorizeWorkflowAccess(ctx, workflowID, runID); err != nil {
		return nil, err
	}
	info, err := s.querier.DescribeWorkflow(ctx, workflowID, runID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve workflow summary from Temporal").
			Mark(ierr.ErrInternal)
	}
	activities, err := s.querier.ParseActivitiesFromHistory(ctx, workflowID, runID)
	if err != nil {
		s.log.Error(ctx, "Failed to parse activities for summary", "error", err)
		activities = []*queries.ActivityExecutionInfo{}
	}
	failedCount := 0
	for _, a := range activities {
		if a.Status == "FAILED" {
			failedCount++
		}
	}
	totalDuration := formatDuration(info.StartTime, info.CloseTime)
	return &dto.WorkflowSummaryResponse{
		WorkflowID:       info.WorkflowID,
		RunID:            info.RunID,
		WorkflowType:     info.WorkflowType,
		Status:           info.Status,
		StartTime:        info.StartTime,
		CloseTime:        info.CloseTime,
		DurationMs:       info.DurationMs,
		TotalDuration:    totalDuration,
		ActivityCount:    len(activities),
		FailedActivities: failedCount,
	}, nil
}

func (s *workflowService) GetWorkflowDetails(ctx context.Context, workflowID, runID string) (*dto.WorkflowDetailsResponse, error) {
	if workflowID == "" || runID == "" {
		return nil, ierr.NewError("workflow_id and run_id are required").
			WithHint("Both workflow ID and run ID must be provided").
			Mark(ierr.ErrValidation)
	}
	if _, err := s.authorizeWorkflowAccess(ctx, workflowID, runID); err != nil {
		return nil, err
	}
	info, err := s.querier.DescribeWorkflow(ctx, workflowID, runID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve workflow details from Temporal").
			Mark(ierr.ErrInternal)
	}
	activities, err := s.querier.ParseActivitiesFromHistory(ctx, workflowID, runID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse workflow activities").
			Mark(ierr.ErrInternal)
	}
	timelineEvents, err := s.querier.ParseTimelineFromHistory(ctx, workflowID, runID)
	if err != nil {
		s.log.Error(ctx, "Failed to parse timeline", "error", err)
		timelineEvents = []*queries.TimelineEvent{}
	}
	activityDTOs := activityInfosToDTOs(activities)
	timelineDTOs := timelineEventsToDTOs(timelineEvents)
	var metadata map[string]interface{}
	if dbExec, err := s.workflowExec.GetWorkflowExecution(ctx, workflowID, runID); err == nil {
		metadata = dbExec.Metadata
	}
	totalDuration := formatDuration(info.StartTime, info.CloseTime)
	return &dto.WorkflowDetailsResponse{
		WorkflowID:    info.WorkflowID,
		RunID:         info.RunID,
		WorkflowType:  info.WorkflowType,
		Status:        info.Status,
		StartTime:     info.StartTime,
		CloseTime:     info.CloseTime,
		DurationMs:    info.DurationMs,
		TotalDuration: totalDuration,
		TaskQueue:     info.TaskQueue,
		HistorySize:   info.HistorySize,
		Activities:    activityDTOs,
		Timeline:      timelineDTOs,
		Metadata:      metadata,
	}, nil
}

func (s *workflowService) GetWorkflowTimeline(ctx context.Context, workflowID, runID string) (*dto.WorkflowTimelineResponse, error) {
	if workflowID == "" || runID == "" {
		return nil, ierr.NewError("workflow_id and run_id are required").
			WithHint("Both workflow ID and run ID must be provided").
			Mark(ierr.ErrValidation)
	}
	if _, err := s.authorizeWorkflowAccess(ctx, workflowID, runID); err != nil {
		return nil, err
	}
	info, err := s.querier.DescribeWorkflow(ctx, workflowID, runID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve workflow details from Temporal").
			Mark(ierr.ErrInternal)
	}
	activities, err := s.querier.ParseActivitiesFromHistory(ctx, workflowID, runID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse workflow timeline").
			Mark(ierr.ErrInternal)
	}
	items := []*dto.WorkflowTimelineItemDTO{
		{
			ID:      "workflow",
			Group:   "execution",
			Content: info.WorkflowType,
			Start:   info.StartTime,
			End:     info.CloseTime,
			Status:  info.Status,
		},
	}
	for _, activity := range activities {
		item := &dto.WorkflowTimelineItemDTO{
			ID:      activity.ActivityID,
			Group:   "activities",
			Content: activity.ActivityType,
			Status:  activity.Status,
		}
		if activity.StartTime != nil {
			item.Start = *activity.StartTime
		}
		if activity.CloseTime != nil {
			item.End = activity.CloseTime
		}
		items = append(items, item)
	}
	return &dto.WorkflowTimelineResponse{
		WorkflowID: info.WorkflowID,
		RunID:      info.RunID,
		StartTime:  info.StartTime,
		CloseTime:  info.CloseTime,
		Items:      items,
	}, nil
}

func (s *workflowService) GetWorkflowsBatch(ctx context.Context, req *dto.BatchWorkflowsRequest) (*dto.BatchWorkflowsResponse, error) {
	if req == nil || len(req.Workflows) == 0 {
		return nil, ierr.NewError("workflows list cannot be empty").
			WithHint("At least one workflow must be specified").
			Mark(ierr.ErrValidation)
	}
	if len(req.Workflows) > 50 {
		return nil, ierr.NewError("too many workflows requested").
			WithHint("Maximum 50 workflows can be queried at once").
			Mark(ierr.ErrValidation)
	}
	// Every requested workflow must be authorized: a batch containing a single
	// owned workflow must not pull foreign ones along with it. Resolved in one
	// scoped query rather than a lookup per item.
	refs := make([]workflowexecution.WorkflowRef, 0, len(req.Workflows))
	for _, wf := range req.Workflows {
		refs = append(refs, workflowexecution.NewWorkflowRef(wf.WorkflowID, wf.RunID))
	}
	if err := s.authorizeWorkflowsBatch(ctx, refs); err != nil {
		return nil, err
	}

	executions := make([]struct{ WorkflowID, RunID string }, 0, len(refs))
	for _, ref := range refs {
		executions = append(executions, struct{ WorkflowID, RunID string }{WorkflowID: ref.WorkflowID(), RunID: ref.RunID()})
	}
	infos, err := s.querier.DescribeWorkflowBatch(ctx, executions)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve batch workflow details").
			Mark(ierr.ErrInternal)
	}
	workflowDetails := make([]*dto.WorkflowDetailsResponse, 0, len(infos))
	for i, info := range infos {
		if info == nil {
			s.log.Info(ctx, "Skipping nil workflow info in batch", "index", i)
			continue
		}
		totalDuration := formatDuration(info.StartTime, info.CloseTime)
		detail := &dto.WorkflowDetailsResponse{
			WorkflowID:    info.WorkflowID,
			RunID:         info.RunID,
			WorkflowType:  info.WorkflowType,
			Status:        info.Status,
			StartTime:     info.StartTime,
			CloseTime:     info.CloseTime,
			DurationMs:    info.DurationMs,
			TotalDuration: totalDuration,
			TaskQueue:     info.TaskQueue,
			HistorySize:   info.HistorySize,
			Activities:    []*dto.WorkflowActivityDTO{},
			Timeline:      []*dto.WorkflowTimelineItemDTO{},
		}
		if req.IncludeActivities {
			activities, err := s.querier.ParseActivitiesFromHistory(ctx, info.WorkflowID, info.RunID)
			if err != nil {
				s.log.Error(ctx, "Failed to parse activities for workflow in batch",
					"workflow_id", info.WorkflowID, "run_id", info.RunID, "error", err)
			} else {
				detail.Activities = activityInfosToDTOs(activities)
			}
		}
		workflowDetails = append(workflowDetails, detail)
	}
	return &dto.BatchWorkflowsResponse{Workflows: workflowDetails}, nil
}

func activityInfosToDTOs(activities []*queries.ActivityExecutionInfo) []*dto.WorkflowActivityDTO {
	out := make([]*dto.WorkflowActivityDTO, len(activities))
	for i, a := range activities {
		d := &dto.WorkflowActivityDTO{
			ActivityID:   a.ActivityID,
			ActivityType: a.ActivityType,
			Status:       a.Status,
			StartTime:    a.StartTime,
			CloseTime:    a.CloseTime,
			DurationMs:   a.DurationMs,
			RetryAttempt: a.RetryAttempt,
		}
		if a.ErrorMessage != "" {
			d.Error = &dto.ActivityErrorDTO{Message: a.ErrorMessage, Type: a.ErrorType}
		}
		out[i] = d
	}
	return out
}

func timelineEventsToDTOs(events []*queries.TimelineEvent) []*dto.WorkflowTimelineItemDTO {
	out := make([]*dto.WorkflowTimelineItemDTO, len(events))
	for i, e := range events {
		out[i] = &dto.WorkflowTimelineItemDTO{
			ID:        fmt.Sprintf("event-%d", e.EventID),
			Group:     "events",
			Content:   e.Details,
			Start:     e.EventTime,
			EventType: e.EventType,
		}
	}
	return out
}

func formatDuration(startTime time.Time, closeTime *time.Time) string {
	if closeTime == nil {
		return fmt.Sprintf("running for %s", formatDurationString(time.Since(startTime)))
	}
	return formatDurationString(closeTime.Sub(startTime))
}

func formatDurationString(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		if seconds == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}
