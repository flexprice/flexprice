package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// ManualItem describes a call site that needs human review.
type ManualItem struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Method string `json:"method"`
	Reason string `json:"reason"`
}

func writeReport(items []ManualItem, outFile string) error {
	if len(items) == 0 {
		fmt.Println("No manual review items.")
		return nil
	}
	fmt.Printf("\n%d items need manual review:\n", len(items))
	for _, item := range items {
		fmt.Printf("  %s:%d [%s] — %s\n", item.File, item.Line, item.Method, item.Reason)
	}
	if outFile != "" {
		data, _ := json.MarshalIndent(items, "", "  ")
		return os.WriteFile(outFile, data, 0644)
	}
	return nil
}
