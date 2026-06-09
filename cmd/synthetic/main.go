package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/flexprice/flexprice/internal/synthetic"
)

func main() {
	cfg, err := synthetic.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	if !cfg.Enabled {
		fmt.Println("SYNTHETIC_ENABLED=false; nothing to do")
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	fmt.Printf("synthetic probe boot: host=%s dry_run=%v checks=%d\n",
		cfg.APIHost, cfg.DryRun, len(cfg.Checks))
	<-ctx.Done()
	fmt.Println("shutdown")
}
