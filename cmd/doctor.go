package cmd

import (
	"os"
	"runtime"

	"github.com/aeon022/mailctl/internal/config"
	"github.com/aeon022/mailctl/internal/mail"
	"github.com/aeon022/missionctl-core/doctor"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check config, database, and mail client health",
	Run: func(cmd *cobra.Command, args []string) {
		checks := []doctor.Check{
			doctor.CheckSQLite("Database", config.DBPath(), "messages"),
			doctor.CheckDataDir("Data directory", config.DBPath(), config.Shared()),
		}
		if runtime.GOOS == "linux" {
			if dir, ok := mail.ThunderbirdProfileFound(); ok {
				checks = append(checks, doctor.Check{Label: "Thunderbird profile", OK: true, Detail: dir})
			} else {
				checks = append(checks, doctor.Check{Label: "Thunderbird profile", OK: false, Detail: "no default profile found under ~/.thunderbird"})
			}
		} else {
			checks = append(checks, doctor.CheckAppleApp("Mail.app", "Mail"))
		}
		if !doctor.PrintReport(checks) {
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
