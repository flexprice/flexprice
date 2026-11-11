package export

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/email"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type fakeSender struct {
	sent []email.SendEmailRequest
	resp *email.SendEmailResponse
	err  error
}

func (f *fakeSender) SendEmail(_ context.Context, r email.SendEmailRequest) (*email.SendEmailResponse, error) {
	f.sent = append(f.sent, r)
	if f.resp != nil || f.err != nil {
		return f.resp, f.err
	}
	return &email.SendEmailResponse{Success: true}, nil
}

type fakeUsers struct {
	known     map[string]bool
	tenantCtx []string
}

func (f *fakeUsers) GetByEmail(ctx context.Context, addr string) (*user.User, error) {
	f.tenantCtx = append(f.tenantCtx, types.GetTenantID(ctx))
	if f.known[addr] {
		return &user.User{Email: addr}, nil
	}
	return nil, ierr.NewError("nope").Mark(ierr.ErrNotFound)
}

type fakeCounter struct {
	enabled bool
	n       map[string]int64
	err     error
	keys    []string

	marked   map[string]bool
	setNXErr error
}

func (f *fakeCounter) IsEnabled() bool { return f.enabled }
func (f *fakeCounter) TrySetNX(_ context.Context, key string, _ interface{}, _ time.Duration) (bool, error) {
	if f.setNXErr != nil {
		return false, f.setNXErr
	}
	if f.marked == nil {
		f.marked = map[string]bool{}
	}
	if f.marked[key] {
		return false, nil
	}
	f.marked[key] = true
	return true, nil
}
func (f *fakeCounter) Incr(_ context.Context, key string, _ time.Duration) (int64, error) {
	f.keys = append(f.keys, key)
	if f.err != nil {
		return 0, f.err
	}
	f.n[key]++
	return f.n[key], nil
}

func newDeliveryActivity(sender *fakeSender, users *fakeUsers, c *fakeCounter, available bool, max int) *DeliveryActivity {
	cfg := &config.Configuration{}
	if available {
		cfg.Email.Enabled = true
		cfg.Email.ResendAPIKey = "k"
		cfg.Email.FromAddress = "noreply@x.com"
	}
	cfg.Exports.ConsoleBaseURL = "https://console.example.com/"
	cfg.Exports.MaxEmailsPerHour = max
	return NewDeliveryActivity(sender, users, c, nil, cfg, logger.NewNoopLogger())
}

func baseInput(recipients ...string) SendExportEmailsInput {
	return SendExportEmailsInput{
		TaskID: "task 1", ScheduledTaskID: "st_1", TenantID: "t1", EnvID: "e1",
		ExportName: "Monthly\nrevenue", Interval: types.ScheduledTaskIntervalDaily,
		Recipients: recipients, RunStatus: types.TaskStatusCompleted,
		WindowStart: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		WindowEnd:   time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
		RecordCount: 42,
	}
}

func TestSendExportEmails_Rules(t *testing.T) {
	tests := []struct {
		name       string
		available  bool
		counter    *fakeCounter
		max        int
		sender     *fakeSender
		recipients []string
		want       []string
		detail     string
		wantSent   int
	}{
		{"unavailable", false, &fakeCounter{enabled: true, n: map[string]int64{}}, 5, &fakeSender{}, []string{"a@x.com"}, []string{"skipped"}, "email not configured", 0},
		{"not a tenant user", true, &fakeCounter{enabled: true, n: map[string]int64{}}, 5, &fakeSender{}, []string{"zz@x.com"}, []string{"skipped"}, "not a tenant user", 0},
		{"delivered", true, &fakeCounter{enabled: true, n: map[string]int64{}}, 5, &fakeSender{}, []string{"a@x.com"}, []string{"delivered"}, "", 1},
		{"rate limited", true, &fakeCounter{enabled: true, n: map[string]int64{}}, 1, &fakeSender{}, []string{"a@x.com", "b@x.com"}, []string{"delivered", "rate_limited"}, "", 1},
		{"redis disabled no cap", true, &fakeCounter{enabled: false, n: map[string]int64{}}, 1, &fakeSender{}, []string{"a@x.com", "b@x.com"}, []string{"delivered", "delivered"}, "", 2},
		{"incr error", true, &fakeCounter{enabled: true, err: errors.New("boom"), n: map[string]int64{}}, 5, &fakeSender{}, []string{"a@x.com"}, []string{"failed"}, "rate limit check failed", 0},
		{"send error", true, &fakeCounter{enabled: true, n: map[string]int64{}}, 5, &fakeSender{err: errors.New("to a@x.com failed")}, []string{"a@x.com"}, []string{"failed"}, "send failed", 1},
		{"send not success", true, &fakeCounter{enabled: true, n: map[string]int64{}}, 5, &fakeSender{resp: &email.SendEmailResponse{Success: false, Error: "a@x.com bounced"}}, []string{"a@x.com"}, []string{"failed"}, "send failed", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			users := &fakeUsers{known: map[string]bool{"a@x.com": true, "b@x.com": true}}
			a := newDeliveryActivity(tc.sender, users, tc.counter, tc.available, tc.max)
			out, err := a.SendExportEmails(context.Background(), baseInput(tc.recipients...))
			require.NoError(t, err)
			require.Len(t, out.Deliveries, len(tc.want))
			for i, d := range out.Deliveries {
				require.Equal(t, types.ExportDeliveryTypeEmail, d.Type)
				require.Equal(t, tc.want[i], d.Status)
				require.Equal(t, email.MaskAddress(tc.recipients[i]), d.Target)
				require.NotContains(t, d.Target, "a@x.com")
				require.NotContains(t, d.Detail, "@")
				require.False(t, d.At.IsZero())
			}
			if tc.detail != "" {
				require.Equal(t, tc.detail, out.Deliveries[0].Detail)
			}
			require.Len(t, tc.sender.sent, tc.wantSent)
		})
	}
}

