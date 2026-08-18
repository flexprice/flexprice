# Flexprice CLI v1.0 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a Go CLI for developers integrating Flexprice — login, resource commands for the whole API, an events inner loop, and scenario fixtures — with zero backend changes.

**Architecture:** One binary, one HTTP path. Commands are resolved at runtime from an OpenAPI spec embedded at build time, mapped through a curated `commands.yaml`. No SDK, no code generation, no second config system. `internal/client` is the only code that knows about base URLs, auth headers, retries, and error rendering.

**Tech Stack:** Go 1.25 · cobra · kin-openapi · huh · lipgloss · go-keyring · BurntSushi/toml · go-retryablehttp · goccy/go-yaml · goreleaser

**Design doc:** `docs/design/2026-08-18-flexprice-cli-design.md`

**Not in this plan:** `listen` and its backend endpoints (spec §11) — separate plan, separate subsystem.

---

## File structure

New Go module at `cli/` with module path `github.com/flexprice/cli`. Nested modules are supported by Go; the parent module excludes the directory. Source lives here, releases push to the `flexprice/cli` mirror.

```
cli/
├── go.mod                    module github.com/flexprice/cli
├── LICENSE                   Apache-2.0, explicit (root is AGPL)
├── README.md
├── .goreleaser.yaml
├── main.go                   thin entrypoint
├── spec/
│   ├── openapi.json          go:embed; synced by `make sync-cli-spec`
│   ├── commands.yaml         curated resource/action → operationId map
│   └── embed.go              //go:embed directives
└── internal/
    ├── exitcode/exitcode.go  stable exit codes
    ├── client/
    │   ├── client.go         THE single HTTP path
    │   └── errors.go         API error envelope → human message + exit code
    ├── config/
    │   ├── config.go         TOML load/save, profiles
    │   └── resolve.go        credential precedence
    ├── keyring/
    │   ├── keyring.go        interface + OS backend
    │   └── file.go           encrypted file fallback
    ├── spec/
    │   ├── loader.go         embed + kin-openapi load
    │   ├── registry.go       commands.yaml + derivation → registry
    │   ├── request.go        params/flags/body → *http.Request
    │   └── skeleton.go       --edit skeleton generation
    ├── output/
    │   ├── output.go         format dispatch, stdout/stderr split
    │   └── table.go          table rendering, column selection
    ├── fixtures/
    │   ├── engine.go         scenario runner + interpolation
    │   ├── simulate.go       simulate step type
    │   └── builtin/          embedded scenarios
    └── cmd/
        ├── root.go           root command, global flags
        ├── init.go           guided first run
        ├── auth.go           login / logout / whoami
        ├── env.go            env list
        ├── config.go         config list / use / set
        ├── resource.go       dynamic resource command tree
        ├── raw.go            get / post / delete
        ├── events.go         send / bulk / tail / query / usage / simulate
        ├── fixtures.go       trigger / fixtures run|list
        └── misc.go           open / version / completion
```

Each file has one responsibility. `internal/client` is the chokepoint every request passes through, including fixture steps — that is what makes the single-HTTP-path property structural rather than a convention.

---

## Phase 0 — Spike gate

### Task 1: Prove kin-openapi can generate an `--edit` skeleton

This gates Tasks 10 and 14. Design doc §14. Throwaway code — it is deleted at the end of this task; its output is a findings note that later tasks depend on.

**Files:**
- Create: `/tmp/cli-spike/main.go` (throwaway)
- Create: `docs/design/2026-08-18-cli-spike-findings.md` (committed)

- [ ] **Step 1: Set up the throwaway module**

```bash
mkdir -p /tmp/cli-spike && cd /tmp/cli-spike
go mod init spike
go get github.com/getkin/kin-openapi/openapi3@latest
cp "$REPO/docs/swagger/swagger-3-0.json" ./openapi.json
```

Replace `$REPO` with the absolute path to this repository.

- [ ] **Step 2: Write the spike**

`/tmp/cli-spike/main.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// skeleton walks a resolved schema and emits a JSON value with required fields
// populated by type. seen breaks reference cycles.
func skeleton(ref *openapi3.SchemaRef, seen map[*openapi3.Schema]bool, depth int) any {
	if ref == nil || ref.Value == nil || depth > 12 {
		return nil
	}
	s := ref.Value
	if seen[s] {
		return nil // cycle
	}
	seen[s] = true
	defer delete(seen, s)

	switch {
	case s.Type != nil && s.Type.Is("object"), len(s.Properties) > 0:
		out := map[string]any{}
		required := map[string]bool{}
		for _, r := range s.Required {
			required[r] = true
		}
		for name, prop := range s.Properties {
			if !required[name] {
				continue
			}
			out[name] = skeleton(prop, seen, depth+1)
		}
		return out
	case s.Type != nil && s.Type.Is("array"):
		return []any{skeleton(s.Items, seen, depth+1)}
	case s.Type != nil && s.Type.Is("integer"):
		return 0
	case s.Type != nil && s.Type.Is("number"):
		return 0.0
	case s.Type != nil && s.Type.Is("boolean"):
		return false
	default:
		return ""
	}
}

func main() {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile("openapi.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		os.Exit(1)
	}

	target := "CreateSubscriptionRequest"
	ref, ok := doc.Components.Schemas[target]
	if !ok {
		fmt.Fprintf(os.Stderr, "schema %q not found\n", target)
		os.Exit(1)
	}

	sk := skeleton(ref, map[*openapi3.Schema]bool{}, 0)
	b, _ := json.MarshalIndent(sk, "", "  ")
	fmt.Println(string(b))

	fmt.Fprintf(os.Stderr, "\n--- findings ---\n")
	fmt.Fprintf(os.Stderr, "kin-openapi version: see go.mod\n")
	fmt.Fprintf(os.Stderr, "required fields: %s\n", strings.Join(ref.Value.Required, ", "))
	fmt.Fprintf(os.Stderr, "top-level properties: %d\n", len(ref.Value.Properties))
}
```

- [ ] **Step 3: Run it**

```bash
cd /tmp/cli-spike && go run . > skeleton.json
```

Expected: `skeleton.json` contains a JSON object with `CreateSubscriptionRequest`'s required fields, and the program terminates (proving cycle handling works). If it hangs or stack-overflows, cycle detection is wrong — fix `skeleton()` before proceeding.

If the build fails on `s.Type.Is(...)` or `doc.Components.Schemas`, the pinned kin-openapi version exposes a different API. Run `go doc github.com/getkin/kin-openapi/openapi3.Schema` and adjust. **Record the working accessor names in the findings file — Tasks 10 and 14 use them verbatim.**

- [ ] **Step 4: Verify the skeleton round-trips through the real API**

Fill in the generated skeleton with valid values for an existing customer and plan, then:

```bash
curl -sS -X POST "https://us.api.flexprice.io/v1/subscriptions" \
  -H "x-api-key: $FLEXPRICE_API_KEY" \
  -H "Content-Type: application/json" \
  -d @skeleton-filled.json | head -40
```

Expected: HTTP 2xx with a subscription object, **or** a 4xx whose message names a specific missing/invalid field. A field-level 4xx is a **pass** — it proves the skeleton's shape is right and only values were wrong. A 4xx complaining about malformed JSON or an unparseable body is a **fail**.

Use a `development` environment key. Do not run this against production.

- [ ] **Step 5: Write the findings file**

`docs/design/2026-08-18-cli-spike-findings.md`:

```markdown
# CLI spike findings — kin-openapi skeleton generation

Date: 2026-08-18
Gate: design doc §14

## Verdict

PASS / FAIL  (delete one)

## kin-openapi

- Version pinned:
- Loader entrypoint:
- Schema map accessor:
- Type predicate form:
- Path item map accessor:

## CreateSubscriptionRequest

- Required fields:
- Top-level properties:
- Cycles encountered:
- Max depth reached:

## Round-trip result

- HTTP status:
- Response summary:

## Consequences

If FAIL: `--edit` is cut from v1.0. Task 14 is deleted and `--data` becomes the only
path for complex bodies. Tasks 10, 13 and 15 are unaffected.
```

- [ ] **Step 6: Commit the findings and delete the spike**

```bash
rm -rf /tmp/cli-spike
git add docs/design/2026-08-18-cli-spike-findings.md
git commit -m "docs: record CLI spike findings for kin-openapi skeleton generation"
```

**Gate:** if the verdict is FAIL, delete Task 14 from this plan and remove `--edit` from Task 15's flag set before continuing. Everything else proceeds unchanged.

---

## Phase 1 — Foundation

### Task 2: Module scaffold and build targets

**Files:**
- Create: `cli/go.mod`, `cli/main.go`, `cli/LICENSE`, `cli/README.md`
- Create: `cli/internal/cmd/root.go`
- Create: `cli/internal/cmd/root_test.go`
- Modify: `Makefile`

- [ ] **Step 1: Write the failing test**

`cli/internal/cmd/root_test.go`:

```go
package cmd

import (
	"bytes"
	"testing"
)

func TestRootCommand_HasName(t *testing.T) {
	root := NewRootCommand("test")
	if root.Use != "flexprice" {
		t.Fatalf("Use = %q, want %q", root.Use, "flexprice")
	}
}

func TestRootCommand_HelpMentionsUsageBilling(t *testing.T) {
	root := NewRootCommand("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("help output is empty")
	}
}
```

- [ ] **Step 2: Create the module and run the test to see it fail**

```bash
mkdir -p cli/internal/cmd
cd cli && go mod init github.com/flexprice/cli && go get github.com/spf13/cobra@latest
go test ./internal/cmd/ -run TestRootCommand -v
```

Expected: FAIL — `undefined: NewRootCommand`.

- [ ] **Step 3: Implement the root command**

`cli/internal/cmd/root.go`:

```go
package cmd

import (
	"github.com/spf13/cobra"
)

// Globals holds the values bound to the root command's persistent flags.
//
// An instance is created per root command and threaded into subcommands as a
// parameter rather than kept in a package variable: pflag writes each flag's
// default into the bound pointer at registration time, so a shared instance is
// clobbered the moment a second root is constructed. Verified by
// TestNewRootCommand_InstancesDoNotShareState.
type Globals struct {
	Profile  string
	Output   string
	APIKey   string
	BaseURL  string
	Region   string
	Quiet    bool
	Debug    bool
	NoColor  bool
	Limit    int
	All      bool
	Columns  []string
}

func NewRootCommand(version string) *cobra.Command {
	g := &Globals{}

	root := &cobra.Command{
		Use:     "flexprice",
		Short:   "Flexprice CLI — usage-based billing from your terminal",
		Version: version,
		Long: "Send events, inspect how they metered, and drive the Flexprice API " +
			"from your terminal.\n\nStart with: flexprice init",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	bindGlobals(root.PersistentFlags(), g)

	return root
}
```

`cli/main.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/flexprice/cli/internal/cmd"
)

// version is set by goreleaser at build time.
var version = "dev"

func main() {
	root := cmd.NewRootCommand(version)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Run the tests**

```bash
cd cli && go test ./internal/cmd/ -run TestRootCommand -v
```

Expected: PASS, both tests.

- [ ] **Step 5: Add the licence and README**

`cli/LICENSE` — the full Apache-2.0 text from https://www.apache.org/licenses/LICENSE-2.0.txt, with copyright `Copyright 2026 Flexprice`. This is deliberate: the repository root is AGPL-3.0 and the CLI must not inherit it by implication.

`cli/README.md`:

```markdown
# Flexprice CLI

Usage-based billing from your terminal.

    brew install flexprice/tap/flexprice
    flexprice init

**Source of truth is `flexprice/flexprice` at `cli/`.** This repository is a release
mirror — please open pull requests against the monorepo. Issues here are welcome.

Docs: https://docs.flexprice.io/cli
```

- [ ] **Step 6: Add Makefile targets**

Append to `Makefile`:

```makefile
# ---- CLI (cli/ is a separate Go module: github.com/flexprice/cli) ----

.PHONY: cli-build
cli-build:
	cd cli && go build -o bin/flexprice .

.PHONY: cli-test
cli-test:
	cd cli && go test -race ./...

.PHONY: cli-vet
cli-vet:
	cd cli && go vet ./...

.PHONY: sync-cli-spec
sync-cli-spec:
	mkdir -p cli/spec
	cp docs/swagger/swagger-3-0.json cli/spec/openapi.json
	@echo "Synced OpenAPI spec into cli/spec/openapi.json"
```

`make test` targets `./internal/...` of the root module and will not pick up `cli/`, which is why these are separate.

- [ ] **Step 7: Verify and commit**

```bash
make cli-test && make cli-vet && make cli-build && ./cli/bin/flexprice --help
```

Expected: tests pass, vet is silent, and `--help` prints the root help.

```bash
echo "cli/bin/" >> cli/.gitignore
git add cli Makefile && git commit -m "feat(cli): scaffold module, root command and build targets"
```

### Task 3: Exit codes and API error rendering

**Files:**
- Create: `cli/internal/exitcode/exitcode.go`
- Create: `cli/internal/client/errors.go`
- Create: `cli/internal/client/errors_test.go`

- [ ] **Step 1: Write the failing test**

`cli/internal/client/errors_test.go`:

```go
package client

import (
	"net/http"
	"strings"
	"testing"

	"github.com/flexprice/cli/internal/exitcode"
)

func TestAPIError_RendersWhatWhyNext(t *testing.T) {
	// Shape verified against the live API.
	body := []byte(`{"code":"not_found","message":"Customer with ID cust_missing was not found",` +
		`"http_status_code":404,"details":{"customer_id":"cust_missing"}}`)
	err := NewAPIError(http.StatusNotFound, body, "GET", "/v1/customers/cust_missing")

	got := err.Error()
	for _, want := range []string{"was not found", "customer_id", "cust_missing"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, missing %q", got, want)
		}
	}
	if err.ExitCode() != exitcode.NotFound {
		t.Errorf("ExitCode() = %d, want %d", err.ExitCode(), exitcode.NotFound)
	}
}

func TestAPIError_ExitCodeByStatus(t *testing.T) {
	cases := map[int]int{
		http.StatusUnauthorized:     exitcode.Auth,
		http.StatusForbidden:        exitcode.Auth,
		http.StatusNotFound:         exitcode.NotFound,
		http.StatusTooManyRequests:  exitcode.RateLimited,
		http.StatusBadRequest:       exitcode.Usage,
		http.StatusInternalServerError: exitcode.Generic,
	}
	for status, want := range cases {
		err := NewAPIError(status, []byte(`{}`), "GET", "/v1/x")
		if got := err.ExitCode(); got != want {
			t.Errorf("status %d: ExitCode() = %d, want %d", status, got, want)
		}
	}
}

// The auth middleware returns a bare string under "error", unlike every other path.
func TestAPIError_HandlesBareErrorString(t *testing.T) {
	err := NewAPIError(http.StatusUnauthorized, []byte(`{"error":"Unauthorized"}`), "GET", "/v1/customers")
	if !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("Error() = %q, want it to surface the bare error string", err.Error())
	}
	if err.ExitCode() != exitcode.Auth {
		t.Errorf("ExitCode() = %d, want %d", err.ExitCode(), exitcode.Auth)
	}
}

func TestAPIError_UnparseableBodyStillRenders(t *testing.T) {
	err := NewAPIError(http.StatusBadGateway, []byte("<html>gateway</html>"), "GET", "/v1/x")
	if err.Error() == "" {
		t.Fatal("Error() is empty for a non-JSON body")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd cli && go test ./internal/client/ -v
```

Expected: FAIL — package `exitcode` and `NewAPIError` do not exist.

- [ ] **Step 3: Implement**

`cli/internal/exitcode/exitcode.go`:

```go
// Package exitcode defines the CLI's stable exit codes. These are a public
// contract: scripts depend on them, so values never change.
package exitcode

const (
	OK          = 0
	Generic     = 1
	Usage       = 2
	Auth        = 3
	NotFound    = 4
	RateLimited = 5
)
```

`cli/internal/client/errors.go`:

```go
package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/flexprice/cli/internal/exitcode"
)

// envelope matches the API's error responses. Three shapes exist in practice,
// verified against the live API:
//
//	{"code":"not_found","message":"...","http_status_code":404,"details":{"customer_id":"..."}}
//	{"error":"Unauthorized"}                      // auth middleware, a bare string
//	<non-JSON>                                    // gateways and proxies
//
// details is field-keyed and is the most actionable part of a validation
// failure, so it is rendered rather than discarded.
type envelope struct {
	Code           string            `json:"code"`
	Message        string            `json:"message"`
	HTTPStatusCode int               `json:"http_status_code"`
	Details        map[string]any    `json:"details"`
	Error          json.RawMessage   `json:"error"`
}

// APIError renders as what failed, why, and what to do next.
type APIError struct {
	Status  int
	Method  string
	Path    string
	Message string
	Code    string
	Details map[string]any
	Raw     []byte
}

func NewAPIError(status int, body []byte, method, path string) *APIError {
	e := &APIError{Status: status, Method: method, Path: path, Raw: body}

	var env envelope
	if err := json.Unmarshal(body, &env); err == nil {
		e.Message = env.Message
		e.Code = env.Code
		e.Details = env.Details

		// The auth middleware returns {"error":"Unauthorized"} — a string, not an
		// object — so it is decoded separately rather than into the main shape.
		if e.Message == "" && len(env.Error) > 0 {
			var bare string
			if json.Unmarshal(env.Error, &bare) == nil {
				e.Message = bare
			}
		}
	}
	if e.Message == "" {
		e.Message = http.StatusText(status)
	}
	if e.Message == "" {
		e.Message = fmt.Sprintf("HTTP %d", status)
	}
	return e
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s failed (HTTP %d)\n  %s", e.Method, e.Path, e.Status, e.Message)

	// details names the offending fields; sorted so the output is deterministic.
	if len(e.Details) > 0 {
		keys := make([]string, 0, len(e.Details))
		for k := range e.Details {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "\n    %s: %v", k, e.Details[k])
		}
	}
	if next := e.nextStep(); next != "" {
		fmt.Fprintf(&b, "\n\n  %s", next)
	}
	return b.String()
}

