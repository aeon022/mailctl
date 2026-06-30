package cmd

import (
	"fmt"

	"github.com/aeon022/mailctl/internal/mail"
	"github.com/aeon022/mailctl/internal/markdown"
	"github.com/spf13/cobra"
)

var draftCmd = &cobra.Command{
	Use:     "draft <draft.md>",
	Short:   "Save an email to Apple Mail Drafts folder",
	Example: `  mailctl draft email.md`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		draft, err := markdown.ParseFile(args[0])
		if err != nil {
			return err
		}
		if err := mail.SaveDraft(draft); err != nil {
			return fmt.Errorf("save draft: %w", err)
		}
		if isJSON() {
			outputJSON(map[string]any{
				"status":  "drafted",
				"to":      draft.To,
				"subject": draft.Subject,
			})
			return nil
		}
		fmt.Printf("Saved to Drafts: %q → %v\n", draft.Subject, draft.To)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(draftCmd)
}
