package internal

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

const defaultCalendarBillingAPIBase = "https://api.cloud.flexprice.io/v1"

// MigrateCalendarBillingCSV calls the Flexprice HTTP API to schedule-cancel each subscription
// in the CSV and create a new calendar-billing subscription (cancel then create; not atomic).
//
// Environment (set via scripts/main.go flags or env):
//   - SCRIPT_FLEXPRICE_API_KEY or FLEXPRICE_API_KEY (required): API key (see API_KEY_HEADER)
//   - API_KEY_HEADER (optional): default x-api-key (must match server auth.api_key.header if non-default)
//   - API_BASE_URL (optional): default https://api.cloud.flexprice.io/v1
//   - FILE_PATH (required): CSV of subscription IDs (column id or subscription_id, or first column)
//   - EFFECTIVE_DATE (required): RFC3339 or YYYY-MM-DD
//   - ENVIRONMENT_ID (optional): sent as X-Environment-ID when the key has no env
//   - FAILED_OUTPUT_PATH (optional): defaults to failed_calendar_billing_migration.csv
//   - SUCCESS_OUTPUT_PATH (optional): defaults to successful_calendar_billing_migration.csv (rows appended on each success)
//   - DRY_RUN (optional): true / 1 — GET + idempotency only, no cancel/create
//   - WORKER_COUNT (optional): default 3
//
// Caveats: cancel and create are separate HTTP calls (partial failure possible). Webhook suppression
// is not available on the public cancel API JSON body.
func MigrateCalendarBillingCSV() error {
	apiKey := strings.TrimSpace(os.Getenv("SCRIPT_FLEXPRICE_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("FLEXPRICE_API_KEY"))
	}
	keyHeader := strings.TrimSpace(os.Getenv("API_KEY_HEADER"))
	if keyHeader == "" {
		keyHeader = "x-api-key"
	}
	baseURL := strings.TrimSpace(os.Getenv("API_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultCalendarBillingAPIBase
	}
	baseURL = strings.TrimRight(baseURL, "/")

	filePath := os.Getenv("FILE_PATH")
	effectiveDateStr := os.Getenv("EFFECTIVE_DATE")
	failedOut := os.Getenv("FAILED_OUTPUT_PATH")
	successOut := os.Getenv("SUCCESS_OUTPUT_PATH")
	dryRun := os.Getenv("DRY_RUN") == "true" || os.Getenv("DRY_RUN") == "1"
	envID := strings.TrimSpace(os.Getenv("ENVIRONMENT_ID"))

	workerCount := 3
	if w := os.Getenv("WORKER_COUNT"); w != "" {
		if n, err := strconv.Atoi(w); err == nil && n > 0 {
			workerCount = n
		}
	}

	if apiKey == "" {
		return fmt.Errorf("SCRIPT_FLEXPRICE_API_KEY or FLEXPRICE_API_KEY is required")
	}
	if filePath == "" || effectiveDateStr == "" {
		return fmt.Errorf("FILE_PATH and EFFECTIVE_DATE are required")
	}
	if failedOut == "" {
		failedOut = "failed_calendar_billing_migration.csv"
	}
	if successOut == "" {
		successOut = "successful_calendar_billing_migration.csv"
	}

	effectiveDate, err := parseEffectiveDate(effectiveDateStr)
	if err != nil {
		return err
	}
	if !effectiveDate.After(time.Now().UTC()) {
		return fmt.Errorf("EFFECTIVE_DATE must be in the future (required by CancelSubscription validation)")
	}

	ids, err := readSubscriptionIDCSV(filePath)
	if err != nil {
		return err
	}

	client := &calendarBillingAPIClient{
		httpClient:   &http.Client{Timeout: 120 * time.Second},
		baseURL:      baseURL,
		apiKey:       apiKey,
		apiKeyHeader: keyHeader,
		envID:        envID,
	}

	log.Printf("migrate-calendar-billing-csv (API): %d ids, base=%s, effective=%s, workers=%d, dry_run=%v, api_key_len=%d, failed_out=%s, success_out=%s\n",
		len(ids), baseURL, effectiveDate.UTC().Format(time.RFC3339), workerCount, dryRun, len(apiKey), failedOut, successOut)

	failedWriter, failedFile, err := openFailedCSVAPI(failedOut)
	if err != nil {
		return err
	}
	defer failedFile.Close()

	successWriter, successFile, err := openSuccessCSVAPI(successOut)
	if err != nil {
		return err
	}
	defer successFile.Close()

	var (
		mu             sync.Mutex
		seenID         = make(map[string]struct{})
		ok, skipped    int
		failed         int
		failedWriteMu  sync.Mutex
		successWriteMu sync.Mutex
	)

	jobs := make(chan string)
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		for subID := range jobs {
			reason, successRow, err := processSubscriptionViaAPI(client, subID, effectiveDate, dryRun)
			mu.Lock()
			switch {
			case err != nil:
				failed++
				mu.Unlock()
				failedWriteMu.Lock()
				_ = failedWriter.Write([]string{subID, err.Error()})
				failedWriter.Flush()
				failedWriteMu.Unlock()
				log.Printf("FAILED sub=%s: %v\n", subID, err)
			case reason != "":
				skipped++
				mu.Unlock()
				log.Printf("SKIPPED sub=%s: %s\n", subID, reason)
			default:
				ok++
				mu.Unlock()
				if successRow != nil {
					successWriteMu.Lock()
					_ = successWriter.Write(successRow.csvRecord())
					successWriter.Flush()
					successWriteMu.Unlock()
				}
				log.Printf("SUCCESS sub=%s\n", subID)
			}
		}
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go worker()
	}

	for _, id := range ids {
		if id == "" {
			continue
		}
		mu.Lock()
		if _, dup := seenID[id]; dup {
			mu.Unlock()
			log.Printf("SKIPPED duplicate subscription id in CSV: %s\n", id)
			continue
		}
		seenID[id] = struct{}{}
		mu.Unlock()
		jobs <- id
	}
	close(jobs)
	wg.Wait()

	log.Printf("Summary: success=%d skipped=%d failed=%d (success rows → %s, failed → %s)\n", ok, skipped, failed, successOut, failedOut)
	if failed > 0 {
		return fmt.Errorf("completed with %d failed rows", failed)
	}
	return nil
}

type calendarBillingAPIClient struct {
	httpClient   *http.Client
	baseURL      string
	apiKey       string
	apiKeyHeader string
	envID        string
}

func (c *calendarBillingAPIClient) doJSON(method, path string, reqBody any, respBody any) error {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("url: %w", err)
	}

	var bodyReader io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, u.String(), bodyReader)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set(c.apiKeyHeader, c.apiKey)
	if c.envID != "" {
		req.Header.Set(types.HeaderEnvironment, c.envID)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if respBody == nil || len(respBytes) == 0 {
			return nil
		}
		if err := json.Unmarshal(respBytes, respBody); err != nil {
			return fmt.Errorf("decode success body: %w", err)
		}
		return nil
	}

	if resp.StatusCode == http.StatusUnauthorized {
		body := strings.TrimSpace(string(respBytes))
		return fmt.Errorf("api %s %s: %s — %s (use -api-key or SCRIPT_FLEXPRICE_API_KEY / FLEXPRICE_API_KEY; key must be valid for %s; if the key has no environment, set -environment-id)",
			method, path, resp.Status, body, c.baseURL)
	}

	var er ierr.ErrorResponse
	if json.Unmarshal(respBytes, &er) == nil && er.Error.Display != "" {
		return fmt.Errorf("api %s %s: %s (%s)", method, path, er.Error.Display, resp.Status)
	}
	return fmt.Errorf("api %s %s: %s — %s", method, path, resp.Status, strings.TrimSpace(string(respBytes)))
}