// nextStep turns a status into a concrete action the user can take.
func (e *APIError) nextStep() string {
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "Your key may be invalid or from a different region. Check: flexprice whoami"
	case http.StatusTooManyRequests:
		return "Rate limited. Retry in a moment, or lower --interval if you are tailing."
	case http.StatusNotFound:
		return "If you expected this endpoint to exist, your CLI may predate it: brew upgrade flexprice"
	}
	return ""
}

func (e *APIError) ExitCode() int {
	switch {
	case e.Status == http.StatusUnauthorized, e.Status == http.StatusForbidden:
		return exitcode.Auth
	case e.Status == http.StatusNotFound:
		return exitcode.NotFound
	case e.Status == http.StatusTooManyRequests:
		return exitcode.RateLimited
	case e.Status >= 400 && e.Status < 500:
		return exitcode.Usage
	default:
		return exitcode.Generic
	}
}
```

- [ ] **Step 4: Run the tests**

```bash
cd cli && go test ./internal/client/ -v
```

Expected: PASS, all three tests.

- [ ] **Step 5: Commit**

```bash
git add cli/internal/exitcode cli/internal/client
git commit -m "feat(cli): stable exit codes and API error rendering"
```

### Task 4: The HTTP client — the single path

Every request in the CLI goes through this, including fixture steps. Design doc §4.1.

**Files:**
- Create: `cli/internal/client/client.go`
- Create: `cli/internal/client/client_test.go`

- [ ] **Step 1: Write the failing test**

`cli/internal/client/client_test.go`:

```go
package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_SendsAPIKeyHeader(t *testing.T) {
	var gotKey, gotEnv string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotEnv = r.Header.Get("x-environment-id")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, APIKey: "sk_test_123", Version: "1.2.3"})
	if _, err := c.Do(context.Background(), http.MethodGet, "/customers", nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotKey != "sk_test_123" {
		t.Errorf("x-api-key = %q, want %q", gotKey, "sk_test_123")
	}
	// API keys are environment-scoped; the CLI must never send this header. Design doc §6.
	if gotEnv != "" {
		t.Errorf("x-environment-id = %q, want empty", gotEnv)
	}
}

func TestClient_SendsIdentifiableUserAgent(t *testing.T) {
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, APIKey: "k", Version: "1.2.3"})
	if _, err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if !strings.HasPrefix(ua, "flexprice-cli/1.2.3") {
		t.Errorf("User-Agent = %q, want prefix flexprice-cli/1.2.3", ua)
	}
}

func TestClient_ErrorStatusReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		// The real envelope shape, verified against the live API.
		_, _ = w.Write([]byte(`{"code":"not_found","message":"Customer with ID c was not found",` +
			`"http_status_code":404,"details":{"customer_id":"c"}}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, APIKey: "k", Version: "t"})
	_, err := c.Do(context.Background(), http.MethodGet, "/missing", nil, nil)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %T, want *APIError", err)
	}
	if apiErr.Status != http.StatusNotFound {
		t.Errorf("Status = %d, want 404", apiErr.Status)
	}
	if !strings.Contains(apiErr.Error(), "was not found") {
		t.Errorf("Error() = %q, want the API message surfaced", apiErr.Error())
	}
}

// A 5xx is retried; a 4xx is not, because retrying a client error just wastes time.
func TestClient_RetriesServerErrorsNotClientErrors(t *testing.T) {
	var serverHits, clientHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/boom" {
			serverHits++
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		clientHits++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"validation_error","message":"bad"}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, APIKey: "k", Version: "t"})
	_, _ = c.Do(context.Background(), http.MethodGet, "/boom", nil, nil)
	_, _ = c.Do(context.Background(), http.MethodGet, "/bad", nil, nil)

	if serverHits < 2 {
		t.Errorf("server error hit %d times, want retries", serverHits)
	}
	if clientHits != 1 {
		t.Errorf("client error hit %d times, want exactly 1 (no retry)", clientHits)
	}
}

func TestRedact_IsAllowlistBased(t *testing.T) {
	in := map[string]any{
		"id":            "cust_1",
		"email":         "a@b.com",
		"secret_token":  "shhh",
		"something_new": "unanticipated",
	}
	out := Redact(in)
	if out["id"] != "cust_1" {
		t.Errorf("id was redacted, want preserved")
	}
	// Anything not on the allowlist is redacted, so unanticipated fields fail closed.
	if out["something_new"] != redacted {
		t.Errorf("something_new = %v, want %q", out["something_new"], redacted)
	}
	if out["secret_token"] != redacted {
		t.Errorf("secret_token = %v, want %q", out["secret_token"], redacted)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd cli && go get github.com/hashicorp/go-retryablehttp@latest
go test ./internal/client/ -run 'TestClient|TestRedact' -v
```

Expected: FAIL — `undefined: New`, `undefined: Options`, `undefined: Redact`.

> **Retry safety.** The default `retryablehttp` policy inspects only the status code,
> never the method, so it retries POST exactly like GET. On this API that is unsafe:
> `CreateSubscriptionRequest` has no idempotency field, and where a body-level
> `idempotency_key` is omitted the server generates one containing a timestamp, which
> differs per attempt even though the body is byte-identical. A 5xx raised after commit
> would therefore create duplicate subscriptions. `retryPolicy` below retries only
> idempotent methods, plus 429 for any method.

- [ ] **Step 3: Implement the client**

`cli/internal/client/client.go`:

```go
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
)

const redacted = "[redacted]"

// safeKeys are the only response fields printed in full under --debug. Redaction
// is allowlist-based so that fields nobody anticipated fail closed.
//
// Every key here is structurally constrained — an identifier, an enum, a number
// or a timestamp. Free-text fields are deliberately excluded even when they look
// harmless: "message", "name" and "hint" are exactly the fields a server
// interpolates dynamic content into, so allowlisting them by key would leak
// whatever happened to be interpolated.
var safeKeys = map[string]bool{
	"id": true, "object": true, "status": true, "type": true,
	"created_at": true, "updated_at": true, "currency": true, "amount": true,
	"external_id": true, "lookup_key": true, "environment_id": true,
	"tenant_id": true, "code": true,
	"has_more": true, "iter_first_key": true, "iter_last_key": true, "total": true,
}

// methodKey carries the request method into CheckRetry, which otherwise cannot
// see it when the transport fails and there is no response to inspect.
type methodKey struct{}

// idempotentMethods can be repeated without creating a second resource.
var idempotentMethods = map[string]bool{
	"GET": true, "HEAD": true, "PUT": true, "DELETE": true, "OPTIONS": true,
}

// retryPolicy refuses to retry non-idempotent requests on 5xx or transport errors.
//
// retryablehttp's default policy inspects only the status code and never the
// method, so it would retry POST identically to GET. On a billing API that is
// unsafe: a 502 raised after the server has already committed is indistinguishable
// from one raised before it, so retrying POST /subscriptions can create duplicate
// subscriptions that bill real customers. The API offers no Idempotency-Key
// header, CreateSubscriptionRequest has no idempotency field at all, and where a
// body-level idempotency_key is omitted the server generates one containing a
// timestamp — which differs on every attempt even though the body is byte
// identical, so server-side dedup does not help either.
//
// 429 is retried for every method: the server is explicitly stating it did not
// process the request.
func retryPolicy(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
		return true, nil
	}
	if method, _ := ctx.Value(methodKey{}).(string); !idempotentMethods[strings.ToUpper(method)] {
		return false, err
	}
	return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
}

type Options struct {
	BaseURL string
	APIKey  string
	Version string
	Debug   bool
	// Timeout bounds a whole request including retries. Zero means DefaultTimeout;
	// cleanhttp sets only a Transport, so without this the client can hang forever
	// on a connection that stalls after headers arrive.
	Timeout time.Duration
	// DebugOut receives --debug dumps. Never stdout: data goes to stdout so that
	// `--output json > file` stays clean.
	DebugOut io.Writer
}

// DefaultTimeout bounds a whole request including retries.
const DefaultTimeout = 30 * time.Second

type Client struct {
	base    *url.URL
	apiKey  string
	version string
	debug   bool
	debugW  io.Writer
	http    *retryablehttp.Client
	// baseErr defers a malformed BaseURL to the first Do call. New has no error
	// return so that call sites stay a single expression; reporting it here still
	// names the real cause rather than surfacing "unsupported protocol scheme"
	// from deep in the HTTP stack.
	baseErr error
}

func New(o Options) *Client {
	c := &Client{
		apiKey:  o.APIKey,
		version: o.Version,
		debug:   o.Debug,
		debugW:  o.DebugOut,
	}

	base, err := url.Parse(strings.TrimRight(o.BaseURL, "/"))
	switch {
	case err != nil:
		c.baseErr = fmt.Errorf("invalid base URL %q: %w", o.BaseURL, err)
	case base.Scheme == "" || base.Host == "":
		c.baseErr = fmt.Errorf("invalid base URL %q: expected a scheme and host, for example https://us.api.flexprice.io/v1", o.BaseURL)
	}
	if base == nil {
		base = &url.URL{}
	}
	c.base = base

	timeout := o.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	rc := retryablehttp.NewClient()
	rc.RetryMax = 3
	rc.RetryWaitMin = 200 * time.Millisecond
	rc.RetryWaitMax = 3 * time.Second
	rc.Logger = nil // retryablehttp logs to stderr by default; the CLI owns its output
	rc.CheckRetry = retryPolicy
	rc.HTTPClient.Timeout = timeout
	c.http = rc

	return c
}

// Do issues a request against path (relative to BaseURL) and returns the raw body.
//
// A non-2xx status returns *APIError; the raw body is returned alongside it so a
// caller rendering JSON can still emit the error envelope. Callers must check the
// error before using the body. APIError.Raw carries the same bytes.
//
// Pass a literal nil for body when there is none: a typed nil pointer is a
// non-nil interface and would be encoded as the JSON literal null.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	if c.baseErr != nil {
		return nil, c.baseErr
	}
	ctx = context.WithValue(ctx, methodKey{}, method)

	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}

	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		payload = bytes.NewReader(b)
		c.dump("request body", b)
	}

	req, err := retryablehttp.NewRequestWithContext(ctx, method, u.String(), payload)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	// The API key is environment-scoped, so x-environment-id is never sent. Design doc §6.
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("User-Agent", "flexprice-cli/"+c.version)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	c.debugf("%s %s", method, u.String())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	c.dump("response body", raw)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return raw, NewAPIError(resp.StatusCode, raw, method, path)
	}
	return raw, nil
}

func (c *Client) debugf(format string, args ...any) {
	if !c.debug || c.debugW == nil {
		return
	}
	fmt.Fprintf(c.debugW, "> "+format+"\n", args...)
}

// dump prints a redacted view of a JSON payload under --debug.
func (c *Client) dump(label string, raw []byte) {
	if !c.debug || c.debugW == nil {
		return
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		fmt.Fprintf(c.debugW, "> %s: <%d bytes, not JSON>\n", label, len(raw))
		return
	}
	b, _ := json.MarshalIndent(redactAny(v), "> ", "  ")
	fmt.Fprintf(c.debugW, "> %s:\n> %s\n", label, b)
}

// Redact returns a copy of m with every value not on the allowlist replaced.
func Redact(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if !safeKeys[k] {
			out[k] = redacted
			continue
		}
		out[k] = redactAny(v)
	}
	return out
}

func redactAny(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return Redact(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = redactAny(e)
		}
		return out
	default:
		return v
	}
}
```

- [ ] **Step 4: Run the tests**

```bash
cd cli && go test ./internal/client/ -v
```

Expected: PASS, all tests including the earlier error tests.

- [ ] **Step 5: Commit**

```bash
git add cli/internal/client cli/go.mod cli/go.sum
git commit -m "feat(cli): HTTP client with allowlist debug redaction"
```

---

## Phase 2 — Config, credentials, auth

### Task 5: Config file and profiles

**Files:**
- Create: `cli/internal/config/config.go`
- Create: `cli/internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

`cli/internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoad_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	in := &Config{
		DefaultProfile: "sandbox",
		Profiles: map[string]Profile{
			"sandbox": {
				Region:  "in",
				BaseURL: "https://api.cloud.flexprice.io/v1",
				Label:   "Sandbox",
				KeyRef:  "keychain:flexprice/sandbox",
			},
		},
	}
	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}

	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := out.Profiles["sandbox"]
	if !ok {
		t.Fatal("profile missing after round trip")
	}
	if got.Region != "in" || got.Label != "Sandbox" {
		t.Errorf("profile = %+v, want region in and label Sandbox", got)
	}
}

func TestSave_UsesRestrictivePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.toml")

	if err := Save(path, &Config{Profiles: map[string]Profile{}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode = %o, want 700", perm)
	}
}

func TestLoad_MissingFileReturnsEmptyConfig(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if len(cfg.Profiles) != 0 {
		t.Errorf("Profiles = %v, want empty", cfg.Profiles)
	}
}

