package service

import (
	"context"
	"errors"
	"reflect"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

const exportCatchupWindow = 24 * time.Hour

// connectionJobConfig validates connection-backed job config.
func (s *scheduledTaskService) connectionJobConfig(ctx context.Context, req dto.CreateScheduledTaskRequest) (*types.S3JobConfig, error) {
	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)

	conn, err := s.connectionRepo.Get(ctx, req.ConnectionID)
	if err != nil {
		s.logger.Error(ctx, "failed to get connection", "connection_id", req.ConnectionID, "error", err)
		return nil, ierr.WithError(err).
			WithHint("Connection not found").
			Mark(ierr.ErrNotFound)
	}

	jobConfig := req.JobConfig
	isManaged := conn.SyncConfig != nil && conn.SyncConfig.Storage != nil && conn.SyncConfig.Storage.IsFlexpriceManaged

	if conn.ProviderType == types.SecretProviderGCS && isManaged {
		s.logger.Info(ctx, "handling flexprice-managed GCS connection for scheduled task",
			"connection_id", req.ConnectionID,
			"tenant_id", tenantID)

		if req.JobConfig == nil {
			return nil, ierr.NewError("job config is required for flexprice-managed GCS").
				WithHint("Storage job configuration must be provided").
				Mark(ierr.ErrValidation)
		}

		if err := req.JobConfig.Compression.Validate(); err != nil {
			return nil, err
		}

		if err := s.config.FlexpriceGCSExports.Validate(); err != nil {
			return nil, err
		}

		jobConfig = &types.S3JobConfig{
			Provider:             types.SecretProviderGCS,
			Bucket:               s.config.FlexpriceGCSExports.Bucket,
			KeyPrefix:            conn.SyncConfig.Storage.KeyPrefix,
			Compression:          req.JobConfig.Compression,
			ExportMetadataFields: req.JobConfig.ExportMetadataFields,
		}
		if jobConfig.Compression == "" {
			jobConfig.Compression = types.S3CompressionTypeNone
		}

		if err := jobConfig.ExportMetadataFields.ValidateAndDefault(req.EntityType); err != nil {
			return nil, err
		}

		s.logger.Info(ctx, "populated job config for flexprice-managed GCS",
			"bucket", jobConfig.Bucket,
			"key_prefix", jobConfig.KeyPrefix,
			"tenant_id", tenantID,
			"environment_id", envID,
			"compression", jobConfig.Compression)
	} else if conn.ProviderType == types.SecretProviderS3 && isManaged {
		s.logger.Info(ctx, "handling flexprice-managed S3 connection for scheduled task",
			"connection_id", req.ConnectionID,
			"tenant_id", tenantID)

		if req.JobConfig == nil {
			return nil, ierr.NewError("job config is required for flexprice-managed S3").
				WithHint("S3 job configuration must be provided").
				Mark(ierr.ErrValidation)
		}

		if err := req.JobConfig.ValidateForFlexpriceManaged(); err != nil {
			s.logger.Error(ctx, "invalid job config for flexprice-managed S3", "error", err)
			return nil, err
		}

		jobConfig = &types.S3JobConfig{
			Bucket:               s.config.FlexpriceS3Exports.Bucket,
			Region:               s.config.FlexpriceS3Exports.Region,
			KeyPrefix:            conn.SyncConfig.Storage.KeyPrefix,
			Compression:          req.JobConfig.Compression,
			Encryption:           req.JobConfig.Encryption,
			ExportMetadataFields: req.JobConfig.ExportMetadataFields,
		}

		if jobConfig.Compression == "" {
			jobConfig.Compression = types.S3CompressionTypeNone
		}
		if jobConfig.Encryption == "" {
			jobConfig.Encryption = types.S3EncryptionTypeAES256
		}

		s.logger.Info(ctx, "populated job config for flexprice-managed S3",
			"bucket", jobConfig.Bucket,
			"region", jobConfig.Region,
			"key_prefix", jobConfig.KeyPrefix,
			"tenant_id", tenantID,
			"environment_id", envID,
			"compression", jobConfig.Compression,
			"encryption", jobConfig.Encryption)

		if err := jobConfig.Validate(); err != nil {
			s.logger.Error(ctx, "invalid populated job config for flexprice-managed S3", "error", err)
			return nil, err
		}
		if err := jobConfig.ExportMetadataFields.ValidateAndDefault(req.EntityType); err != nil {
			return nil, err
		}
	} else {
		if conn.ProviderType == types.SecretProviderGCS {
			return nil, ierr.NewError("custom GCS storage is not supported").
				WithHint("Only Flexprice-managed GCS storage is supported").
				Mark(ierr.ErrValidation)
		}
		if req.JobConfig != nil {
			req.JobConfig.Provider = conn.ProviderType
		}
		if err := req.JobConfig.Validate(); err != nil {
			s.logger.Error(ctx, "invalid job config for custom S3 connection", "error", err)
			return nil, err
		}
		if err := req.JobConfig.ExportMetadataFields.ValidateAndDefault(req.EntityType); err != nil {
			return nil, err
		}
		jobConfig = req.JobConfig
	}

	exportStorage, err := s.storageResolver.ForConnectionExport(ctx, req.ConnectionID, jobConfig)
	if err != nil {
		s.logger.Error(ctx, "export storage is not resolvable for scheduled task",
			"error", err, "connection_id", req.ConnectionID)
		return nil, err
	}

	// Probe customer buckets before saving.
	if !isManaged {
		probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := exportStorage.ValidateConnection(probeCtx); err != nil {
			s.logger.Error(ctx, "export bucket is not reachable with the connection's credentials",
				"connection_id", req.ConnectionID,
				"bucket", jobConfig.Bucket,
				"region", jobConfig.Region,
				"error", err)
			return nil, err
		}

		markerKey := storage.ObjectKey(jobConfig.KeyPrefix, string(req.EntityType), "flexprice-write-check", "txt", false)
		if err := exportStorage.ValidateWrite(probeCtx, markerKey); err != nil {
			s.logger.Error(ctx, "export bucket is not writable with the connection's credentials",
				"connection_id", req.ConnectionID,
				"bucket", jobConfig.Bucket,
				"key", markerKey,
				"error", err)
			return nil, err
		}
	}

	return jobConfig, nil
}

