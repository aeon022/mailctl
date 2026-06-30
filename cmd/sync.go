package cmd

import (
	"context"
	"fmt"

	"github.com/aeon022/mailctl/internal/config"
	"github.com/aeon022/mailctl/internal/mail"
	"github.com/aeon022/mailctl/internal/store"
	"github.com/spf13/cobra"
)

var syncCount int

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync inbox from Apple Mail into local cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		msgs, err := mail.FetchInbox(syncCount, false)
		if err != nil {
			return fmt.Errorf("fetch: %w", err)
		}

		s, err := store.New(config.DBPath())
		if err != nil {
			return err
		}
		defer s.Close()
		ctx := context.Background()

		_ = s.DeleteBySource(ctx, "apple")
		for i := range msgs {
			_ = s.UpsertMessage(ctx, &msgs[i])
		}

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
	syncCmd.Flags().IntVar(&syncCount, "count", 100, "Messages to sync per account")
	rootCmd.AddCommand(syncCmd)
}
