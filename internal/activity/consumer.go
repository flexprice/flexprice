package activity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/config"
	domainActivity "github.com/flexprice/flexprice/internal/domain/activity"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/metrics"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/types"
)

// ParentLookup returns the customer id of a parent entity (an invoice or a subscription).
type ParentLookup interface {
	CustomerID(ctx context.Context, t types.SystemEntityType, id string) (string, error)
}

type repoLookup struct {
	invoices      invoice.Repository
	subscriptions subscription.Repository
}

func NewParentLookup(invoices invoice.Repository, subscriptions subscription.Repository) ParentLookup {
	return &repoLookup{invoices: invoices, subscriptions: subscriptions}
}

func (l *repoLookup) CustomerID(ctx context.Context, t types.SystemEntityType, id string) (string, error) {
	switch t {
	case types.SystemEntityTypeInvoice:
		inv, err := l.invoices.Get(ctx, id)
		if err != nil {
			return "", err
		}
		return inv.CustomerID, nil
	case types.SystemEntityTypeSubscription:
		sub, err := l.subscriptions.Get(ctx, id)
		if err != nil {
			return "", err
		}
		return sub.CustomerID, nil
	}
	return "", fmt.Errorf("activity: no customer lookup for parent %q", t)
}

// Consumer turns activity messages into ClickHouse rows.
type Consumer struct {
	reg    *Registry
	repo   domainActivity.Repository
	lookup ParentLookup
	ps     types.ActivityPubSub
	log    *logger.Logger
}

func NewConsumer(reg *Registry, repo domainActivity.Repository, lookup ParentLookup, ps types.ActivityPubSub, log *logger.Logger) *Consumer {
	return &Consumer{reg: reg, repo: repo, lookup: lookup, ps: ps, log: log}
}

func (c *Consumer) RegisterHandler(r *pubsubRouter.Router, cfg *config.Configuration) {
	throttle := middleware.NewThrottle(cfg.Activity.RateLimit, time.Second)
	r.AddNoPublishHandler("activity_log_consumer", cfg.Activity.Topic, cfg.Activity.TopicDLQ, c.ps, c.process, throttle.Middleware)
}

func (c *Consumer) process(ctx context.Context, msg *message.Message) error {
	var m Message
	if err := json.Unmarshal(msg.Payload, &m); err != nil {
		return ierr.WithError(err).WithHint("Invalid activity message").Mark(ierr.ErrValidation)
	}
	ctx = types.SetTenantID(ctx, m.TenantID)
	ctx = types.SetEnvironmentID(ctx, m.EnvironmentID)

	logs := make([]*domainActivity.Log, 0, len(m.Events))
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
		customerID, err := resolveCustomer(ctx, reg.Customer, state, c.lookup)
		if err != nil {
			metrics.RecordCounter(ctx, metrics.ActivityCustomerUnresolved, 1)
			return err
		}
		logs = append(logs, &domainActivity.Log{
			ID: e.ID, TenantID: m.TenantID, EnvironmentID: m.EnvironmentID,
			EntityType: e.EntityType, EntityID: e.EntityID, EntityLabel: label(reg, state),
			CustomerID: customerID, Action: e.Action,
			ActorType: e.Actor.Type, ActorID: e.Actor.ID, ActorLabel: e.Actor.Label,
			Source: m.Request.Source, RequestID: m.Request.ID, IP: m.Request.IP, UserAgent: m.Request.UserAgent,
			State: stateText, SchemaVersion: uint16(m.SchemaVersion), OccurredAt: m.CommittedAt,
		})
	}
	return c.repo.Insert(ctx, logs)
}

// resolveCustomer finds the customer id an entry rolls up to; "" when the ref points at no customer.
func resolveCustomer(ctx context.Context, ref CustomerRef, state map[string]any, lookup ParentLookup) (string, error) {
	if ref.whenCol != "" && stringValue(state, ref.whenCol) != ref.whenVal {
		return "", nil
	}
	switch ref.kind {
	case refSelf:
		return stringValue(state, "id"), nil
	case refField:
		return stringValue(state, ref.column), nil
	case refParent:
		parentID := stringValue(state, ref.column)
		if parentID == "" {
			return "", nil
		}
		return lookup.CustomerID(ctx, ref.parent, parentID)
	case refFirstOf:
		for _, sub := range ref.refs {
			if sub.whenCol == "" || stringValue(state, sub.whenCol) == sub.whenVal {
				return resolveCustomer(ctx, sub, state, lookup)
			}
		}
	}
	return "", nil
}

// label is the first non-empty label field of the entity's state.
func label(reg *Registration, state map[string]any) string {
	for _, key := range reg.Label {
		if v := stringValue(state, key); v != "" {
			return v
		}
	}
	return ""
}

func stringValue(state map[string]any, key string) string {
	s, _ := state[key].(string)
	return s
}
