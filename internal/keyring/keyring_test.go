package keyring

import (
	"testing"

	zkeyring "github.com/zalando/go-keyring"
)

func TestSetGetPassword(t *testing.T) {
	zkeyring.MockInit()
	if err := SetPassword("jan@example.com", "hunter2"); err != nil {
		t.Fatal(err)
	}
	got, err := GetPassword("jan@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hunter2" {
		t.Errorf("got %q, want %q", got, "hunter2")
	}
}

func TestGetPasswordNotFound(t *testing.T) {
	zkeyring.MockInit()
	if _, err := GetPassword("nobody@example.com"); err == nil {
		t.Fatal("want error for a password that was never stored")
	}
}
