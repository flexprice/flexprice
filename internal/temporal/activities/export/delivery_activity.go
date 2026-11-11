package export

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/email"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	defaultMaxEmailsPerHour = 200
	maxExportNameLen        = 120
	emailCounterTTL         = 2 * time.Hour
)

type emailSender interface {
	SendEmail(ctx context.Context, req email.SendEmailRequest) (*email.SendEmailResponse, error)
}

type userLookup interface {
	GetByEmail(ctx context.Context, email string) (*user.User, error)
}

type emailCounter interface {
	IsEnabled() bool
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
}

// DeliveryActivity sends emails, records deliveries.
type DeliveryActivity struct {
	sender   emailSender
	users    userLookup
	counter  emailCounter
	taskRepo task.Repository
	cfg      *config.Configuration
	logger   *logger.Logger
}

// NewDeliveryActivity creates a delivery activity.
func NewDeliveryActivity(
	sender emailSender,
	users userLookup,
	counter emailCounter,
	taskRepo task.Repository,
	cfg *config.Configuration,
	logger *logger.Logger,
) *DeliveryActivity {
	return &DeliveryActivity{sender: sender, users: users, counter: counter, taskRepo: taskRepo, cfg: cfg, logger: logger}
}

// SendExportEmailsInput describes a run notification.
type SendExportEmailsInput struct {
	TaskID          string
	ScheduledTaskID string
	TenantID        string
	EnvID           string
	ExportName      string
	EntityType      types.ScheduledTaskEntityType
	Interval        types.ScheduledTaskInterval
	Recipients      []string
	RunStatus       types.TaskStatus
	WindowStart     time.Time
	WindowEnd       time.Time
	RecordCount     int
}

// SendExportEmailsOutput holds one delivery per recipient.
type SendExportEmailsOutput struct {
	Deliveries []types.ExportDelivery
}

// SendExportEmails reports per-recipient failures as results.
func (a *DeliveryActivity) SendExportEmails(ctx context.Context, in SendExportEmailsInput) (*SendExportEmailsOutput, error) {
	out := &SendExportEmailsOutput{}
	if in.Interval == types.ScheduledTaskIntervalHourly {
		return out, nil
	}
	record := func(addr, status, detail string) {
		out.Deliveries = append(out.Deliveries, types.ExportDelivery{
			Type:   types.ExportDeliveryTypeEmail,
			Target: email.MaskAddress(addr),
			Status: status,
			Detail: detail,
			At:     clock.Now(),
		})
	}

	if in.TenantID == "" {
		for _, addr := range in.Recipients {
			record(addr, types.ExportDeliveryStatusFailed, "tenant missing")
		}
		return out, nil
	}
	ctx = types.SetTenantID(ctx, in.TenantID)
	ctx = types.SetEnvironmentID(ctx, in.EnvID)

	subject, body := a.compose(in)
	limit := int64(a.cfg.Exports.MaxEmailsPerHour)
	if limit <= 0 {
		limit = defaultMaxEmailsPerHour
	}

	for _, addr := range in.Recipients {
		if !a.cfg.ExportEmailAvailable() {
			record(addr, types.ExportDeliveryStatusSkipped, "email not configured")
			continue
		}
		if _, err := a.users.GetByEmail(ctx, addr); err != nil {
			if ierr.IsNotFound(err) {
				record(addr, types.ExportDeliveryStatusSkipped, "not a tenant user")
			} else {
				a.logger.Error(ctx, "export email user lookup failed", "error", "user lookup failed", "task_id", in.TaskID, "recipient", email.MaskAddress(addr))
				record(addr, types.ExportDeliveryStatusFailed, "user lookup failed")
			}
			continue
		}
		if a.counter != nil && a.counter.IsEnabled() {
			key := fmt.Sprintf("exports:email:%s:%s", in.TenantID, clock.Now().UTC().Format("2006010215"))
			n, err := a.counter.Incr(ctx, key, emailCounterTTL)
			if err != nil {
				a.logger.Error(ctx, "export email rate limit check failed", "error", err, "task_id", in.TaskID)
				record(addr, types.ExportDeliveryStatusFailed, "rate limit check failed")
				continue
			}
			if n > limit {
				record(addr, types.ExportDeliveryStatusRateLimited, "")
				continue
			}
		}

		sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		resp, err := a.sender.SendEmail(sendCtx, email.SendEmailRequest{
			ToAddress:      addr,
			Subject:        subject,
			Text:           body,
			IdempotencyKey: idempotencyKey(in.TaskID, addr),
		})
		cancel()
		if err != nil || resp == nil || !resp.Success {
			a.logger.Error(ctx, "export email send failed", "error", sendErrorText(err, resp), "task_id", in.TaskID, "recipient", email.MaskAddress(addr))
			record(addr, types.ExportDeliveryStatusFailed, "send failed")
			continue
		}
		record(addr, types.ExportDeliveryStatusDelivered, "")
	}
	return out, nil
}

