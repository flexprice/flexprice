package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/samber/lo"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/collections"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// PlaybookService manages collection playbooks, the invoices in them and the tasks their steps create. The
// scheduler that runs the steps is RunPlaybooks.
type PlaybookService interface {
	ListTemplates(ctx context.Context) (*dto.ListPlaybookTemplatesResponse, error)
	List(ctx context.Context, req *dto.ListPlaybooksRequest) (*dto.ListPlaybooksResponse, error)
	Get(ctx context.Context, id string) (*dto.PlaybookResponse, error)
	Create(ctx context.Context, req *dto.CreatePlaybookRequest) (*dto.PlaybookResponse, error)
	Update(ctx context.Context, id string, req *dto.UpdatePlaybookRequest) (*dto.PlaybookResponse, error)
	Delete(ctx context.Context, id string) error
	Duplicate(ctx context.Context, id string) (*dto.PlaybookResponse, error)
	Activate(ctx context.Context, id string, req *dto.PlaybookVersionRequest) (*dto.PlaybookResponse, error)
	Pause(ctx context.Context, id string, req *dto.PlaybookVersionRequest) (*dto.PlaybookResponse, error)
	Resume(ctx context.Context, id string, req *dto.PlaybookVersionRequest) (*dto.PlaybookResponse, error)
	Reorder(ctx context.Context, req *dto.ReorderPlaybooksRequest) (*dto.ListPlaybooksResponse, error)
	Preview(ctx context.Context, id string) (*dto.PlaybookPreviewResponse, error)

	ListRuns(ctx context.Context, playbookID string, req *dto.ListPlaybookRunsRequest) (*dto.ListPlaybookRunsResponse, error)
	PauseRun(ctx context.Context, runID string, req *dto.PausePlaybookRunRequest) (*dto.PlaybookRunResponse, error)
	ResumeRun(ctx context.Context, runID string) (*dto.PlaybookRunResponse, error)
	PauseCustomer(ctx context.Context, customerID string, req *dto.PausePlaybookRunRequest) (*dto.CustomerPlaybookRunsResponse, error)
	ResumeCustomer(ctx context.Context, customerID string) (*dto.CustomerPlaybookRunsResponse, error)

	ListTasks(ctx context.Context, req *dto.ListCollectionTasksRequest) (*dto.ListCollectionTasksResponse, error)
	CompleteTask(ctx context.Context, id string) (*dto.CollectionTaskResponse, error)
	ReopenTask(ctx context.Context, id string) (*dto.CollectionTaskResponse, error)

	// RunPlaybooks runs one scheduler pass in every environment holding a live playbook.
	RunPlaybooks(ctx context.Context) error
	// RunEnvironment runs one scheduler pass in the environment on ctx.
	RunEnvironment(ctx context.Context) (*dto.RunPlaybooksResponse, error)
}

type playbookService struct {
	ServiceParams
}

func NewPlaybookService(params ServiceParams) PlaybookService {
	return &playbookService{ServiceParams: params}
}

const (
	// playbookDefaultName names a blank playbook created without a name.
	playbookDefaultName = "Untitled playbook"
	// playbookPreviewCap stops a preview count; a larger match is reported as truncated.
	playbookPreviewCap = 1000
)

func (s *playbookService) ListTemplates(ctx context.Context) (*dto.ListPlaybookTemplatesResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}
	resp := &dto.ListPlaybookTemplatesResponse{Items: make([]dto.PlaybookTemplateResponse, 0, len(playbookTemplates))}
	for _, t := range playbookTemplates {
		resp.Items = append(resp.Items, dto.PlaybookTemplateResponse{
			ID: t.ID, Name: t.Name, Description: t.Description, Trigger: t.Trigger, Audience: t.Audience, Steps: t.copySteps(),
		})
	}
	return resp, nil
}

func (s *playbookService) List(ctx context.Context, req *dto.ListPlaybooksRequest) (*dto.ListPlaybooksResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}
	states, err := req.States()
	if err != nil {
		return nil, err
	}
	list, err := s.PlaybookRepo.List(ctx, collections.PlaybookFilter{States: states})
	if err != nil {
		return nil, err
	}
	return s.listResponse(ctx, list)
}