func TestSendExportEmails_ContentAndKey(t *testing.T) {
	sender := &fakeSender{}
	users := &fakeUsers{known: map[string]bool{"a@x.com": true}}
	c := &fakeCounter{enabled: true, n: map[string]int64{}}
	a := newDeliveryActivity(sender, users, c, true, 5)

	in := baseInput("a@x.com")
	_, err := a.SendExportEmails(context.Background(), in)
	require.NoError(t, err)
	_, err = a.SendExportEmails(context.Background(), in)
	require.NoError(t, err)

	require.Len(t, sender.sent, 2)
	require.Equal(t, sender.sent[0].IdempotencyKey, sender.sent[1].IdempotencyKey)
	require.True(t, strings.HasPrefix(sender.sent[0].IdempotencyKey, "exports-task 1-"))
	require.Len(t, strings.TrimPrefix(sender.sent[0].IdempotencyKey, "exports-task 1-"), 16)
	require.Equal(t, "Flexprice export completed", sender.sent[0].Subject)
	require.Equal(t, strings.Join([]string{
		"Export: Monthlyrevenue",
		"Data: 2026-11-01 00:00 to 2026-11-02 00:00 (UTC)",
		"Rows: 42",
		"Status: Completed",
		"",
		"View the run: https://console.example.com/exports/st_1/runs/task%201?env=e1",
	}, "\n"), sender.sent[0].Text)
	require.Contains(t, c.keys[0], "exports:email:t1:")
	require.Len(t, c.keys[0], len("exports:email:t1:")+10)
	require.Equal(t, []string{"t1", "t1"}, users.tenantCtx)
}

func TestSendExportEmails_RetryDoesNotRecountCap(t *testing.T) {
	sender := &fakeSender{}
	users := &fakeUsers{known: map[string]bool{"a@x.com": true, "b@x.com": true}}
	c := &fakeCounter{enabled: true, n: map[string]int64{}}
	a := newDeliveryActivity(sender, users, c, true, 2)

	in := baseInput("a@x.com", "b@x.com")
	for i := 0; i < 3; i++ {
		out, err := a.SendExportEmails(context.Background(), in)
		require.NoError(t, err)
		for _, d := range out.Deliveries {
			require.Equal(t, "delivered", d.Status)
		}
	}

	require.Len(t, c.keys, 2)
	require.Len(t, sender.sent, 6)
	require.Equal(t, sender.sent[0].IdempotencyKey, sender.sent[2].IdempotencyKey)
}

func TestSendExportEmails_MixedRecipients(t *testing.T) {
	sender := &fakeSender{}
	users := &fakeUsers{known: map[string]bool{"a@x.com": true, "c@x.com": true}}
	c := &fakeCounter{enabled: true, n: map[string]int64{}}
	a := newDeliveryActivity(sender, users, c, true, 5)

	out, err := a.SendExportEmails(context.Background(), baseInput("a@x.com", "gone@x.com", "c@x.com"))
	require.NoError(t, err)
	var got []string
	for _, d := range out.Deliveries {
		got = append(got, d.Status)
	}
	require.Equal(t, []string{"delivered", "skipped", "delivered"}, got)
	require.Len(t, sender.sent, 2)
}