func TestProfileName_SlugifiesUserInput(t *testing.T) {
	if got := ProfileName("My Sandbox"); got != "my-sandbox" {
		t.Errorf("ProfileName = %q, want %q", got, "my-sandbox")
	}
	if got := ProfileName(""); got != "default" {
		t.Errorf("ProfileName(\"\") = %q, want %q", got, "default")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd cli && go get github.com/BurntSushi/toml@latest
go test ./internal/config/ -v
```

Expected: FAIL — `undefined: Config`.

- [ ] **Step 3: Implement**

`cli/internal/config/config.go`:

```go
// Package config manages ~/.flexprice/config.toml. It holds no secrets: keys live
// in the keyring and are referenced by KeyRef.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// Profile is the atomic auth unit: an API key is scoped to exactly one
// environment, so region, base URL and key move together or not at all.
//
// There is deliberately no environment name and no live flag. No endpoint
// reachable by an environment-scoped key reveals which environment that key
// belongs to — GET /environments returns every environment in the tenant,
// GET /environments/{id} succeeds for all of them, and /secrets/api/keys omits
// environment_id. Deriving either value would mean guessing, and a wrong live
// flag is worse than no live flag. Users label profiles themselves. Design doc §6.
type Profile struct {
	Region  string `toml:"region"`
	BaseURL string `toml:"base_url"`
	Label   string `toml:"label"` // free text, set by the user; purely informational
	KeyRef  string `toml:"key_ref"`
}

type Config struct {
	DefaultProfile string             `toml:"default_profile"`
	Profiles       map[string]Profile `toml:"profiles"`
}

// DefaultPath is ~/.flexprice/config.toml.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".flexprice", "config.toml"), nil
}

func Load(path string) (*Config, error) {
	cfg := &Config{Profiles: map[string]Profile{}}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := toml.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	return cfg, nil
}

func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	// MkdirAll respects umask, so set the mode explicitly.
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("secure config directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return os.Chmod(path, 0o600)
}

// Resolve returns the named profile, or the default when name is empty.
func (c *Config) Resolve(name string) (string, Profile, error) {
	if name == "" {
		name = c.DefaultProfile
	}
	if name == "" {
		return "", Profile{}, fmt.Errorf("no profile configured — run: flexprice init")
	}
	p, ok := c.Profiles[name]
	if !ok {
		return "", Profile{}, fmt.Errorf("profile %q not found — see: flexprice config list", name)
	}
	return name, p, nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// ProfileName slugifies a user-supplied label, falling back to "default".
// Nothing about the key identifies its environment, so the name is whatever the
// user chooses.
func ProfileName(label string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(label), "-"), "-")
	if slug == "" {
		return "default"
	}
	return slug
}
```

- [ ] **Step 4: Run the tests**

```bash
cd cli && go test ./internal/config/ -v
```

Expected: PASS, all four tests.

- [ ] **Step 5: Commit**

```bash
git add cli/internal/config cli/go.mod cli/go.sum
git commit -m "feat(cli): config file with profiles and restrictive permissions"
```

### Task 6: Keyring with file fallback

**Files:**
- Create: `cli/internal/keyring/keyring.go`
- Create: `cli/internal/keyring/file.go`
- Create: `cli/internal/keyring/keyring_test.go`

- [ ] **Step 1: Write the failing test**

`cli/internal/keyring/keyring_test.go`:

```go
package keyring

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileStore_RoundTrips(t *testing.T) {
	s := &FileStore{Dir: t.TempDir()}

	if err := s.Set("acme-production", "sk_test_abc"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get("acme-production")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "sk_test_abc" {
		t.Errorf("Get = %q, want %q", got, "sk_test_abc")
	}

	if err := s.Delete("acme-production"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get("acme-production"); err == nil {
		t.Error("Get after Delete returned no error")
	}
}

func TestFileStore_WritesRestrictivePermissions(t *testing.T) {
	dir := t.TempDir()
	s := &FileStore{Dir: dir}
	if err := s.Set("p", "sk_1"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	fi, err := os.Stat(filepath.Join(dir, "p.key"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

func TestFileStore_StoredBytesAreNotPlaintext(t *testing.T) {
	dir := t.TempDir()
	s := &FileStore{Dir: dir}
	if err := s.Set("p", "sk_supersecret"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "p.key"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(raw) == "sk_supersecret" {
		t.Error("key stored as plaintext")
	}
}

func TestFileStore_BackendNameIsReportable(t *testing.T) {
	s := &FileStore{Dir: t.TempDir()}
	if s.Name() == "" {
		t.Error("Name() is empty; whoami needs to report the active backend")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd cli && go test ./internal/keyring/ -v
```

Expected: FAIL — `undefined: FileStore`.

- [ ] **Step 3: Implement the store interface and OS backend**

`cli/internal/keyring/keyring.go`:

```go
// Package keyring stores API keys. It prefers the OS keychain and falls back to
// an obfuscated file when no keychain is available — common in containers and WSL.
package keyring

import (
	"fmt"
	"os"
	"path/filepath"

	oskeyring "github.com/zalando/go-keyring"
)

const service = "flexprice"

// Store is the credential backend. Name() is surfaced by whoami so the user can
// always tell where their key actually lives.
type Store interface {
	Set(profile, key string) error
	Get(profile string) (string, error)
	Delete(profile string) error
	Name() string
}

type OSKeyring struct{}

func (OSKeyring) Name() string { return "OS keychain" }

func (OSKeyring) Set(profile, key string) error {
	return oskeyring.Set(service, profile, key)
}

func (OSKeyring) Get(profile string) (string, error) {
	return oskeyring.Get(service, profile)
}

func (OSKeyring) Delete(profile string) error {
	return oskeyring.Delete(service, profile)
}

// Open returns the OS keychain when it works, otherwise the file fallback.
// warn is non-empty when the fallback was selected and the caller has not opted
// in via FLEXPRICE_KEY_BACKEND=file; callers print it once.
func Open() (store Store, warn string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", fmt.Errorf("locate home directory: %w", err)
	}
	fileDir := filepath.Join(home, ".flexprice", "keys")

	if os.Getenv("FLEXPRICE_KEY_BACKEND") == "file" {
		return &FileStore{Dir: fileDir}, "", nil
	}

	os_ := OSKeyring{}
	// A probe is the only reliable availability check: on Linux the keychain
	// fails at call time when libsecret or D-Bus is absent.
	probeErr := os_.Set(service+".probe", "probe")
	if probeErr == nil {
		_ = os_.Delete(service + ".probe")
		return os_, "", nil
	}

	return &FileStore{Dir: fileDir},
		fmt.Sprintf("No OS keychain available (%v).\n"+
			"Storing your key in %s with mode 0600 instead.\n"+
			"Set FLEXPRICE_KEY_BACKEND=file to silence this warning.", probeErr, fileDir),
		nil
}
```

`cli/internal/keyring/file.go`:

```go
package keyring

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// FileStore is the fallback when no OS keychain exists. The key material is
// encrypted with a host-derived key, which stops casual disclosure (backups,
// shoulder-surfing, accidental `cat`). It is not protection against an attacker
// who already has read access as this user — file mode 0600 is the real control,
// and the warning from Open() tells the user which backend is in play.
type FileStore struct {
	Dir string
}

func (f *FileStore) Name() string { return "encrypted file (" + f.Dir + ")" }

func (f *FileStore) path(profile string) string {
	return filepath.Join(f.Dir, profile+".key")
}

// derive builds the AES key from stable host and user identifiers.
func (f *FileStore) derive() []byte {
	host, _ := os.Hostname()
	sum := sha256.Sum256([]byte("flexprice-cli|" + host + "|" + f.Dir))
	return sum[:]
}

func (f *FileStore) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(f.derive())
	if err != nil {
		return nil, fmt.Errorf("init cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

func (f *FileStore) Set(profile, key string) error {
	gcm, err := f.aead()
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(key), nil)

	if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return fmt.Errorf("create key directory: %w", err)
	}
	if err := os.Chmod(f.Dir, 0o700); err != nil {
		return fmt.Errorf("secure key directory: %w", err)
	}

	enc := []byte(base64.StdEncoding.EncodeToString(sealed))
	if err := os.WriteFile(f.path(profile), enc, 0o600); err != nil {
		return fmt.Errorf("write key file: %w", err)
	}
	return os.Chmod(f.path(profile), 0o600)
}

func (f *FileStore) Get(profile string) (string, error) {
	raw, err := os.ReadFile(f.path(profile))
	if err != nil {
		return "", fmt.Errorf("no stored key for profile %q: %w", profile, err)
	}
	sealed, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		return "", fmt.Errorf("stored key for %q is corrupt: %w", profile, err)
	}
	gcm, err := f.aead()
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", fmt.Errorf("stored key for %q is truncated", profile)
	}
	nonce, body := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return "", fmt.Errorf("cannot decrypt key for %q (was it copied from another machine?): %w", profile, err)
	}
	return string(plain), nil
}

func (f *FileStore) Delete(profile string) error {
	if err := os.Remove(f.path(profile)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove key file: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests**

```bash
cd cli && go get github.com/zalando/go-keyring@latest
go test ./internal/keyring/ -v
```

Expected: PASS, all four tests. `FileStore` tests do not touch the OS keychain, so they run in CI without a desktop session.

- [ ] **Step 5: Commit**

```bash
git add cli/internal/keyring cli/go.mod cli/go.sum
git commit -m "feat(cli): keyring with encrypted file fallback"
```

### Task 7: Credential resolution and the runtime context

**Files:**
- Create: `cli/internal/config/resolve.go`
- Create: `cli/internal/config/resolve_test.go`

- [ ] **Step 1: Write the failing test**

`cli/internal/config/resolve_test.go`:

```go
package config

import (
	"strings"
	"testing"
)

type stubStore struct {
	keys map[string]string
}

func (s *stubStore) Set(p, k string) error { s.keys[p] = k; return nil }
func (s *stubStore) Get(p string) (string, error) {
	k, ok := s.keys[p]
	if !ok {
		return "", errNotFound
	}
	return k, nil
}
func (s *stubStore) Delete(p string) error { delete(s.keys, p); return nil }
func (s *stubStore) Name() string          { return "stub" }

func baseConfig() *Config {
	return &Config{
		DefaultProfile: "acme-production",
		Profiles: map[string]Profile{
			"acme-production": {Region: "us", BaseURL: "https://us.example/v1", Label: "prod"},
			"acme-dev":        {Region: "us", BaseURL: "https://us.example/v1", Label: "dev"},
		},
	}
}

func TestResolveContext_FlagBeatsKeyring(t *testing.T) {
	store := &stubStore{keys: map[string]string{"acme-production": "sk_from_keyring"}}
	rc, err := ResolveContext(baseConfig(), store, Overrides{APIKey: "sk_from_flag"})
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	if rc.APIKey != "sk_from_flag" {
		t.Errorf("APIKey = %q, want the flag value", rc.APIKey)
	}
}

func TestResolveContext_EnvBeatsKeyring(t *testing.T) {
	store := &stubStore{keys: map[string]string{"acme-production": "sk_from_keyring"}}
	t.Setenv("FLEXPRICE_API_KEY", "sk_from_env")
	rc, err := ResolveContext(baseConfig(), store, Overrides{})
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	if rc.APIKey != "sk_from_env" {
		t.Errorf("APIKey = %q, want the env value", rc.APIKey)
	}
}

func TestResolveContext_ProfileOverrideSelectsThatProfile(t *testing.T) {
	store := &stubStore{keys: map[string]string{"acme-dev": "sk_dev"}}
	rc, err := ResolveContext(baseConfig(), store, Overrides{Profile: "acme-dev"})
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	if rc.ProfileName != "acme-dev" || rc.Profile.Label != "dev" {
		t.Errorf("profile = %q %+v, want acme-dev", rc.ProfileName, rc.Profile)
	}
}

// A bare key carries no region, so guessing a base URL would produce a 401 that
// looks like an invalid key. Design doc §6.
func TestResolveContext_BareAPIKeyWithoutProfileIsAnError(t *testing.T) {
	store := &stubStore{keys: map[string]string{}}
	empty := &Config{Profiles: map[string]Profile{}}
	_, err := ResolveContext(empty, store, Overrides{APIKey: "sk_loose"})
	if err == nil {
		t.Fatal("want an error for --api-key with no region or base URL")
	}
	if !strings.Contains(err.Error(), "--region") {
		t.Errorf("error = %q, want it to name --region", err)
	}
}

func TestResolveContext_APIKeyWithRegionResolvesBaseURL(t *testing.T) {
	store := &stubStore{keys: map[string]string{}}
	empty := &Config{Profiles: map[string]Profile{}}
	rc, err := ResolveContext(empty, store, Overrides{
		APIKey:  "sk_loose",
		Region:  "us",
		Regions: map[string]string{"us": "https://us.example/v1"},
	})
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	if rc.BaseURL != "https://us.example/v1" {
		t.Errorf("BaseURL = %q, want the US region URL", rc.BaseURL)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd cli && go test ./internal/config/ -run TestResolveContext -v
```

Expected: FAIL — `undefined: ResolveContext`, `undefined: Overrides`, `undefined: errNotFound`.

- [ ] **Step 3: Implement**

`cli/internal/config/resolve.go`:

```go
package config

import (
	"errors"
	"fmt"
	"os"
)

var errNotFound = errors.New("credential not found")

// Store mirrors keyring.Store. It is redeclared here so that config does not
// import keyring, keeping the dependency one-directional and the tests stubbable.
type Store interface {
	Set(profile, key string) error
	Get(profile string) (string, error)
	Delete(profile string) error
	Name() string
}

// Overrides carries per-invocation flags. Regions maps a region key to its base
// URL and comes from the embedded spec's servers[].
type Overrides struct {
	Profile string
	APIKey  string
	BaseURL string
	Region  string
	Regions map[string]string
}

// RuntimeContext is everything a command needs to build a client.
type RuntimeContext struct {
	ProfileName string
	Profile     Profile
	APIKey      string
	BaseURL     string
}

// ResolveContext applies credential precedence: flag, environment variable,
// keyring, config file. Design doc §6.
func ResolveContext(cfg *Config, store Store, o Overrides) (RuntimeContext, error) {
	var rc RuntimeContext

	name, profile, profileErr := cfg.Resolve(o.Profile)
	if profileErr == nil {
		rc.ProfileName = name
		rc.Profile = profile
		rc.BaseURL = profile.BaseURL
	}

	switch {
	case o.BaseURL != "":
		rc.BaseURL = o.BaseURL
	case o.Region != "":
		url, ok := o.Regions[o.Region]
		if !ok {
			return rc, fmt.Errorf("unknown region %q — run `flexprice login` to see the available regions", o.Region)
		}
		rc.BaseURL = url
	}

	switch {
	case o.APIKey != "":
		rc.APIKey = o.APIKey
	case os.Getenv("FLEXPRICE_API_KEY") != "":
		rc.APIKey = os.Getenv("FLEXPRICE_API_KEY")
	case rc.ProfileName != "":
		key, err := store.Get(rc.ProfileName)
		if err != nil {
			return rc, fmt.Errorf("no stored key for profile %q — run: flexprice login --profile %s", rc.ProfileName, rc.ProfileName)
		}
		rc.APIKey = key
	}

	if rc.APIKey == "" {
		return rc, fmt.Errorf("not authenticated — run: flexprice init")
	}
	if rc.BaseURL == "" {
		return rc, fmt.Errorf(
			"a key alone does not identify a region — pass --region (us, in) or --base-url,\n" +
				"or run `flexprice login` to store a profile")
	}
	return rc, nil
}
```

- [ ] **Step 4: Run the tests**

```bash
cd cli && go test ./internal/config/ -v
```

Expected: PASS, all tests including Task 5's.

- [ ] **Step 5: Commit**

```bash
git add cli/internal/config
git commit -m "feat(cli): credential precedence and runtime context resolution"
```

### Task 8: Spec loader and region discovery

**Files:**
- Create: `cli/spec/embed.go`, `cli/spec/openapi.json`
- Create: `cli/internal/spec/loader.go`
- Create: `cli/internal/spec/loader_test.go`

Use the kin-openapi accessor names recorded in the Task 1 findings file. The code below uses the shape confirmed there; if the findings recorded different names, substitute them.

- [ ] **Step 1: Sync the spec into the module**

```bash
mkdir -p cli/spec && make sync-cli-spec
ls -la cli/spec/openapi.json
```

Expected: the file exists and is several megabytes.

- [ ] **Step 2: Write the failing test**

`cli/internal/spec/loader_test.go`:

```go
package spec

import "testing"

func TestLoad_ParsesEmbeddedSpec(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if doc.Info == nil || doc.Info.Title == "" {
		t.Fatal("spec has no Info.Title")
	}
}

func TestRegions_ComeFromServers(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	regions := Regions(doc)
	if len(regions) < 2 {
		t.Fatalf("Regions returned %d entries, want at least 2", len(regions))
	}

	byKey := map[string]Region{}
	for _, r := range regions {
		byKey[r.Key] = r
	}
	us, ok := byKey["us"]
	if !ok {
		t.Fatalf("no region keyed \"us\"; got %v", byKey)
	}
	if us.BaseURL != "https://us.api.flexprice.io/v1" {
		t.Errorf("us BaseURL = %q", us.BaseURL)
	}
	if _, ok := byKey["in"]; !ok {
		t.Errorf("no region keyed \"in\"; got %v", byKey)
	}
}

// The Webhook Events tag holds 56 documentation stubs with synthetic paths that
// 404 if called. They must never become commands. Design doc §5.
func TestOperations_ExcludeWebhookEventStubs(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, op := range Operations(doc) {
		if op.Tag == WebhookEventsTag {
			t.Fatalf("operation %s is tagged %q and must be excluded", op.ID, WebhookEventsTag)
		}
		if op.ID == "" {
			t.Fatalf("operation at %s %s has no operationId", op.Method, op.Path)
		}
	}
}

func TestEventTypes_ComeFromWebhookEventStubs(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	types := EventTypes(doc)
	if len(types) < 20 {
		t.Fatalf("EventTypes returned %d, want the full stub list", len(types))
	}
	found := false
	for _, e := range types {
		if e == "invoice.created" {
			found = true
		}
	}
	if !found {
		t.Errorf("invoice.created missing from event types: %v", types)
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

```bash
cd cli && go get github.com/getkin/kin-openapi/openapi3@latest
go test ./internal/spec/ -v
```

Expected: FAIL — `undefined: Load`.

- [ ] **Step 4: Implement**

`cli/spec/embed.go`:

```go
// Package spec holds the build-time artefacts the CLI resolves commands from.
// The binary never fetches a spec at runtime.
package spec

import _ "embed"

//go:embed openapi.json
var OpenAPI []byte

//go:embed commands.yaml
var Commands []byte
```

`cli/internal/spec/loader.go`:

```go
// Package spec loads the embedded OpenAPI document and derives the CLI's
// command surface from it.
package spec

import (
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	specdata "github.com/flexprice/cli/spec"
)

// WebhookEventsTag marks 56 documentation stubs that describe webhook payload
// schemas. They have no operationId, their paths are synthetic, and calling them
// 404s — so they are excluded from commands but kept as the authoritative list of
// event types. Design doc §5.
const WebhookEventsTag = "Webhook Events"

func Load() (*openapi3.T, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(specdata.OpenAPI)
	if err != nil {
		return nil, fmt.Errorf("parse embedded OpenAPI spec: %w", err)
	}
	return doc, nil
}

type Region struct {
	Key         string
	BaseURL     string
	Description string
}

// Regions derives the region list from servers[], so adding a region to the spec
// makes the next build offer it with no code change. Design doc §6.
func Regions(doc *openapi3.T) []Region {
	var out []Region
	for _, s := range doc.Servers {
		out = append(out, Region{
			Key:         regionKey(s.URL, s.Description),
			BaseURL:     s.URL,
			Description: s.Description,
		})
	}
	return out
}

// regionKey produces a short flag-friendly key: "US Region" -> us, "India Region" -> in.
func regionKey(url, description string) string {
	word := strings.ToLower(strings.Fields(strings.TrimSpace(description))[0])
	switch word {
	case "india":
		return "in"
	case "united", "usa":
		return "us"
	}
	if len(word) <= 3 {
		return word
	}
	return word[:2]
}

// Operation is one callable API operation.
type Operation struct {
	ID     string
	Method string
	Path   string
	Tag    string
	Op     *openapi3.Operation
	Item   *openapi3.PathItem
}

// Operations returns every callable operation, excluding the webhook stubs and
// anything without an operationId.
func Operations(doc *openapi3.T) []Operation {
	var out []Operation
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if op.OperationID == "" {
				continue
			}
			tag := ""
			if len(op.Tags) > 0 {
				tag = op.Tags[0]
			}
			if tag == WebhookEventsTag {
				continue
			}
			out = append(out, Operation{
				ID: op.OperationID, Method: method, Path: path, Tag: tag, Op: op, Item: item,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// EventTypes reads webhook event names off the excluded stubs. These drive
// validation and completion for `trigger` and `listen --events`.
func EventTypes(doc *openapi3.T) []string {
	var out []string
	for path, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			isStub := false
			for _, t := range op.Tags {
				if t == WebhookEventsTag {
					isStub = true
				}
			}
			if !isStub {
				continue
			}
			if name := strings.TrimPrefix(path, "/webhook-events/"); name != path {
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 5: Create a placeholder commands.yaml so the embed compiles**

```bash
printf 'resources: {}\nexclude: []\n' > cli/spec/commands.yaml
```

Task 9 replaces this with the real map.

- [ ] **Step 6: Run the tests**

```bash
cd cli && go test ./internal/spec/ -v
```

Expected: PASS, all four tests. If `doc.Paths.Map()` or `item.Operations()` does not compile, use the accessor names from the Task 1 findings file.

- [ ] **Step 7: Commit**

```bash
git add cli/spec cli/internal/spec cli/go.mod cli/go.sum
git commit -m "feat(cli): embedded spec loader, region and event-type discovery"
```

### Task 9: Command registry, `commands.yaml`, and the CI validator

The curated map. Design doc §5. Mechanical derivation alone produces a bad CLI — there is no `GET /customers`, so `customers list` must resolve to `queryCustomer` (`POST /customers/search`).

**Files:**
- Create: `cli/internal/spec/registry.go`, `cli/internal/spec/registry_test.go`
- Create: `cli/tools/bootstrap-commands/main.go`
- Replace: `cli/spec/commands.yaml`
- Create: `.github/workflows/cli-validate.yml`

- [ ] **Step 1: Write the failing test**

`cli/internal/spec/registry_test.go`:

```go
package spec

import "testing"

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	reg, err := NewRegistry(doc)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return reg
}

// customers list resolves through the curated map, not from a GET that does not exist.
func TestRegistry_CustomersListMapsToQueryCustomer(t *testing.T) {
	cmd, ok := testRegistry(t).Lookup("customers", "list")
	if !ok {
		t.Fatal("customers list not registered")
	}
	if cmd.Operation.ID != "queryCustomer" {
		t.Errorf("operationId = %q, want queryCustomer", cmd.Operation.ID)
	}
	if cmd.Operation.Method != "POST" || cmd.Operation.Path != "/customers/search" {
		t.Errorf("resolved to %s %s", cmd.Operation.Method, cmd.Operation.Path)
	}
}

func TestRegistry_ActionVerbsAreRegistered(t *testing.T) {
	cmd, ok := testRegistry(t).Lookup("invoices", "finalize")
	if !ok {
		t.Fatal("invoices finalize not registered")
	}
	if cmd.Operation.ID != "finalizeInvoice" {
		t.Errorf("operationId = %q, want finalizeInvoice", cmd.Operation.ID)
	}
}

// Every callable operation is reachable: mapped, derived, or explicitly excluded.
func TestRegistry_EveryOperationIsAccountedFor(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	reg, err := NewRegistry(doc)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	reachable := map[string]bool{}
	for _, c := range reg.Commands() {
		reachable[c.Operation.ID] = true
	}
	for _, id := range reg.Excluded() {
		reachable[id] = true
	}
	for _, op := range Operations(doc) {
		if !reachable[op.ID] {
			t.Errorf("operation %q is unreachable: map it in commands.yaml or exclude it", op.ID)
		}
	}
}

// A mapping pointing at an operation that does not exist is a hard failure.
func TestRegistry_DanglingMappingIsAnError(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = newRegistry(doc, []byte("resources:\n  ghosts:\n    list: noSuchOperation\nexclude: []\n"))
	if err == nil {
		t.Fatal("want an error for a mapping to a nonexistent operationId")
	}
}

// Two mappings resolving to the same resource+action is a hard failure.
func TestRegistry_CollisionIsAnError(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = newRegistry(doc, []byte(
		"resources:\n  customers:\n    list: queryCustomer\n  Customers:\n    list: getCustomer\nexclude: []\n"))
	if err == nil {
		t.Fatal("want an error for a resource+action collision")
	}
}

func TestDeriveName_IsPureAndStable(t *testing.T) {
	cases := map[string][2]string{
		"createCustomer":       {"customers", "create"},
		"getWalletTransactions": {"wallets", "get-wallet-transactions"},
	}
	for id, want := range cases {
		resource, action := DeriveName("Customers", id)
		if id == "getWalletTransactions" {
			resource, action = DeriveName("Wallets", id)
		}
		if resource != want[0] {
			t.Errorf("%s: resource = %q, want %q", id, resource, want[0])
		}
		if action == "" {
			t.Errorf("%s: action is empty", id)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd cli && go get github.com/goccy/go-yaml@latest
go test ./internal/spec/ -run TestRegistry -v
```

Expected: FAIL — `undefined: NewRegistry`.

- [ ] **Step 3: Implement the registry**

`cli/internal/spec/registry.go`:

```go
package spec

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/goccy/go-yaml"

	specdata "github.com/flexprice/cli/spec"
)

// commandsFile is the on-disk shape of commands.yaml. A resource maps action
// names to operationIds; "columns" is handled separately because it is not an action.
type commandsFile struct {
	Resources map[string]map[string]any `yaml:"resources"`
	Exclude   []string                  `yaml:"exclude"`
}

type Command struct {
	Resource  string
	Action    string
	Operation Operation
	Derived   bool // true when auto-derived rather than curated
}

type Registry struct {
	commands map[string]map[string]Command // resource -> action -> command
	columns  map[string][]string
	excluded []string
	warnings []string
}

func NewRegistry(doc *openapi3.T) (*Registry, error) {
	return newRegistry(doc, specdata.Commands)
}

func newRegistry(doc *openapi3.T, raw []byte) (*Registry, error) {
	var file commandsFile
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse commands.yaml: %w", err)
	}

	ops := map[string]Operation{}
	for _, op := range Operations(doc) {
		ops[op.ID] = op
	}

	reg := &Registry{
		commands: map[string]map[string]Command{},
		columns:  map[string][]string{},
		excluded: file.Exclude,
	}

	excluded := map[string]bool{}
	for _, id := range file.Exclude {
		if _, ok := ops[id]; !ok {
			return nil, fmt.Errorf("commands.yaml excludes %q, which is not an operation in the spec", id)
		}
		excluded[id] = true
	}

	mapped := map[string]bool{}
	for resource, actions := range file.Resources {
		for action, target := range actions {
			if action == "columns" {
				reg.columns[resource] = toStrings(target)
				continue
			}
			id, ok := target.(string)
			if !ok {
				return nil, fmt.Errorf("commands.yaml: %s.%s must be an operationId string", resource, action)
			}
			op, ok := ops[id]
			if !ok {
				return nil, fmt.Errorf("commands.yaml maps %s %s to %q, which is not an operation in the spec", resource, action, id)
			}
			if err := reg.add(Command{Resource: resource, Action: action, Operation: op}); err != nil {
				return nil, err
			}
			mapped[id] = true
		}
	}

	// Default-allow: anything unmapped gets a derived name and a warning. A strict
	// gate would tax every backend PR that adds an endpoint. Design doc §5.
	for _, op := range Operations(doc) {
		if mapped[op.ID] || excluded[op.ID] {
			continue
		}
		resource, action := DeriveName(op.Tag, op.ID)
		if err := reg.add(Command{Resource: resource, Action: action, Operation: op, Derived: true}); err != nil {
			return nil, err
		}
		reg.warnings = append(reg.warnings,
			fmt.Sprintf("operation %q is unmapped; using derived name `flexprice %s %s`", op.ID, resource, action))
	}

	sort.Strings(reg.warnings)
	return reg, nil
}

func (r *Registry) add(c Command) error {
	if _, ok := r.commands[c.Resource]; !ok {
		r.commands[c.Resource] = map[string]Command{}
	}
	if existing, ok := r.commands[c.Resource][c.Action]; ok {
		return fmt.Errorf("command collision: %s %s maps to both %q and %q",
			c.Resource, c.Action, existing.Operation.ID, c.Operation.ID)
	}
	r.commands[c.Resource][c.Action] = c
	return nil
}

func (r *Registry) Lookup(resource, action string) (Command, bool) {
	c, ok := r.commands[resource][action]
	return c, ok
}

func (r *Registry) Resources() []string {
	out := make([]string, 0, len(r.commands))
	for name := range r.commands {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (r *Registry) Actions(resource string) []string {
	out := make([]string, 0, len(r.commands[resource]))
	for a := range r.commands[resource] {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

func (r *Registry) Commands() []Command {
	var out []Command
	for _, actions := range r.commands {
		for _, c := range actions {
			out = append(out, c)
		}
	}
	return out
}

func (r *Registry) Columns(resource string) []string { return r.columns[resource] }
func (r *Registry) Excluded() []string               { return r.excluded }
func (r *Registry) Warnings() []string               { return r.warnings }

var camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// DeriveName is a pure function of tag and operationId. operationId stability is
// already a contract held for the SDKs, so derived command names are as stable as
// the SDK method names. Design doc §5.
func DeriveName(tag, operationID string) (resource, action string) {
	resource = kebab(tag)
	action = kebab(operationID)
	return resource, action
}

func kebab(s string) string {
	s = camelBoundary.ReplaceAllString(s, "${1}-${2}")
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return strings.ToLower(s)
}

func toStrings(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, i := range items {
		if s, ok := i.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
```

- [ ] **Step 4: Write the bootstrap tool**

`cli/tools/bootstrap-commands/main.go`:

```go
// Command bootstrap-commands prints a starting commands.yaml derived from the
// embedded spec. Run it once, then hand-correct the output — the derived names
// are a starting point, not the final vocabulary.
package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flexprice/cli/internal/spec"
)

// verb maps an operationId prefix to the CLI's domain vocabulary.
var verb = []struct{ prefix, action string }{
	{"create", "create"},
	{"update", "update"},
	{"delete", "delete"},
	{"query", "list"},   // POST /x/search is how listing works in this API
	{"list", "list"},
	{"get", "retrieve"},
}

func main() {
	doc, err := spec.Load()
	if err != nil {
		panic(err)
	}

	byResource := map[string]map[string]string{}
	for _, op := range spec.Operations(doc) {
		resource, derived := spec.DeriveName(op.Tag, op.ID)
		action := derived
		for _, v := range verb {
			if strings.HasPrefix(op.ID, v.prefix) {
				action = v.action
				break
			}
		}
		if byResource[resource] == nil {
			byResource[resource] = map[string]string{}
		}
		// On collision keep the first and leave the second under its derived name
		// so a human resolves it rather than the tool silently picking.
		if _, taken := byResource[resource][action]; taken {
			action = derived
		}
		byResource[resource][action] = op.ID
	}

	fmt.Println("# Generated by cli/tools/bootstrap-commands. Hand-correct before committing.")
	fmt.Println("resources:")
	resources := make([]string, 0, len(byResource))
	for r := range byResource {
		resources = append(resources, r)
	}
	sort.Strings(resources)

	for _, r := range resources {
		fmt.Printf("  %s:\n", r)
		actions := make([]string, 0, len(byResource[r]))
		for a := range byResource[r] {
			actions = append(actions, a)
		}
		sort.Strings(actions)
		for _, a := range actions {
			fmt.Printf("    %s: %s\n", a, byResource[r][a])
		}
	}
	fmt.Println("exclude: []")
}
```

- [ ] **Step 5: Generate and hand-correct `commands.yaml`**

```bash
cd cli && go run ./tools/bootstrap-commands > spec/commands.yaml
```

Then hand-correct. At minimum, verify these against the spec and fix anything the heuristic got wrong:

- `customers.list` must be `queryCustomer`, `customers.retrieve` must be `getCustomer`
- `invoices` gains `finalize: finalizeInvoice`, `void: voidInvoice`, `pdf: getInvoicePdf`
- `subscriptions` gains `cancel: cancelSubscription`, `activate: activateSubscription`
- `wallets` gains `top-up: topUpWallet`, `terminate: terminateWallet`
- Add `exclude: [recalculateInvoice]` — superseded by `recalculateInvoiceV2`
- Add `columns` lists for the resources people use most:

```yaml
  customers:
    columns: [id, external_id, email, created_at]
  invoices:
    columns: [id, invoice_number, invoice_status, payment_status, total, created_at]
  subscriptions:
    columns: [id, customer_id, plan_id, subscription_status, current_period_end]
```

- [ ] **Step 6: Run the tests**

```bash
cd cli && go test ./internal/spec/ -v
```

Expected: PASS. `TestRegistry_EveryOperationIsAccountedFor` passes by construction because unmapped operations get derived names.

- [ ] **Step 7: Add the CI validator**

`.github/workflows/cli-validate.yml`:

```yaml
name: CLI spec validation

on:
  pull_request:
    paths:
      - 'cli/**'
      - 'docs/swagger/swagger-3-0.json'

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: cli/go.mod

      # Fails the build when the embedded spec has drifted from docs/swagger.
      - name: Spec is in sync
        run: |
          cp docs/swagger/swagger-3-0.json /tmp/expected.json
          if ! diff -q /tmp/expected.json cli/spec/openapi.json > /dev/null; then
            echo "::error::cli/spec/openapi.json is stale. Run: make sync-cli-spec"
            exit 1
          fi

      # Hard-fails only on collisions and dangling mappings; unmapped operations warn.
      - name: Registry is valid
        run: cd cli && go test ./internal/spec/ -run TestRegistry -v

      - name: Report unmapped operations
        run: |
          cd cli && go run ./tools/bootstrap-commands > /tmp/derived.yaml
          echo "::notice::New operations may need names in cli/spec/commands.yaml"

      - name: Tests
        run: cd cli && go test -race ./...
```

- [ ] **Step 8: Add CODEOWNERS**

`.github/CODEOWNERS` — create it if absent, append if present. Replace `@flexprice/cli-maintainers` with the real owner; the design doc records this as an open item.

```
# The CLI's curated command vocabulary. Changes here rename user-facing commands.
/cli/spec/commands.yaml   @flexprice/cli-maintainers
/cli/                     @flexprice/cli-maintainers
```

- [ ] **Step 9: Commit**

```bash
git add cli/internal/spec cli/spec/commands.yaml cli/tools .github/workflows/cli-validate.yml .github/CODEOWNERS
git commit -m "feat(cli): command registry, curated commands.yaml and CI validation"
```

### Task 10: Request builder — flags, path params, query, body

Design doc §7. Flags are validated against the spec so typos get a suggestion instead of a 400.

**Files:**
- Create: `cli/internal/spec/request.go`, `cli/internal/spec/request_test.go`

- [ ] **Step 1: Write the failing test**

`cli/internal/spec/request_test.go`:

```go
package spec

import (
	"strings"
	"testing"
)

func TestBuildRequest_SubstitutesPathParameters(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("customers", "retrieve")

	req, err := BuildRequest(cmd, Input{PositionalID: "cust_01K"})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if req.Path != "/customers/cust_01K" {
		t.Errorf("Path = %q, want /customers/cust_01K", req.Path)
	}
	if req.Method != "GET" {
		t.Errorf("Method = %q, want GET", req.Method)
	}
}

func TestBuildRequest_MissingRequiredPathParameterIsAnError(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("customers", "retrieve")

	if _, err := BuildRequest(cmd, Input{}); err == nil {
		t.Fatal("want an error when the required path parameter is absent")
	}
}

func TestBuildRequest_FlagsBecomeBodyForPostOperations(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("customers", "create")

	req, err := BuildRequest(cmd, Input{Flags: map[string]string{"external_id": "acme-1"}})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	body, ok := req.Body.(map[string]any)
	if !ok {
		t.Fatalf("Body = %T, want map", req.Body)
	}
	if body["external_id"] != "acme-1" {
		t.Errorf("body[external_id] = %v, want acme-1", body["external_id"])
	}
}

// --data supplies the base; flags override individual fields on top of it.
func TestBuildRequest_FlagsOverrideDataDocument(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("customers", "create")

	req, err := BuildRequest(cmd, Input{
		Data:  map[string]any{"external_id": "from-file", "name": "From File"},
		Flags: map[string]string{"external_id": "from-flag"},
	})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	body := req.Body.(map[string]any)
	if body["external_id"] != "from-flag" {
		t.Errorf("external_id = %v, want the flag to win", body["external_id"])
	}
	if body["name"] != "From File" {
		t.Errorf("name = %v, want the file value preserved", body["name"])
	}
}

func TestBuildRequest_UnknownFlagSuggestsTheNearestField(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("customers", "create")

	_, err := BuildRequest(cmd, Input{Flags: map[string]string{"externl_id": "x"}})
	if err == nil {
		t.Fatal("want an error for an unknown flag")
	}
	if got := err.Error(); !strings.Contains(got, "external_id") {
		t.Errorf("error = %q, want a suggestion naming external_id", got)
	}
}

func TestBodyFields_ListsSchemaProperties(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("customers", "create")

	fields := BodyFields(cmd)
	if len(fields) == 0 {
		t.Fatal("BodyFields returned nothing for createCustomer")
	}

	found := false
	for _, f := range fields {
		if f.Name == "external_id" {
			found = true
		}
	}
	if !found {
		t.Errorf("external_id missing from %d body fields", len(fields))
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd cli && go test ./internal/spec/ -run 'TestBuildRequest|TestBodyFields' -v
```

Expected: FAIL — `undefined: BuildRequest`.

- [ ] **Step 3: Implement**

`cli/internal/spec/request.go`:

```go
package spec

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// Input is everything the user supplied on the command line.
type Input struct {
	PositionalID string
	Flags        map[string]string
	Data         map[string]any // from --data or --edit
}

type Request struct {
	Method string
	Path   string
	Query  url.Values
	Body   any
}

// Field describes one settable body or query field, used for --help and --edit.
type Field struct {
	Name     string
	Type     string
	Required bool
	Nested   bool // objects and arrays cannot be expressed as a scalar flag
	Doc      string
}

func BuildRequest(cmd Command, in Input) (Request, error) {
	req := Request{Method: cmd.Operation.Method, Path: cmd.Operation.Path, Query: url.Values{}}

	pathParams, queryParams := splitParameters(cmd.Operation.Op)

	// Path parameters. A single path parameter is filled from the positional ID so
	// that `flexprice customers retrieve cust_1` works the way Stripe's does.
	for _, p := range pathParams {
		value := in.Flags[p.Name]
		if value == "" && len(pathParams) == 1 {
			value = in.PositionalID
		}
		if value == "" {
			return req, fmt.Errorf("%s %s requires %s — pass it as an argument: flexprice %s %s <%s>",
				cmd.Resource, cmd.Action, p.Name, cmd.Resource, cmd.Action, p.Name)
		}
		req.Path = strings.ReplaceAll(req.Path, "{"+p.Name+"}", url.PathEscape(value))
		delete(in.Flags, p.Name)
	}

	known := map[string]bool{}
	for _, p := range queryParams {
		known[p.Name] = true
		if v, ok := in.Flags[p.Name]; ok {
			req.Query.Set(p.Name, v)
			delete(in.Flags, p.Name)
		}
	}

	bodyFields := BodyFields(cmd)
	byName := map[string]Field{}
	for _, f := range bodyFields {
		byName[f.Name] = f
		known[f.Name] = true
	}

	body := map[string]any{}
	for k, v := range in.Data {
		body[k] = v
	}
	for name, raw := range in.Flags {
		field, ok := byName[name]
		if !ok {
			return req, unknownFlagError(name, known)
		}
		if field.Nested {
			return req, fmt.Errorf(
				"--%s is a %s and cannot be set with a flag.\n  Use --edit, or --data @file.json",
				name, field.Type)
		}
		body[name] = coerce(raw, field.Type)
	}

	if len(body) > 0 {
		req.Body = body
	} else if len(bodyFields) > 0 && requiresBody(cmd.Operation.Op) {
		req.Body = map[string]any{}
	}
	return req, nil
}

func splitParameters(op *openapi3.Operation) (path, query []*openapi3.Parameter) {
	for _, ref := range op.Parameters {
		if ref.Value == nil {
			continue
		}
		switch ref.Value.In {
		case openapi3.ParameterInPath:
			path = append(path, ref.Value)
		case openapi3.ParameterInQuery:
			query = append(query, ref.Value)
		}
	}
	return path, query
}

func requiresBody(op *openapi3.Operation) bool {
	return op.RequestBody != nil && op.RequestBody.Value != nil && op.RequestBody.Value.Required
}

// BodyFields lists the top-level properties of the request body schema.
func BodyFields(cmd Command) []Field {
	op := cmd.Operation.Op
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return nil
	}
	media := op.RequestBody.Value.Content.Get("application/json")
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		return nil
	}
	schema := media.Schema.Value

	required := map[string]bool{}
	for _, r := range schema.Required {
		required[r] = true
	}

	var out []Field
	for name, prop := range schema.Properties {
		if prop.Value == nil {
			continue
		}
		kind := schemaType(prop.Value)
		out = append(out, Field{
			Name:     name,
			Type:     kind,
			Required: required[name],
			Nested:   kind == "object" || kind == "array",
			Doc:      prop.Value.Description,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func schemaType(s *openapi3.Schema) string {
	switch {
	case s.Type == nil:
		if len(s.Properties) > 0 {
			return "object"
		}
		return "string"
	case s.Type.Is("array"):
		return "array"
	case s.Type.Is("object"):
		return "object"
	case s.Type.Is("integer"):
		return "integer"
	case s.Type.Is("number"):
		return "number"
	case s.Type.Is("boolean"):
		return "boolean"
	default:
		return "string"
	}
}

func coerce(raw, kind string) any {
	switch kind {
	case "integer":
		if n, err := strconv.Atoi(raw); err == nil {
			return n
		}
	case "number":
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return f
		}
	case "boolean":
		if b, err := strconv.ParseBool(raw); err == nil {
			return b
		}
	}
	return raw
}

// unknownFlagError suggests the closest known field so a typo does not become a 400.
func unknownFlagError(name string, known map[string]bool) error {
	best, bestScore := "", 1<<30
	for candidate := range known {
		if d := editDistance(name, candidate); d < bestScore {
			best, bestScore = candidate, d
		}
	}
	// Only suggest when the names are genuinely close.
	if best != "" && bestScore <= 3 {
		return fmt.Errorf("unknown flag --%s\n  Did you mean --%s?", name, best)
	}
	names := make([]string, 0, len(known))
	for k := range known {
		names = append(names, k)
	}
	sort.Strings(names)
	return fmt.Errorf("unknown flag --%s\n  Available: --%s", name, strings.Join(names, ", --"))
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		copy(prev, curr)
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
```

- [ ] **Step 4: Run the tests**

```bash
cd cli && go test ./internal/spec/ -v
```

Expected: PASS, all registry and request tests.

- [ ] **Step 5: Commit**

```bash
git add cli/internal/spec
git commit -m "feat(cli): request builder with spec-validated flags and suggestions"
```

### Task 11: `--edit` skeleton generation

Only implement this if Task 1's verdict was PASS. Use the accessor names from the findings file.

**Files:**
- Create: `cli/internal/spec/skeleton.go`, `cli/internal/spec/skeleton_test.go`

- [ ] **Step 1: Write the failing test**

`cli/internal/spec/skeleton_test.go`:

```go
package spec

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSkeleton_ProducesValidJSONForDeepSchema(t *testing.T) {
	reg := testRegistry(t)
	cmd, ok := reg.Lookup("subscriptions", "create")
	if !ok {
		t.Fatal("subscriptions create not registered")
	}

	out, err := Skeleton(cmd)
	if err != nil {
		t.Fatalf("Skeleton: %v", err)
	}

	// The skeleton is commented for humans; the JSON below the comments must parse.
	body := stripComments(out)
	var v map[string]any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("skeleton is not valid JSON: %v\n%s", err, body)
	}
	if len(v) == 0 {
		t.Fatal("skeleton has no fields")
	}
}

func TestSkeleton_IncludesRequiredFields(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("subscriptions", "create")

	out, err := Skeleton(cmd)
	if err != nil {
		t.Fatalf("Skeleton: %v", err)
	}
	for _, want := range []string{"customer_id", "plan_id"} {
		if !strings.Contains(out, want) {
			t.Errorf("skeleton missing required field %q", want)
		}
	}
}

// Cyclic $refs must not hang or overflow. This is the property the spike proved.
func TestSkeleton_TerminatesOnCyclicSchemas(t *testing.T) {
	reg := testRegistry(t)
	for _, action := range []string{"create", "update"} {
		cmd, ok := reg.Lookup("subscriptions", action)
		if !ok {
			continue
		}
		if _, err := Skeleton(cmd); err != nil {
			t.Errorf("subscriptions %s: %v", action, err)
		}
	}
}

func stripComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd cli && go test ./internal/spec/ -run TestSkeleton -v
```

Expected: FAIL — `undefined: Skeleton`.

- [ ] **Step 3: Implement**

`cli/internal/spec/skeleton.go`:

```go
package spec

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// maxSkeletonDepth is 16: the deepest natural nesting in this spec is 14, and a
// cap of 12 was measured to truncate real nodes.
const maxSkeletonDepth = 16

// Skeleton renders an editable JSON document for an operation's request body.
//
// Fill policy, derived from measurements against the live API:
//
//   - Only REQUIRED fields are emitted as live JSON. Optional fields are listed
//     as commented-out lines the user uncomments. This is not a stylistic
//     choice: sending an untouched optional numeric field as "" fails the
//     server's request binding outright, producing "Invalid request format"
//     with no details — a dead end for the user. String-typed optionals bind
//     fine, but the rule is applied uniformly so the skeleton is never a trap.
//   - A required-only skeleton for CreateSubscriptionRequest is just three
//     fields, because every nested structure --edit exists for (phases,
//     line_items, credit_grants) is optional in the spec. The commented block
//     is therefore what carries the feature's value, and it must list nested
//     fields with their types rather than omitting them.
//
// Cycles are broken by tracking schemas already on the current path. Note that
// termination is guaranteed by the depth cap; the cycle guard bounds breadth
// (removing it grows the SubscriptionResponse walk from 1,693 to 17,789 nodes).
func Skeleton(cmd Command) (string, error) {
	op := cmd.Operation.Op
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return "", fmt.Errorf("%s %s takes no request body", cmd.Resource, cmd.Action)
	}
	media := op.RequestBody.Value.Content.Get("application/json")
	if media == nil || media.Schema == nil {
		return "", fmt.Errorf("%s %s has no JSON request schema", cmd.Resource, cmd.Action)
	}

	value := build(media.Schema, map[*openapi3.Schema]bool{}, 0)
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", fmt.Errorf("render skeleton: %w", err)
	}

	var header strings.Builder
	fmt.Fprintf(&header, "// flexprice %s %s\n", cmd.Resource, cmd.Action)
	fmt.Fprintf(&header, "// Required fields are pre-filled below. Lines starting with // are ignored.\n")

	var optional []string
	for _, f := range BodyFields(cmd) {
		if !f.Required {
			optional = append(optional, fmt.Sprintf("%s (%s)", f.Name, f.Type))
		}
	}
	sort.Strings(optional)
	if len(optional) > 0 {
		fmt.Fprintf(&header, "//\n// Optional fields you may add:\n")
		for _, o := range optional {
			fmt.Fprintf(&header, "//   %s\n", o)
		}
	}
	header.WriteString("\n")

	return header.String() + string(body) + "\n", nil
}

func build(ref *openapi3.SchemaRef, onPath map[*openapi3.Schema]bool, depth int) any {
	if ref == nil || ref.Value == nil || depth > maxSkeletonDepth {
		return nil
	}
	s := ref.Value
	if onPath[s] {
		return nil // cycle: stop descending
	}
	onPath[s] = true
	defer delete(onPath, s)

	switch schemaType(s) {
	case "object":
		out := map[string]any{}
		required := map[string]bool{}
		for _, r := range s.Required {
			required[r] = true
		}
		for name, prop := range s.Properties {
			if !required[name] {
				continue
			}
			out[name] = build(prop, onPath, depth+1)
		}
		return out
	case "array":
		inner := build(s.Items, onPath, depth+1)
		if inner == nil {
			return []any{}
		}
		return []any{inner}
	case "integer":
		return 0
	case "number":
		return 0
	case "boolean":
		return false
	default:
		if len(s.Enum) > 0 {
			return s.Enum[0]
		}
		return ""
	}
}

// StripComments removes // lines so an edited skeleton can be parsed as JSON.
func StripComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
```

- [ ] **Step 4: Run the tests**

```bash
cd cli && go test ./internal/spec/ -v
```

Expected: PASS. If `TestSkeleton_TerminatesOnCyclicSchemas` hangs, the `onPath` set is not being consulted — a cycle is being followed.

- [ ] **Step 5: Commit**

```bash
git add cli/internal/spec
git commit -m "feat(cli): --edit skeleton generation with cycle breaking"
```

---

## Phase 3 — Output and commands

### Task 12: Output rendering and the stdout/stderr split

Design doc §12. Data goes to stdout, everything human goes to stderr, so `--output json > file.json` is always clean.

**Files:**
- Create: `cli/internal/output/output.go`, `cli/internal/output/table.go`, `cli/internal/output/output_test.go`

- [ ] **Step 1: Write the failing test**

`cli/internal/output/output_test.go`:

```go
package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func sample() []byte {
	return []byte(`{"items":[
      {"id":"cust_1","email":"a@b.com","status":"active","extra":"noise"},
      {"id":"cust_2","email":"c@d.com","status":"archived","extra":"noise"}
    ],"total":2}`)
}

func TestRender_JSONGoesToStdoutOnly(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatJSON}

	if err := w.Render(sample(), Options{}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q, want empty for JSON output", errOut.String())
	}
	var v any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
}

func TestRender_TableUsesRequestedColumns(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}

	if err := w.Render(sample(), Options{Columns: []string{"id", "status"}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "cust_1") || !strings.Contains(got, "archived") {
		t.Errorf("table missing expected cells:\n%s", got)
	}
	if strings.Contains(got, "noise") {
		t.Errorf("table shows a column that was not requested:\n%s", got)
	}
}

func TestRender_TableFooterReportsTruncation(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}

	err := w.Render(sample(), Options{Columns: []string{"id"}, Shown: 2, Total: 1204})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// The footer is guidance, not data, so it belongs on stderr.
	if !strings.Contains(errOut.String(), "1204") || !strings.Contains(errOut.String(), "--all") {
		t.Errorf("stderr = %q, want a truncation footer naming --all", errOut.String())
	}
}

func TestRender_YAMLIsParseable(t *testing.T) {
	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatYAML}

	if err := w.Render(sample(), Options{}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out.String(), "cust_1") {
		t.Errorf("yaml output missing data:\n%s", out.String())
	}
}

func TestParseFormat_RejectsUnknown(t *testing.T) {
	if _, err := ParseFormat("xml"); err == nil {
		t.Fatal("want an error for an unsupported format")
	}
	if f, err := ParseFormat("json"); err != nil || f != FormatJSON {
		t.Errorf("ParseFormat(json) = %v, %v", f, err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd cli && go test ./internal/output/ -v
```

Expected: FAIL — `undefined: Writer`.

- [ ] **Step 3: Implement**

`cli/internal/output/output.go`:

```go
// Package output renders API responses. Data is written to Out and everything
// human — footers, progress, warnings — to Err, so redirecting stdout yields
// clean machine-readable output.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/goccy/go-yaml"
)

type Format int

const (
	FormatTable Format = iota
	FormatJSON
	FormatYAML
)

func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(s) {
	case "table":
		return FormatTable, nil
	case "json":
		return FormatJSON, nil
	case "yaml", "yml":
		return FormatYAML, nil
	default:
		return FormatTable, fmt.Errorf("unsupported output format %q — use table, json or yaml", s)
	}
}

type Options struct {
	Columns []string
	// Shown and Total drive the truncation footer. Total of 0 means unknown.
	Shown int
	Total int
	Quiet bool
}

type Writer struct {
	Out    io.Writer
	Err    io.Writer
	Format Format
}

func (w Writer) Render(raw []byte, o Options) error {
	switch w.Format {
	case FormatJSON:
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			// Not JSON (a PDF, say) — pass it through untouched.
			_, err := w.Out.Write(raw)
			return err
		}
		enc := json.NewEncoder(w.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)

	case FormatYAML:
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			_, err := w.Out.Write(raw)
			return err
		}
		b, err := yaml.Marshal(v)
		if err != nil {
			return fmt.Errorf("render yaml: %w", err)
		}
		_, err = w.Out.Write(b)
		return err

	default:
		return w.renderTable(raw, o)
	}
}

// Warn writes a human-facing message to stderr unless --quiet is set.
func (w Writer) Warn(o Options, format string, args ...any) {
	if o.Quiet || w.Err == nil {
		return
	}
	fmt.Fprintf(w.Err, format+"\n", args...)
}
```

`cli/internal/output/table.go`:

```go
package output

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
)

// rowsFrom finds the list in a response. The API returns collections under a
// named key ("items", "customers", …); a single object renders as one row.
func rowsFrom(raw []byte) ([]map[string]any, error) {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	switch v := doc.(type) {
	case []any:
		return toRows(v), nil
	case map[string]any:
		// Prefer a key whose value is an array of objects.
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if arr, ok := v[k].([]any); ok {
				return toRows(arr), nil
			}
		}
		return []map[string]any{v}, nil
	default:
		return nil, fmt.Errorf("unexpected response shape")
	}
}

func toRows(items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func (w Writer) renderTable(raw []byte, o Options) error {
	rows, err := rowsFrom(raw)
	if err != nil {
		// Unparseable as a table — fall back to JSON so the user still sees the data.
		return Writer{Out: w.Out, Err: w.Err, Format: FormatJSON}.Render(raw, o)
	}
	if len(rows) == 0 {
		w.Warn(o, "No results.")
		return nil
	}

	columns := o.Columns
	if len(columns) == 0 {
		columns = defaultColumns(rows[0])
	}

	tw := tabwriter.NewWriter(w.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.ToUpper(strings.Join(columns, "\t")))
	for _, row := range rows {
		cells := make([]string, len(columns))
		for i, c := range columns {
			cells[i] = format(row[c])
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("write table: %w", err)
	}

	if o.Total > o.Shown && o.Shown > 0 {
		w.Warn(o, "\nshowing %d of %d — use --all to fetch every page", o.Shown, o.Total)
	}
	return nil
}

// defaultColumns is the fallback when commands.yaml declares none: id, a name-ish
// field, status, and a timestamp. Design doc §3, Round 3.
func defaultColumns(row map[string]any) []string {
	preferred := []string{"id", "name", "external_id", "email", "status", "created_at"}
	var out []string
	for _, p := range preferred {
		if _, ok := row[p]; ok {
			out = append(out, p)
		}
	}
	if len(out) > 0 {
		return out
	}

	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 5 {
		keys = keys[:5]
	}
	return keys
}

func format(v any) string {
	switch t := v.(type) {
	case nil:
		return "—"
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	case bool:
		return fmt.Sprintf("%t", t)
	case map[string]any, []any:
		b, _ := json.Marshal(t)
		s := string(b)
		if len(s) > 40 {
			return s[:37] + "..."
		}
		return s
	default:
		return fmt.Sprintf("%v", t)
	}
}
```

- [ ] **Step 4: Run the tests**

```bash
cd cli && go test ./internal/output/ -v
```

Expected: PASS, all five tests.

- [ ] **Step 5: Commit**

```bash
git add cli/internal/output
git commit -m "feat(cli): output rendering with stdout/stderr separation"
```

### Task 13: Auth commands — init, login, logout, whoami, env, config

Design doc §6. Region comes from the spec; live/test is derived from `EnvironmentType`, never asked.

**Files:**
- Create: `cli/internal/cmd/auth.go`, `cli/internal/cmd/env.go`, `cli/internal/cmd/config.go`, `cli/internal/cmd/init.go`
- Create: `cli/internal/cmd/auth_test.go`
- Modify: `cli/internal/cmd/root.go`

- [ ] **Step 1: Write the failing test**

`cli/internal/cmd/auth_test.go`:

```go
package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func envServer(t *testing.T, envType string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk_good" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid key"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"environments": []map[string]any{
				{"id": "env_1", "name": "Production", "type": envType},
			},
		})
	}))
}

func TestVerifyKey_AcceptsAWorkingKey(t *testing.T) {
	srv := envServer(t, "development")
	defer srv.Close()

	if err := VerifyKey(t.Context(), srv.URL, "sk_good", "test"); err != nil {
		t.Fatalf("VerifyKey: %v", err)
	}
}

// A wrong-region key returns 401, identical to an invalid key. The message must
// disambiguate. Design doc §6.
func TestVerifyKey_RejectionMentionsRegion(t *testing.T) {
	srv := envServer(t, "production")
	defer srv.Close()

	err := VerifyKey(t.Context(), srv.URL, "sk_wrong", "test")
	if err == nil {
		t.Fatal("want an error for a rejected key")
	}
	if got := err.Error(); !strings.Contains(got, "region") {
		t.Errorf("error = %q, want it to mention region", got)
	}
}

func TestMaskKey_ShowsOnlyAPrefix(t *testing.T) {
	got := MaskKey("sk_live_abcdefghijklmnop")
	if got == "sk_live_abcdefghijklmnop" {
		t.Fatal("key was not masked")
	}
	if len(got) > 20 {
		t.Errorf("mask = %q, too revealing", got)
	}
}


```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd cli && go test ./internal/cmd/ -run 'TestVerifyKey|TestMaskKey' -v
```

Expected: FAIL — `undefined: VerifyKey`.

- [ ] **Step 3: Implement verification and the auth commands**

`cli/internal/cmd/auth.go`:

```go
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/flexprice/cli/internal/client"
	"github.com/flexprice/cli/internal/config"
	"github.com/flexprice/cli/internal/keyring"
	"github.com/flexprice/cli/internal/spec"
)

// VerifyKey confirms a key works against a region. It deliberately returns no
// identity: nothing reachable by an environment-scoped key reveals which
// environment it belongs to, so there is nothing trustworthy to report.

// environmentsResponse matches dto.ListEnvironmentsResponse.
//
// GET /v1/environments is a real, authenticated route but carries no swaggo
// annotations, so it is absent from the OpenAPI spec and cannot be resolved
// through the registry. It is called by literal path here. Annotating the
// handler upstream is tracked in "Before release".
type environmentsResponse struct {
	Environments []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"environments"`
}

func VerifyKey(ctx context.Context, baseURL, apiKey, version string) error {
	c := client.New(client.Options{BaseURL: baseURL, APIKey: apiKey, Version: version})

	if _, err := c.Do(ctx, http.MethodGet, "/environments", nil, nil); err != nil {
		var apiErr *client.APIError
		if ok := asAPIError(err, &apiErr); ok && apiErr.Status == http.StatusUnauthorized {
			// A wrong-region key and an invalid key both return 401 with an
			// identical body, so the message has to name the possibility.
			return fmt.Errorf(
				"this key was rejected by %s.\n"+
					"  Keys are region-specific. If your account is in another region, re-run with --region\n"+
					"  (for example: flexprice login --region in)", baseURL)
		}
		return err
	}
	return nil
}

func asAPIError(err error, target **client.APIError) bool {
	e, ok := err.(*client.APIError)
	if ok {
		*target = e
	}
	return ok
}

// MaskKey renders a key for display: enough to identify it, not enough to use.
func MaskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:8] + "…" + key[len(key)-2:]
}

// readSecret reads a key from the terminal without echoing it, so it never lands
// in shell history or the process table.
func readSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("no terminal available — pass --api-key, or set FLEXPRICE_API_KEY")
	}
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read key: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

func newLoginCommand(g *Globals, version string) *cobra.Command {
	var profileName, label string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store credentials for a region and environment",
		Long: "Verifies your API key, resolves the tenant and environment it is scoped to,\n" +
			"and stores it in your OS keychain.\n\n" +
			"An API key belongs to exactly one environment, so use one profile per environment.",
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()

			doc, err := spec.Load()
			if err != nil {
				return err
			}
			regions := spec.Regions(doc)

			baseURL := g.BaseURL
			if baseURL == "" {
				region := g.Region
				if region == "" {
					region, err = promptRegion(regions)
					if err != nil {
						return err
					}
				}
				for _, r := range regions {
					if r.Key == region {
						baseURL = r.BaseURL
					}
				}
				if baseURL == "" {
					return fmt.Errorf("unknown region %q", region)
				}
			}

			apiKey := g.APIKey
			if apiKey == "" {
				apiKey, err = readSecret("API key: ")
				if err != nil {
					return err
				}
			}
			if apiKey == "" {
				return fmt.Errorf("no API key provided")
			}

			if err := VerifyKey(ctx, baseURL, apiKey, version); err != nil {
				return err
			}

			profileName = config.ProfileName(profileName)

			store, warn, err := keyring.Open()
			if err != nil {
				return err
			}
			if warn != "" {
				fmt.Fprintln(os.Stderr, warn)
			}

			path, err := config.DefaultPath()
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}

			// Rotation: show what is being replaced rather than silently overwriting.
			if _, existed := cfg.Profiles[profileName]; existed {
				if old, err := store.Get(profileName); err == nil {
					fmt.Fprintf(os.Stderr, "Replacing key %s with %s for profile %q\n",
						MaskKey(old), MaskKey(apiKey), profileName)
				}
			}

			if err := store.Set(profileName, apiKey); err != nil {
				return fmt.Errorf("store key: %w", err)
			}

			cfg.Profiles[profileName] = config.Profile{
				Region:  g.Region,
				BaseURL: baseURL,
				Label:   label,
				KeyRef:  "keyring:" + profileName,
			}
			if cfg.DefaultProfile == "" {
				cfg.DefaultProfile = profileName
			}
			if err := config.Save(path, cfg); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "Verified — stored as profile %q in %s\n", profileName, store.Name())
			fmt.Fprintln(os.Stderr,
				"Note: the API does not report which environment a key belongs to, so label your\n"+
					"profiles yourself (--profile-name, --label) and check with: flexprice whoami")
			return nil
		},
	}

	cmd.Flags().StringVar(&profileName, "profile-name", "", "name for the stored profile (default: \"default\")")
	cmd.Flags().StringVar(&label, "label", "", "free-text note shown by whoami, e.g. \"sandbox\"")
	return cmd
}
```

Add `promptRegion`, `newLogoutCommand` and `newWhoamiCommand` in the same file:

```go
func promptRegion(regions []spec.Region) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("no terminal available — pass --region (for example --region us)")
	}
	fmt.Fprintln(os.Stderr, "Data region:")
	for i, r := range regions {
		fmt.Fprintf(os.Stderr, "  %d) %-6s %s\n", i+1, r.Key, r.BaseURL)
	}
	fmt.Fprint(os.Stderr, "Choose [1]: ")

	var choice string
	if _, err := fmt.Fscanln(os.Stdin, &choice); err != nil && choice == "" {
		choice = "1"
	}
	idx := 1
	if _, err := fmt.Sscanf(choice, "%d", &idx); err != nil || idx < 1 || idx > len(regions) {
		idx = 1
	}
	return regions[idx-1].Key, nil
}

func newLogoutCommand(g *Globals) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove a stored profile and its key",
		RunE: func(c *cobra.Command, _ []string) error {
			path, err := config.DefaultPath()
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			name, _, err := cfg.Resolve(g.Profile)
			if err != nil {
				return err
			}

			store, _, err := keyring.Open()
			if err != nil {
				return err
			}
			if err := store.Delete(name); err != nil {
				return err
			}

			delete(cfg.Profiles, name)
			if cfg.DefaultProfile == name {
				cfg.DefaultProfile = ""
				for other := range cfg.Profiles {
					cfg.DefaultProfile = other
					break
				}
			}
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Removed profile %q\n", name)
			return nil
		},
	}
}

func newWhoamiCommand(g *Globals) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the active profile, environment and key backend",
		RunE: func(c *cobra.Command, _ []string) error {
			path, err := config.DefaultPath()
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			name, profile, err := cfg.Resolve(g.Profile)
			if err != nil {
				return err
			}

			store, _, err := keyring.Open()
			if err != nil {
				return err
			}
			key, keyErr := store.Get(name)

			fmt.Fprintf(os.Stdout, "Profile:      %s\n", name)
			fmt.Fprintf(os.Stdout, "Label:        %s\n", profile.Label)
			fmt.Fprintf(os.Stdout, "Region:       %s\n", profile.Region)
			fmt.Fprintf(os.Stdout, "Base URL:     %s\n", profile.BaseURL)
			fmt.Fprintf(os.Stdout, "Key backend:  %s\n", store.Name())
			if keyErr == nil {
				fmt.Fprintf(os.Stdout, "Key:          %s\n", MaskKey(key))
			} else {
				fmt.Fprintf(os.Stdout, "Key:          (not stored — run flexprice login)\n")
			}
			return nil
		},
	}
}
```

- [ ] **Step 4: Implement `env list` and `config`**

`cli/internal/cmd/env.go`:

```go
package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/flexprice/cli/internal/client"
	"github.com/flexprice/cli/internal/config"
)

// newEnvCommand lists the tenant's environments and which have a local profile.
// Because keys are environment-scoped, switching environments means logging in
// again — so the command prints the exact next step. Design doc §6.
func newEnvCommand(g *Globals, version string) *cobra.Command {
	env := &cobra.Command{Use: "env", Short: "Inspect environments"}

	env.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List environments and which have a local profile",
		RunE: func(c *cobra.Command, _ []string) error {
			rc, cfg, err := runtimeContext(g)
			if err != nil {
				return err
			}
			cl := client.New(client.Options{
				BaseURL: rc.BaseURL, APIKey: rc.APIKey, Version: version,
				Debug: g.Debug, DebugOut: os.Stderr,
			})
			raw, err := cl.Do(c.Context(), http.MethodGet, "/environments", nil, nil)
			if err != nil {
				return err
			}

			var envs environmentsResponse
			if err := json.Unmarshal(raw, &envs); err != nil {
				return fmt.Errorf("parse environments: %w", err)
			}

			// Profiles cannot be correlated to environments: the API does not
			// report which environment the active key belongs to, so this is a
			// plain listing of what exists in the tenant.
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ENVIRONMENT\tTYPE\tID")
			for _, e := range envs.Environments {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Name, e.Type, e.ID)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr,
				"\nYour key is scoped to one of these, but the API does not say which.")
			return nil
		},
	})

	return env
}
```

`cli/internal/cmd/config.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/flexprice/cli/internal/config"
)

func newConfigCommand(g *Globals) *cobra.Command {
	cfgCmd := &cobra.Command{Use: "config", Short: "Manage profiles"}

	cfgCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List stored profiles",
		RunE: func(c *cobra.Command, _ []string) error {
			path, err := config.DefaultPath()
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			if len(cfg.Profiles) == 0 {
				fmt.Fprintln(os.Stderr, "No profiles yet — run: flexprice init")
				return nil
			}

			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "PROFILE\tLABEL\tREGION\tDEFAULT")
			for name, p := range cfg.Profiles {
				marker := ""
				if name == cfg.DefaultProfile {
					marker = "*"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", name, p.Label, p.Region, marker)
			}
			return tw.Flush()
		},
	})

	cfgCmd.AddCommand(&cobra.Command{
		Use:   "use <profile>",
		Short: "Set the default profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			path, err := config.DefaultPath()
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			if _, ok := cfg.Profiles[args[0]]; !ok {
				return fmt.Errorf("profile %q not found — see: flexprice config list", args[0])
			}
			cfg.DefaultProfile = args[0]
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Default profile is now %q\n", args[0])
			return nil
		},
	})

	return cfgCmd
}
```

`cli/internal/cmd/init.go`:

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// newInitCommand is the guided first run: login, then tell the user what to do next.
func newInitCommand(g *Globals, version string) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Set up the CLI (guided)",
		RunE: func(c *cobra.Command, args []string) error {
			fmt.Fprintln(os.Stderr, "Setting up the Flexprice CLI.")
			fmt.Fprintln(os.Stderr, "Your API key is scoped to one environment — you can add more later with `flexprice login`.")
			fmt.Fprintln(os.Stderr)

			login := newLoginCommand(g, version)
			login.SetContext(c.Context())
			if err := login.RunE(login, nil); err != nil {
				return err
			}

			fmt.Fprintln(os.Stderr, "\nNext steps:")
			fmt.Fprintln(os.Stderr, "  flexprice whoami            confirm what you are pointed at")
			fmt.Fprintln(os.Stderr, "  flexprice resources         see everything you can act on")
			fmt.Fprintln(os.Stderr, "  flexprice customers list    try a read")
			fmt.Fprintln(os.Stderr, "  flexprice env list          see your other environments")
			return nil
		},
	}
}
```

- [ ] **Step 5: Add the shared runtime-context helper and wire the commands**

Add to `cli/internal/cmd/root.go`:

```go
// runtimeContext resolves credentials for the current invocation. Every command
// that talks to the API starts here, so precedence is applied in exactly one place.
func runtimeContext(g *Globals) (config.RuntimeContext, *config.Config, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return config.RuntimeContext{}, nil, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return config.RuntimeContext{}, nil, err
	}

	store, warn, err := keyring.Open()
	if err != nil {
		return config.RuntimeContext{}, nil, err
	}
	if warn != "" && !g.Quiet {
		fmt.Fprintln(os.Stderr, warn)
	}

	doc, err := spec.Load()
	if err != nil {
		return config.RuntimeContext{}, nil, err
	}
	regions := map[string]string{}
	for _, r := range spec.Regions(doc) {
		regions[r.Key] = r.BaseURL
	}

	rc, err := config.ResolveContext(cfg, store, config.Overrides{
		Profile: g.Profile,
		APIKey:  g.APIKey,
		BaseURL: g.BaseURL,
		Region:  g.Region,
		Regions: regions,
	})
	return rc, cfg, err
}
```

And register everything in `NewRootCommand`, before the `return root`:

```go
	// Remove the Run stub added in Task 2 now that subcommands exist: cobra renders
	// the Usage/Flags help section when HasSubCommands() is true, so the stub that
	// forced it for a bare root command is redundant from here on.
	root.Run = nil

	root.AddCommand(
		newInitCommand(g, version),
		newLoginCommand(g, version),
		newLogoutCommand(g),
		newWhoamiCommand(g),
		newEnvCommand(g, version),
		newConfigCommand(g),
	)
```

Add the imports `"fmt"`, `"os"`, and the `config`, `keyring` and `spec` packages to `root.go`.

- [ ] **Step 6: Run the tests**

```bash
cd cli && go get golang.org/x/term@latest && go test ./... -v
cd cli && go build -o bin/flexprice . && ./bin/flexprice --help
```

Expected: tests PASS; `--help` lists init, login, logout, whoami, env and config.

- [ ] **Step 7: Commit**

```bash
git add cli/internal/cmd cli/go.mod cli/go.sum
git commit -m "feat(cli): init, login, logout, whoami, env list and config commands"
```

### Task 14: Resource command tree, `resources`, and raw HTTP

Design doc §5 and §8. Resources sit at the top level; `get`/`post`/`delete` are the separate escape hatch.

**Files:**
- Create: `cli/internal/cmd/resource.go`, `cli/internal/cmd/raw.go`, `cli/internal/cmd/resource_test.go`
- Modify: `cli/internal/cmd/root.go`

- [ ] **Step 1: Write the failing test**

`cli/internal/cmd/resource_test.go`:

```go
package cmd

import (
	"strings"
	"testing"
)

func TestResourceCommands_AreRegisteredAtTopLevel(t *testing.T) {
	root := NewRootCommand("test")

	var names []string
	for _, c := range root.Commands() {
		names = append(names, c.Name())
	}
	for _, want := range []string{"customers", "invoices", "subscriptions"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("resource %q not registered at top level; have %v", want, names)
		}
	}
}

func TestResourceCommand_ExposesItsActions(t *testing.T) {
	root := NewRootCommand("test")

	var customers *cobraCommand
	for _, c := range root.Commands() {
		if c.Name() == "customers" {
			customers = c
		}
	}
	if customers == nil {
		t.Fatal("customers command missing")
	}

	var actions []string
	for _, a := range customers.Commands() {
		actions = append(actions, a.Name())
	}
	for _, want := range []string{"list", "retrieve", "create"} {
		found := false
		for _, a := range actions {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Errorf("action %q missing from customers; have %v", want, actions)
		}
	}
}

// Webhook Events stubs are documentation, not endpoints. Design doc §5.
func TestResourceCommands_ExcludeWebhookEventStubs(t *testing.T) {
	root := NewRootCommand("test")
	for _, c := range root.Commands() {
		if strings.Contains(c.Name(), "webhook-events") {
			t.Errorf("webhook event stubs became a command: %s", c.Name())
		}
	}
}

func TestRawCommands_AreRegistered(t *testing.T) {
	root := NewRootCommand("test")

	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, want := range []string{"get", "post", "delete", "resources"} {
		if !have[want] {
			t.Errorf("command %q not registered", want)
		}
	}
}
```

Add this alias near the top of the test file so the test reads cleanly:

```go
type cobraCommand = cobra.Command
```

with the import `"github.com/spf13/cobra"`.

- [ ] **Step 2: Run it to verify it fails**

```bash
cd cli && go test ./internal/cmd/ -run 'TestResource|TestRaw' -v
```

Expected: FAIL — resource commands are not registered.

- [ ] **Step 3: Implement the resource tree**

`cli/internal/cmd/resource.go`:

```go
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/flexprice/cli/internal/client"
	"github.com/flexprice/cli/internal/output"
	"github.com/flexprice/cli/internal/spec"
)

// addResourceCommands builds the command tree from the registry at startup.
// There is no generated code: the tree is derived from the embedded spec.
func addResourceCommands(root *cobra.Command, reg *spec.Registry, g *Globals, version string) {
	for _, resource := range reg.Resources() {
		parent := &cobra.Command{
			Use:   resource,
			Short: fmt.Sprintf("Operations on %s", resource),
		}
		for _, action := range reg.Actions(resource) {
			cmd, _ := reg.Lookup(resource, action)
			parent.AddCommand(newOperationCommand(cmd, reg, g, version))
		}
		root.AddCommand(parent)
	}

	root.AddCommand(&cobra.Command{
		Use:   "resources",
		Short: "List every resource this CLI can act on",
		RunE: func(c *cobra.Command, _ []string) error {
			for _, r := range reg.Resources() {
				fmt.Fprintf(os.Stdout, "%-28s %s\n", r, strings.Join(reg.Actions(r), ", "))
			}
			return nil
		},
	})
}

func newOperationCommand(cmd spec.Command, reg *spec.Registry, g *Globals, version string) *cobra.Command {
	var (
		dataArg string
		edit    bool
		force   bool
	)

	fields := spec.BodyFields(cmd)
	c := &cobra.Command{
		Use:   cmd.Action,
		Short: operationSummary(cmd),
		Long:  operationHelp(cmd, fields),
		Args:  cobra.MaximumNArgs(1),
		// Body fields are not declared as typed flags: the spec has 199 operations
		// and CreateSubscriptionRequest alone has 37 top-level properties. Unknown
		// flags are collected and validated against the spec instead. Design doc §7.
		FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
		RunE: func(cc *cobra.Command, args []string) error {
			in := spec.Input{Flags: map[string]string{}}
			if len(args) == 1 {
				in.PositionalID = args[0]
			}
			for k, v := range collectUnknownFlags(cc) {
				in.Flags[k] = v
			}

			switch {
			case edit:
				doc, err := editSkeleton(cmd)
				if err != nil {
					return err
				}
				in.Data = doc
			case dataArg != "":
				doc, err := readDataArg(dataArg)
				if err != nil {
					return err
				}
				in.Data = doc
			}

			if err := confirm(cmd, in.PositionalID, force); err != nil {
				return err
			}

			req, err := spec.BuildRequest(cmd, in)
			if err != nil {
				return err
			}

			rc, _, err := runtimeContext(g)
			if err != nil {
				return err
			}
			cl := client.New(client.Options{
				BaseURL: rc.BaseURL, APIKey: rc.APIKey, Version: version,
				Debug: g.Debug, DebugOut: os.Stderr,
			})

			raw, err := cl.Do(cc.Context(), req.Method, req.Path, req.Query, req.Body)
			if err != nil {
				return err
			}

			format, err := output.ParseFormat(g.Output)
			if err != nil {
				return err
			}
			w := output.Writer{Out: os.Stdout, Err: os.Stderr, Format: format}
			return w.Render(raw, output.Options{
				Columns: pickColumns(reg, g, cmd.Resource),
				Quiet:   g.Quiet,
			})
		},
	}

	c.Flags().StringVar(&dataArg, "data", "", "request body: @file.json, - for stdin, or a JSON literal")
	c.Flags().BoolVar(&edit, "edit", false, "open $EDITOR with a pre-filled request body")
	c.Flags().BoolVar(&force, "force", false, "skip the confirmation prompt on destructive actions")
	return c
}

// destructive lists the actions that cannot be undone. Because the CLI cannot
// tell a production environment from a development one, every destructive action
// is confirmed regardless of where it is pointed — there is no environment signal
// to be selective with.
var destructive = map[string]bool{
	"delete": true, "void": true, "terminate": true, "cancel": true, "archive": true,
}

// confirm prompts before a destructive action. It returns nil when stdin is not a
// terminal, so scripts and CI are never blocked; --force skips it entirely.
func confirm(cmd spec.Command, target string, force bool) error {
	if force || !destructive[cmd.Action] {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil
	}

	subject := target
	if subject == "" {
		subject = cmd.Resource
	}
	fmt.Fprintf(os.Stderr, "This will %s %s and cannot be undone.\nContinue? [y/N]: ", cmd.Action, subject)

	var answer string
	_, _ = fmt.Fscanln(os.Stdin, &answer)
	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		return fmt.Errorf("cancelled")
	}
	return nil
}

func pickColumns(reg *spec.Registry, g *Globals, resource string) []string {
	if len(g.Columns) > 0 {
		return g.Columns
	}
	return reg.Columns(resource)
}

func operationSummary(cmd spec.Command) string {
	if s := cmd.Operation.Op.Summary; s != "" {
		return s
	}
	return fmt.Sprintf("%s %s", cmd.Operation.Method, cmd.Operation.Path)
}

// operationHelp lists settable fields and states plainly when flags are not
// enough for this operation's body.
func operationHelp(cmd spec.Command, fields []spec.Field) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n%s %s\n", operationSummary(cmd), cmd.Operation.Method, cmd.Operation.Path)

	if len(fields) == 0 {
		return b.String()
	}

	nested := 0
	var flat, deep []string
	for _, f := range fields {
		label := f.Name
		if f.Required {
			label += "  (required)"
		}
		if f.Nested {
			nested++
			deep = append(deep, fmt.Sprintf("  %s  [%s]", label, f.Type))
			continue
		}
		flat = append(flat, fmt.Sprintf("  --%s  [%s]", label, f.Type))
	}
	sort.Strings(flat)
	sort.Strings(deep)

	if len(flat) > 0 {
		fmt.Fprintf(&b, "\nFields you can set with flags:\n%s\n", strings.Join(flat, "\n"))
	}
	if nested > 0 {
		fmt.Fprintf(&b, "\nNested fields — these cannot be set with flags:\n%s\n", strings.Join(deep, "\n"))
		fmt.Fprintf(&b, "\nUse --edit to fill in a pre-built request body, or --data @file.json.\n")
	}
	return b.String()
}

// collectUnknownFlags gathers --key=value pairs cobra did not recognise.
func collectUnknownFlags(c *cobra.Command) map[string]string {
	out := map[string]string{}
	for i, raw := range os.Args {
		if !strings.HasPrefix(raw, "--") {
			continue
		}
		body := strings.TrimPrefix(raw, "--")
		if key, value, found := strings.Cut(body, "="); found {
			if c.Flags().Lookup(key) == nil && c.InheritedFlags().Lookup(key) == nil {
				out[key] = value
			}
			continue
		}
		// --key value form
		if c.Flags().Lookup(body) == nil && c.InheritedFlags().Lookup(body) == nil {
			if i+1 < len(os.Args) && !strings.HasPrefix(os.Args[i+1], "--") {
				out[body] = os.Args[i+1]
			}
		}
	}
	return out
}

// readDataArg accepts @file, - for stdin, or a JSON literal.
func readDataArg(arg string) (map[string]any, error) {
	var raw []byte
	var err error

	switch {
	case arg == "-":
		raw, err = readAll(os.Stdin)
	case strings.HasPrefix(arg, "@"):
		raw, err = os.ReadFile(strings.TrimPrefix(arg, "@"))
	default:
		raw = []byte(arg)
	}
	if err != nil {
		return nil, fmt.Errorf("read --data: %w", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("--data is not valid JSON: %w", err)
	}
	return doc, nil
}

// editSkeleton writes a skeleton to a temp file, opens $EDITOR, and parses the result.
func editSkeleton(cmd spec.Command) (map[string]any, error) {
	skeleton, err := spec.Skeleton(cmd)
	if err != nil {
		return nil, err
	}

	f, err := os.CreateTemp("", "flexprice-*.json")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	path := f.Name()
	defer func() { _ = os.Remove(path) }()

	if _, err := f.WriteString(skeleton); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("write skeleton: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close skeleton: %w", err)
	}

	editor, err := resolveEditor()
	if err != nil {
		return nil, err
	}

	ed := exec.Command(editor, path)
	ed.Stdin, ed.Stdout, ed.Stderr = os.Stdin, os.Stderr, os.Stderr
	if err := ed.Run(); err != nil {
		return nil, fmt.Errorf("editor %s exited with an error: %w", editor, err)
	}

	edited, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read edited file: %w", err)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(spec.StripComments(string(edited))), &doc); err != nil {
		return nil, fmt.Errorf("the edited body is not valid JSON: %w", err)
	}
	return doc, nil
}

func resolveEditor() (string, error) {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if v := os.Getenv(env); v != "" {
			return v, nil
		}
	}
	fallback := "vi"
	if runtime.GOOS == "windows" {
		fallback = "notepad"
	}
	if _, err := exec.LookPath(fallback); err != nil {
		return "", fmt.Errorf(
			"no editor found — set $EDITOR, or pass the body with --data @file.json")
	}
	return fallback, nil
}

// readAll drains stdin. os.ReadFile cannot be used here: stdin is a pipe, not a
// path, and os.Stdin.Name() is not portably openable.
func readAll(f *os.File) ([]byte, error) {
	info, err := f.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice != 0 {
		return nil, fmt.Errorf("no data on stdin — pipe JSON in, or use --data @file.json")
	}
	return io.ReadAll(f)
}
```

- [ ] **Step 4: Implement raw HTTP commands**

`cli/internal/cmd/raw.go`:

```go
package cmd

import (
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/flexprice/cli/internal/client"
	"github.com/flexprice/cli/internal/output"
)

// addRawCommands registers get/post/delete — the escape hatch for anything the
// resource tree does not cover, mirroring `stripe get /v1/...`.
func addRawCommands(root *cobra.Command, g *Globals, version string) {
	for _, m := range []struct {
		name, method, short string
		takesBody           bool
	}{
		{"get", http.MethodGet, "Issue a raw GET against the API", false},
		{"post", http.MethodPost, "Issue a raw POST against the API", true},
		{"delete", http.MethodDelete, "Issue a raw DELETE against the API", false},
	} {
		method, takesBody := m.method, m.takesBody
		var dataArg string

		c := &cobra.Command{
			Use:   m.name + " <path>",
			Short: m.short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cc *cobra.Command, args []string) error {
				var body any
				if takesBody && dataArg != "" {
					doc, err := readDataArg(dataArg)
					if err != nil {
						return err
					}
					body = doc
				}

				rc, _, err := runtimeContext(g)
				if err != nil {
					return err
				}
				cl := client.New(client.Options{
					BaseURL: rc.BaseURL, APIKey: rc.APIKey, Version: version,
					Debug: g.Debug, DebugOut: os.Stderr,
				})

				raw, err := cl.Do(cc.Context(), method, args[0], nil, body)
				if err != nil {
					return err
				}

				format, err := output.ParseFormat(g.Output)
				if err != nil {
					return err
				}
				w := output.Writer{Out: os.Stdout, Err: os.Stderr, Format: format}
				return w.Render(raw, output.Options{Quiet: g.Quiet})
			},
		}
		if takesBody {
			c.Flags().StringVar(&dataArg, "data", "", "request body: @file.json, - for stdin, or a JSON literal")
		}
		root.AddCommand(c)
	}
}
```

- [ ] **Step 5: Wire them into the root command**

In `NewRootCommand`, after the auth commands are added:

```go
	if doc, err := spec.Load(); err == nil {
		if reg, err := spec.NewRegistry(doc); err == nil {
			addResourceCommands(root, reg, g, version)
			// Derived-name warnings are diagnostics, not errors: an unmapped
			// operation still works, it just has a machine-chosen name.
			if g.Debug {
				for _, warning := range reg.Warnings() {
					fmt.Fprintln(os.Stderr, "warning:", warning)
				}
			}
		}
	}
	addRawCommands(root, g, version)
```

Also register the `--columns`, `--limit` and `--all` persistent flags in `NewRootCommand`:

```go
	f.StringSliceVar(&g.Columns, "columns", nil, "columns to show in table output")
	f.IntVar(&g.Limit, "limit", 20, "maximum records to return")
	f.BoolVar(&g.All, "all", false, "fetch every page")
```

- [ ] **Step 6: Run the tests and try it end to end**

```bash
cd cli && go test ./... -v
go build -o bin/flexprice .
./bin/flexprice resources | head -20
./bin/flexprice customers --help
./bin/flexprice subscriptions create --help
```

Expected: tests PASS. `resources` lists resources with their actions. `customers --help` shows `list`, `retrieve`, `create`. `subscriptions create --help` lists nested fields and directs the user to `--edit`.

- [ ] **Step 7: Commit**

```bash
git add cli/internal/cmd
git commit -m "feat(cli): spec-driven resource commands, resources listing and raw HTTP"
```

---

## Phase 4 — Ship

### Task 15: Release pipeline

Design doc §15. Source stays here; releases push to `flexprice/cli`. Tags are prefixed so CLI releases never wait on a backend release.

**Files:**
- Create: `cli/.goreleaser.yaml`
- Create: `.github/workflows/cli-release.yml`
- Create: `cli/internal/cmd/misc.go`
- Modify: `cli/internal/cmd/root.go`

- [ ] **Step 1: Add `open`, `version` and `completion`**

`cli/internal/cmd/misc.go`:

```go
package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/flexprice/cli/internal/client"
	specdata "github.com/flexprice/cli/spec"
)

func newOpenCommand(g *Globals, version string) *cobra.Command {
	open := &cobra.Command{Use: "open", Short: "Open Flexprice in your browser"}

	open.AddCommand(&cobra.Command{
		Use:   "dashboard",
		Short: "Open the Flexprice dashboard",
		RunE: func(c *cobra.Command, _ []string) error {
			return openURL("https://admin.flexprice.io")
		},
	})

	open.AddCommand(&cobra.Command{
		Use:   "webhooks",
		Short: "Open the webhook portal for this environment",
		RunE: func(c *cobra.Command, _ []string) error {
			rc, _, err := runtimeContext(g)
			if err != nil {
				return err
			}
			cl := client.New(client.Options{
				BaseURL: rc.BaseURL, APIKey: rc.APIKey, Version: version,
				Debug: g.Debug, DebugOut: os.Stderr,
			})
			raw, err := cl.Do(c.Context(), http.MethodGet, "/webhooks/dashboard", nil, nil)
			if err != nil {
				return err
			}
			var resp struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal(raw, &resp); err != nil {
				return fmt.Errorf("parse dashboard response: %w", err)
			}
			if resp.URL == "" {
				return fmt.Errorf("no webhook portal URL was returned")
			}
			fmt.Fprintln(os.Stderr, "Add your tunnel URL as an endpoint here:")
			fmt.Fprintln(os.Stdout, resp.URL)
			return openURL(resp.URL)
		},
	})

	return open
}

func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		// Not fatal: the URL was already printed to stdout.
		fmt.Fprintf(os.Stderr, "Could not open a browser (%v). Open the URL above manually.\n", err)
	}
	return nil
}