func (s *playbookService) listResponse(ctx context.Context, list []*collections.Playbook) (*dto.ListPlaybooksResponse, error) {
	stats, err := s.PlaybookRepo.Stats(ctx, lo.Map(list, func(p *collections.Playbook, _ int) string { return p.ID }), clock.Now())
	if err != nil {
		return nil, err
	}
	resp := &dto.ListPlaybooksResponse{Items: make([]*dto.PlaybookResponse, 0, len(list))}
	for _, p := range list {
		st := stats[p.ID]
		resp.Items = append(resp.Items, dto.NewPlaybookResponse(p, &st))
	}
	return resp, nil
}

func (s *playbookService) Get(ctx context.Context, id string) (*dto.PlaybookResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}
	p, err := s.PlaybookRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.response(ctx, p)
}

func (s *playbookService) response(ctx context.Context, p *collections.Playbook) (*dto.PlaybookResponse, error) {
	stats, err := s.PlaybookRepo.Stats(ctx, []string{p.ID}, clock.Now())
	if err != nil {
		return nil, err
	}
	st := stats[p.ID]
	return dto.NewPlaybookResponse(p, &st), nil
}

func (s *playbookService) Create(ctx context.Context, req *dto.CreatePlaybookRequest) (*dto.PlaybookResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	p := &collections.Playbook{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COLLECTION_PLAYBOOK),
		Name:          lo.CoalesceOrEmpty(req.Name, playbookDefaultName),
		State:         types.PlaybookStateDraft,
		Trigger:       types.PlaybookTriggerOverdue,
		Audience:      types.PlaybookAudience{Kind: types.PlaybookAudienceAll},
		Steps:         []types.PlaybookStep{emailStep("step_1", 3, types.ReminderToneFriendly, false)},
		Stops:         types.NormalizePlaybookStops([]types.PlaybookStopCondition{types.PlaybookStopManuallyPaused}),
		MinGapDays:    3,
		SkipWeekends:  true,
		Version:       1,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	if req.TemplateID != "" {
		t, ok := findPlaybookTemplate(req.TemplateID)
		if !ok {
			return nil, ierr.NewErrorf("unknown template %s", req.TemplateID).
				WithHint("Pick a template from GET /collections/playbook-templates.").
				Mark(ierr.ErrValidation)
		}
		p.Name = lo.CoalesceOrEmpty(req.Name, t.Name)
		p.Description, p.Trigger, p.Audience, p.Steps, p.TemplateID = t.Description, t.Trigger, t.Audience, t.copySteps(), t.ID
	}

	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		top, err := s.PlaybookRepo.MaxPriority(txCtx)
		if err != nil {
			return err
		}
		p.Priority = top + 1
		return s.PlaybookRepo.Create(txCtx, p)
	})
	if err != nil {
		return nil, err
	}
	s.Logger.Info(ctx, "playbook created", "playbook_id", p.ID, "template_id", p.TemplateID)
	return dto.NewPlaybookResponse(p, &collections.PlaybookStats{}), nil
}

func (s *playbookService) Update(ctx context.Context, id string, req *dto.UpdatePlaybookRequest) (*dto.PlaybookResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var saved *collections.Playbook
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		cur, err := s.PlaybookRepo.Get(txCtx, id)
		if err != nil {
			return err
		}
		if cur.Version != req.Version {
			return playbookStale()
		}
		next := *cur
		next.Name, next.Description, next.Trigger, next.Audience = req.Name, req.Description, req.Trigger, req.Audience
		next.Steps, next.Stops = req.Steps, req.Stops
		if req.MinGapDays != nil {
			next.MinGapDays = *req.MinGapDays
		}
		if req.SkipWeekends != nil {
			next.SkipWeekends = *req.SkipWeekends
		}
		if next.State == types.PlaybookStateLive {
			// A live playbook stays ready: an edit that would block activation is refused rather than saved half-done.
			if problems := next.Problems(); len(problems) > 0 {
				return playbookNotReady("This playbook is live, so every step must stay complete.", problems)
			}
		}
		if err := s.PlaybookRepo.Update(txCtx, &next, cur.Version); err != nil {
			return err
		}
		saved = &next
		return s.syncPendingSteps(txCtx, cur, &next)
	})
	if err != nil {
		return nil, err
	}
	return s.response(ctx, saved)
}

