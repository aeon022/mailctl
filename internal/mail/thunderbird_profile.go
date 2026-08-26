package mail

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// tbAccount is one IMAP account resolved from a Thunderbird profile's
// prefs.js — enough to locate its inbox mbox file and, separately, its
// SMTP server for sending.
type tbAccount struct {
	Email     string
	Hostname  string
	Port      int
	Username  string
	Directory string
	SMTPHost  string
	SMTPPort  int
}

type iniSection struct {
	name  string
	props map[string]string
}

// parseINI parses the subset of INI syntax profiles.ini actually uses:
// `[Section]` headers and `key=value` lines, nothing else (no comments
// inside values, no multi-line values).
func parseINI(data []byte) []iniSection {
	var sections []iniSection
	var current *iniSection
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			sections = append(sections, iniSection{name: line[1 : len(line)-1], props: map[string]string{}})
			current = &sections[len(sections)-1]
			continue
		}
		if current == nil {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		current.props[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return sections
}

// resolveDefaultProfileDir picks the profile Thunderbird itself would use.
// A `[InstallXXXX]` section's `Default=` (an install-specific relative
// path) takes precedence when present — that's what modern Thunderbird
// actually consults first. profiles.ini carries no timestamps, so if more
// than one Install section somehow declares a default, this takes the
// first one rather than guessing at "most recent" from data that isn't
// there. Falls back to the classic `[ProfileN] Default=1` marker.
func resolveDefaultProfileDir(sections []iniSection, thunderbirdDir string) (string, error) {
	for _, s := range sections {
		if strings.HasPrefix(s.name, "Install") {
			if p := s.props["Default"]; p != "" {
				return filepath.Join(thunderbirdDir, p), nil
			}
		}
	}
	for _, s := range sections {
		if strings.HasPrefix(s.name, "Profile") && s.props["Default"] == "1" {
			path := s.props["Path"]
			if path == "" {
				continue
			}
			if s.props["IsRelative"] == "0" {
				return path, nil
			}
			return filepath.Join(thunderbirdDir, path), nil
		}
	}
	return "", fmt.Errorf("no default profile found in profiles.ini")
}

// parsePrefsJS reads every `user_pref("key", value);` line into a flat
// map. Thunderbird's prefs.js has no nesting — every setting is one such
// line — so a flat string map is all the structure this needs.
func parsePrefsJS(data []byte) map[string]string {
	prefs := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		const prefix = `user_pref("`
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rest := line[len(prefix):]
		keyEnd := strings.Index(rest, `"`)
		if keyEnd < 0 {
			continue
		}
		key := rest[:keyEnd]
		rest = strings.TrimSpace(rest[keyEnd+1:])
		rest = strings.TrimPrefix(rest, ",")
		rest = strings.TrimSpace(rest)
		rest = strings.TrimSuffix(rest, ");")
		val := strings.TrimSpace(rest)
		if len(val) >= 2 && strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`) {
			val = val[1 : len(val)-1]
			val = strings.ReplaceAll(val, `\"`, `"`)
			val = strings.ReplaceAll(val, `\\`, `\`)
		}
		prefs[key] = val
	}
	return prefs
}

// buildTBAccounts turns the flat prefs map into one tbAccount per IMAP
// account, skipping POP3/RSS/NNTP/Local-Folders entries entirely — those
// have no inbox to read via mbox in the sense mailctl cares about here.
func buildTBAccounts(prefs map[string]string) []tbAccount {
	var accounts []tbAccount
	for _, accID := range strings.Split(prefs["mail.accountmanager.accounts"], ",") {
		accID = strings.TrimSpace(accID)
		if accID == "" {
			continue
		}
		serverID := prefs["mail.account."+accID+".server"]
		if serverID == "" {
			continue
		}
		if prefs["mail.server."+serverID+".type"] != "imap" {
			continue
		}
		port, _ := strconv.Atoi(prefs["mail.server."+serverID+".port"])
		a := tbAccount{
			Hostname:  prefs["mail.server."+serverID+".hostname"],
			Port:      port,
			Username:  prefs["mail.server."+serverID+".userName"],
			Directory: prefs["mail.server."+serverID+".directory"],
		}
		identIDs := strings.Split(prefs["mail.account."+accID+".identities"], ",")
		if len(identIDs) > 0 && strings.TrimSpace(identIDs[0]) != "" {
			identID := strings.TrimSpace(identIDs[0])
			a.Email = prefs["mail.identity."+identID+".useremail"]
			if smtpID := prefs["mail.identity."+identID+".smtpServer"]; smtpID != "" {
				a.SMTPHost = prefs["mail.smtpserver."+smtpID+".hostname"]
				a.SMTPPort, _ = strconv.Atoi(prefs["mail.smtpserver."+smtpID+".port"])
			}
		}
		if a.Email == "" {
			a.Email = a.Username
		}
		accounts = append(accounts, a)
	}
	return accounts
}

// defaultThunderbirdProfileDir resolves ~/.thunderbird/profiles.ini to the
// default profile's directory.
func defaultThunderbirdProfileDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	tbDir := filepath.Join(home, ".thunderbird")
	data, err := os.ReadFile(filepath.Join(tbDir, "profiles.ini"))
	if err != nil {
		return "", fmt.Errorf("read profiles.ini: %w", err)
	}
	return resolveDefaultProfileDir(parseINI(data), tbDir)
}

// thunderbirdAccounts resolves the default profile and returns every IMAP
// account configured in it.
func thunderbirdAccounts() ([]tbAccount, error) {
	profileDir, err := defaultThunderbirdProfileDir()
	if err != nil {
		return nil, err
	}
	prefsData, err := os.ReadFile(filepath.Join(profileDir, "prefs.js"))
	if err != nil {
		return nil, fmt.Errorf("read prefs.js: %w", err)
	}
	accounts := buildTBAccounts(parsePrefsJS(prefsData))
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no IMAP accounts found in Thunderbird profile %s", profileDir)
	}
	return accounts, nil
}

// ThunderbirdProfileFound reports whether a default Thunderbird profile
// could be resolved, and its directory if so. Used by `mailctl doctor` on
// Linux, where there's no Mail.app-equivalent single app to probe.
func ThunderbirdProfileFound() (string, bool) {
	dir, err := defaultThunderbirdProfileDir()
	return dir, err == nil
}
