package auth

import (
	"errors"
	"fmt"
	"strings"
)

// The phone number IS the identity in this system, so normalisation is a
// security boundary, not a formatting convenience. Two spellings of the same
// number must never become two accounts, and a number we cannot parse must
// never be stored.
//
// The reference implementation appended the literal string "-need_update" to
// numbers it could not parse and stored them anyway. Those rows then failed to
// match on M-Pesa deposits, which caused the callback handler to auto-create a
// SECOND account for the same person -- with the phone number as its password.
// Rejecting unparseable input is the fix.

var (
	ErrPhoneEmpty   = errors.New("phone number is required")
	ErrPhoneInvalid = errors.New("not a valid Kenyan mobile number")
)

// Normalise converts any accepted Kenyan mobile spelling to 254XXXXXXXXX.
//
// Accepted inputs (7 and 1 prefixes cover Safaricom, Airtel and Telkom):
//
//	0712345678      +254712345678     254712345678     712345678
//	0112345678      +254 112 345 678  254-112-345-678  112345678
//
// Anything else is rejected rather than coerced.
func Normalise(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", ErrPhoneEmpty
	}

	// Keep digits only. This absorbs spaces, dashes, parentheses, dots and a
	// leading +; it also means any letter makes the number invalid below,
	// because letters are dropped and the length check then fails.
	var b strings.Builder
	b.Grow(len(raw))
	hadNonPhoneChar := false
	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' || r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
			// punctuation people actually type
		default:
			hadNonPhoneChar = true
		}
	}
	if hadNonPhoneChar {
		return "", fmt.Errorf("%w: %q contains non-numeric characters", ErrPhoneInvalid, raw)
	}
	d := b.String()

	var national string // the 9 digits after the country code
	switch {
	case len(d) == 12 && strings.HasPrefix(d, "254"):
		national = d[3:]
	case len(d) == 10 && d[0] == '0':
		national = d[1:]
	case len(d) == 9:
		national = d
	default:
		return "", fmt.Errorf("%w: %q has %d digits after cleanup, expected 9, 10 or 12",
			ErrPhoneInvalid, raw, len(d))
	}

	if national[0] != '7' && national[0] != '1' {
		return "", fmt.Errorf("%w: %q is not a mobile number (must start 07/01 or 7/1)",
			ErrPhoneInvalid, raw)
	}

	out := "254" + national
	// Belt and braces: this must satisfy the CHECK constraint on users.phone,
	// so an invalid value can never reach the database in the first place.
	if len(out) != 12 {
		return "", fmt.Errorf("%w: %q normalised to %q", ErrPhoneInvalid, raw, out)
	}
	return out, nil
}

// Mask renders a number for logs and for referral listings.
//
// Logs must never carry a full phone number: it is the account identifier, so
// leaking it into a log aggregator leaks the user list. The reference
// implementation also rendered referees' full numbers to anyone holding a
// referral code.
func Mask(phone string) string {
	if len(phone) < 7 {
		return "***"
	}
	return phone[:4] + strings.Repeat("*", len(phone)-7) + phone[len(phone)-3:]
}

// Pretty renders 254712345678 as "0712 345 678" for display in the app.
func Pretty(phone string) string {
	if len(phone) != 12 || !strings.HasPrefix(phone, "254") {
		return phone
	}
	n := phone[3:] // 712345678
	return "0" + n[:3] + " " + n[3:6] + " " + n[6:]
}