func readSubscriptionIDCSV(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	first, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}

	idCol, hasHeader := headerSubscriptionIDColumn(first)

	var ids []string
	appendID := func(rec []string) {
		if idCol >= len(rec) {
			return
		}
		id := strings.TrimSpace(rec[idCol])
		if id != "" {
			ids = append(ids, id)
		}
	}

	if !hasHeader {
		appendID(first)
	}

	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		appendID(rec)
	}
	return ids, nil
}

func headerSubscriptionIDColumn(rec []string) (idCol int, ok bool) {
	idx := make(map[string]int)
	for i, c := range rec {
		k := strings.ToLower(strings.TrimSpace(c))
		if _, exists := idx[k]; !exists {
			idx[k] = i
		}
	}
	if i, ok := idx["id"]; ok {
		return i, true
	}
	if i, ok := idx["subscription_id"]; ok {
		return i, true
	}
	return 0, false
}

func openFailedCSVAPI(path string) (*csv.Writer, *os.File, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("create failed csv: %w", err)
	}
	w := csv.NewWriter(f)
	if err := w.Write([]string{"id", "error"}); err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	w.Flush()
	return w, f, nil
}

func openSuccessCSVAPI(path string) (*csv.Writer, *os.File, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("create success csv: %w", err)
	}
	w := csv.NewWriter(f)
	if err := w.Write([]string{
		"original_subscription_id", "customer_id", "plan_id", "currency", "effective_date", "new_subscription_id", "mode",
	}); err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	w.Flush()
	return w, f, nil
}

// calendarBillingSuccessRow is written when a row completes without error and without skip.
type calendarBillingSuccessRow struct {
	OriginalSubscriptionID string
	CustomerID             string
	PlanID                 string
	Currency               string
	EffectiveDate          time.Time
	NewSubscriptionID      string
	Mode                   string // "migrated" or "dry_run"
}