// flexpriceJobConfig targets the Flexprice bucket.
func (s *scheduledTaskService) flexpriceJobConfig(ctx context.Context, req dto.CreateScheduledTaskRequest, taskID string) (*types.S3JobConfig, error) {
	store, err := s.storageResolver.ForPlatform(ctx, storage.PurposeExport)
	if err != nil {
		return nil, err
	}

	jobConfig := &types.S3JobConfig{
		KeyPrefix: "exports/" + types.GetTenantID(ctx) + "/" + types.GetEnvironmentID(ctx) + "/" + taskID,
	}
	if req.JobConfig != nil {
		jobConfig.Compression = req.JobConfig.Compression
		jobConfig.ExportMetadataFields = req.JobConfig.ExportMetadataFields
	}
	if jobConfig.Compression == "" {
		jobConfig.Compression = types.S3CompressionTypeNone
	}

	switch store.Provider() {
	case storage.ProviderGCS:
		if err := s.config.FlexpriceGCSExports.Validate(); err != nil {
			return nil, err
		}
		jobConfig.Provider = types.SecretProviderGCS
		jobConfig.Bucket = s.config.FlexpriceGCSExports.Bucket
	default:
		jobConfig.Provider = types.SecretProviderS3
		jobConfig.Bucket = s.config.FlexpriceS3Exports.Bucket
		jobConfig.Region = s.config.FlexpriceS3Exports.Region
		if err := jobConfig.Validate(); err != nil {
			return nil, err
		}
	}

	if err := jobConfig.ExportMetadataFields.ValidateAndDefault(req.EntityType); err != nil {
		return nil, err
	}
	return jobConfig, nil
}

