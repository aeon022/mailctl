// Package templates manages reusable email snippet templates — plain
// Markdown files with YAML frontmatter, same format mailctl's own
// draft/send commands already use (internal/markdown), minus the required
// "to" field since a template's recipient is filled in when it's used, not
// baked into the file.
package templates

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aeon022/mailctl/internal/markdown"
	"github.com/aeon022/mailctl/internal/models"
)

// Dir returns the templates directory, creating it if missing.
func Dir() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "mailctl", "templates")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

// List returns the names of all saved templates (filenames minus ".md"),
// sorted.
func List() ([]string, error) {
	entries, err := os.ReadDir(Dir())
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	return names, nil
}

// Load reads and parses the named template.
func Load(name string) (*models.Draft, error) {
	data, err := os.ReadFile(filepath.Join(Dir(), name+".md"))
	if err != nil {
		return nil, fmt.Errorf("read template %q: %w", name, err)
	}
	return markdown.ParseTemplate(data)
}

// Save writes a template file. Overwrites any existing template of the
// same name.
func Save(name, subject, body string) error {
	content := fmt.Sprintf("---\nsubject: %q\n---\n%s\n", subject, body)
	return os.WriteFile(filepath.Join(Dir(), name+".md"), []byte(content), 0644)
}

// Delete removes a template.
func Delete(name string) error {
	return os.Remove(filepath.Join(Dir(), name+".md"))
}
