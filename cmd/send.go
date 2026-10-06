package cmd

import (
	"fmt"
	"github.com/aeon022/mailctl/internal/actlog"

	"github.com/aeon022/mailctl/internal/mail"
	"github.com/aeon022/mailctl/internal/markdown"
	"github.com/spf13/cobra"
)

// sendMail is a variable so tests never reach a mail client or SMTP server.
var sendMail = mail.Send

var sendCmd = &cobra.Command{
	Use:     "send <draft.md>",
	Short:   "Send an email from a Markdown file",
	Example: `  mailctl send email.md`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		draft, err := markdown.ParseFile(args[0])
		if err != nil {
			return err
		}
		if err := sendMail(draft); err != nil {
			return fmt.Errorf("send: %w", err)
		}
		actlog.Sent(draft.Subject)
		if isJSON() {
			outputJSON(map[string]any{
				"status":  "sent",
				"to":      draft.To,
				"subject": draft.Subject,
			})
			return nil
		}
		fmt.Printf("Sent: %q → %v\n", draft.Subject, draft.To)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(sendCmd)
}
