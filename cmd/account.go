package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/aeon022/mailctl/internal/keyring"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var accountCmd = &cobra.Command{
	Use:   "account",
	Short: "Manage per-account credentials",
}

var setPasswordCmd = &cobra.Command{
	Use:     "set-password <email>",
	Short:   "Store an SMTP app password for an account in the OS keyring",
	Example: `  mailctl account set-password jan@example.com`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		email := args[0]
		fmt.Printf("Password for %s: ", email)
		password, err := readPassword()
		fmt.Println()
		if err != nil {
			return err
		}
		if password == "" {
			return fmt.Errorf("password cannot be empty")
		}
		if err := keyring.SetPassword(email, password); err != nil {
			return fmt.Errorf("store password: %w", err)
		}
		fmt.Printf("Stored password for %s.\n", email)
		return nil
	},
}

// readPassword reads a password from stdin without echoing it when stdin
// is a real terminal; falls back to a plain line read (e.g. piped input in
// scripts) otherwise.
func readPassword() (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		return string(b), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

func init() {
	accountCmd.AddCommand(setPasswordCmd)
	rootCmd.AddCommand(accountCmd)
}
