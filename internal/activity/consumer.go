package activity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/config"
	domainActivity "github.com/flexprice/flexprice/internal/domain/activity"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/metrics"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/types"
)

// Consumer turns activity messages into ClickHouse rows.
type Consumer struct {
	reg       *Registry
	repo      domainActivity.Repository
	customers LinkedCustomers
	ps        types.ActivityPubSub
	log       *logger.Logger
}

func NewConsumer(reg *Registry, repo domainActivity.Repository, customers LinkedCustomers, ps types.ActivityPubSub, log *logger.Logger) *Consumer {
	return &Consumer{reg: reg, repo: repo, customers: customers, ps: ps, log: log}
}

func (c *Consumer) RegisterHandler(r *pubsubRouter.Router, cfg *config.Configuration) {
	if !cfg.Activity.Enabled {
		c.log.Info(context.Background(), "activity log handler disabled by configuration")
		return
	}

	throttle := middleware.NewThrottle(cfg.Activity.RateLimit, time.Second)
	r.AddNoPublishHandler(activityHandlerName, cfg.Activity.Topic, cfg.Activity.TopicDLQ, c.ps, c.process, throttle.Middleware)

	c.log.Info(context.Background(), "registered activity log handler",
		"handler_name", activityHandlerName,
		"topic", cfg.Activity.Topic,
		"consumer_group", cfg.Activity.ConsumerGroup,
		"rate_limit", cfg.Activity.RateLimit,
	)
}

const activityHandlerName = "activity_log_handler"

func (c *Consumer) process(ctx context.Context, msg *message.Message) error {
	var m Message
	if err := json.Unmarshal(msg.Payload, &m); err != nil {
		return ierr.WithError(err).WithHint("Invalid activity message").Mark(ierr.ErrValidation)
	}
	if m.SchemaVersion < 1 || m.SchemaVersion > SchemaVersion {
		return ierr.NewErrorf("unknown activity schema version %d", m.SchemaVersion).Mark(ierr.ErrValidation)
	}
	ctx = types.SetTenantID(ctx, m.TenantID)
	ctx = types.SetEnvironmentID(ctx, m.EnvironmentID)

	logs := make([]*domainActivity.Log, 0, len(m.Events))
	var unresolved []error
	for _, e := range m.Events {
		reg, ok := c.reg.ByType(types.SystemEntityType(e.EntityType))
		if !ok {
			return ierr.NewErrorf("unknown activity entity type %q", e.EntityType).Mark(ierr.ErrValidation)
		}
		var state map[string]any
		stateText := ""
		if len(e.State) > 0 && !bytes.Equal(e.State, nullState) {
			if err := json.Unmarshal(e.State, &state); err != nil {
				return ierr.WithError(err).WithHint("Invalid activity state").Mark(ierr.ErrValidation)
			}
			stateText = string(e.State)
		}
		customerID, err := c.customers.customerID(ctx, reg.EntityType, e.EntityID, state)
		if err != nil {
			metrics.RecordCounter(ctx, metrics.ActivityCustomerUnresolved, 1, metrics.L(metrics.KeyEntity, e.EntityType))
			unresolved = append(unresolved, fmt.Errorf("activity %s (%s %s): customer lookup: %w", e.ID, e.EntityType, e.EntityID, err))
			continue
		}
		logs = append(logs, &domainActivity.Log{
			ID: e.ID, TenantID: m.TenantID, EnvironmentID: m.EnvironmentID,
			Entity:     domainActivity.Entity{Type: e.EntityType, ID: e.EntityID, Label: reg.label(state)},
			CustomerID: customerID, Action: e.Action,
			Actor:   domainActivity.Actor{Type: e.Actor.Type, ID: e.Actor.ID},
			Request: domainActivity.Request{ID: m.Request.ID, Source: m.Request.Source, IP: m.Request.IP, UserAgent: m.Request.UserAgent},
			State:   stateText, SchemaVersion: uint16(m.SchemaVersion), OccurredAt: m.CommittedAt,
		})
	}
	// Resolved entries are stored now; the message is retried for the rest and duplicates collapse by id.
	if len(logs) > 0 {
		if err := c.repo.Insert(ctx, logs); err != nil {
			return err
		}
	}
	return errors.Join(unresolved...)
}
