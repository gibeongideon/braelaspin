package auth

import (
	"regexp"
	"testing"
)

// The pattern the users.phone CHECK constraint enforces. Anything Normalise
// returns must satisfy it, or the insert fails at the database instead of at
// the boundary where we can give the user a decent message.
var schemaPattern = regexp.MustCompile(`^254[17][0-9]{8}$`)

func TestNormaliseAcceptsEveryKenyanSpelling(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// Safaricom, every way a user might type it
		{"0712345678", "254712345678"},
		{"+254712345678", "254712345678"},
		{"254712345678", "254712345678"},
		{"712345678", "254712345678"},
		{"0712 345 678", "254712345678"},
		{"+254 712 345 678", "254712345678"},
		{"254-712-345-678", "254712345678"},
		{"(0712) 345-678", "254712345678"},
		{"  0712345678  ", "254712345678"},
		{"0712.345.678", "254712345678"},

		// Airtel / Telkom 01x range -- easy to forget, and the reference
		// implementation's own normaliser handled it inconsistently.
		{"0112345678", "254112345678"},
		{"+254112345678", "254112345678"},
		{"254112345678", "254112345678"},
		{"112345678", "254112345678"},
		{"0110000000", "254110000000"},

		// boundaries of the valid range
		{"0700000000", "254700000000"},
		{"0799999999", "254799999999"},
		{"0100000000", "254100000000"},
		{"0199999999", "254199999999"},
	}
	for _, c := range cases {
		got, err := Normalise(c.in)
		if err != nil {
			t.Errorf("Normalise(%q) returned error %v, want %q", c.in, err, c.want)
			continue
		}
		if got != c.want {
			t.Errorf("Normalise(%q) = %q, want %q", c.in, got, c.want)
		}
		if !schemaPattern.MatchString(got) {
			t.Errorf("Normalise(%q) = %q which violates the users.phone CHECK constraint", c.in, got)
		}
	}
}

func TestNormaliseRejectsEverythingElse(t *testing.T) {
	cases := []struct{ in, why string }{
		{"", "empty"},
		{"   ", "whitespace only"},
		{"0812345678", "08 is not a mobile prefix"},
		{"0212345678", "02 is a landline"},
		{"812345678", "8 is not a mobile prefix"},
		{"071234567", "too short (8 national digits)"},
		{"07123456789", "too long (10 national digits)"},
		{"2547123456789", "13 digits"},
		{"25471234567", "11 digits"},
		{"1234567890123456", "far too long"},
		{"abcdefghij", "letters"},
		{"0712345abc", "trailing letters"},
		{"254712345678-need_update", "the reference implementation's corrupted form"},
		{"+1 555 123 4567", "not a Kenyan number"},
		{"07123456e8", "scientific-looking junk"},
		{"0", "single digit"},
	}
	for _, c := range cases {
		got, err := Normalise(c.in)
		if err == nil {
			t.Errorf("Normalise(%q) = %q with no error, but it is invalid (%s)", c.in, got, c.why)
		}
	}
}

// This is the specific regression the reference implementation's design
// invited: a stored number that no longer matches what M-Pesa reports, which
// caused its callback handler to create a duplicate account for the same
// person whose password was their own phone number.
func TestNormaliseIsIdempotent(t *testing.T) {
	for _, in := range []string{"0712345678", "+254712345678", "712345678", "0112345678"} {
		once, err := Normalise(in)
		if err != nil {
			t.Fatalf("Normalise(%q): %v", in, err)
		}
		twice, err := Normalise(once)
		if err != nil {
			t.Fatalf("Normalise(Normalise(%q)) = %q: %v", in, once, err)
		}
		if once != twice {
			t.Errorf("not idempotent: %q -> %q -> %q", in, once, twice)
		}
	}
}

// Every accepted spelling of one number must collapse to a single identity, or
// one person becomes several accounts.
func TestAllSpellingsOfOneNumberCollapse(t *testing.T) {
	spellings := []string{
		"0712345678", "+254712345678", "254712345678", "712345678",
		"0712 345 678", "254-712-345-678", "(0712)345-678",
	}
	seen := map[string]bool{}
	for _, s := range spellings {
		n, err := Normalise(s)
		if err != nil {
			t.Fatalf("Normalise(%q): %v", s, err)
		}
		seen[n] = true
	}
	if len(seen) != 1 {
		t.Errorf("the same number produced %d distinct identities: %v", len(seen), seen)
	}
}

func TestMaskHidesTheSubscriberDigits(t *testing.T) {
	got := Mask("254712345678")
	if got == "254712345678" {
		t.Fatal("Mask returned the number unchanged; logs would leak the user list")
	}
	if want := "2547*****678"; got != want {
		t.Errorf("Mask = %q, want %q", got, want)
	}
	// Must not panic or leak on junk.
	for _, in := range []string{"", "123", "254"} {
		if m := Mask(in); m == in && len(in) >= 7 {
			t.Errorf("Mask(%q) leaked", in)
		}
	}
}

func TestPretty(t *testing.T) {
	if got, want := Pretty("254712345678"), "0712 345 678"; got != want {
		t.Errorf("Pretty = %q, want %q", got, want)
	}
	// Unparseable input passes through rather than panicking.
	if got := Pretty("garbage"); got != "garbage" {
		t.Errorf("Pretty(garbage) = %q", got)
	}
}

func FuzzNormalise(f *testing.F) {
	for _, s := range []string{"0712345678", "+254712345678", "712345678", "", "abc", "254"} {
		f.Add(s)
	}
	// The contract: Normalise either errors, or returns something that
	// satisfies the database CHECK. It must never panic and never return a
	// value the schema would reject.
	f.Fuzz(func(t *testing.T, in string) {
		got, err := Normalise(in)
		if err != nil {
			return
		}
		if !schemaPattern.MatchString(got) {
			t.Fatalf("Normalise(%q) = %q, which the users.phone CHECK would reject", in, got)
		}
	})
}
