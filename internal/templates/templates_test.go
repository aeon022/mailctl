package templates

import (
	"os"
	"testing"
)

func withTempDir(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
}

func TestSaveLoadList(t *testing.T) {
	withTempDir(t)

	if err := Save("followup", "Following up", "Hi {{.name}}, checking in."); err != nil {
		t.Fatal(err)
	}

	names, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "followup" {
		t.Fatalf("List() = %v, want [followup]", names)
	}

	d, err := Load("followup")
	if err != nil {
		t.Fatal(err)
	}
	if d.Subject != "Following up" {
		t.Errorf("Subject = %q, want %q", d.Subject, "Following up")
	}
	if len(d.To) != 0 {
		t.Errorf("expected no 'to' on a template, got %v", d.To)
	}
}

func TestDelete(t *testing.T) {
	withTempDir(t)

	if err := Save("temp", "Subject", "body"); err != nil {
		t.Fatal(err)
	}
	if err := Delete("temp"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Dir() + "/temp.md"); err == nil {
		t.Fatal("expected template file to be gone after Delete")
	}
}

func TestLoad_MissingSubjectErrors(t *testing.T) {
	withTempDir(t)
	if err := os.WriteFile(Dir()+"/broken.md", []byte("---\nto: []\n---\nbody"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("broken"); err == nil {
		t.Fatal("expected an error for a template with no subject")
	}
}
