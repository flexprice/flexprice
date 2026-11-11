package service

import (
	"context"
	cerrors "github.com/cockroachdb/errors"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type emailUserRepoFake struct {
	user.Repository
	known       map[string]bool
	sawEmptyCtx bool
}

func (r *emailUserRepoFake) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	if types.GetTenantID(ctx) == "" {
		r.sawEmptyCtx = true
	}
	if r.known[email] {
		return &user.User{Email: email}, nil
	}
	return nil, ierr.NewError("user not found").Mark(ierr.ErrNotFound)
}

func emailService(repo *exportRepoFake, available bool) (*scheduledTaskService, *emailUserRepoFake) {
	s := newExportService(repo, &exportTemporalFake{})
	if available {
		s.config.Email = config.EmailConfig{Enabled: true, ResendAPIKey: "k"}
		s.config.Exports.ConsoleBaseURL = "https://app"
	}
	users := &emailUserRepoFake{known: map[string]bool{"a@x.com": true, "b@x.com": true}}
	s.userRepo = users
	return s, users
}

func emailCreateReq(interval types.ScheduledTaskInterval, recipients ...string) dto.CreateScheduledTaskRequest {
	return dto.CreateScheduledTaskRequest{
		EntityType:  types.ScheduledTaskEntityTypeInvoice,
		Interval:    interval,
		Enabled:     true,
		EmailConfig: &types.ExportEmailConfig{Recipients: recipients},
	}
}

func TestCreate_EmailRecipientValidation(t *testing.T) {
	tests := []struct {
		name      string
		available bool
		interval  types.ScheduledTaskInterval
		rcpts     []string
		wantErr   string
	}{
		{"unavailable rejects", false, types.ScheduledTaskIntervalDaily, []string{"a@x.com"}, "Email is not configured"},
		{"hourly rejects", true, types.ScheduledTaskIntervalHourly, []string{"a@x.com"}, "hourly"},
		{"bad syntax rejects", true, types.ScheduledTaskIntervalDaily, []string{"not-an-email"}, "invalid"},
		{"non-user rejects", true, types.ScheduledTaskIntervalDaily, []string{"z@x.com"}, "z***@x.com is not a user in this account"},
		{"empty ok even if unavailable", false, types.ScheduledTaskIntervalDaily, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &exportRepoFake{}
			s, _ := emailService(repo, tt.available)
			_, err := s.CreateScheduledTask(exportCtx(), emailCreateReq(tt.interval, tt.rcpts...))
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.True(t, ierr.IsValidation(err), "got %v", err)
			require.Contains(t, err.Error()+" "+errText(err), tt.wantErr)
			require.Nil(t, repo.created)
		})
	}

	t.Run("11 rejects", func(t *testing.T) {
		s, _ := emailService(&exportRepoFake{}, true)
		s.userRepo = &emailUserRepoFake{known: map[string]bool{"a@x.com": true}}
		distinct := make([]string, 11)
		for i := range distinct {
			distinct[i] = string(rune('a'+i)) + "@x.com"
		}
		_, err := s.CreateScheduledTask(exportCtx(), emailCreateReq(types.ScheduledTaskIntervalDaily, distinct...))
		require.Error(t, err)
		require.True(t, ierr.IsValidation(err))
		require.Contains(t, errText(err), "10")
	})

	t.Run("dupes collapse", func(t *testing.T) {
		repo := &exportRepoFake{}
		s, _ := emailService(repo, true)
		resp, err := s.CreateScheduledTask(exportCtx(), emailCreateReq(types.ScheduledTaskIntervalDaily, " A@X.com", "a@x.com", "A@x.COM"))
		require.NoError(t, err)
		require.Equal(t, []string{"a@x.com"}, repo.created.EmailConfig.Recipients)
		require.Equal(t, []string{"a@x.com"}, resp.EmailConfig.Recipients)
	})
}

