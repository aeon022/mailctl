package mail

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
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

// Thunderbird's X-Mozilla-Status/-Status2 flag bits (nsMsgMessageFlags).
// Deleting a message only sets a flag — the bytes stay in the mbox file
// until the user runs Compact Folders, which many never do, so a parser
// that ignores these shows deleted mail as if it were still in the inbox.
const (
	mozStatusRead     = 0x0001 // nsMsgMessageFlags::Read
	mozStatusExpunged = 0x0008 // nsMsgMessageFlags::Expunged
	// Status2 carries the high word; IMAP-side deletions land here.
	mozStatus2IMAPDeleted = 0x00200000 // nsMsgMessageFlags::IMAPDeleted
)

// errMessageExpunged marks a message the user already deleted in
// Thunderbird. parseMboxFile drops these; DeleteInMail can't remove them
// on Linux, so showing them would leave the user no way out.
var errMessageExpunged = errors.New("message is expunged")

// decodeHeader decodes RFC 2047 encoded-words ("=?UTF-8?B?...?=") in a
// header value. net/mail's Header.Get does not do this, so any non-ASCII
// Subject or From otherwise reaches the store and TUI as a raw blob — and
// breaks substring matching in SearchMessages/FetchThread. Plain ASCII
// decodes to itself, so this is safe to apply unconditionally; an
// undecodable value (e.g. an unsupported charset) keeps its raw form.
func decodeHeader(s string) string {
	var dec mime.WordDecoder
	if out, err := dec.DecodeHeader(s); err == nil {
		return out
	}
	return s
}

// decodeTransferEncoding undoes a Content-Transfer-Encoding. Undecodable
// input degrades to the raw bytes rather than losing the message.
func decodeTransferEncoding(enc string, raw []byte) string {
	var r io.Reader
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "quoted-printable":
		r = quotedprintable.NewReader(bytes.NewReader(raw))
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, bytes.NewReader(raw))
	default:
		return string(raw)
	}
	out, err := io.ReadAll(r)
	if err != nil && len(out) == 0 {
		return string(raw)
	}
	return string(out)
}

// mimeText turns a MIME body into readable text: transfer-encodings are
// decoded, and multipart containers are walked (recursively, for the
// multipart/mixed-around-multipart/alternative shape) for their first
// text/plain part. With no text/plain part it falls back to the first
// other text part — a text/html-only mail is still better than nothing —
// and non-text parts (attachments) are ignored rather than dumped into
// the body as binary noise.
func mimeText(contentType, transferEnc string, raw []byte) string {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") || params["boundary"] == "" {
		return decodeTransferEncoding(transferEnc, raw)
	}
	mr := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
	var fallback string
	parts := 0
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		partRaw, err := io.ReadAll(p)
		if err != nil {
			continue
		}
		parts++
		ct := p.Header.Get("Content-Type")
		// multipart.Part transparently decodes quoted-printable parts and
		// hides the header when it does, so this never double-decodes.
		text := mimeText(ct, p.Header.Get("Content-Transfer-Encoding"), partRaw)
		partType, _, _ := mime.ParseMediaType(ct)
		switch {
		// A part with no Content-Type is text/plain by RFC 2045 default.
		case partType == "" || partType == "text/plain" || strings.HasPrefix(partType, "multipart/"):
			if strings.TrimSpace(text) != "" {
				return text
			}
		case strings.HasPrefix(partType, "text/"):
			if fallback == "" && strings.TrimSpace(text) != "" {
				fallback = text
			}
		}
	}
	if parts == 0 {
		// Boundary declared but nothing parsed — a truncated or malformed
		// message. Raw beats empty.
		return decodeTransferEncoding(transferEnc, raw)
	}
	return fallback
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
		if v, err := strconv.ParseUint(strings.TrimSpace(status), 16, 32); err == nil {
			if v&mozStatusExpunged != 0 {
				return models.Message{}, errMessageExpunged
			}
			read = v&mozStatusRead != 0
		}
	}
	if status2 := m.Header.Get("X-Mozilla-Status2"); status2 != "" {
		if v, err := strconv.ParseUint(strings.TrimSpace(status2), 16, 64); err == nil {
			if v&mozStatus2IMAPDeleted != 0 {
				return models.Message{}, errMessageExpunged
			}
		}
	}
	id := m.Header.Get("Message-Id")
	if id == "" {
		id = uuid.New().String()
	}
	text := mimeText(m.Header.Get("Content-Type"), m.Header.Get("Content-Transfer-Encoding"), body)
	return models.Message{
		ID:      id,
		Subject: decodeHeader(m.Header.Get("Subject")),
		From:    decodeHeader(m.Header.Get("From")),
		Date:    date,
		Read:    read,
		Body:    strings.TrimSpace(text),
		Mailbox: mailboxName,
		Account: account,
		Source:  source,
	}, nil
}