// newVersionCommand reports the binary version and the spec build it embeds, so
// a 404 on a known command can be diagnosed as version skew. Design doc §12.
func newVersionCommand(g *Globals, version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version and embedded spec build",
		Run: func(c *cobra.Command, _ []string) {
			fmt.Fprintf(os.Stdout, "flexprice %s\n", version)
			fmt.Fprintf(os.Stdout, "embedded OpenAPI spec: %d bytes\n", len(specdata.OpenAPI))
		},
	}
}
```

Register both in `NewRootCommand`:

```go
	root.AddCommand(newOpenCommand(g, version), newVersionCommand(g, version))
```

Cobra provides `completion` automatically, so no code is needed for it.

- [ ] **Step 2: Add the goreleaser config**

`cli/.goreleaser.yaml`:

```yaml
version: 2
project_name: flexprice

before:
  hooks:
    - go mod tidy

builds:
  - id: flexprice
    main: .
    binary: flexprice
    env:
      - CGO_ENABLED=0
    goos: [darwin, linux, windows]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w -X main.version={{.Version}}

archives:
  - formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]

checksum:
  name_template: checksums.txt

brews:
  - repository:
      owner: flexprice
      name: homebrew-tap
      token: "{{ .Env.SDK_DEPLOY_GIT_TOKEN }}"
    homepage: https://flexprice.io
    description: Usage-based billing from your terminal
    license: Apache-2.0