func TestValidateRecipients_TenantOnContext(t *testing.T) {
	s, users := emailService(&exportRepoFake{}, true)
	cfg := &types.ExportEmailConfig{Recipients: []string{"a@x.com"}}

	require.NoError(t, s.validateRecipients(exportCtx(), types.ScheduledTaskIntervalDaily, cfg))
	require.False(t, users.sawEmptyCtx)

	err := s.validateRecipients(context.Background(), types.ScheduledTaskIntervalDaily, cfg)
	require.Error(t, err)
	require.False(t, users.sawEmptyCtx)
}

func TestNormalizeRecipients(t *testing.T) {
	got := types.NormalizeRecipients([]string{" B@x.com", "a@x.com", "b@X.com", ""})
	require.Equal(t, []string{"b@x.com", "a@x.com"}, got)
}

func TestUpdate_EmailConfig(t *testing.T) {
	newTask := func() *scheduledtask.ScheduledTask {
		return &scheduledtask.ScheduledTask{
			ID: "st1", Interval: types.ScheduledTaskIntervalDaily, EntityType: types.ScheduledTaskEntityTypeInvoice,
			Status:      types.StatusPublished,
			EmailConfig: &types.ExportEmailConfig{Recipients: []string{"a@x.com"}},
		}
	}

	t.Run("sets normalized list", func(t *testing.T) {
		repo := &exportRepoFake{task: newTask()}
		s, _ := emailService(repo, true)
		resp, err := s.UpdateScheduledTask(exportCtx(), "st1", dto.UpdateScheduledTaskRequest{
			EmailConfig: &types.ExportEmailConfig{Recipients: []string{"B@x.com", "b@x.com"}},
		})
		require.NoError(t, err)
		require.Equal(t, []string{"b@x.com"}, repo.settings.EmailConfig.Recipients)
		require.Equal(t, []string{"b@x.com"}, resp.EmailConfig.Recipients)
	})

	t.Run("empty list clears", func(t *testing.T) {
		repo := &exportRepoFake{task: newTask()}
		s, _ := emailService(repo, false)
		resp, err := s.UpdateScheduledTask(exportCtx(), "st1", dto.UpdateScheduledTaskRequest{
			EmailConfig: &types.ExportEmailConfig{Recipients: []string{}},
		})
		require.NoError(t, err)
		require.NotNil(t, repo.settings)
		require.Nil(t, repo.settings.EmailConfig)
		require.Nil(t, resp.EmailConfig)
	})

	t.Run("absent keeps", func(t *testing.T) {
		repo := &exportRepoFake{task: newTask()}
		s, _ := emailService(repo, true)
		name := "renamed"
		resp, err := s.UpdateScheduledTask(exportCtx(), "st1", dto.UpdateScheduledTaskRequest{Name: &name})
		require.NoError(t, err)
		require.Equal(t, []string{"a@x.com"}, repo.settings.EmailConfig.Recipients)
		require.Equal(t, []string{"a@x.com"}, resp.EmailConfig.Recipients)
	})

	t.Run("hourly stored interval rejects", func(t *testing.T) {
		task := newTask()
		task.Interval = types.ScheduledTaskIntervalHourly
		repo := &exportRepoFake{task: task}
		s, _ := emailService(repo, true)
		_, err := s.UpdateScheduledTask(exportCtx(), "st1", dto.UpdateScheduledTaskRequest{
			EmailConfig: &types.ExportEmailConfig{Recipients: []string{"a@x.com"}},
		})
		require.Error(t, err)
		require.True(t, ierr.IsValidation(err))
		require.Nil(t, repo.settings)
	})

	t.Run("error never names raw address", func(t *testing.T) {
		repo := &exportRepoFake{task: newTask()}
		s, _ := emailService(repo, true)
		_, err := s.UpdateScheduledTask(exportCtx(), "st1", dto.UpdateScheduledTaskRequest{
			EmailConfig: &types.ExportEmailConfig{Recipients: []string{"zed@x.com"}},
		})
		require.Error(t, err)
		require.False(t, strings.Contains(err.Error()+errText(err), "zed@"))
	})
}

func errText(err error) string {
	return err.Error() + " " + cerrors.FlattenHints(err)
}
