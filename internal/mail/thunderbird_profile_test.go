package mail

import "testing"

const testProfilesINI = `[Profile0]
Name=default
IsRelative=1
Path=abc123.default-release
Default=1

[Install4F96D1932A9F858F]
Default=abc123.default-release
Locked=1
`

const testProfilesININoInstall = `[Profile0]
Name=default
IsRelative=1
Path=xyz789.default-release
Default=1
`

func TestParseINI(t *testing.T) {
	sections := parseINI([]byte(testProfilesINI))
	if len(sections) != 2 {
		t.Fatalf("got %d sections, want 2", len(sections))
	}
	if sections[0].name != "Profile0" || sections[0].props["Path"] != "abc123.default-release" {
		t.Errorf("unexpected Profile0 section: %+v", sections[0])
	}
	if sections[1].name != "Install4F96D1932A9F858F" || sections[1].props["Default"] != "abc123.default-release" {
		t.Errorf("unexpected Install section: %+v", sections[1])
	}
}

func TestResolveDefaultProfileDir_PrefersInstallSection(t *testing.T) {
	sections := parseINI([]byte(testProfilesINI))
	dir, err := resolveDefaultProfileDir(sections, "/home/jan/.thunderbird")
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/home/jan/.thunderbird/abc123.default-release" {
		t.Errorf("got %q", dir)
	}
}

func TestResolveDefaultProfileDir_FallsBackToProfileDefault(t *testing.T) {
	sections := parseINI([]byte(testProfilesININoInstall))
	dir, err := resolveDefaultProfileDir(sections, "/home/jan/.thunderbird")
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/home/jan/.thunderbird/xyz789.default-release" {
		t.Errorf("got %q", dir)
	}
}

func TestResolveDefaultProfileDir_NoDefault(t *testing.T) {
	sections := parseINI([]byte("[Profile0]\nName=x\n"))
	if _, err := resolveDefaultProfileDir(sections, "/home/jan/.thunderbird"); err == nil {
		t.Fatal("want error when no default profile is declared")
	}
}

const testPrefsJS = `user_pref("mail.accountmanager.accounts", "account1,account2");
user_pref("mail.account.account1.identities", "id1");
user_pref("mail.account.account1.server", "server1");
user_pref("mail.account.account2.server", "server2");
user_pref("mail.identity.id1.useremail", "jan@example.com");
user_pref("mail.identity.id1.smtpServer", "smtp1");
user_pref("mail.server.server1.type", "imap");
user_pref("mail.server.server1.hostname", "imap.example.com");
user_pref("mail.server.server1.port", 993);
user_pref("mail.server.server1.userName", "jan@example.com");
user_pref("mail.server.server1.directory", "/home/jan/.thunderbird/abc123.default-release/ImapMail/imap.example.com");
user_pref("mail.smtpserver.smtp1.hostname", "smtp.example.com");
user_pref("mail.smtpserver.smtp1.port", 587);
user_pref("mail.smtpserver.smtp1.username", "jan.smtp");
user_pref("mail.server.server2.type", "rss");
user_pref("mail.server.server2.hostname", "");
`

// The single-account shape: Thunderbird writes no per-identity smtpServer
// pref when the identity uses the default server, and no SMTP username
// pref when it matches the IMAP one.
const testPrefsJSDefaultSMTP = `user_pref("mail.accountmanager.accounts", "account1");
user_pref("mail.account.account1.identities", "id1");
user_pref("mail.account.account1.server", "server1");
user_pref("mail.identity.id1.useremail", "lisa@example.com");
user_pref("mail.server.server1.type", "imap");
user_pref("mail.server.server1.hostname", "imap.example.com");
user_pref("mail.server.server1.port", 993);
user_pref("mail.server.server1.userName", "lisa@example.com");
user_pref("mail.server.server1.directory", "/home/lisa/.thunderbird/p/ImapMail/imap.example.com");
user_pref("mail.smtp.defaultserver", "smtp1");
user_pref("mail.smtpserver.smtp1.hostname", "smtp.example.com");
user_pref("mail.smtpserver.smtp1.port", 587);
`

func TestParsePrefsJS(t *testing.T) {
	prefs := parsePrefsJS([]byte(testPrefsJS))
	if prefs["mail.server.server1.hostname"] != "imap.example.com" {
		t.Errorf("got %q", prefs["mail.server.server1.hostname"])
	}
	if prefs["mail.server.server1.port"] != "993" {
		t.Errorf("got %q", prefs["mail.server.server1.port"])
	}
}

func TestBuildTBAccounts_SkipsNonIMAPAndFillsSMTP(t *testing.T) {
	prefs := parsePrefsJS([]byte(testPrefsJS))
	accounts := buildTBAccounts(prefs)
	if len(accounts) != 1 {
		t.Fatalf("got %d accounts, want 1 (rss account2 should be skipped)", len(accounts))
	}
	a := accounts[0]
	if a.Email != "jan@example.com" {
		t.Errorf("Email = %q", a.Email)
	}
	if a.Hostname != "imap.example.com" || a.Port != 993 {
		t.Errorf("Hostname/Port = %q/%d", a.Hostname, a.Port)
	}
	if a.SMTPHost != "smtp.example.com" || a.SMTPPort != 587 {
		t.Errorf("SMTPHost/SMTPPort = %q/%d", a.SMTPHost, a.SMTPPort)
	}
	if a.Directory != "/home/jan/.thunderbird/abc123.default-release/ImapMail/imap.example.com" {
		t.Errorf("Directory = %q", a.Directory)
	}
	if a.SMTPUsername != "jan.smtp" {
		t.Errorf("SMTPUsername = %q, want the smtpserver username, not the IMAP one", a.SMTPUsername)
	}
}

func TestBuildTBAccounts_FallsBackToDefaultSMTPServer(t *testing.T) {
	accounts := buildTBAccounts(parsePrefsJS([]byte(testPrefsJSDefaultSMTP)))
	if len(accounts) != 1 {
		t.Fatalf("got %d accounts, want 1", len(accounts))
	}
	a := accounts[0]
	if a.SMTPHost != "smtp.example.com" || a.SMTPPort != 587 {
		t.Errorf("SMTPHost/SMTPPort = %q/%d, want the mail.smtp.defaultserver values", a.SMTPHost, a.SMTPPort)
	}
	if a.SMTPUsername != "lisa@example.com" {
		t.Errorf("SMTPUsername = %q, want fallback to the IMAP username", a.SMTPUsername)
	}
}