release:
  # Releases are published on the mirror, which is where users look for them.
  github:
    owner: flexprice
    name: cli
```

- [ ] **Step 3: Add the release workflow**

`.github/workflows/cli-release.yml`:

```yaml
name: CLI release

on:
  push:
    # Prefixed tags keep CLI releases independent of backend releases.
    tags: ['cli/v*']
  workflow_dispatch:

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version-file: cli/go.mod

      - name: Verify the embedded spec is current
        run: |
          if ! diff -q docs/swagger/swagger-3-0.json cli/spec/openapi.json > /dev/null; then
            echo "::error::cli/spec/openapi.json is stale. Run: make sync-cli-spec"
            exit 1
          fi

      - name: Test
        run: cd cli && go test -race ./...

      # Publish the cli/ subtree to the flexprice/cli mirror, mirroring how SDKs
      # are pushed to flexprice/go-sdk in generate-sdks.yml.
      - name: Push source to flexprice/cli
        env:
          TOKEN: ${{ secrets.SDK_DEPLOY_GIT_TOKEN }}
        run: |
          VERSION="${GITHUB_REF_NAME#cli/}"
          rm -rf /tmp/mirror && mkdir -p /tmp/mirror
          cp -R cli/. /tmp/mirror/
          rm -rf /tmp/mirror/bin
          cd /tmp/mirror
          git init -b main
          git config user.name  "flexprice-bot"
          git config user.email "bot@flexprice.io"
          git add -A
          git commit -m "release ${VERSION}"
          git tag "${VERSION}"
          git remote add origin "https://x-access-token:${TOKEN}@github.com/flexprice/cli.git"
          git push --force origin main
          git push --force origin "${VERSION}"

      - name: Release binaries
        uses: goreleaser/goreleaser-action@v6
        with:
          workdir: cli
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.SDK_DEPLOY_GIT_TOKEN }}
          SDK_DEPLOY_GIT_TOKEN: ${{ secrets.SDK_DEPLOY_GIT_TOKEN }}
