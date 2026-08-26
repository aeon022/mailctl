//go:build linux

package mail

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aeon022/mailctl/internal/keyring"
	"github.com/aeon022/mailctl/internal/models"
)

const sourceName = "thunderbird"

var errNotSupportedOnLinux = fmt.Errorf(
	"not supported on Linux — mailctl reads Thunderbird's mailbox files directly and won't write back into a file Thunderbird may have open")

func inboxPath(acc tbAccount) string {
	return filepath.Join(acc.Directory, "INBOX")
}

// FetchInbox returns recent message headers+bodies from every IMAP
// account's inbox in the default Thunderbird profile. Unlike apple.go,
// body is included here directly: parsing an mbox file is a cheap local
// read, not a round trip through AppleScript, so there's no reason to
// defer it to FetchMessageBody the way the Apple backend does.
func FetchInbox(count int, unreadOnly bool) ([]models.Message, error) {
	accounts, err := thunderbirdAccounts()
	if err != nil {
		return nil, err
	}
	var all []models.Message
	for _, acc := range accounts {
		msgs, err := parseMboxFile(inboxPath(acc), acc.Email, "INBOX", sourceName)
		if err != nil {
			continue // no INBOX file yet for this account — skip, not fatal
		}
		all = append(all, msgs...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Date.After(all[j].Date) })
	if unreadOnly {
		var unread []models.Message
		for _, m := range all {
			if !m.Read {
				unread = append(unread, m)
			}
		}
		all = unread
	}
	if len(all) > count {
		all = all[:count]
	}
	return all, nil
}

// FetchMessageBody re-scans the owning account's inbox for a message
// matching subject+from. account narrows the scan when known; empty
// scans every account.
func FetchMessageBody(account, subject, from string) (string, error) {
	accounts, err := thunderbirdAccounts()
	if err != nil {
		return "", err
	}
	for _, acc := range accounts {
		if account != "" && acc.Email != account {
			continue
		}
		msgs, err := parseMboxFile(inboxPath(acc), acc.Email, "INBOX", sourceName)
		if err != nil {
			continue
		}
		for _, m := range msgs {
			if m.Subject == subject && strings.Contains(m.From, from) {
				return m.Body, nil
			}
		}
	}
	return "", fmt.Errorf("message not found: subject=%q from=%q", subject, from)
}

// SearchMessages scans every account's inbox for a subject match, mirroring
// apple.go's subject-only live search.
func SearchMessages(query string, count int) ([]models.Message, error) {
	accounts, err := thunderbirdAccounts()
	if err != nil {
		return nil, err
	}
	var out []models.Message
	q := strings.ToLower(query)
	for _, acc := range accounts {
		msgs, err := parseMboxFile(inboxPath(acc), acc.Email, "INBOX", sourceName)
		if err != nil {
			continue
		}
		for _, m := range msgs {
			if strings.Contains(strings.ToLower(m.Subject), q) {
				out = append(out, m)
			}
		}
	}
	if len(out) > count {
		out = out[:count]
	}
	return out, nil
}

// FetchThread is the same subject-contains scan as SearchMessages — the
// Apple backend's two AppleScript blocks for this are likewise near-
// identical, just named for two different call sites.
func FetchThread(subject string, count int) ([]models.Message, error) {
	return SearchMessages(subject, count)
}

// ListAccounts returns every IMAP account's email address.
func ListAccounts() ([]string, error) {
	accounts, err := thunderbirdAccounts()
	if err != nil {
		return nil, err
	}
	out := make([]string, len(accounts))
	for i, a := range accounts {
		out[i] = a.Email
	}
	return out, nil
}

func pickSendingAccount(accounts []tbAccount, wantEmail string) (tbAccount, error) {
	if wantEmail == "" {
		if len(accounts) == 1 {
			return accounts[0], nil
		}
		return tbAccount{}, fmt.Errorf("multiple Thunderbird accounts found — specify an account in the draft's `account` field")
	}
	for _, a := range accounts {
		if a.Email == wantEmail {
			return a, nil
		}
	}
	return tbAccount{}, fmt.Errorf("no Thunderbird account found for %q", wantEmail)
}

// Send delivers d via the sending account's own SMTP server, authenticated
// with a password from the OS keyring.
func Send(d *models.Draft) error {
	if len(d.Attachments) > 0 {
		return fmt.Errorf("attachments are not supported when sending via the Linux/Thunderbird backend yet")
	}
	accounts, err := thunderbirdAccounts()
	if err != nil {
		return err
	}
	acc, err := pickSendingAccount(accounts, d.Account)
	if err != nil {
		return err
	}
	if acc.SMTPHost == "" {
		return fmt.Errorf("no SMTP server configured for %s in Thunderbird", acc.Email)
	}
	password, err := keyring.GetPassword(acc.Email)
	if err != nil {
		return fmt.Errorf("no password stored for %s — run: mailctl account set-password %s", acc.Email, acc.Email)
	}
	msg := buildMIMEMessage(d, acc.Email)
	// Two things that aren't obvious from the call: the envelope recipient
	// list — not the Cc:/Bcc: headers — is what actually decides who
	// receives the mail, so CC and BCC have to be in it; and the SMTP login
	// is SMTPUsername, which Thunderbird stores separately from the IMAP
	// one (buildTBAccounts falls back to the IMAP one when it's absent).
	return sendViaSMTP(acc.SMTPHost, acc.SMTPPort, acc.SMTPUsername, password, acc.Email, envelopeRecipients(d), msg)
}

// SaveDraft is not supported on Linux — see the plan's Global Constraints:
// writing into Thunderbird's Drafts mbox carries the same live-file
// corruption risk as MarkUnreadInMail/DeleteInMail.
func SaveDraft(d *models.Draft) error { return errNotSupportedOnLinux }

func MarkUnreadInMail(messageID string) error { return errNotSupportedOnLinux }
func DeleteInMail(messageID string) error     { return errNotSupportedOnLinux }
func OpenInMail(messageID string) error       { return errNotSupportedOnLinux }
