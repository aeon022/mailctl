package mail

import (
	"bytes"
	"io"
	netmail "net/mail"
	"os"
	"strconv"
	"strings"

	"github.com/aeon022/mailctl/internal/models"
	"github.com/google/uuid"
)

// splitMbox splits raw mbox content into per-message chunks. mbox messages
// are separated by a "From " envelope line at the very start of a line;
// mbox writers (Thunderbird included) escape any body line that would
// otherwise start with "From " as ">From ", so a bare-column-0 "From " is
// unambiguous as an envelope delimiter. The envelope line itself is
// dropped — what's returned is just the RFC822 header+body that follows.
func splitMbox(data []byte) [][]byte {
	var messages [][]byte
	var current []byte
	inMsg := false
	for _, line := range bytes.Split(data, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("From ")) {
			if inMsg {
				messages = append(messages, current)
			}
			current = nil
			inMsg = true
			continue
		}
		if inMsg {
			current = append(current, line...)
			current = append(current, '\n')
		}
	}
	if inMsg {
		messages = append(messages, current)
	}
	return messages
}

// parseMboxMessage parses one RFC822 message (as produced by splitMbox)
// into a models.Message. account/mailboxName/source are stamped onto every
// message the same way apple.go's parseMessages does for its account/
// mailbox/"apple" fields.
func parseMboxMessage(raw []byte, account, mailboxName, source string) (models.Message, error) {
	m, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return models.Message{}, err
	}
	body, err := io.ReadAll(m.Body)
	if err != nil {
		return models.Message{}, err
	}
	date, _ := m.Header.Date()
	read := false
	if status := m.Header.Get("X-Mozilla-Status"); status != "" {
		if v, err := strconv.ParseUint(status, 16, 16); err == nil {
			read = v&0x0001 != 0
		}
	}
	id := m.Header.Get("Message-Id")
	if id == "" {
		id = uuid.New().String()
	}
	return models.Message{
		ID:      id,
		Subject: m.Header.Get("Subject"),
		From:    m.Header.Get("From"),
		Date:    date,
		Read:    read,
		Body:    strings.TrimSpace(string(body)),
		Mailbox: mailboxName,
		Account: account,
		Source:  source,
	}, nil
}

// parseMboxFile reads and parses an entire mbox file. A single malformed
// entry is skipped rather than aborting the whole file — Thunderbird may
// be mid-write to the last entry when this runs.
func parseMboxFile(path, account, mailboxName, source string) ([]models.Message, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var msgs []models.Message
	for _, raw := range splitMbox(data) {
		m, err := parseMboxMessage(raw, account, mailboxName, source)
		if err != nil {
			continue
		}
		if m.Subject == "" && m.From == "" {
			continue
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}
