package typst

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCompileFailureRemovesTempOutput(t *testing.T) {
	falsePath, err := exec.LookPath("false")
	if err != nil {
		t.Skip("false binary not available")
	}
	log, err := logger.NewLogger(config.GetDefaultConfig())
	require.NoError(t, err)

	dir := t.TempDir()
	c := NewCompiler(log, falsePath, "", "", dir)

	_, err = c.Compile(CompileOpts{InputFile: "in.typ"})
	require.Error(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)

	tmplDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmplDir, "x"), []byte("x"), 0o600))
	ct := NewCompiler(log, falsePath, "", tmplDir, dir)
	_, err = ct.CompileTemplate(types.TemplateName("x"), []byte("{}"))
	require.Error(t, err)
	entries, err = os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)

	supplied := filepath.Join(dir, "keep.pdf")
	require.NoError(t, os.WriteFile(supplied, []byte("x"), 0o600))
	_, err = c.Compile(CompileOpts{InputFile: "in.typ", OutputFile: "keep.pdf"})
	require.Error(t, err)
	require.FileExists(t, supplied)
}

func TestCompileUsesUniqueOutputFiles(t *testing.T) {
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true binary not available")
	}
	log, err := logger.NewLogger(config.GetDefaultConfig())
	require.NoError(t, err)

	c := NewCompiler(log, truePath, "", "", t.TempDir())

	const renders = 50
	paths := make([]string, renders)
	var wg sync.WaitGroup
	for i := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, cerr := c.Compile(CompileOpts{InputFile: "in.typ"})
			if cerr == nil {
				paths[i] = p
			}
		}()
	}
	wg.Wait()

	seen := make(map[string]struct{}, renders)
	for _, p := range paths {
		require.NotEmpty(t, p)
		_, dup := seen[p]
		require.False(t, dup, "duplicate output path %s", p)
		seen[p] = struct{}{}
	}
}