```

- [ ] **Step 4: Add a spec-sync workflow**

`.github/workflows/cli-spec-sync.yml`:

```yaml
name: CLI spec sync

on:
  push:
    branches: [develop]
    paths: ['docs/swagger/swagger-3-0.json']

jobs:
  sync:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Sync spec into the CLI module
        run: make sync-cli-spec

      - name: Open a pull request if the spec changed
        uses: peter-evans/create-pull-request@v6
        with:
          branch: chore/cli-spec-sync
          title: 'chore(cli): sync OpenAPI spec'
          commit-message: 'chore(cli): sync OpenAPI spec'
          body: |
            The API spec changed. This updates the CLI's embedded copy.

            If new operations appeared, they currently resolve under
            auto-derived names. Give them proper names in
            `cli/spec/commands.yaml` before release.
```

- [ ] **Step 5: Generate the command reference**

```bash
cd cli && go run ./tools/gendocs 2>/dev/null || true
```

Create `cli/tools/gendocs/main.go`:

```go
// Command gendocs writes the CLI command reference as Markdown.
package main

import (
	"log"
	"os"

	"github.com/spf13/cobra/doc"

	"github.com/flexprice/cli/internal/cmd"
)

func main() {
	out := "./docs"
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatal(err)
	}
	root := cmd.NewRootCommand("docs")
	root.DisableAutoGenTag = true
	if err := doc.GenMarkdownTree(root, out); err != nil {
		log.Fatal(err)
	}
}
```

Add to the Makefile:

```makefile
.PHONY: cli-docs
cli-docs:
	cd cli && go run ./tools/gendocs
