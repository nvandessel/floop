package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			jsonOut, _ := cmd.Flags().GetBool("json")
			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]string{
					"version": version,
					"commit":  commit,
					"date":    date,
				})
			} else {
				fmt.Printf("floop version %s (commit: %s, built: %s)\n", version, commit, date)
			}
		},
	}
}
