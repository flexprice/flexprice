package telemetry

import (
	"encoding/json"
	"runtime"

	"github.com/flexprice/flexprice/internal/buildinfo"
)

// EventType names a telemetry event; also its collector route suffix.
type EventType string

const (
	EventInstallInit EventType = "install_init"
	EventSystemEvent EventType = "system_event"
)

var eventRoutes = map[EventType]string{
	EventInstallInit: "/v1/init",
	EventSystemEvent: "/v1/event",
}

// InstallPayload is the one-shot process-start report.
type InstallPayload struct {
	InstanceID   string `json:"instance_id"`
	Version      string `json:"version"`
	GitSHA       string `json:"git_sha"`
	BuildVariant string `json:"build_variant"`
	Env          string `json:"env"`
	Region       string `json:"region"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	Role         string `json:"role"`
	Platform     string `json:"platform"`
	Cloud        string `json:"cloud"`
	NumCPU       int    `json:"num_cpu"`
	InstallLabel string `json:"install_label,omitempty"`
}

func newInstallPayload(r *Reporter) InstallPayload {
	return InstallPayload{
		InstanceID:   r.instanceID,
		Version:      buildinfo.Version,
		GitSHA:       buildinfo.GitSHA,
		BuildVariant: buildinfo.BuildVariant,
		Env:          r.cfg.Logging.Environment,
		Region:       r.cfg.Logging.Region,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		Role:         string(r.cfg.Deployment.Mode),
		Platform:     detectPlatform(),
		Cloud:        r.cloud.Load().(string),
		NumCPU:       runtime.NumCPU(),
		InstallLabel: r.cfg.Telemetry.InstallLabel,
	}
}

// SystemEventPayload is a lightweight per-webhook-event ping.
type SystemEventPayload struct {
	InstanceID    string          `json:"instance_id"`
	TenantID      string          `json:"tenant_id"`
	EnvironmentID string          `json:"environment_id"`
	EventName     string          `json:"event_name"`
	EventPayload  json.RawMessage `json:"event_payload,omitempty"`
}
