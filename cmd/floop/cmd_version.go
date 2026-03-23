package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "version",
		Short:  "Print version information",
		Hidden: true,
		Run: func(cmd *cobra.Command, args []string) {
			jsonOut, _ := cmd.Flags().GetBool("json")
			if jsonOut {
				json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{
					"version": version,
					"commit":  commit,
					"date":    date,
				})
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "floop version %s (commit: %s, built: %s)\n", version, commit, date)
			}
		},
	}
}