func (r *calendarBillingSuccessRow) csvRecord() []string {
	return []string{
		r.OriginalSubscriptionID,
		r.CustomerID,
		r.PlanID,
		r.Currency,
		r.EffectiveDate.UTC().Format(time.RFC3339),
		r.NewSubscriptionID,
		r.Mode,
	}
}

func parseEffectiveDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t.UTC(), nil
	}
	t, err = time.Parse("2006-01-02", s)
	if err == nil {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	return time.Time{}, fmt.Errorf("invalid EFFECTIVE_DATE (use RFC3339 or YYYY-MM-DD): %w", err)
}

func utcDay(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func processSubscriptionViaAPI(client *calendarBillingAPIClient, subscriptionID string, effectiveDate time.Time, dryRun bool) (skipReason string, success *calendarBillingSuccessRow, err error) {
	path := "/subscriptions/" + url.PathEscape(subscriptionID)

	var subResp dto.SubscriptionResponse
	if err := client.doJSON(http.MethodGet, path, nil, &subResp); err != nil {
		return "", nil, err
	}
	if subResp.Subscription == nil {
		return "", nil, fmt.Errorf("empty subscription in response")
	}
	sub := subResp.Subscription

	if err := types.ValidateCurrencyCode(sub.Currency); err != nil {
		return "", nil, err
	}

	if sub.SubscriptionStatus == types.SubscriptionStatusCancelled {
		return "already cancelled", nil, nil
	}

	skipDup, dupReason, err := idempotentAlreadyMigratedAPI(client, subscriptionID, sub.CustomerID, sub.PlanID, effectiveDate)
	if err != nil {
		return "", nil, err
	}
	if skipDup {
		return dupReason, nil, nil
	}

	successBase := calendarBillingSuccessRow{
		OriginalSubscriptionID: subscriptionID,
		CustomerID:             sub.CustomerID,
		PlanID:                 sub.PlanID,
		Currency:               strings.ToLower(sub.Currency),
		EffectiveDate:          effectiveDate,
	}

	cancelReq := dto.CancelSubscriptionRequest{
		CancellationType:  types.CancellationTypeScheduledDate,
		CancelAt:          &effectiveDate,
		ProrationBehavior: types.ProrationBehaviorNone,
		Reason:            "migrating-to-calendar-billing",
	}

	monthly := types.BILLING_PERIOD_MONTHLY
	createReq := dto.CreateSubscriptionRequest{
		CustomerID:         sub.CustomerID,
		PlanID:             sub.PlanID,
		Currency:           strings.ToLower(sub.Currency),
		StartDate:          &effectiveDate,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleCalendar,
		ProrationBehavior:  types.ProrationBehaviorNone,
		CommitmentDuration: &monthly,
		EnableTrueUp:       false,
	}

	if dryRun {
		row := successBase
		row.Mode = "dry_run"
		return "", &row, nil
	}

	skipCancel := sub.CancelAt != nil && utcDay(*sub.CancelAt).Equal(utcDay(effectiveDate))

	if !skipCancel {
		cancelPath := path + "/cancel"
		if err := client.doJSON(http.MethodPost, cancelPath, cancelReq, nil); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "scheduled") {
				return "", nil, fmt.Errorf("subscription already has a scheduled cancellation; resolve manually: %w", err)
			}
			return "", nil, err
		}
	}

	var created dto.SubscriptionResponse
	if err := client.doJSON(http.MethodPost, "/subscriptions", createReq, &created); err != nil {
		return "", nil, err
	}
	row := successBase
	row.Mode = "migrated"
	if created.Subscription != nil {
		row.NewSubscriptionID = created.Subscription.ID
	}
	return "", &row, nil
}

func idempotentAlreadyMigratedAPI(
	client *calendarBillingAPIClient,
	currentSubID, customerID, planID string,
	effectiveDate time.Time,
) (bool, string, error) {
	f := types.NewNoLimitSubscriptionFilter()
	// API validates limit ∈ [1, 1000] (see types.QueryFilter.Validate).
	lim := 1000
	f.Limit = &lim
	f.CustomerID = customerID
	f.PlanID = planID
	f.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
		types.SubscriptionStatusPaused,
	}

	var listResp types.ListResponse[*dto.SubscriptionResponse]
	if err := client.doJSON(http.MethodPost, "/subscriptions/search", f, &listResp); err != nil {
		return false, "", err
	}

	wantDay := utcDay(effectiveDate)
	for _, item := range listResp.Items {
		if item == nil || item.Subscription == nil {
			continue
		}
		other := item.Subscription
		if other.ID == currentSubID {
			continue
		}
		if other.BillingCycle == types.BillingCycleCalendar && utcDay(other.StartDate).Equal(wantDay) {
			return true, "active calendar subscription already exists for customer+plan with same start date (idempotent skip)", nil
		}
	}
	return false, "", nil
}