// syncPendingSteps carries an edit into the runs in progress: steps that were removed are skipped, steps whose day
// moved are rescheduled (never into the past) and new steps are scheduled when their day is still ahead. Steps that
// already ran keep their history.
func (s *playbookService) syncPendingSteps(ctx context.Context, before, after *collections.Playbook) error {
	runs, err := s.PlaybookRunRepo.ListLiveRuns(ctx, after.ID, "")
	if err != nil || len(runs) == 0 {
		return err
	}
	stepRuns, err := s.PlaybookRunRepo.ListStepRuns(ctx, lo.Map(runs, func(r *collections.PlaybookRun, _ int) string { return r.ID }))
	if err != nil {
		return err
	}
	byRun := lo.GroupBy(stepRuns, func(sr *collections.PlaybookStepRun) string { return sr.RunID })
	today := types.UTCDay(clock.Now())

	var created []*collections.PlaybookStepRun
	for _, run := range runs {
		scheduled := lo.SliceToMap(byRun[run.ID], func(sr *collections.PlaybookStepRun) (string, *collections.PlaybookStepRun) { return sr.StepID, sr })
		for _, sr := range byRun[run.ID] {
			if sr.State != types.PlaybookStepRunPending {
				continue
			}
			step, ok := after.Step(sr.StepID)
			next := *sr
			if !ok {
				next.State, next.SkipReason = types.PlaybookStepRunSkipped, types.PlaybookSkipEditedOut
			} else {
				// A step keeps its row when it moves or changes kind; it runs as it is when it falls due.
				due := types.PlaybookStepDueOn(run.InvoiceDueDate, step.Day)
				if due.Before(today) {
					due = today
				}
				if due.Equal(sr.DueAt) && step.Kind == sr.Kind {
					continue
				}
				next.DueAt, next.Kind = due, step.Kind
			}
			if err := s.PlaybookRunRepo.UpdateStepRun(ctx, &next, types.PlaybookStepRunPending); err != nil {
				return err
			}
		}
		for _, step := range after.Steps {
			if _, ok := scheduled[step.ID]; ok {
				continue
			}
			if _, existed := before.Step(step.ID); existed {
				// The step was already past when this invoice enrolled; it is not scheduled late.
				continue
			}
			due := types.PlaybookStepDueOn(run.InvoiceDueDate, step.Day)
			if due.Before(today) {
				continue
			}
			created = append(created, newStepRun(ctx, run.ID, step, due))
		}
	}
	return s.PlaybookRunRepo.CreateStepRuns(ctx, created)
}

func (s *playbookService) Delete(ctx context.Context, id string) error {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return err
	}
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		p, err := s.PlaybookRepo.Get(txCtx, id)
		if err != nil {
			return err
		}
		// Invoices in the playbook leave it; the scheduler offers them to the next playbook they match.
		runs, err := s.PlaybookRunRepo.ListLiveRuns(txCtx, id, "")
		if err != nil {
			return err
		}
		if err := s.endRuns(txCtx, runs, types.PlaybookStopReasonPlaybookDeleted); err != nil {
			return err
		}
		if err := s.PlaybookRepo.Delete(txCtx, p); err != nil {
			return err
		}
		s.Logger.Info(txCtx, "playbook deleted", "playbook_id", id, "runs_stopped", len(runs))
		return s.compactPriorities(txCtx)
	})
}

