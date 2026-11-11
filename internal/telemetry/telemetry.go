package telemetry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"go.uber.org/fx"
)

const maxInflightEvents = 50

type Reporter struct {
	cfg          *config.Configuration
	log          *logger.Logger
	http         *http.Client
	instanceID   string
	deploymentID string
	cloud        atomic.Value // string
	inflight     chan struct{}
}

func NewReporter(cfg *config.Configuration, log *logger.Logger) *Reporter {
	hostname, _ := os.Hostname()
	r := &Reporter{
		cfg:          cfg,
		log:          log,
		http:         &http.Client{Timeout: 5 * time.Second},
		instanceID:   hostname,
		deploymentID: deploymentFingerprint(cfg),
		inflight:     make(chan struct{}, maxInflightEvents),
	}
	r.cloud.Store("unknown")
	return r
}

// deploymentFingerprint groups pods of one install under a stable ID shared
// by every replica, without a DB round-trip: every pod of one install
// already shares the same Postgres host in config.
func deploymentFingerprint(cfg *config.Configuration) string {
	host := cfg.Postgres.Host
	if host == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(host))
	return hex.EncodeToString(sum[:])[:16]
}

func RegisterHooks(lc fx.Lifecycle, r *Reporter) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if r.cfg.Telemetry.Disabled {
				r.log.Info(ctx, "telemetry off", "reason", "FLEXPRICE_TELEMETRY_DISABLED")
				return nil
			}
			r.log.Info(ctx, "telemetry on, set FLEXPRICE_TELEMETRY_DISABLED=true to opt out")
			cloudDone := make(chan struct{})
			go func() {
				r.cloud.Store(probeCloud())
				close(cloudDone)
			}()
			select {
			case <-cloudDone:
			case <-time.After(2 * time.Second):
			}
			r.reportInstallInit(ctx)
			return nil
		},
	})
}

func (r *Reporter) reportInstallInit(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	r.send(ctx, EventInstallInit, newInstallPayload(r))
}

// ReportEvent pings the collector for event names isTrackedEventName allows,
// with a redacted payload (redact.go), dispatched off the caller's goroutine.
func (r *Reporter) ReportEvent(ctx context.Context, event *types.WebhookEvent) {
	if r.cfg.Telemetry.Disabled {
		return
	}
	if !isTrackedEventName(string(event.EventName)) {
		return
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	eventName := string(event.EventName)
	rawPayload := append(json.RawMessage(nil), event.Payload...)

	select {
	case r.inflight <- struct{}{}:
	default:
		r.log.Debug(ctx, "telemetry event dropped, too many in flight")
		return
	}
	go func() {
		defer func() { <-r.inflight }()
		p := newSystemEventPayload(r, &systemEventInput{
			TenantID:      tenantID,
			EnvironmentID: environmentID,
			EventName:     eventName,
			Payload:       redactEventPayload(rawPayload),
		})
		sendCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r.send(sendCtx, EventSystemEvent, p)
	}()
}

func (r *Reporter) send(ctx context.Context, event EventType, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	path := eventRoutes[event]
	if path == "" {
		return
	}
	host := r.cfg.Telemetry.Host
	if host == "" {
		host = "https://telemetry.flexprice.io"
	}
	host = strings.TrimRight(host, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+path, bytes.NewReader(b))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.http.Do(req)
	if err != nil {
		r.log.Debug(ctx, "telemetry send failed", "event", event, "error", err)
		return
	}
	defer resp.Body.Close()
	r.log.Debug(ctx, "telemetry sent", "event", event, "status", resp.StatusCode)
}
