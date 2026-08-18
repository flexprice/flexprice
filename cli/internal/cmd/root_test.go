package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/flexprice/cli/internal/config"
	"github.com/flexprice/cli/internal/style"
)

func TestRootCommand_HasName(t *testing.T) {
	root := NewRootCommand("test")
	if root.Use != "flexprice" {
		t.Fatalf("Use = %q, want %q", root.Use, "flexprice")
	}
}

func TestRootCommand_HelpShowsDescriptionAndFlags(t *testing.T) {
	root := NewRootCommand("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Flexprice API") {
		t.Errorf("help output missing the product description:\n%s", out)
	}
	// Guards the Run stub: cobra omits Flags for a root with no Runnable and
	// no subcommands, so removing it stops help from listing flags.
	if !strings.Contains(out, "--profile") {
		t.Errorf("help output missing the Flags section:\n%s", out)
	}
}

// pflag writes defaults into the bound pointer at registration, so a
// package-level Globals would let a second construction clobber the first.
func TestNewRootCommand_InstancesDoNotShareState(t *testing.T) {
	rootA := NewRootCommand("a")
	rootA.SetOut(&bytes.Buffer{})
	rootA.SetArgs([]string{"--api-key", "secret-from-A", "--profile", "profileA"})
	if err := rootA.Execute(); err != nil {
		t.Fatalf("rootA.Execute: %v", err)
	}

	gotKey, err := rootA.PersistentFlags().GetString("api-key")
	if err != nil {
		t.Fatalf("GetString: %v", err)
	}
	if gotKey != "secret-from-A" {
		t.Fatalf("rootA api-key = %q before second construction, want secret-from-A", gotKey)
	}

	_ = NewRootCommand("b")

	gotKey, err = rootA.PersistentFlags().GetString("api-key")
	if err != nil {
		t.Fatalf("GetString: %v", err)
	}
	if gotKey != "secret-from-A" {
		t.Errorf("rootA api-key = %q after constructing a second root; state leaked", gotKey)
	}
}

// Wired through PersistentPreRunE since flags are not populated until
// Execute() parses them — NewRootCommand runs before that.
func TestNoColorFlag_DisablesStyling(t *testing.T) {
	style.EnableForTests()
	defer style.EnableForTests()

	root := NewRootCommand("test")
	root.SetArgs([]string{"--no-color", "version"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := style.Header("test"); strings.Contains(got, "\x1b[") {
		t.Errorf("style.Header still produces color after --no-color: %q", got)
	}
}

// Bare `flexprice` with no config file shows the welcome banner instead of
// plain help. A scratch HOME keeps this off any real config on the machine.
func TestBareInvocation_NoConfigShowsWelcomeBanner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	root := NewRootCommand("test")
	root.SetArgs([]string{})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(out.String(), "Usage-based billing from your terminal") {
		t.Errorf("bare invocation with no config did not show the welcome banner: %q", out.String())
	}
}

// Once a config file exists, bare invocation reverts to plain help — the
// banner is only for a genuinely fresh install, not every bare invocation.
func TestBareInvocation_WithConfigShowsPlainHelp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(dir+"/.flexprice", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/.flexprice/config.toml", []byte("default_profile = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	root := NewRootCommand("test")
	root.SetArgs([]string{})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if strings.Contains(out.String(), "Usage-based billing from your terminal") {
		t.Errorf("bare invocation with an existing config showed the welcome banner: %q", out.String())
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("bare invocation with an existing config did not show normal help: %q", out.String())
	}
}

func TestStatusLine_IncludesProfileRegionAndVersion(t *testing.T) {
	got := statusLine(config.RuntimeContext{
		ProfileName: "sandbox",
		Profile:     config.Profile{Region: "in"},
	}, "1.2.3")

	for _, want := range []string{"profile: sandbox", "region: in", "v1.2.3"} {
		if !strings.Contains(got, want) {
			t.Errorf("statusLine = %q, missing %q", got, want)
		}
	}
}

// Region and label are optional on a profile (a --base-url login sets neither),
// so the line must not render a dangling separator or an empty field.
func TestStatusLine_OmitsEmptyFields(t *testing.T) {
	got := statusLine(config.RuntimeContext{ProfileName: "default"}, "1.0.0")

	if strings.Contains(got, "region:") {
		t.Errorf("statusLine = %q, should omit region when unset", got)
	}
	if strings.Contains(got, "·  ·") || strings.HasSuffix(got, "·") {
		t.Errorf("statusLine = %q, has a dangling separator", got)
	}
}

func TestStatusLine_IncludesLabelWhenSet(t *testing.T) {
	got := statusLine(config.RuntimeContext{
		ProfileName: "sandbox",
		Profile:     config.Profile{Region: "in", Label: "my sandbox"},
	}, "1.0.0")

	if !strings.Contains(got, "my sandbox") {
		t.Errorf("statusLine = %q, want it to include the profile label", got)
	}
}
