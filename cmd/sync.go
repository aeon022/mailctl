package cmd

import (
	"fmt"
	"time"

	"github.com/aeon022/mailctl/internal/config"
	"github.com/aeon022/mailctl/internal/mail"
	"github.com/aeon022/missionctl-core/lastsync"
	"github.com/spf13/cobra"
)

var syncCount int

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync inbox from Apple Mail into local cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		msgs, _, err := mail.Sync(syncCount)
		if err != nil {
			return fmt.Errorf("fetch: %w", err)
		}
		_ = lastsync.Save(config.LastSyncedPath(), time.Now())

		if isJSON() {
			outputJSON(map[string]any{
				"tool":   "mailctl",
				"synced": len(msgs),
			})
			return nil
		}
		fmt.Printf("Synced %d messages\n", len(msgs))
		return nil
	},
}

func init() {
	syncCmd.Flags().IntVar(&syncCount, "count", 50, "Messages to sync per account")
	rootCmd.AddCommand(syncCmd)
}
