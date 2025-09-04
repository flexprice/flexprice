// internal/temporal/activities/module.go
package activities

import "go.uber.org/fx"

var Module = fx.Module("plan-activities",
	fx.Provide(NewPlanActivities),
)