// endRuns stops the runs with the reason and skips their pending steps.
func (s *playbookService) endRuns(ctx context.Context, runs []*collections.PlaybookRun, reason types.PlaybookStopReason) error {
	if len(runs) == 0 {
		return nil
	}
	now := clock.Now()
	steps, err := s.PlaybookRunRepo.ListStepRuns(ctx, lo.Map(runs, func(r *collections.PlaybookRun, _ int) string { return r.ID }))
	if err != nil {
		return err
	}
	for _, sr := range steps {
		if sr.State != types.PlaybookStepRunPending {
			continue
		}
		next := *sr
		next.State, next.SkipReason = types.PlaybookStepRunSkipped, types.PlaybookSkipRunStopped
		if err := s.PlaybookRunRepo.UpdateStepRun(ctx, &next, types.PlaybookStepRunPending); err != nil {
			return err
		}
	}
	for _, run := range runs {
		next := *run
		next.State, next.StopReason, next.EndedAt, next.NextStepAt = types.PlaybookRunStopped, reason, &now, nil
		if err := s.PlaybookRunRepo.UpdateRun(ctx, &next); err != nil {
			return err
		}
	}
	return nil
}

// compactPriorities renumbers the remaining playbooks 1..n in their current order.
func (s *playbookService) compactPriorities(ctx context.Context) error {
	list, err := s.PlaybookRepo.List(ctx, collections.PlaybookFilter{})
	if err != nil {
		return err
	}
	changes := map[string]int{}
	for i, p := range list {
		if p.Priority != i+1 {
			changes[p.ID] = i + 1
		}
	}
	if len(changes) == 0 {
		return nil
	}
	return s.PlaybookRepo.SetPriorities(ctx, changes)
}

func (s *playbookService) Duplicate(ctx context.Context, id string) (*dto.PlaybookResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}
	var copied *collections.Playbook
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		src, err := s.PlaybookRepo.Get(txCtx, id)
		if err != nil {
			return err
		}
		top, err := s.PlaybookRepo.MaxPriority(txCtx)
		if err != nil {
			return err
		}
		name := src.Name + " (copy)"
		if len(name) > dto.PlaybookNameMaxLength {
			name = name[:dto.PlaybookNameMaxLength]
		}
		copied = &collections.Playbook{
			ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COLLECTION_PLAYBOOK), Name: name, Description: src.Description,
			State: types.PlaybookStateDraft, Priority: top + 1, Trigger: src.Trigger, Audience: src.Audience,
			Steps: append([]types.PlaybookStep(nil), src.Steps...), Stops: append([]types.PlaybookStopCondition(nil), src.Stops...),
			MinGapDays: src.MinGapDays, SkipWeekends: src.SkipWeekends, TemplateID: src.TemplateID, Version: 1,
			EnvironmentID: types.GetEnvironmentID(txCtx), BaseModel: types.GetDefaultBaseModel(txCtx),
		}
		return s.PlaybookRepo.Create(txCtx, copied)
	})
	if err != nil {
		return nil, err
	}
	return dto.NewPlaybookResponse(copied, &collections.PlaybookStats{}), nil
}

func (s *playbookService) Activate(ctx context.Context, id string, req *dto.PlaybookVersionRequest) (*dto.PlaybookResponse, error) {
	return s.transition(ctx, id, req, []types.PlaybookState{types.PlaybookStateDraft, types.PlaybookStatePaused}, types.PlaybookStateLive)
}

func (s *playbookService) Pause(ctx context.Context, id string, req *dto.PlaybookVersionRequest) (*dto.PlaybookResponse, error) {
	return s.transition(ctx, id, req, []types.PlaybookState{types.PlaybookStateLive}, types.PlaybookStatePaused)
}

func (s *playbookService) Resume(ctx context.Context, id string, req *dto.PlaybookVersionRequest) (*dto.PlaybookResponse, error) {
	return s.transition(ctx, id, req, []types.PlaybookState{types.PlaybookStatePaused}, types.PlaybookStateLive)
}

