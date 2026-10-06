package cmd

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/aeon022/mailctl/internal/actlog"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aeon022/mailctl/internal/config"
	"github.com/aeon022/mailctl/internal/mail"
	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/mailctl/internal/store"
	"github.com/aeon022/mailctl/internal/unsub"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var unsubscribeYes bool

var unsubscribeCmd = &cobra.Command{
	Use:   "unsubscribe <message-id>",
	Short: "Unsubscribe from a mailing list using the message's own List-Unsubscribe header",
	Long: `Looks up the message's List-Unsubscribe headers and prints what it would do:
a one-click request (RFC 8058), opening the unsubscribe page, or drafting an
unsubscribe email. Nothing happens until you confirm (or pass --yes). A mailto
method only ever creates a draft — mailctl never sends it for you here.`,
	Example: `  mailctl inbox                         # note the message ID
  mailctl unsubscribe <message-id>      # shows the plan, asks to proceed
  mailctl unsubscribe <message-id> --yes`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return err
		}
		defer s.Close()
		msg, err := s.GetMessage(context.Background(), args[0])
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("message %q not in the local cache — run: mailctl sync", args[0])
		}
		if err != nil {
			return err
		}
		interactive := term.IsTerminal(int(os.Stdin.Fd()))
		return unsubscribeFlow(cmd.OutOrStdout(), os.Stdin, interactive, *msg, unsubscribeYes, unsubActions{
			headers: mail.FetchUnsubscribeHeaders,
			do: func(t unsub.Target) error {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				return unsub.Do(ctx, t)
			},
			draft: func(t unsub.Target) error {
				return mail.SaveDraft(&models.Draft{To: []string{t.To}, Subject: t.Subject, Body: t.Body})
			},
		})
	},
}

// unsubActions are the side effects of the flow, injected so tests run
// without a mail client or network.
type unsubActions struct {
	headers func(account, subject, from string) (listUnsub, post string, err error)
	do      func(unsub.Target) error
	draft   func(unsub.Target) error
}

func unsubscribeFlow(out io.Writer, in io.Reader, interactive bool, msg models.Message, yes bool, a unsubActions) error {
	lu, post, err := a.headers(msg.Account, msg.Subject, msg.From)
	if err != nil {
		return fmt.Errorf("couldn't read the message headers: %w", err)
	}
	target, ok := unsub.Choose(unsub.Parse(lu, post))
	if !ok {
		return fmt.Errorf("no List-Unsubscribe header in this message — nothing to do automatically")
	}
	fmt.Fprintf(out, "From:    %s\nSubject: %s\nPlan:    %s\n", msg.From, msg.Subject, target.Describe())

	if !yes {
		if !interactive {
			return fmt.Errorf("not a terminal — pass --yes to proceed")
		}
		fmt.Fprint(out, "Proceed? [y/N] ")
		line, _ := bufio.NewReader(in).ReadString('\n')
		if r := strings.ToLower(strings.TrimSpace(line)); r != "y" && r != "yes" {
			fmt.Fprintln(out, "Cancelled — nothing was done.")
			return nil
		}
	}

	if target.Kind == unsub.Mailto {
		if err := a.draft(target); err != nil {
			fmt.Fprintf(out, "Couldn't save a draft (%v).\nSend an email yourself:\n  To:      %s\n  Subject: %s\n", err, target.To, target.Subject)
			return nil
		}
		fmt.Fprintln(out, "Draft saved — review it and send it from your mail client.")
		return nil
	}
	if err := a.do(target); err != nil {
		return err
	}
	actlog.Unsubscribed(msg.From)
	if target.Kind == unsub.OneClick {
		fmt.Fprintln(out, "✓ Unsubscribe request accepted.")
	} else {
		fmt.Fprintln(out, "Unsubscribe page opened in your browser.")
	}
	return nil
}

func init() {
	unsubscribeCmd.Flags().BoolVar(&unsubscribeYes, "yes", false, "Proceed without asking")
	rootCmd.AddCommand(unsubscribeCmd)
}
