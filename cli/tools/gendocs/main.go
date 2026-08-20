// Command gendocs writes the CLI command reference as Markdown.
// TODO: not wired into cli-release.yml; runs only via `make cli-docs`.
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
