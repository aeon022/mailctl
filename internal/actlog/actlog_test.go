package actlog

import "testing"

func TestSenderNameNeverLeaksAnAddress(t *testing.T) {
	for in, want := range map[string]string{
		`News Team <news@list.test>`:    "News Team",
		`"Doe, Jane" <jane@corp.test>`:  "Doe, Jane",
		`news@list.test`:                "list.test", // bare address → organisation only
		`<noreply@shop.test>`:           "shop.test",
		`Weird Name <not an address>`:   "Weird Name",
		``:                              "sender",
		`just text without any address`: "sender",
	} {
		if got := SenderName(in); got != want {
			t.Errorf("SenderName(%q) = %q, want %q", in, got, want)
		}
	}
}
