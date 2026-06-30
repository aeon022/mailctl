package cmd

import (
	"github.com/aeon022/mailctl/internal/tui"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Open interactive inbox (default when no command given)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.Run()
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
	// also run TUI when mailctl is called with no subcommand
	rootCmd.RunE = tuiCmd.RunE
}
