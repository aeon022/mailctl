package cmd

import (
	"fmt"

	"github.com/aeon022/mailctl/internal/mail"
	"github.com/spf13/cobra"
)

var accountsCmd = &cobra.Command{
	Use:   "accounts",
	Short: "List all Apple Mail accounts",
	RunE: func(cmd *cobra.Command, args []string) error {
		accounts, err := mail.ListAccounts()
		if err != nil {
			return err
		}
		if isJSON() {
			outputJSON(accounts)
			return nil
		}
		for _, a := range accounts {
			fmt.Println(a)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(accountsCmd)
}
