//go:build ignore

// Replicates cmd/migrate/postgres.go AutoMigrate against an explicit URL,
// bypassing viper config. POC only.
//   go run -mod=mod poc/atlas/automigrate/main.go "postgres://...?sslmode=disable"
package main

import (
	"context"
	"log"
	"os"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/flexprice/flexprice/ent"
	_ "github.com/lib/pq"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalln("usage: main.go <postgres-url>")
	}
	client, err := ent.Open("postgres", os.Args[1])
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer client.Close()

	// Same skip set as cmd/migrate/postgres.go. Set POC_ALLOW_MODIFY_INDEX=1 to
	// drop the ModifyIndex guard so Ent actually rebuilds changed indexes.
	skip := schema.DropIndex | schema.DropColumn | schema.ModifyIndex
	if os.Getenv("POC_ALLOW_MODIFY_INDEX") == "1" {
		skip = schema.DropIndex | schema.DropColumn
	}
	opts := []schema.MigrateOption{schema.WithSkipChanges(skip)}
	ctx := context.Background()
	if os.Getenv("POC_DRYRUN") == "1" {
		if err := client.Schema.WriteTo(ctx, os.Stdout, opts...); err != nil { log.Fatalf("writeto: %v", err) }
		return
	}
	if err := client.Schema.Create(ctx, opts...); err != nil {
		log.Fatalf("create: %v", err)
	}
	log.Println("automigrate done")
}
