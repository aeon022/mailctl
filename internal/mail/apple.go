package mail

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode"

	"github.com/aeon022/mailctl/internal/models"
	"github.com/google/uuid"
)

// Send sends an email via Apple Mail.
func Send(d *models.Draft) error {
	script := buildOutgoingScript(d, false)
	_, err := runAppleScript(script)
	return err
}

// SaveDraft saves an email to the Drafts folder via Apple Mail.
func SaveDraft(d *models.Draft) error {
	script := buildOutgoingScript(d, true)
	_, err := runAppleScript(script)
	return err
}

func buildOutgoingScript(d *models.Draft, draftOnly bool) string {
	toList := formatAddressList(d.To)
	ccList := formatAddressList(d.CC)
	bccList := formatAddressList(d.BCC)

	accountLine := ""
	if d.Account != "" {
		accountLine = fmt.Sprintf(`set sender of msg to "%s"`, escapeAS(d.Account))
	}

	attachLines := ""
	for _, a := range d.Attachments {
		attachLines += fmt.Sprintf(`
		make new attachment with properties {file name:(POSIX file "%s")} at after the last paragraph of content of msg`, escapeAS(a))
	}

	action := `send msg`
	if draftOnly {
		action = `save msg`
	}

	return fmt.Sprintf(`
tell application "Mail"
	set msg to make new outgoing message with properties {subject:"%s", content:"%s", visible:false}
	tell msg
		%s
		%s
		%s
		%s
		%s
	end tell
	%s
end tell
`,
		escapeAS(d.Subject),
		escapeAS(d.Body),
		accountLine,
		toList,
		ccList,
		bccList,
		attachLines,
		action,
	)
}

func formatAddressList(addrs []string) string {
	var lines []string
	for _, a := range addrs {
		lines = append(lines, fmt.Sprintf(`make new to recipient with properties {address:"%s"}`, escapeAS(a)))
	}
	return strings.Join(lines, "\n\t\t")
}

// FetchInbox returns recent message headers from all accounts' inboxes.
// Body is NOT fetched here — use FetchMessageBody for on-demand loading.
func FetchInbox(count int, unreadOnly bool) ([]models.Message, error) {
	unreadFilter := ""
	if unreadOnly {
		unreadFilter = "whose read status is false"
	}
	script := fmt.Sprintf(`
tell application "Mail"
	set output to ""
	repeat with a in accounts
		try
			set mbox to mailbox "INBOX" of a
			set msgs to (messages of mbox %s)
			set msgCount to count of msgs
			if msgCount > %d then set msgCount to %d
			repeat with i from 1 to msgCount
				set m to item i of msgs
				set mSubject to subject of m
				set mFrom to sender of m
				set mDate to date received of m
				set mRead to read status of m
				set mID to message id of m
				set readStr to "0"
				if mRead then set readStr to "1"
				set output to output & "ID:" & mID & linefeed
				set output to output & "SUBJECT:" & mSubject & linefeed
				set output to output & "FROM:" & mFrom & linefeed
				set output to output & "DATE:" & ((mDate as string)) & linefeed
				set output to output & "READ:" & readStr & linefeed
				set output to output & "ACCOUNT:" & (name of a) & linefeed
				set output to output & "BODY:" & linefeed
				set output to output & "---MSG---" & linefeed
			end repeat
		end try
	end repeat
	return output
end tell
`, unreadFilter, count, count)
	out, err := runAppleScript(script)
	if err != nil {
		return nil, err
	}
	return parseMessages(out, "INBOX"), nil
}

// FetchMessageBody fetches the full body of a single message by subject+sender.
func FetchMessageBody(subject, from string) (string, error) {
	script := fmt.Sprintf(`
tell application "Mail"
	repeat with a in accounts
		repeat with mbox in mailboxes of a
			try
				set msgs to (messages of mbox whose subject is "%s")
				repeat with m in msgs
					if sender of m is "%s" then
						set mBody to content of m
						return mBody
					end if
				end repeat
			end try
		end repeat
	end repeat
	return ""
end tell
`, escapeAS(subject), escapeAS(from))
	return runAppleScript(script)
}

// SearchMessages searches all accounts for messages matching a query.
func SearchMessages(query string, count int) ([]models.Message, error) {
	script := fmt.Sprintf(`
tell application "Mail"
	set output to ""
	set results to search every mailbox for "%s"
	set found to {}
	repeat with r in results
		set found to found & (messages of r)
	end repeat
	set msgCount to count of found
	if msgCount > %d then set msgCount to %d
	repeat with i from 1 to msgCount
		set m to item i of found
		set mSubject to subject of m
		set mFrom to sender of m
		set mDate to date received of m
		set mRead to read status of m
		set mID to message id of m
		set mBody to ""
		try
			set mBody to content of m
			if length of mBody > 2000 then set mBody to text 1 thru 2000 of mBody
		end try
		set readStr to "0"
		if mRead then set readStr to "1"
		set output to output & "ID:" & mID & linefeed
		set output to output & "SUBJECT:" & mSubject & linefeed
		set output to output & "FROM:" & mFrom & linefeed
		set output to output & "DATE:" & ((mDate as string)) & linefeed
		set output to output & "READ:" & readStr & linefeed
		set output to output & "BODY:" & mBody & linefeed
		set output to output & "---MSG---" & linefeed
	end repeat
	return output
end tell
`, escapeAS(query), count, count)
	out, err := runAppleScript(script)
	if err != nil {
		return nil, err
	}
	return parseMessages(out, ""), nil
}