// streamMboxMessages parses path's messages one at a time without ever
// holding the whole file — or every parsed message — in memory at once
// (mailctl#1: a full os.ReadFile + splitMbox + "parse everything, truncate
// later" pipeline OOM'd on multi-GB real-world Thunderbird inboxes). visit
// is called for each valid, non-expunged message in file order; returning
// false stops reading the rest of the file. A single malformed or expunged
// entry is skipped rather than aborting the whole file — Thunderbird may be
// mid-write to the last entry when this runs.
func streamMboxMessages(path, account, mailboxName, source string, visit func(models.Message) bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 64*1024)
	var current []byte
	inMsg := false
	stop := false

	flush := func() {
		if !inMsg || stop {
			return
		}
		raw := current
		current = nil
		inMsg = false
		m, err := parseMboxMessage(raw, account, mailboxName, source)
		if err != nil || (m.Subject == "" && m.From == "") {
			return
		}
		if !visit(m) {
			stop = true
		}
	}

	for !stop {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if bytes.HasPrefix(line, []byte("From ")) {
				flush()
				inMsg = true
			} else if inMsg {
				current = append(current, line...)
			}
		}
		if err != nil {
			break
		}
	}
	flush()
	return nil
}

// parseMboxFile reads and parses an entire mbox file into memory. Only use
// this where the caller genuinely needs every message; anything driven by
// a result count or an early-exit condition should call streamMboxMessages
// or mboxWindow instead, or it reintroduces the OOM this was written to fix.
func parseMboxFile(path, account, mailboxName, source string) ([]models.Message, error) {
	var msgs []models.Message
	err := streamMboxMessages(path, account, mailboxName, source, func(m models.Message) bool {
		msgs = append(msgs, m)
		return true
	})
	return msgs, err
}

// appendBounded appends m to buf, keeping only the newest limit entries —
// the small trailing-window a "give me the last N" caller actually needs,
// instead of retaining everything and truncating at the end. limit<=0
// means "keep none" (matches a caller who asked for zero results).
func appendBounded(buf []models.Message, m models.Message, limit int) []models.Message {
	if limit <= 0 {
		return buf
	}
	buf = append(buf, m)
	if len(buf) > limit {
		buf = buf[1:]
	}
	return buf
}

// mboxWindow streams path and returns at most limit messages: the newest
// ones (last in file order) that satisfy keep (keep == nil accepts all).
// This is the bounded building block FetchInbox/SearchMessages use so a
// huge mbox never gets fully materialized just to answer "give me the
// most recent N".
func mboxWindow(path, account, mailboxName, source string, limit int, keep func(models.Message) bool) ([]models.Message, error) {
	var window []models.Message
	err := streamMboxMessages(path, account, mailboxName, source, func(m models.Message) bool {
		if keep == nil || keep(m) {
			window = appendBounded(window, m, limit)
		}
		return true
	})
	return window, err
}