// transition moves a playbook to target from one of the states in from. Going live checks readiness and, from a
// paused state, reschedules steps that fell due while paused so they do not all run at once.
func (s *playbookService) transition(ctx context.Context, id string, req *dto.PlaybookVersionRequest, from []types.PlaybookState, target types.PlaybookState) (*dto.PlaybookResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var saved *collections.Playbook
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		cur, err := s.PlaybookRepo.Get(txCtx, id)
		if err != nil {
			return err
		}
		if cur.Version != req.Version {
			return playbookStale()
		}
		if !lo.Contains(from, cur.State) {
			return ierr.NewErrorf("playbook is %s", cur.State).
				WithHintf("This playbook is %s, so it cannot be set to %s.", cur.State, target).
				Mark(ierr.ErrVersionConflict)
		}
		next := *cur
		next.State = target
		if target == types.PlaybookStateLive {
			if problems := next.Problems(); len(problems) > 0 {
				return playbookNotReady("Finish the playbook before it goes live.", problems)
			}
			if next.ActivatedAt == nil {
				next.ActivatedAt = lo.ToPtr(clock.Now())
			}
		}
		if err := s.PlaybookRepo.Update(txCtx, &next, cur.Version); err != nil {
			return err
		}
		saved = &next
		if cur.State == types.PlaybookStatePaused && target == types.PlaybookStateLive {
			return s.supersedeMissedSteps(txCtx, next.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.Logger.Info(ctx, "playbook state changed", "playbook_id", id, "state", target)
	return s.response(ctx, saved)
}

// supersedeMissedSteps keeps a resumed playbook from sending a burst: for each run, of the steps that fell due while
// it was paused only those on the latest due day stay pending; earlier ones are skipped.
func (s *playbookService) supersedeMissedSteps(ctx context.Context, playbookID string) error {
	runs, err := s.PlaybookRunRepo.ListLiveRuns(ctx, playbookID, "")
	if err != nil || len(runs) == 0 {
		return err
	}
	steps, err := s.PlaybookRunRepo.ListStepRuns(ctx, lo.Map(runs, func(r *collections.PlaybookRun, _ int) string { return r.ID }))
	if err != nil {
		return err
	}
	for _, sr := range supersededSteps(steps, clock.Now()) {
		next := *sr
		next.State, next.SkipReason = types.PlaybookStepRunSkipped, types.PlaybookSkipSuperseded
		if err := s.PlaybookRunRepo.UpdateStepRun(ctx, &next, types.PlaybookStepRunPending); err != nil {
			return err
		}
	}
	return nil
}

// supersededSteps returns, per run, the pending steps due by now that are older than the run's latest due day.
func supersededSteps(steps []*collections.PlaybookStepRun, now time.Time) []*collections.PlaybookStepRun {
	latest := map[string]time.Time{}
	for _, sr := range steps {
		if sr.State == types.PlaybookStepRunPending && !sr.DueAt.After(now) && sr.DueAt.After(latest[sr.RunID]) {
			latest[sr.RunID] = sr.DueAt
		}
	}
	var out []*collections.PlaybookStepRun
	for _, sr := range steps {
		if sr.State == types.PlaybookStepRunPending && !sr.DueAt.After(now) && sr.DueAt.Before(types.UTCDay(latest[sr.RunID])) {
			out = append(out, sr)
		}
	}
	return out
}

func (s *playbookService) Reorder(ctx context.Context, req *dto.ReorderPlaybooksRequest) (*dto.ListPlaybooksResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var list []*collections.Playbook
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		cur, err := s.PlaybookRepo.List(txCtx, collections.PlaybookFilter{})
		if err != nil {
			return err
		}
		ids := lo.Map(cur, func(p *collections.Playbook, _ int) string { return p.ID })
		missing, extra := lo.Difference(ids, req.IDs)
		if len(missing) > 0 || len(extra) > 0 {
			return ierr.NewError("playbook list does not match").
				WithHint("List every playbook of this environment exactly once. Reload the list and try again.").
				WithReportableDetails(map[string]any{"missing": missing, "unknown": extra}).
				Mark(ierr.ErrValidation)
		}
		changes := map[string]int{}
		current := lo.SliceToMap(cur, func(p *collections.Playbook) (string, int) { return p.ID, p.Priority })
		for i, id := range req.IDs {
			if current[id] != i+1 {
				changes[id] = i + 1
			}
		}
		if err := s.PlaybookRepo.SetPriorities(txCtx, changes); err != nil {
			return err
		}
		list, err = s.PlaybookRepo.List(txCtx, collections.PlaybookFilter{})
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.listResponse(ctx, list)
}

func (s *playbookService) Preview(ctx context.Context, id string) (*dto.PlaybookPreviewResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}
	p, err := s.PlaybookRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	now := clock.Now()
	resp := &dto.PlaybookPreviewResponse{AsOf: now, Sample: []dto.PlaybookPreviewInvoice{}}
	if len(p.Steps) == 0 {
		return resp, nil
	}
	matches, err := s.PlaybookRunRepo.ListCandidates(ctx, p, now, playbookPreviewCap+1)
	if err != nil {
		return nil, err
	}
	if len(matches) > playbookPreviewCap {
		matches, resp.Truncated = matches[:playbookPreviewCap], true
	}

	// Invoices a live playbook higher in the list would take first are counted apart.
	live, err := s.PlaybookRepo.List(ctx, collections.PlaybookFilter{States: []types.PlaybookState{types.PlaybookStateLive}})
	if err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	for _, other := range live {
		if other.ID == p.ID || other.Priority >= p.Priority {
			continue
		}
		theirs, err := s.PlaybookRunRepo.ListCandidates(ctx, other, now, playbookPreviewCap+1)
		if err != nil {
			return nil, err
		}
		for _, c := range theirs {
			taken[c.InvoiceID] = true
		}
	}

	for _, c := range matches {
		if taken[c.InvoiceID] {
			resp.TakenByHigherPriority++
			continue
		}
		resp.MatchingCount++
		if len(resp.Sample) >= dto.PlaybookPreviewSampleSize {
			continue
		}
		item := dto.PlaybookPreviewInvoice{
			InvoiceID: c.InvoiceID, InvoiceNumber: c.InvoiceNumber, CustomerID: c.CustomerID, CustomerName: c.CustomerName,
			Currency: c.Currency, AmountRemaining: c.AmountRemaining, DueDate: c.DueDate, DaysOverdue: types.CalendarDaysBetween(c.DueDate, now),
		}
		if step, due, ok := firstFutureStep(p, c.DueDate, now); ok {
			item.FirstStep = &dto.PlaybookPreviewStep{StepID: step.ID, Kind: step.Kind, DueAt: due}
		}
		resp.Sample = append(resp.Sample, item)
	}
	return resp, nil
}

// firstFutureStep is the first step of p an invoice due on dueDate would still get as of now.
func firstFutureStep(p *collections.Playbook, dueDate, now time.Time) (types.PlaybookStep, time.Time, bool) {
	today := types.UTCDay(now)
	for _, step := range p.OrderedSteps() {
		due := types.PlaybookStepDueOn(dueDate, step.Day)
		if !due.Before(today) {
			return step, due, true
		}
	}
	return types.PlaybookStep{}, time.Time{}, false
}

func (s *playbookService) ListRuns(ctx context.Context, playbookID string, req *dto.ListPlaybookRunsRequest) (*dto.ListPlaybookRunsResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	states, err := req.States()
	if err != nil {
		return nil, err
	}
	if _, err := s.PlaybookRepo.Get(ctx, playbookID); err != nil {
		return nil, err
	}
	views, total, err := s.PlaybookRunRepo.ListRunViews(ctx, playbookID, states, req.Offset, req.PageLimit())
	if err != nil {
		return nil, err
	}
	now := clock.Now()
	resp := &dto.ListPlaybookRunsResponse{
		Items:      make([]*dto.PlaybookRunResponse, 0, len(views)),
		Pagination: types.NewPaginationResponse(total, req.PageLimit(), req.Offset),
	}
	for _, v := range views {
		resp.Items = append(resp.Items, dto.NewPlaybookRunViewResponse(v, now))
	}
	return resp, nil
}

func (s *playbookService) PauseRun(ctx context.Context, runID string, req *dto.PausePlaybookRunRequest) (*dto.PlaybookRunResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	run, err := s.PlaybookRunRepo.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	p, err := s.PlaybookRepo.Get(ctx, run.PlaybookID)
	if err != nil {
		return nil, err
	}
	if !p.HasStop(types.PlaybookStopManuallyPaused) {
		return nil, ierr.NewError("pausing is off for this playbook").
			WithHint("This playbook does not let people pause an invoice. Turn on the \"Someone pauses the case\" stop condition first.").
			Mark(ierr.ErrInvalidOperation)
	}
	if run.State != types.PlaybookRunActive {
		return nil, runStateConflict(run, "paused")
	}
	next := *run
	next.State, next.PausedReason = types.PlaybookRunPaused, req.Reason
	if err := s.PlaybookRunRepo.UpdateRun(ctx, &next); err != nil {
		return nil, err
	}
	return dto.NewPlaybookRunResponse(&next, clock.Now()), nil
}

func (s *playbookService) ResumeRun(ctx context.Context, runID string) (*dto.PlaybookRunResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}
	var resumed *collections.PlaybookRun
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		run, err := s.PlaybookRunRepo.GetRun(txCtx, runID)
		if err != nil {
			return err
		}
		if run.State != types.PlaybookRunPaused {
			return runStateConflict(run, "resumed")
		}
		resumed, err = s.resumeRun(txCtx, run)
		return err
	})
	if err != nil {
		return nil, err
	}
	return dto.NewPlaybookRunResponse(resumed, clock.Now()), nil
}

