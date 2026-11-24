package emoji

import "testing"

func TestCode(t *testing.T) {
	for e, want := range map[string]string{
		"📄":    "1f4c4",
		"❤️":   "2764", // the variation selector goes
		"👨‍💻":  "1f468-200d-1f4bb",
		"🏳️‍🌈": "1f3f3-fe0f-200d-1f308", // kept in a ZWJ sequence
	} {
		if got := Code(e); got != want {
			t.Errorf("Code(%q) = %q, want %q", e, got, want)
		}
	}
}
