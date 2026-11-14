package nano

import (
	"errors"
	"strings"
	"testing"
)

func TestXNOToRaw(t *testing.T) {
	for amount, want := range map[string]string{
		"1":                                  "1" + strings.Repeat("0", 30),
		"0.01":                               "1" + strings.Repeat("0", 28),
		".5":                                 "5" + strings.Repeat("0", 29),
		" 2.5 ":                              "25" + strings.Repeat("0", 29),
		"0." + strings.Repeat("0", 29) + "1": "1",
	} {
		got, err := XNOToRaw(amount)
		if err != nil || got != want {
			t.Errorf("XNOToRaw(%q) = %q, %v; want %q", amount, got, err, want)
		}
	}
}

func TestXNOToRawRejects(t *testing.T) {
	for _, amount := range []string{
		"", "0", "0.0", "-1", "+1", "1.", "1e-3", "1,0", "0x10", "abc",
		"0." + strings.Repeat("0", 30) + "1",
		"1." + strings.Repeat("0", 31),
	} {
		if got, err := XNOToRaw(amount); !errors.Is(err, ErrInvalidAmount) {
			t.Errorf("XNOToRaw(%q) = %q, %v; want ErrInvalidAmount", amount, got, err)
		}
	}
}
