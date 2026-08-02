package cmd

import (
	"os"

	"github.com/aeon022/mailctl/internal/config"
	"github.com/aeon022/missionctl-core/doctor"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check config, database, and Mail.app health",
	Run: func(cmd *cobra.Command, args []string) {
		checks := []doctor.Check{
			doctor.CheckSQLite("Database", config.DBPath(), "messages"),
			doctor.CheckDataDir("Data directory", config.DBPath(), config.Shared()),
			doctor.CheckAppleApp("Mail.app", "Mail"),
		}
		if !doctor.PrintReport(checks) {
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
