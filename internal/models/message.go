package models

import "time"

type Message struct {
	ID          string    `json:"id"`
	Subject     string    `json:"subject"`
	From        string    `json:"from"`
	To          []string  `json:"to"`
	CC          []string  `json:"cc,omitempty"`
	Body        string    `json:"body"`
	BodyHTML    string    `json:"body_html,omitempty"`
	Date        time.Time `json:"date"`
	Read        bool      `json:"read"`
	Mailbox     string    `json:"mailbox"`
	Account     string    `json:"account"`
	ThreadID    string    `json:"thread_id,omitempty"`
	Source      string    `json:"source"` // "apple"
}

// Draft is the parsed content of a Markdown email file.
type Draft struct {
	To          []string          `yaml:"to"`
	CC          []string          `yaml:"cc"`
	BCC         []string          `yaml:"bcc"`
	Subject     string            `yaml:"subject"`
	From        string            `yaml:"from"`
	Account     string            `yaml:"account"`
	Attachments []string          `yaml:"attachments"`
	Vars        map[string]string `yaml:"vars"`
	Body        string            // rendered Markdown body
}
