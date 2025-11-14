package models

import (
	"context"

	"go.temporal.io/sdk/client"
)

// ScheduleHandle represents a handle to a Temporal schedule
type ScheduleHandle interface {
	// Pause pauses the schedule
	Pause(ctx context.Context, options client.SchedulePauseOptions) error
	// Unpause unpauses the schedule
	Unpause(ctx context.Context, options client.ScheduleUnpauseOptions) error
	// Delete deletes the schedule
	Delete(ctx context.Context) error
	// Describe gets schedule information
	Describe(ctx context.Context) (*client.ScheduleDescription, error)
	// Update updates the schedule
	Update(ctx context.Context, options client.ScheduleUpdateOptions) error
}

// scheduleHandle wraps the SDK schedule handle
type scheduleHandle struct {
	handle client.ScheduleHandle
}

// NewScheduleHandle creates a new schedule handle wrapper
func NewScheduleHandle(handle client.ScheduleHandle) ScheduleHandle {
	return &scheduleHandle{
		handle: handle,
	}
}

// Pause pauses the schedule
func (s *scheduleHandle) Pause(ctx context.Context, options client.SchedulePauseOptions) error {
	return s.handle.Pause(ctx, options)
}

// Unpause unpauses the schedule
func (s *scheduleHandle) Unpause(ctx context.Context, options client.ScheduleUnpauseOptions) error {
	return s.handle.Unpause(ctx, options)
}

// Delete deletes the schedule
func (s *scheduleHandle) Delete(ctx context.Context) error {
	return s.handle.Delete(ctx)
}

// Describe gets schedule information
func (s *scheduleHandle) Describe(ctx context.Context) (*client.ScheduleDescription, error) {
	return s.handle.Describe(ctx)
}

// Update updates the schedule
func (s *scheduleHandle) Update(ctx context.Context, options client.ScheduleUpdateOptions) error {
	return s.handle.Update(ctx, options)
}
