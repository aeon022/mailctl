package cmd

import (
	"context"
	"fmt"

	"github.com/aeon022/mailctl/internal/config"
	"github.com/aeon022/mailctl/internal/mail"
	"github.com/aeon022/mailctl/internal/store"
	"github.com/spf13/cobra"
)

var threadCount int

var threadCmd = &cobra.Command{
	Use:     "thread <subject>",
	Short:   "Show all messages in a thread (matched by subject)",
	Example: `  mailctl thread "Re: Invoice #123"`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		subject := args[0]

		// try SQLite cache first
		s, err := store.New(config.DBPath())
		if err == nil {
			defer s.Close()
			msgs, _ := s.ListMessages(context.Background(), store.Filter{
				Query: subject,
				Limit: threadCount,
			})
			if len(msgs) > 0 {
				return printMessages(msgs)
			}
		}

		// fall back to live Apple Mail search
		fmt.Printf("Not in cache — searching Apple Mail for %q...\n", subject)
		msgs, err := mail.FetchThread(subject, threadCount)
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			fmt.Println("No messages found.")
			return nil
		}
		return printMessages(msgs)
	},
}

func init() {
	threadCmd.Flags().IntVar(&threadCount, "count", 50, "Max messages to show")
	rootCmd.AddCommand(threadCmd)
}
