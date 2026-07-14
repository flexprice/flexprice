package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/spf13/cobra"
)

//go:embed data.json
var svixDataFS embed.FS

type svixEventType struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type svixEventTypeFile struct {
	Data []svixEventType `json:"data"`
}

func newSvixCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "svix",
		Short: "Create Svix event types from the embedded event-type catalog",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSvixMigration(dryRun)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "log intended actions without applying")

	return cmd
}

func runSvixMigration(dryRun bool) error {
	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	baseURL := strings.TrimRight(cfg.Webhook.Svix.BaseURL, "/")
	token := cfg.Webhook.Svix.AuthToken
	if baseURL == "" || token == "" {
		log.Fatalf("svix-migrate: webhook.svix_config.base_url and auth_token must be set (FLEXPRICE_WEBHOOK_SVIX_CONFIG_BASE_URL / FLEXPRICE_WEBHOOK_SVIX_CONFIG_AUTH_TOKEN)")
	}

	raw, err := svixDataFS.ReadFile("data.json")
	if err != nil {
		log.Fatalf("read embedded data.json: %v", err)
	}
	var file svixEventTypeFile
	if err := json.Unmarshal(raw, &file); err != nil {
		log.Fatalf("parse data.json: %v", err)
	}

	log.Printf("svix-migrate: url=%s event-types=%d dry-run=%v", baseURL, len(file.Data), dryRun)

	client := &http.Client{Timeout: 15 * time.Second}
	created, failed := 0, 0
	for _, et := range file.Data {
		if dryRun {
			log.Printf("WOULD CREATE %s", et.Name)
			continue
		}
		if err := createSvixEventType(client, baseURL, token, et); err != nil {
			log.Printf("  FAILED %s: %v", et.Name, err)
			failed++
			continue
		}
		log.Printf("  OK %s", et.Name)
		created++
	}

	if dryRun {
		return nil
	}
	log.Printf("svix-migrate done: created=%d failed=%d", created, failed)
	if failed > 0 {
		log.Fatalf("svix-migrate: %d event-type(s) failed", failed)
	}
	return nil
}

func createSvixEventType(client *http.Client, baseURL, token string, et svixEventType) error {
	payload, err := json.Marshal(et)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/event-type/", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("idempotency-key", et.Name)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