func TestSendExportEmails_HourlySendsNothing(t *testing.T) {
	sender := &fakeSender{}
	users := &fakeUsers{known: map[string]bool{"a@x.com": true}}
	a := newDeliveryActivity(sender, users, &fakeCounter{n: map[string]int64{}}, true, 5)
	in := baseInput("a@x.com")
	in.Interval = types.ScheduledTaskIntervalHourly

	out, err := a.SendExportEmails(context.Background(), in)
	require.NoError(t, err)
	require.Empty(t, out.Deliveries)
	require.Empty(t, sender.sent)
}

func TestSendExportEmails_EmptyNameUsesEntityType(t *testing.T) {
	sender := &fakeSender{}
	users := &fakeUsers{known: map[string]bool{"a@x.com": true}}
	a := newDeliveryActivity(sender, users, &fakeCounter{n: map[string]int64{}}, true, 5)
	in := baseInput("a@x.com")
	in.ExportName = ""
	in.EntityType = types.ScheduledTaskEntityTypeEvents

	_, err := a.SendExportEmails(context.Background(), in)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(sender.sent[0].Text, "Export: "+string(types.ScheduledTaskEntityTypeEvents)+"\n"))
}

func TestSendExportEmails_FailedRun(t *testing.T) {
	sender := &fakeSender{}
	a := newDeliveryActivity(sender, &fakeUsers{known: map[string]bool{"a@x.com": true}}, &fakeCounter{enabled: true, n: map[string]int64{}}, true, 5)
	in := baseInput("a@x.com")
	in.RunStatus = types.TaskStatusFailed
	in.ExportName = strings.Repeat("n", 200)
	_, err := a.SendExportEmails(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, "Flexprice export failed", sender.sent[0].Subject)
	require.NotContains(t, sender.sent[0].Text, "Rows:")
	require.Contains(t, sender.sent[0].Text, "Status: Failed")
	require.Contains(t, sender.sent[0].Text, "Export: "+strings.Repeat("n", 120)+"\n")
	require.NotContains(t, sender.sent[0].Subject, "nnn")
}

func TestSendExportEmails_EmptyTenantRefused(t *testing.T) {
	sender := &fakeSender{}
	users := &fakeUsers{known: map[string]bool{"a@x.com": true}}
	a := newDeliveryActivity(sender, users, &fakeCounter{enabled: true, n: map[string]int64{}}, true, 5)
	in := baseInput("a@x.com")
	in.TenantID = ""
	out, err := a.SendExportEmails(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, "failed", out.Deliveries[0].Status)
	require.Empty(t, users.tenantCtx)
	require.Empty(t, sender.sent)
}

type staleReaderRepo struct {
	task.Repository
	t      *testing.T
	stored *task.Task
}

func (r *staleReaderRepo) Get(context.Context, string) (*task.Task, error) {
	r.t.Fatal("deliveries must not read the task")
	return nil, nil
}

func (r *staleReaderRepo) Update(context.Context, *task.Task) error {
	r.t.Fatal("deliveries must not rewrite the task")
	return nil
}

func (r *staleReaderRepo) SetTaskMetadataKey(_ context.Context, _, key string, value any) error {
	r.stored.Metadata[key] = value
	return nil
}

func TestRecordDeliveries_OnlyWritesDeliveriesKey(t *testing.T) {
	repo := &staleReaderRepo{t: t, stored: &task.Task{
		TaskStatus: types.TaskStatusCompleted,
		Metadata:   map[string]interface{}{"start_time": "s"},
	}}
	a := NewDeliveryActivity(nil, nil, nil, repo, &config.Configuration{}, logger.NewNoopLogger())
	ds := []types.ExportDelivery{{Type: "flexprice", Target: "Flexprice", Status: "delivered"}}

	require.NoError(t, a.RecordDeliveries(context.Background(), RecordDeliveriesInput{TaskID: "t", TenantID: "t1", EnvID: "e1", Deliveries: ds}))
	require.Equal(t, ds, repo.stored.Metadata["deliveries"])
	require.Equal(t, "s", repo.stored.Metadata["start_time"])
	require.Equal(t, types.TaskStatusCompleted, repo.stored.TaskStatus)
}
