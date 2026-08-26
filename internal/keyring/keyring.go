// Package keyring stores mailctl's per-account SMTP passwords in the OS
// keyring (Secret Service/libsecret on Linux, Keychain on macOS) rather
// than in mailctl's plaintext config.yaml.
package keyring

import zkeyring "github.com/zalando/go-keyring"

const service = "mailctl"

func SetPassword(email, password string) error {
	return zkeyring.Set(service, email, password)
}

func GetPassword(email string) (string, error) {
	return zkeyring.Get(service, email)
}