// FetchThread returns all messages with a given subject (simple thread simulation).
func FetchThread(subject string, count int) ([]models.Message, error) {
	script := fmt.Sprintf(`
tell application "Mail"
	set output to ""
	repeat with a in accounts
		repeat with mbox in mailboxes of a
			try
				set msgs to (messages of mbox whose subject contains "%s")
				set msgCount to count of msgs
				if msgCount > %d then set msgCount to %d
				repeat with i from 1 to msgCount
					set m to item i of msgs
					set mSubject to subject of m
					set mFrom to sender of m
					set mDate to date received of m
					set mRead to read status of m
					set mID to message id of m
					set mBody to ""
					try
						set mBody to content of m
						if length of mBody > 3000 then set mBody to text 1 thru 3000 of mBody
					end try
					set readStr to "0"
					if mRead then set readStr to "1"
					set output to output & "ID:" & mID & linefeed
					set output to output & "SUBJECT:" & mSubject & linefeed
					set output to output & "FROM:" & mFrom & linefeed
					set output to output & "DATE:" & ((mDate as string)) & linefeed
					set output to output & "READ:" & readStr & linefeed
					set output to output & "ACCOUNT:" & (name of a) & linefeed
					set output to output & "BODY:" & mBody & linefeed
					set output to output & "---MSG---" & linefeed
				end repeat
			end try
		end repeat
	end repeat
	return output
end tell
`, escapeAS(subject), count, count)
	out, err := runAppleScript(script)
	if err != nil {
		return nil, err
	}
	return parseMessages(out, ""), nil
}

// ListAccounts returns all Apple Mail account names.
func ListAccounts() ([]string, error) {
	script := `
tell application "Mail"
	set output to ""
	repeat with a in accounts
		set output to output & (name of a) & linefeed
	end repeat
	return output
end tell`
	out, err := runAppleScript(script)
	if err != nil {
		return nil, err
	}
	var accounts []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			accounts = append(accounts, line)
		}
	}
	return accounts, nil
}

// ── Parsing ───────────────────────────────────────────────────────────────────

func parseMessages(raw, defaultMailbox string) []models.Message {
	var msgs []models.Message
	for _, block := range strings.Split(raw, "---MSG---") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		m := models.Message{
			ID:      uuid.New().String(),
			Mailbox: defaultMailbox,
			Source:  "apple",
		}
		// body may contain colons, so collect body lines separately
		var bodyLines []string
		inBody := false
		for _, line := range strings.Split(block, "\n") {
			if inBody {
				bodyLines = append(bodyLines, line)
				continue
			}
			key, val, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			val = strings.TrimSpace(val)
			switch key {
			case "ID":
				if val != "" {
					m.ID = val
				}
			case "SUBJECT":
				m.Subject = val
			case "FROM":
				m.From = val
			case "DATE":
				m.Date = parseAppleDate(val)
			case "READ":
				m.Read = val == "1"
			case "ACCOUNT":
				m.Account = val
			case "BODY":
				inBody = true
				bodyLines = append(bodyLines, val)
			}
		}
		m.Body = strings.TrimSpace(strings.Join(bodyLines, "\n"))
		if m.Subject == "" && m.From == "" {
			continue
		}
		msgs = append(msgs, m)
	}
	return msgs
}

// parseAppleDate handles Apple's locale-dependent date strings.
func parseAppleDate(s string) time.Time {
	formats := []string{
		"Monday, January 2, 2006 at 3:04:05 PM",
		"Monday, January 2, 2006 at 15:04:05",
		"January 2, 2006 at 3:04:05 PM",
		"2006-01-02 15:04:05 +0000",
		time.RFC1123Z,
		time.RFC1123,
	}
	// strip timezone name in parens e.g. "(CET)"
	if idx := strings.LastIndex(s, "("); idx > 0 {
		s = strings.TrimSpace(s[:idx])
	}
	for _, f := range formats {
		if t, err := time.ParseInLocation(f, s, time.Local); err == nil {
			return t
		}
	}
	// try stripping non-printable / unusual chars
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, s)
	_ = cleaned
	return time.Now()
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func runAppleScript(script string) (string, error) {
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("osascript: %s", string(exitErr.Stderr))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func escapeAS(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", ``)
	return s
}
