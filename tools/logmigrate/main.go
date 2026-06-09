package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	dir := flag.String("dir", ".", "directory to rewrite (recursive)")
	reportFile := flag.String("report", "logmigrate-report.json", "output file for manual items")
	dryRun := flag.Bool("dry-run", false, "print what would change without writing")
	flag.Parse()

	if *dryRun {
		fmt.Println("DRY RUN — no files will be modified")
	}

	var allManual []ManualItem
	err := filepath.WalkDir(*dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			// Skip dirs that don't need migration.
			if base == "vendor" || base == ".git" || base == "api" ||
				base == "tools" || base == "ent" || base == "docs" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if *dryRun {
			fmt.Printf("  would process: %s\n", path)
			return nil
		}
		items, err := rewriteFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s: %v\n", path, err)
			return nil
		}
		allManual = append(allManual, items...)
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "walk error: %v\n", err)
		os.Exit(1)
	}
	if err := writeReport(allManual, *reportFile); err != nil {
		fmt.Fprintf(os.Stderr, "report error: %v\n", err)
		os.Exit(1)
	}
}
