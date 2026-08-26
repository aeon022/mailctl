package config

import (
	"path/filepath"
	"testing"
)

func TestAppSupportDirFor(t *testing.T) {
	cases := []struct {
		goos, home, xdgDataHome, want string
	}{
		{"darwin", "/Users/jan", "", "/Users/jan/Library/Application Support/mailctl"},
		{"linux", "/home/jan", "", "/home/jan/.local/share/mailctl"},
		{"linux", "/home/jan", "/home/jan/.custom-data", "/home/jan/.custom-data/mailctl"},
	}
	for _, c := range cases {
		t.Setenv("XDG_DATA_HOME", c.xdgDataHome)
		got := appSupportDirFor(c.goos, c.home)
		want := filepath.FromSlash(c.want)
		if got != want {
			t.Errorf("appSupportDirFor(%q, %q) with XDG_DATA_HOME=%q = %q, want %q",
				c.goos, c.home, c.xdgDataHome, got, want)
		}
	}
}
