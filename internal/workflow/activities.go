package workflow

import (
	"context"

	"github.com/flexprice/flexprice/internal/logger"
)

type Activities struct {
	log *logger.Logger
}

func NewActivities(log *logger.Logger) *Activities {
	return &Activities{log: log}
}

func (a *Activities) LogStep1(ctx context.Context) error {
	a.log.Info("Executing workflow step 1")
	return nil
}

func (a *Activities) LogStep2(ctx context.Context) error {
	a.log.Info("Executing workflow step 2")
	return nil
}

func (a *Activities) LogStep3(ctx context.Context) error {
	a.log.Info("Executing workflow step 3")
	return nil
}
