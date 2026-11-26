package service

import (
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

const collectionsNeedEmailsHint = "Turn on emails before Collections, and turn off Collections before emails."

func TestCollectionsConfig_DefaultsToOff(t *testing.T) {
	defaults, err := types.GetDefaultSettings()
	require.NoError(t, err)
	require.Equal(t, map[string]interface{}{"enabled": false}, defaults[types.SettingKeyCollectionsConfig].DefaultValue)

	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.settings.Clear()
	got, err := GetSetting[types.CollectionsConfig](NewSettingsService(f.params(f.reminders)).(*settingsService), ctx, types.SettingKeyCollectionsConfig)
	require.NoError(t, err)
	require.False(t, got.Enabled)
}

func TestCollectionsConfig_IsKeptInStepWithEmails(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	svc := NewSettingsService(f.params(f.reminders))
	value := func(enabled bool) *dto.UpdateSettingRequest {
		return &dto.UpdateSettingRequest{Value: map[string]interface{}{"enabled": enabled}}
	}
	refused := func(err error) {
		requireKind(t, err, ierr.ErrValidation)
		require.Equal(t, collectionsNeedEmailsHint, errors.FlattenHints(err))
	}

	// The fixture starts with Collections and emails both on.
	_, err := svc.UpdateSettingByKey(ctx, types.SettingKeyEmailNotificationConfig, value(false))
	refused(err)
	refused(svc.DeleteSettingByKey(ctx, types.SettingKeyEmailNotificationConfig))

	_, err = svc.UpdateSettingByKey(ctx, types.SettingKeyEmailNotificationConfig, &dto.UpdateSettingRequest{Value: map[string]interface{}{"attach_invoice_pdf": true}})
	require.NoError(t, err, "a change that leaves emails on is fine")

	// Turn Collections off first, then emails; turning Collections on needs emails.
	_, err = svc.UpdateSettingByKey(ctx, types.SettingKeyCollectionsConfig, value(false))
	require.NoError(t, err, "turning Collections off is always allowed")
	_, err = svc.UpdateSettingByKey(ctx, types.SettingKeyEmailNotificationConfig, value(false))
	require.NoError(t, err)

	_, err = svc.UpdateSettingByKey(ctx, types.SettingKeyCollectionsConfig, value(true))
	refused(err)

	_, err = svc.UpdateSettingByKey(ctx, types.SettingKeyEmailNotificationConfig, value(true))
	require.NoError(t, err)
	_, err = svc.UpdateSettingByKey(ctx, types.SettingKeyCollectionsConfig, value(true))
	require.NoError(t, err)

	require.NoError(t, svc.DeleteSettingByKey(ctx, types.SettingKeyCollectionsConfig), "deleting Collections is always allowed")
	require.NoError(t, svc.DeleteSettingByKey(ctx, types.SettingKeyEmailNotificationConfig), "and then emails can be deleted")
}

func TestCollections_EveryRouteIsForbiddenWhileDisabled(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 5)
	board := NewCollectionsService(f.params(f.reminders))

	calls := map[string]func() error{
		"board":  func() error { _, err := board.GetBoard(ctx, &dto.CollectionsBoardRequest{}); return err },
		"column": func() error { _, err := board.GetColumn(ctx, "not_due", &dto.CollectionsBoardRequest{}); return err },
		"list":   func() error { _, err := f.svc.List(ctx, &dto.ListRemindersRequest{}); return err },
		"get":    func() error { _, err := f.svc.Get(ctx, "a1"); return err },
		"update": func() error {
			_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
			return err
		},
		"bulk update": func() error {
			_, err := f.svc.BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusApproved, []string{"a1"}))
			return err
		},
	}

	f.setCollections(t, ctx, false)
	for name, call := range calls {
		err := call()
		requireKind(t, err, ierr.ErrPermissionDenied)
		require.Equal(t, "Collections is not enabled for this environment. Ask an administrator to turn it on in Settings.", errors.FlattenHints(err), name)
	}
	require.Empty(t, f.reminders.All(ctx), "nothing is written while disabled")

	f.settings.Clear()
	for name, call := range calls {
		require.True(t, ierr.IsPermissionDenied(call()), name+" with no setting at all")
	}

	f.setCollections(t, ctx, true)
	f.withEmail(t, ctx, emailsOn)
	for name, call := range calls {
		if name != "list" && name != "get" {
			continue
		}
		require.False(t, ierr.IsPermissionDenied(call()), name)
	}
}