// resumeRun sets a paused run active and skips the steps it missed except those on the latest due day.
func (s *playbookService) resumeRun(ctx context.Context, run *collections.PlaybookRun) (*collections.PlaybookRun, error) {
	steps, err := s.PlaybookRunRepo.ListStepRuns(ctx, []string{run.ID})
	if err != nil {
		return nil, err
	}
	for _, sr := range supersededSteps(steps, clock.Now()) {
		next := *sr
		next.State, next.SkipReason = types.PlaybookStepRunSkipped, types.PlaybookSkipSuperseded
		if err := s.PlaybookRunRepo.UpdateStepRun(ctx, &next, types.PlaybookStepRunPending); err != nil {
			return nil, err
		}
	}
	next := *run
	next.State, next.PausedReason = types.PlaybookRunActive, ""
	if err := s.PlaybookRunRepo.UpdateRun(ctx, &next); err != nil {
		return nil, err
	}
	return &next, nil
}

func (s *playbookService) PauseCustomer(ctx context.Context, customerID string, req *dto.PausePlaybookRunRequest) (*dto.CustomerPlaybookRunsResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.CustomerRepo.Get(ctx, customerID); err != nil {
		return nil, err
	}
	resp := &dto.CustomerPlaybookRunsResponse{Runs: []*dto.PlaybookRunResponse{}}
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		runs, err := s.PlaybookRunRepo.ListLiveRuns(txCtx, "", customerID)
		if err != nil {
			return err
		}
		now := clock.Now()
		for _, run := range runs {
			if run.State != types.PlaybookRunActive {
				continue
			}
			next := *run
			next.State, next.PausedReason = types.PlaybookRunPaused, req.Reason
			if err := s.PlaybookRunRepo.UpdateRun(txCtx, &next); err != nil {
				return err
			}
			resp.Runs = append(resp.Runs, dto.NewPlaybookRunResponse(&next, now))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	resp.RunsAffected = len(resp.Runs)
	return resp, nil
}

func (s *playbookService) ResumeCustomer(ctx context.Context, customerID string) (*dto.CustomerPlaybookRunsResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}
	if _, err := s.CustomerRepo.Get(ctx, customerID); err != nil {
		return nil, err
	}
	resp := &dto.CustomerPlaybookRunsResponse{Runs: []*dto.PlaybookRunResponse{}}
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		runs, err := s.PlaybookRunRepo.ListLiveRuns(txCtx, "", customerID)
		if err != nil {
			return err
		}
		now := clock.Now()
		for _, run := range runs {
			if run.State != types.PlaybookRunPaused {
				continue
			}
			resumed, err := s.resumeRun(txCtx, run)
			if err != nil {
				return err
			}
			resp.Runs = append(resp.Runs, dto.NewPlaybookRunResponse(resumed, now))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	resp.RunsAffected = len(resp.Runs)
	return resp, nil
}

func (s *playbookService) ListTasks(ctx context.Context, req *dto.ListCollectionTasksRequest) (*dto.ListCollectionTasksResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	filter := collections.TaskFilter{InvoiceID: req.InvoiceID, CustomerID: req.CustomerID, AssigneeUserID: req.AssigneeUserID}
	if req.State != "" {
		filter.States = []types.CollectionTaskState{req.State}
	}
	list, total, err := s.PlaybookRunRepo.ListTasks(ctx, filter, req.Offset, req.PageLimit())
	if err != nil {
		return nil, err
	}
	return &dto.ListCollectionTasksResponse{
		Items: lo.Map(list, func(t *collections.CollectionTask, _ int) *dto.CollectionTaskResponse {
			return dto.NewCollectionTaskResponse(t)
		}),
		Pagination: types.NewPaginationResponse(total, req.PageLimit(), req.Offset),
	}, nil
}

func (s *playbookService) CompleteTask(ctx context.Context, id string) (*dto.CollectionTaskResponse, error) {
	return s.setTaskState(ctx, id, types.CollectionTaskDone)
}

func (s *playbookService) ReopenTask(ctx context.Context, id string) (*dto.CollectionTaskResponse, error) {
	return s.setTaskState(ctx, id, types.CollectionTaskOpen)
}

func (s *playbookService) setTaskState(ctx context.Context, id string, state types.CollectionTaskState) (*dto.CollectionTaskResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}
	t, err := s.PlaybookRunRepo.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if t.State == state {
		return nil, ierr.NewErrorf("task is already %s", state).WithHintf("This task is already %s.", state).Mark(ierr.ErrVersionConflict)
	}
	next := *t
	next.State = state
	if state == types.CollectionTaskDone {
		next.DoneAt, next.DoneBy = lo.ToPtr(clock.Now()), types.GetUserID(ctx)
	} else {
		next.DoneAt, next.DoneBy = nil, ""
	}
	if err := s.PlaybookRunRepo.UpdateTask(ctx, &next); err != nil {
		return nil, err
	}
	return dto.NewCollectionTaskResponse(&next), nil
}

