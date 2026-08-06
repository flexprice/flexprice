package main

import (
	"log"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "migrate",
		Short: "FlexPrice migration tool",
	}

	root.AddCommand(newPostgresCmd())
	root.AddCommand(newClickHouseCmd())
	root.AddCommand(newKafkaCmd())
	root.AddCommand(newSvixCmd())

	if err := root.Execute(); err != nil {
		log.Fatal(err)
	}
}
