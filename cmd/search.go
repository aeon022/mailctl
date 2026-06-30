package cmd

import (
	"context"
	"fmt"

	"github.com/aeon022/mailctl/internal/config"
	"github.com/aeon022/mailctl/internal/mail"
	"github.com/aeon022/mailctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	searchCount int
	searchLive  bool
)

var searchCmd = &cobra.Command{
	Use:     "search <query>",
	Short:   "Search emails by subject, sender, or body",
	Example: `  mailctl search "invoice"`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := args[0]

		if searchLive {
			msgs, err := mail.SearchMessages(query, searchCount)
			if err != nil {
				return err
			}
			return printMessages(msgs)
		}

		s, err := store.New(config.DBPath())
		if err != nil {
			return err
		}
		defer s.Close()
		msgs, err := s.ListMessages(context.Background(), store.Filter{
			Query: query,
			Limit: searchCount,
		})
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			fmt.Printf("No results for %q — try --live for a fresh search\n", query)
			return nil
		}
		return printMessages(msgs)
	},
}

func init() {
	searchCmd.Flags().IntVar(&searchCount, "count", 20, "Max results")
	searchCmd.Flags().BoolVar(&searchLive, "live", false, "Search Apple Mail directly")
	rootCmd.AddCommand(searchCmd)
}
