// Command gendocs writes the CLI command reference as Markdown.
//
// TODO: not currently wired into cli-release.yml — it only runs when invoked
// by hand via `make cli-docs`. Check with the team before adding an automated
// call on release; revisit this deliberately rather than assuming it should
// just be hooked in.
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
