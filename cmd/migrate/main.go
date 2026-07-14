package main

import (
	"fmt"
	"log"
	"os"

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
		fmt.Fprintln(os.Stderr, err)
		log.Fatal(err)
	}
}