// scheduleSpec builds the Temporal spec.
func (s *scheduledTaskService) scheduleSpec(task *scheduledtask.ScheduledTask) (client.ScheduleSpec, error) {
	if task.Schedule == nil {
		return client.ScheduleSpec{CronExpressions: []string{s.getCronExpression(task.Interval)}}, nil
	}
	cron, err := scheduledtask.Cron(task.Interval, task.Schedule)
	if err != nil {
		return client.ScheduleSpec{}, err
	}
	return client.ScheduleSpec{
		CronExpressions: []string{cron},
		TimeZoneName:    task.Schedule.Timezone,
	}, nil
}

// replaceScheduleSpec keeps pause state.
func (s *scheduledTaskService) replaceScheduleSpec(ctx context.Context, scheduleID string, spec client.ScheduleSpec, timezoneSet bool) (bool, error) {
	handle := s.temporalClient.GetScheduleHandle(ctx, scheduleID)
	err := handle.Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(in client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			in.Description.Schedule.Spec = &spec
			if timezoneSet {
				policy := client.SchedulePolicies{}
				if in.Description.Schedule.Policy != nil {
					policy = *in.Description.Schedule.Policy
				}
				policy.Overlap = enums.SCHEDULE_OVERLAP_POLICY_BUFFER_ALL
				policy.CatchupWindow = exportCatchupWindow
				in.Description.Schedule.Policy = &policy
			}
			return &client.ScheduleUpdate{Schedule: &in.Description.Schedule}, nil
		},
	})
	var notFound *serviceerror.NotFound
	if errors.As(err, &notFound) {
		s.logger.Info(ctx, "temporal schedule not found; skipping spec sync", "schedule_id", scheduleID)
		return false, nil
	}
	if err != nil {
		return false, ierr.WithError(err).
			WithHint("Failed to update Temporal schedule").
			Mark(ierr.ErrInternal)
	}
	return true, nil
}

// updateExportSettings applies name, schedule.
func (s *scheduledTaskService) updateExportSettings(ctx context.Context, task *scheduledtask.ScheduledTask, req dto.UpdateScheduledTaskRequest) error {
	if req.Schedule != nil {
		if err := types.ValidateExportSchedule(task.Interval, req.Schedule); err != nil {
			return err
		}
	}

	previous := *task
	scheduleChanged := req.Schedule != nil && !reflect.DeepEqual(task.Schedule, req.Schedule)
	if req.Name != nil {
		task.Name = *req.Name
	}
	if scheduleChanged {
		task.Schedule = req.Schedule
	}
	task.UpdatedBy = types.GetUserID(ctx)

	synced := false
	if scheduleChanged && s.temporalClient != nil && task.TemporalScheduleID != "" {
		spec, err := s.scheduleSpec(task)
		if err != nil {
			return err
		}
		if synced, err = s.replaceScheduleSpec(ctx, task.TemporalScheduleID, spec, task.Schedule != nil); err != nil {
			return err
		}
	}

	if err := s.repo.UpdateSettings(ctx, task); err != nil {
		s.logger.Error(ctx, "failed to update scheduled task settings", "id", task.ID, "error", err)
		if synced {
			if spec, specErr := s.scheduleSpec(&previous); specErr == nil {
				if _, revertErr := s.replaceScheduleSpec(ctx, task.TemporalScheduleID, spec, previous.Schedule != nil); revertErr != nil {
					s.logger.Error(ctx, "failed to revert temporal schedule spec", "id", task.ID, "error", revertErr)
				}
			}
		}
		return ierr.WithError(err).
			WithHint("Failed to update scheduled task").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

// timezoneScheduleOptions sets overlap, catch-up.
func timezoneScheduleOptions(task *scheduledtask.ScheduledTask, opts *models.CreateScheduleOptions) {
	if task.Schedule == nil {
		return
	}
	opts.Overlap = enums.SCHEDULE_OVERLAP_POLICY_BUFFER_ALL
	opts.CatchupWindow = exportCatchupWindow
}
