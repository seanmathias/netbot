package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const Version = "0.2.1"

var root = &cobra.Command{
	Use:     "netbot",
	Short:   "Network automation and utility CLI",
	Version: Version,
}

func Execute() {
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	root.AddCommand(backupCmd)
	root.AddCommand(pingCmd)
}