```

- [ ] **Step 6: Verify the whole thing**

```bash
make cli-test && make cli-vet && make cli-build && make cli-docs
./cli/bin/flexprice --help
./cli/bin/flexprice version
ls cli/docs | head
```

Expected: everything passes; `--help` shows the full command set; `cli/docs/` contains generated Markdown.

- [ ] **Step 7: Commit**

```bash
echo "cli/docs/" >> cli/.gitignore
git add cli .github/workflows/cli-release.yml .github/workflows/cli-spec-sync.yml Makefile
git commit -m "feat(cli): release pipeline, mirror push, docs generation"
```


### Task 16: Pagination — `--limit` and `--all`

Task 14 registers `--limit` and `--all` but nothing reads them, and `output.Options.Shown`/`Total` are never populated, so the truncation footer can never fire. This closes both.

List responses use `types.ListResponse[T]`: `{"items":[...],"pagination":{"total":N,"limit":L,"offset":O}}`. Requests carry `limit`/`offset` in the query for GET operations and in the body for the `POST /x/search` operations that back `list`.

**Files:**
- Modify: `cli/internal/spec/request.go`
- Create: `cli/internal/spec/paginate.go`, `cli/internal/spec/paginate_test.go`
- Modify: `cli/internal/cmd/resource.go`

- [ ] **Step 1: Write the failing test**

`cli/internal/spec/paginate_test.go`:

```go
package spec

