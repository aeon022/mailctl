package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/aeon022/mailctl/internal/config"
	"github.com/spf13/cobra"
)

var jsonFlag bool

var rootCmd = &cobra.Command{
	Use:   "mailctl",
	Short: "Email from the terminal — send, draft, read via Apple Mail",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return config.Load()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func isJSON() bool { return jsonFlag }

func outputJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "Output as JSON")
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