func (a *DeliveryActivity) compose(in SendExportEmailsInput) (subject, body string) {
	completed := in.RunStatus == types.TaskStatusCompleted
	subject, status := "Flexprice export failed", "Failed"
	if completed {
		subject, status = "Flexprice export completed", "Completed"
	}
	link := fmt.Sprintf("%s/exports/%s/runs/%s?env=%s",
		strings.TrimRight(a.cfg.Exports.ConsoleBaseURL, "/"), in.ScheduledTaskID, url.PathEscape(in.TaskID), in.EnvID)

	const layout = "2006-01-02 15:04"
	lines := []string{
		"Export: " + exportLabel(in),
		fmt.Sprintf("Data: %s to %s (UTC)", in.WindowStart.UTC().Format(layout), in.WindowEnd.UTC().Format(layout)),
	}
	if completed {
		lines = append(lines, fmt.Sprintf("Rows: %d", in.RecordCount))
	}
	lines = append(lines, "Status: "+status, "", "View the run: "+link)
	return subject, strings.Join(lines, "\n")
}

func exportLabel(in SendExportEmailsInput) string {
	if name := sanitizeName(in.ExportName); name != "" {
		return name
	}
	return sanitizeName(string(in.EntityType))
}

func sanitizeName(name string) string {
	clean := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	if r := []rune(clean); len(r) > maxExportNameLen {
		clean = string(r[:maxExportNameLen])
	}
	return clean
}

func idempotencyKey(taskID, recipient string) string {
	sum := sha256.Sum256([]byte(recipient))
	return "exports-" + taskID + "-" + hex.EncodeToString(sum[:])[:16]
}

// Avoids logging provider text.
func sendErrorText(err error, resp *email.SendEmailResponse) string {
	if err != nil {
		return "send error"
	}
	if resp == nil {
		return "empty response"
	}
	return "provider rejected"
}

// RecordDeliveriesInput carries delivery results for a run.
type RecordDeliveriesInput struct {
	TaskID     string
	TenantID   string
	EnvID      string
	Deliveries []types.ExportDelivery
}

// RecordDeliveries writes deliveries metadata.
func (a *DeliveryActivity) RecordDeliveries(ctx context.Context, in RecordDeliveriesInput) error {
	ctx = types.SetTenantID(ctx, in.TenantID)
	ctx = types.SetEnvironmentID(ctx, in.EnvID)

	existing, err := a.taskRepo.Get(ctx, in.TaskID)
	if err != nil {
		a.logger.Error(ctx, "failed to get task for deliveries", "error", err, "task_id", in.TaskID)
		return ierr.WithError(err).WithHint("Failed to get task").Mark(ierr.ErrDatabase)
	}
	if existing.Metadata == nil {
		existing.Metadata = make(map[string]interface{})
	}
	existing.Metadata["deliveries"] = in.Deliveries

	userID := types.GetUserID(ctx)
	if userID == "" {
		userID = types.DefaultUserID
	}
	ctx = types.SetUserID(ctx, userID)
	existing.UpdatedAt = clock.Now()
	existing.UpdatedBy = userID

	if err := a.taskRepo.Update(ctx, existing); err != nil {
		a.logger.Error(ctx, "failed to record deliveries", "error", err, "task_id", in.TaskID)
		return ierr.WithError(err).WithHint("Failed to record deliveries").Mark(ierr.ErrDatabase)
	}
	return nil
}
