package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const Version = "0.2.1"

// metaCommands are Cobra's built-in utility sub-commands. They are shown
// after operational commands in the help output, separated by a blank line.
var metaCommands = map[string]bool{
	"completion": true,
	"help":       true,
}

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
	root.SetHelpFunc(rootHelp)
}

func rootHelp(cmd *cobra.Command, _ []string) {
	fmt.Println(cmd.Short)
	fmt.Printf("\nUsage:\n  %s\n", cmd.UseLine())

	// Split sub-commands into operational (user-facing) and meta (built-in).
	var operational, meta []*cobra.Command
	for _, sub := range cmd.Commands() {
		if !sub.IsAvailableCommand() {
			continue
		}
		if metaCommands[sub.Name()] {
			meta = append(meta, sub)
		} else {
			operational = append(operational, sub)
		}
	}

	// Align descriptions to the longest command name across both groups.
	nameWidth := 0
	for _, sub := range append(operational, meta...) {
		if n := len(sub.Name()); n > nameWidth {
			nameWidth = n
		}
	}
	row := fmt.Sprintf("  %%-%ds   %%s\n", nameWidth)

	fmt.Println("\nCommands:")
	for _, sub := range operational {
		fmt.Printf(row, sub.Name(), sub.Short)
	}

	if len(meta) > 0 {
		fmt.Println()
		for _, sub := range meta {
			fmt.Printf(row, sub.Name(), sub.Short)
		}
	}

	fmt.Println("\nFlags:")
	fmt.Print(cmd.Flags().FlagUsages())

	fmt.Printf("\nRun \"%s [command] --help\" for more information about a command.\n", cmd.Name())
}
