package cron

import (
	"context"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
)

// CollectionPlaybookActivities wraps the collection playbook scheduler.
type CollectionPlaybookActivities struct {
	playbookService service.PlaybookService
	logger          *logger.Logger
}

// NewCollectionPlaybookActivities builds activities for the collection playbooks cron workflow.
func NewCollectionPlaybookActivities(playbookService service.PlaybookService, log *logger.Logger) *CollectionPlaybookActivities {
	return &CollectionPlaybookActivities{playbookService: playbookService, logger: log}
}

// RunCollectionPlaybooksActivity runs one playbook pass in every environment holding a live playbook.
func (a *CollectionPlaybookActivities) RunCollectionPlaybooksActivity(ctx context.Context) error {
	if err := a.playbookService.RunPlaybooks(ctx); err != nil {
		a.logger.Error(ctx, "RunCollectionPlaybooksActivity failed", "error", err)
		return err
	}
	return nil
}
