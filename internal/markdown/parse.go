package markdown

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/aeon022/mailctl/internal/models"
	"gopkg.in/yaml.v3"
)

// ParseFile reads a Markdown email file with YAML frontmatter.
// Template variables in the body ({{.name}}, {{.date}}, etc.) are expanded.
func ParseFile(path string) (*models.Draft, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return Parse(data)
}

// Parse parses raw Markdown email bytes.
func Parse(data []byte) (*models.Draft, error) {
	const sep = "---"
	content := string(data)

	// strip leading whitespace/newlines
	content = strings.TrimLeft(content, " \n\r")

	if !strings.HasPrefix(content, sep) {
		return nil, fmt.Errorf("missing YAML frontmatter (file must start with ---)")
	}

	// find end of frontmatter
	rest := content[len(sep):]
	end := strings.Index(rest, "\n"+sep)
	if end == -1 {
		return nil, fmt.Errorf("unclosed YAML frontmatter (missing closing ---)")
	}

	fmStr := strings.TrimSpace(rest[:end])
	body := strings.TrimSpace(rest[end+len("\n"+sep):])

	var draft models.Draft
	if err := yaml.Unmarshal([]byte(fmStr), &draft); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	// build template vars
	vars := map[string]string{
		"date": time.Now().Format("January 2, 2006"),
		"year": time.Now().Format("2006"),
	}
	for k, v := range draft.Vars {
		vars[k] = v
	}

	// expand template
	tmpl, err := template.New("body").Delims("{{", "}}").Parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return nil, fmt.Errorf("render template: %w", err)
	}
	draft.Body = buf.String()

	if len(draft.To) == 0 {
		return nil, fmt.Errorf("frontmatter missing 'to' field")
	}
	if draft.Subject == "" {
		return nil, fmt.Errorf("frontmatter missing 'subject' field")
	}

	return &draft, nil
}
