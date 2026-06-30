package mail

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

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
	-- inbox names vary by account type: "INBOX" for IMAP, "Posteingang" for Exchange
	set inboxNames to {"INBOX", "Posteingang", "Inbox"}
	repeat with a in accounts
		set mbox to missing value
		try
			repeat with mb in mailboxes of a
				if (name of mb) is in inboxNames then
					set mbox to mb
					exit repeat
				end if
			end repeat
		end try
		if mbox is missing value then
			-- skip accounts where no inbox-named mailbox was found
		else
			try
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
					set yr to year of mDate as string
					set mo to text -2 thru -1 of ("0" & ((month of mDate as integer) as string))
					set dy to text -2 thru -1 of ("0" & (day of mDate as string))
					set hr to text -2 thru -1 of ("0" & (hours of mDate as string))
					set mn to text -2 thru -1 of ("0" & (minutes of mDate as string))
					set sc to text -2 thru -1 of ("0" & (seconds of mDate as string))
					set mDateStr to yr & "-" & mo & "-" & dy & "T" & hr & ":" & mn & ":" & sc
					set output to output & "ID:" & mID & linefeed
					set output to output & "SUBJECT:" & mSubject & linefeed
					set output to output & "FROM:" & mFrom & linefeed
					set output to output & "DATE:" & mDateStr & linefeed
					set output to output & "READ:" & readStr & linefeed
					set output to output & "ACCOUNT:" & (name of a) & linefeed
					set output to output & "BODY:" & linefeed
					set output to output & "---MSG---" & linefeed
				end repeat
			end try
		end if
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
// Searches inbox mailboxes first ("INBOX"/"Posteingang"), then all mailboxes.
func FetchMessageBody(subject, from string) (string, error) {
	script := fmt.Sprintf(`
tell application "Mail"
	set inboxNames to {"INBOX", "Posteingang", "Inbox"}
	-- check inbox of each account first (fastest path)
	repeat with a in accounts
		try
			repeat with mb in mailboxes of a
				if (name of mb) is in inboxNames then
					try
						set msgs to (messages of mb whose subject is "%s")
						repeat with m in msgs
							try
								if sender of m contains "%s" then
									return content of m
								end if
							end try
						end repeat
					end try
					exit repeat
				end if
			end repeat
		end try
	end repeat
	-- fall back to all mailboxes (sent, archive, etc.)
	repeat with a in accounts
		repeat with mbox in mailboxes of a
			try
				set msgs to (messages of mbox whose subject is "%s")
				repeat with m in msgs
					try
						if sender of m contains "%s" then
							return content of m
						end if
					end try
				end repeat
			end try
		end repeat
	end repeat
	return ""
end tell
`, escapeAS(subject), escapeAS(from), escapeAS(subject), escapeAS(from))
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
		set readStr to "0"
		if mRead then set readStr to "1"
		set yr to year of mDate as string
		set mo to text -2 thru -1 of ("0" & ((month of mDate as integer) as string))
		set dy to text -2 thru -1 of ("0" & (day of mDate as string))
		set hr to text -2 thru -1 of ("0" & (hours of mDate as string))
		set mn to text -2 thru -1 of ("0" & (minutes of mDate as string))
		set sc to text -2 thru -1 of ("0" & (seconds of mDate as string))
		set mDateStr to yr & "-" & mo & "-" & dy & "T" & hr & ":" & mn & ":" & sc
		set output to output & "ID:" & mID & linefeed
		set output to output & "SUBJECT:" & mSubject & linefeed
		set output to output & "FROM:" & mFrom & linefeed
		set output to output & "DATE:" & mDateStr & linefeed
		set output to output & "READ:" & readStr & linefeed
		set output to output & "BODY:" & linefeed
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
					set readStr to "0"
					if mRead then set readStr to "1"
					set yr to year of mDate as string
					set mo to text -2 thru -1 of ("0" & ((month of mDate as integer) as string))
					set dy to text -2 thru -1 of ("0" & (day of mDate as string))
					set hr to text -2 thru -1 of ("0" & (hours of mDate as string))
					set mn to text -2 thru -1 of ("0" & (minutes of mDate as string))
					set sc to text -2 thru -1 of ("0" & (seconds of mDate as string))
					set mDateStr to yr & "-" & mo & "-" & dy & "T" & hr & ":" & mn & ":" & sc
					set output to output & "ID:" & mID & linefeed
					set output to output & "SUBJECT:" & mSubject & linefeed
					set output to output & "FROM:" & mFrom & linefeed
					set output to output & "DATE:" & mDateStr & linefeed
					set output to output & "READ:" & readStr & linefeed
					set output to output & "ACCOUNT:" & (name of a) & linefeed
					set output to output & "BODY:" & linefeed
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

// OpenInMail activates Apple Mail and opens the message with the given message-id.
func OpenInMail(messageID string) error {
	script := fmt.Sprintf(`
tell application "Mail"
	activate
	repeat with a in accounts
		repeat with mbox in mailboxes of a
			try
				set found to (messages of mbox whose message id is %q)
				if (count of found) > 0 then
					open item 1 of found
					return
				end if
			end try
		end repeat
	end repeat
end tell`, messageID)
	_, err := runAppleScript(script)
	return err
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

// parseAppleDate parses the ISO date string we format in AppleScript.
func parseAppleDate(s string) time.Time {
	s = strings.TrimSpace(s)
	t, err := time.ParseInLocation("2006-01-02T15:04:05", s, time.Local)
	if err == nil {
		return t
	}
	// fallback for any legacy cached values
	for _, f := range []string{time.RFC3339, time.RFC1123Z, time.RFC1123} {
		if t, err := time.Parse(f, s); err == nil {
			return t.Local()
		}
	}
	return time.Time{}
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