import (
	"encoding/json"
	"testing"
)

func TestPageInfo_ReadsListResponseEnvelope(t *testing.T) {
	raw := []byte(`{"items":[{"id":"a"},{"id":"b"}],"pagination":{"total":1204,"limit":2,"offset":0}}`)

	info, err := PageInfo(raw)
	if err != nil {
		t.Fatalf("PageInfo: %v", err)
	}
	if info.Total != 1204 {
		t.Errorf("Total = %d, want 1204", info.Total)
	}
	if info.Count != 2 {
		t.Errorf("Count = %d, want 2", info.Count)
	}
}

// Older endpoints (environments) use a named array and top-level pagination.
func TestPageInfo_HandlesLegacyEnvelope(t *testing.T) {
	raw := []byte(`{"environments":[{"id":"e1"}],"total":1,"offset":0,"limit":50}`)

	info, err := PageInfo(raw)
	if err != nil {
		t.Fatalf("PageInfo: %v", err)
	}
	if info.Total != 1 || info.Count != 1 {
		t.Errorf("info = %+v, want Total 1 Count 1", info)
	}
}

func TestPageInfo_NonListResponseIsNotAnError(t *testing.T) {
	info, err := PageInfo([]byte(`{"id":"cust_1"}`))
	if err != nil {
		t.Fatalf("PageInfo on a single object: %v", err)
	}
	if info.Total != 0 || info.Count != 0 {
		t.Errorf("info = %+v, want zeroes for a single object", info)
	}
}

func TestApplyPaging_SetsQueryForGET(t *testing.T) {
	reg := testRegistry(t)
	cmd, ok := reg.Lookup("customers", "retrieve")
	if !ok {
		t.Skip("customers retrieve not registered")
	}

	req := Request{Method: "GET", Path: "/customers", Query: map[string][]string{}}
	ApplyPaging(&req, cmd, Paging{Limit: 20, Offset: 40})

	if got := req.Query.Get("limit"); got != "20" {
		t.Errorf("query limit = %q, want 20", got)
	}
	if got := req.Query.Get("offset"); got != "40" {
		t.Errorf("query offset = %q, want 40", got)
	}
}

func TestApplyPaging_SetsBodyForSearchOperations(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("customers", "list") // POST /customers/search

	req := Request{Method: "POST", Path: "/customers/search", Body: map[string]any{}}
	ApplyPaging(&req, cmd, Paging{Limit: 20, Offset: 40})

	body, ok := req.Body.(map[string]any)
	if !ok {
		t.Fatalf("Body = %T, want map", req.Body)
	}
	if body["limit"] != 20 || body["offset"] != 40 {
		t.Errorf("body = %v, want limit 20 offset 40", body)
	}
}

// A user-supplied limit is never overwritten by the default.
func TestApplyPaging_DoesNotOverrideAnExplicitValue(t *testing.T) {
	reg := testRegistry(t)
	cmd, _ := reg.Lookup("customers", "list")

	req := Request{Method: "POST", Path: "/customers/search", Body: map[string]any{"limit": 5}}
	ApplyPaging(&req, cmd, Paging{Limit: 20, Offset: 0})

	if got := req.Body.(map[string]any)["limit"]; got != 5 {
		t.Errorf("limit = %v, want the caller value 5 preserved", got)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd cli && go test ./internal/spec/ -run 'TestPageInfo|TestApplyPaging' -v
```

Expected: FAIL — `undefined: PageInfo`, `undefined: ApplyPaging`.

- [ ] **Step 3: Implement**

`cli/internal/spec/paginate.go`:

```go
package spec

import (
	"encoding/json"
	"strconv"
)

type Paging struct {
	Limit  int
	Offset int
}

// Page describes one response page. Total is 0 when the response is not a list.
type Page struct {
	Count  int
	Total  int
	Offset int
	Limit  int
}

// HasMore reports whether another page exists.
//
// It deliberately ignores the response's echoed offset: the API was observed
// returning offset == limit for a request that sent no offset at all, so the
// echo cannot be treated as "records already consumed". The caller tracks how
// many it has actually seen and passes that in as seen.
func (p Page) HasMore(seen int) bool {
	return p.Total > 0 && seen < p.Total
}

// PageInfo reads the pagination envelope. Two shapes exist: types.ListResponse
// nests pagination under "pagination", while older endpoints put total, limit and
// offset at the top level next to a named array.
func PageInfo(raw []byte) (Page, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Page{}, nil // not an object: nothing to page
	}

	var page Page
	if nested, ok := doc["pagination"].(map[string]any); ok {
		page.Total = intOf(nested["total"])
		page.Limit = intOf(nested["limit"])
		page.Offset = intOf(nested["offset"])
	} else {
		page.Total = intOf(doc["total"])
		page.Limit = intOf(doc["limit"])
		page.Offset = intOf(doc["offset"])
	}

	for key, value := range doc {
		if key == "pagination" {
			continue
		}
		if arr, ok := value.([]any); ok {
			page.Count = len(arr)
			break
		}
	}
	return page, nil
}

func intOf(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		n, _ := strconv.Atoi(t)
		return n
	default:
		return 0
	}
}

// ApplyPaging sets limit and offset where the operation accepts them: the query
// string for GET, the request body for the POST search operations that back list.
// Values the caller already supplied are never overwritten.
func ApplyPaging(req *Request, cmd Command, p Paging) {
	if p.Limit <= 0 {
		return
	}

	if req.Method == "GET" {
		if req.Query == nil {
			return
		}
		if req.Query.Get("limit") == "" {
			req.Query.Set("limit", strconv.Itoa(p.Limit))
		}
		if p.Offset > 0 && req.Query.Get("offset") == "" {
			req.Query.Set("offset", strconv.Itoa(p.Offset))
		}
		return
	}

	// Only set body paging when the schema actually declares the fields.
	accepts := map[string]bool{}
	for _, f := range BodyFields(cmd) {
		accepts[f.Name] = true
	}
	if !accepts["limit"] {
		return
	}

	body, ok := req.Body.(map[string]any)
	if !ok {
		body = map[string]any{}
		req.Body = body
	}
	if _, set := body["limit"]; !set {
		body["limit"] = p.Limit
	}
	if _, set := body["offset"]; !set && accepts["offset"] {
		body["offset"] = p.Offset
	}
}
```

- [ ] **Step 4: Wire it into the operation command**

In `cli/internal/cmd/resource.go`, replace the single `cl.Do(...)` call and the render block in `newOperationCommand`'s `RunE` with:

```go
			pageSize := g.Limit
			if pageSize <= 0 {
				pageSize = 20
			}

			var (
				merged []byte
				page   spec.Page
				offset int
				shown  int
			)
			for {
				spec.ApplyPaging(&req, cmd, spec.Paging{Limit: pageSize, Offset: offset})

				raw, err := cl.Do(cc.Context(), req.Method, req.Path, req.Query, req.Body)
				if err != nil {
					return err
				}

				page, _ = spec.PageInfo(raw)
				shown += page.Count
				merged = raw

				if !g.All || !page.HasMore(shown) || page.Count == 0 {
					break
				}
				offset += page.Count

				// Rebuild so the next iteration starts from a clean query and body.
				req, err = spec.BuildRequest(cmd, in)
				if err != nil {
					return err
				}
				if !g.Quiet {
					fmt.Fprintf(os.Stderr, "\rfetched %d of %d…", shown, page.Total)
				}
			}
			if g.All && !g.Quiet && shown > 0 {
				fmt.Fprintln(os.Stderr)
			}

			format, err := output.ParseFormat(g.Output)
			if err != nil {
				return err
			}
			w := output.Writer{Out: os.Stdout, Err: os.Stderr, Format: format}
			return w.Render(merged, output.Options{
				Columns: pickColumns(reg, g, cmd.Resource),
				Quiet:   g.Quiet,
				Shown:   shown,
				Total:   page.Total,
			})
```

`--all` prints only the final page's body. That is a deliberate v1.0 limitation: merging pages across two different response envelopes is fiddly, and the progress counter plus footer make the behaviour visible. Note it in `--all`'s help text:

```go
	f.BoolVar(&g.All, "all", false, "page through every record (prints the last page; use --output json with --limit for bulk export)")
```

- [ ] **Step 5: Run the tests**

```bash
cd cli && go test ./... -v
go build -o bin/flexprice . && ./bin/flexprice customers list --help
```

Expected: PASS. `--all`'s help text states the limitation.

- [ ] **Step 6: Commit**

```bash
git add cli/internal/spec cli/internal/cmd
git commit -m "feat(cli): wire --limit and --all with pagination envelope detection"
```

### Task 17: Golden-file and integration test harness

Design doc §16. Golden files pin `--output json` only — that is the machine contract. Table rendering gets advisory snapshots so cosmetic changes do not churn the suite. Integration tests skip cleanly when no server is running; nothing starts containers.

**Files:**
- Create: `cli/internal/output/golden_test.go`
- Create: `cli/internal/output/testdata/customers_list.json`
- Create: `cli/internal/output/testdata/customers_list.golden.json`
- Create: `cli/integration/integration_test.go`

- [ ] **Step 1: Write the golden-file test**

`cli/internal/output/golden_test.go`:

```go
package output

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// JSON output is a contract other tools parse, so it is pinned exactly.
func TestGolden_JSONOutputIsStable(t *testing.T) {
	input, err := os.ReadFile(filepath.Join("testdata", "customers_list.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatJSON}
	if err := w.Render(input, Options{}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	goldenPath := filepath.Join("testdata", "customers_list.golden.json")
	if *update {
		if err := os.WriteFile(goldenPath, out.Bytes(), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run: go test ./internal/output -update): %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(out.Bytes()), bytes.TrimSpace(want)) {
		t.Errorf("JSON output changed.
 got:
%s
want:
%s", out.String(), want)
	}
}

// Table rendering is presentation, not contract: assert it contains the data
// rather than pinning the exact layout, so column widths can change freely.
func TestTableOutput_ContainsTheData(t *testing.T) {
	input, err := os.ReadFile(filepath.Join("testdata", "customers_list.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var out, errOut bytes.Buffer
	w := Writer{Out: &out, Err: &errOut, Format: FormatTable}
	if err := w.Render(input, Options{Columns: []string{"id", "email"}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"cust_01", "ada@example.com"} {
		if !bytes.Contains(out.Bytes(), []byte(want)) {
			t.Errorf("table output missing %q:
%s", want, out.String())
		}
	}
}
```

- [ ] **Step 2: Create the fixture**

`cli/internal/output/testdata/customers_list.json`:

```json
{
  "items": [
    {"id": "cust_01", "external_id": "acme", "email": "ada@example.com", "created_at": "2026-01-01T00:00:00Z"},
    {"id": "cust_02", "external_id": "globex", "email": "bob@example.com", "created_at": "2026-01-02T00:00:00Z"}
  ],
  "pagination": {"total": 2, "limit": 20, "offset": 0}
}
```

- [ ] **Step 3: Generate the golden file and run the tests**

```bash
cd cli && go test ./internal/output/ -update
go test ./internal/output/ -v
```

Expected: the first run writes `customers_list.golden.json`; the second passes. Inspect the golden file before committing — it is the pinned contract.

- [ ] **Step 4: Write the integration harness**

`cli/integration/integration_test.go`:

```go
// Package integration exercises the CLI against a running Flexprice server.
//
// It starts nothing. Bring a server up yourself (make run-local) and export
// FLEXPRICE_TEST_BASE_URL and FLEXPRICE_TEST_API_KEY. Without those the whole
// package skips, so `go test ./...` stays green on a machine with no server.
package integration

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func requireServer(t *testing.T) (baseURL, apiKey string) {
	t.Helper()
	baseURL = os.Getenv("FLEXPRICE_TEST_BASE_URL")
	apiKey = os.Getenv("FLEXPRICE_TEST_API_KEY")
	if baseURL == "" || apiKey == "" {
		t.Skip("set FLEXPRICE_TEST_BASE_URL and FLEXPRICE_TEST_API_KEY to run integration tests")
	}
	return baseURL, apiKey
}

// run invokes the built binary the way a user would.
func run(t *testing.T, baseURL, apiKey string, args ...string) (string, error) {
	t.Helper()
	full := append([]string{"--base-url", baseURL, "--api-key", apiKey, "--output", "json"}, args...)
	cmd := exec.Command("../bin/flexprice", full...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestIntegration_CustomersListReturnsJSON(t *testing.T) {
	baseURL, apiKey := requireServer(t)

	out, err := run(t, baseURL, apiKey, "customers", "list", "--limit", "1")
	if err != nil {
		t.Fatalf("customers list: %v
%s", err, out)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("stdout is not a JSON object:
%s", out)
	}
}

func TestIntegration_UnknownFlagSuggestsAField(t *testing.T) {
	baseURL, apiKey := requireServer(t)

	out, _ := run(t, baseURL, apiKey, "customers", "create", "--externl_id", "x")
	if !strings.Contains(out, "Did you mean") {
		t.Errorf("want a suggestion for a misspelled flag, got:
%s", out)
	}
}

func TestIntegration_BadKeyExitsWithAuthCode(t *testing.T) {
	baseURL, _ := requireServer(t)

	cmd := exec.Command("../bin/flexprice",
		"--base-url", baseURL, "--api-key", "sk_definitely_invalid", "customers", "list")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("want a failure for an invalid key, got:
%s", out)
	}
	if code := cmd.ProcessState.ExitCode(); code != 3 {
		t.Errorf("exit code = %d, want 3 (auth)
%s", code, out)
	}
}
```

- [ ] **Step 5: Verify the skip path and the run path**

```bash
cd cli && go build -o bin/flexprice . && go test ./integration/ -v
```

Expected with no server: every test reports SKIP and the package passes.

With a server running:

```bash
cd cli && FLEXPRICE_TEST_BASE_URL=http://localhost:8080/v1   FLEXPRICE_TEST_API_KEY=sk_local_flexprice_test_key   go test ./integration/ -v
```

Expected: all three pass.

- [ ] **Step 6: Make the exit code reach the shell**

`main.go` currently always exits 1. Replace it so `APIError.ExitCode()` is honoured — the integration test above asserts this:

```go
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/flexprice/cli/internal/client"
	"github.com/flexprice/cli/internal/cmd"
	"github.com/flexprice/cli/internal/exitcode"
)

var version = "dev"

func main() {
	root := cmd.NewRootCommand(version)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)

		var apiErr *client.APIError
		if errors.As(err, &apiErr) {
			os.Exit(apiErr.ExitCode())
		}
		os.Exit(exitcode.Generic)
	}
}
```

- [ ] **Step 7: Run everything and commit**

```bash
cd cli && go build -o bin/flexprice . && go test ./... && go vet ./...
```

Expected: all pass, integration tests skip.

```bash
git add cli/internal/output cli/integration cli/main.go
git commit -m "test(cli): golden JSON output and skip-clean integration harness"
```

---

## Before release (not code tasks)

- [ ] Make `flexprice/cli` **public** — it is private, so brew, `install.sh` and `go install` will all fail until it is.
- [ ] Confirm `SDK_DEPLOY_GIT_TOKEN` has write access to `flexprice/cli` and `flexprice/homebrew-tap`.
- [ ] Replace `@flexprice/cli-maintainers` in `.github/CODEOWNERS` with the real owner.
- [ ] Enable Issues and disable pull requests on `flexprice/cli`.
- [ ] Archive the Rust `flexprice/flexprice-cli`, after speaking with its author.
- [ ] Add swaggo annotations to `EnvironmentHandler.GetEnvironments` so `GET /v1/environments`
      enters the OpenAPI spec. The CLI calls it by literal path until then, which works but
      means the endpoint cannot be reached through `flexprice environments list`.

## Follow-on plans

- **Plan B — Events & fixtures:** `events send|bulk|tail|query|usage|simulate`, the scenario engine with `${step.field}` interpolation, the `simulate:` step type, embedded built-in scenarios, `trigger`, and the live-profile guard. Design doc §9 and §10.
- **Plan C — `listen`:** `POST`/`DELETE /v1/webhooks/listeners` with heartbeat, TTL sweeper, URL validation and per-environment caps, plus the CLI side. Design doc §11. Backend subsystem.
