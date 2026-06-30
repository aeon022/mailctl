package cmd

import (
	"fmt"

	"github.com/aeon022/mailctl/internal/config"
	"github.com/aeon022/mailctl/internal/mail"
	"github.com/aeon022/mailctl/internal/store"
	"github.com/spf13/cobra"
	"context"
)

var (
	inboxUnread bool
	inboxCount  int
	inboxLive   bool
)

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "List inbox messages",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		if inboxLive {
			// fetch directly from Apple Mail
			msgs, err := mail.FetchInbox(inboxCount, inboxUnread)
			if err != nil {
				return err
			}
			return printMessages(msgs)
		}

		// read from SQLite cache
		s, err := store.New(config.DBPath())
		if err != nil {
			return err
		}
		defer s.Close()
		msgs, err := s.ListMessages(ctx, store.Filter{
			Mailbox:    "INBOX",
			UnreadOnly: inboxUnread,
			Limit:      inboxCount,
		})
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			fmt.Println("No messages in cache — run: mailctl sync")
			return nil
		}
		return printMessages(msgs)
	},
}

func init() {
	inboxCmd.Flags().BoolVar(&inboxUnread, "unread", false, "Only show unread messages")
	inboxCmd.Flags().IntVar(&inboxCount, "count", 20, "Number of messages to show")
	inboxCmd.Flags().BoolVar(&inboxLive, "live", false, "Fetch directly from Apple Mail (slower)")
	rootCmd.AddCommand(inboxCmd)
}
