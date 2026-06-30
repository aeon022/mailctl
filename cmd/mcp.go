package cmd

import (
	"github.com/aeon022/mailctl/internal/mcpserver"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the mailctl MCP server (stdio)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return mcpserver.Serve()
	},
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}