func newStepRun(ctx context.Context, runID string, step types.PlaybookStep, due time.Time) *collections.PlaybookStepRun {
	now := clock.Now()
	return &collections.PlaybookStepRun{
		ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COLLECTION_PLAYBOOK_STEP_RUN), RunID: runID, StepID: step.ID,
		Kind: step.Kind, DueAt: due, State: types.PlaybookStepRunPending,
		TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx), CreatedAt: now, UpdatedAt: now,
	}
}

func playbookStale() error {
	return ierr.NewError("playbook changed").
		WithHint("This playbook was changed by someone else since you opened it. Reload it and try again.").
		Mark(ierr.ErrVersionConflict)
}

// playbookNotReady refuses a change that would leave a live playbook incomplete; details.problems lists each gap.
func playbookNotReady(hint string, problems []types.PlaybookProblem) error {
	messages := lo.Map(problems, func(p types.PlaybookProblem, _ int) string {
		if p.StepID != "" {
			return fmt.Sprintf("step %s: %s", p.StepID, p.Message)
		}
		return p.Message
	})
	return ierr.NewError("playbook is not ready").
		WithHint(hint + " " + strings.Join(messages, "; ") + ".").
		WithReportableDetails(map[string]any{"problems": problems}).
		Mark(ierr.ErrInvalidOperation)
}

func runStateConflict(run *collections.PlaybookRun, action string) error {
	return ierr.NewErrorf("playbook run is %s", run.State).
		WithHintf("This invoice's playbook run is %s, so it cannot be %s.", run.State, action).
		Mark(ierr.ErrVersionConflict)
}

// isAlreadyExists reports a uniqueness conflict, which the scheduler treats as work someone else already did.
func isAlreadyExists(err error) bool {
	return errors.Is(err, ierr.ErrAlreadyExists)
}
