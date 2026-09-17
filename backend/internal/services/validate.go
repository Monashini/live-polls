// Package services holds the application's rules. Every constraint lives here
// rather than in a handler, so the same rule applies no matter what calls it:
// an HTTP request today, a WebSocket message or an admin CLI later.
package services

import (
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"livepolls/internal/apperr"
	"livepolls/internal/models"
)

// Bounds are named constants rather than inline numbers so the API docs, the
// error messages and the checks themselves cannot drift apart.
const (
	EmailMaxLen = 254 // the practical maximum for an address per RFC 5321

	// bcrypt only hashes the first 72 bytes of input and silently ignores the
	// rest. Accepting a longer password would mean two different passwords
	// could unlock the same account, so we reject rather than truncate.
	PasswordMinLen = 8
	PasswordMaxLen = 72

	QuestionMinLen = 3
	QuestionMaxLen = 280

	OptionMinLen = 1
	OptionMaxLen = 120
	MinOptions   = 2
	MaxOptions   = 10

	// A poll cannot be scheduled to close more than this far out. It bounds
	// how long a single document can stay writable and keeps obviously bogus
	// dates (year 9999) out of the database.
	MaxPollDuration = 30 * 24 * time.Hour
)

// NormalizeEmail lowercases and trims so that the unique index on email
// actually means "one account per human", not "one account per capitalisation".
func NormalizeEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// collapseSpace trims the ends and squeezes internal runs of whitespace into a
// single space. Without it, "Yes" and "Yes " are stored as different options
// and the duplicate check would let both through.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// hasControlChars rejects text containing control characters. These do not
// render, but they let someone smuggle newlines or terminal escape sequences
// into a question that will be displayed elsewhere.
func hasControlChars(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// ValidateCredentials checks a signup or login payload.
//
// Lengths are measured in runes, not bytes, everywhere except the password.
// len("मत") is 6 bytes but 2 characters; byte-based limits quietly give
// non-Latin scripts a much shorter allowance. The password is the exception:
// bcrypt's limit is a byte limit, so that one is checked in bytes.
func ValidateCredentials(email, password string) (string, error) {
	fields := map[string]string{}

	normalized := NormalizeEmail(email)
	switch {
	case normalized == "":
		fields["email"] = "is required"
	case utf8.RuneCountInString(normalized) > EmailMaxLen:
		fields["email"] = "is too long"
	default:
		addr, err := mail.ParseAddress(normalized)
		if err != nil || addr.Address != normalized {
			// The second condition rejects "Name <a@b.com>", which parses
			// fine but is not a bare address.
			fields["email"] = "is not a valid email address"
		} else if at := strings.LastIndex(normalized, "@"); at < 0 || !strings.Contains(normalized[at+1:], ".") {
			// ParseAddress accepts "user@localhost". For a public signup we
			// want a domain that could actually receive mail.
			fields["email"] = "must include a domain, for example name@example.com"
		}
	}

	switch {
	case password == "":
		fields["password"] = "is required"
	case len(password) < PasswordMinLen:
		fields["password"] = "must be at least 8 characters"
	case len(password) > PasswordMaxLen:
		fields["password"] = "must be 72 characters or fewer"
	}

	if len(fields) > 0 {
		return "", apperr.Validation("Please check the highlighted fields.", fields)
	}
	return normalized, nil
}

// CleanQuestion validates and returns the normalised question text.
func CleanQuestion(raw string) (string, map[string]string) {
	q := collapseSpace(raw)
	switch {
	case q == "":
		return "", map[string]string{"question": "is required"}
	case hasControlChars(q):
		return "", map[string]string{"question": "must not contain control characters"}
	case utf8.RuneCountInString(q) < QuestionMinLen:
		return "", map[string]string{"question": "must be at least 3 characters"}
	case utf8.RuneCountInString(q) > QuestionMaxLen:
		return "", map[string]string{"question": "must be 280 characters or fewer"}
	}
	return q, nil
}

// CleanOptions validates the choice list and returns the normalised texts.
//
// Duplicates are compared case-insensitively after whitespace collapsing, so
// "Yes", "yes" and " YES " count as the same option. Allowing near-identical
// choices makes results meaningless and is usually a copy-paste mistake.
func CleanOptions(raw []string) ([]string, map[string]string) {
	if len(raw) < MinOptions || len(raw) > MaxOptions {
		return nil, map[string]string{"options": "must contain between 2 and 10 choices"}
	}

	cleaned := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))

	for i, opt := range raw {
		text := collapseSpace(opt)
		field := "options." + itoa(i)

		switch {
		case text == "":
			return nil, map[string]string{field: "must not be empty"}
		case hasControlChars(text):
			return nil, map[string]string{field: "must not contain control characters"}
		case utf8.RuneCountInString(text) < OptionMinLen:
			return nil, map[string]string{field: "is too short"}
		case utf8.RuneCountInString(text) > OptionMaxLen:
			return nil, map[string]string{field: "must be 120 characters or fewer"}
		}

		key := strings.ToLower(text)
		if _, dup := seen[key]; dup {
			return nil, map[string]string{field: "duplicates another choice"}
		}
		seen[key] = struct{}{}

		cleaned = append(cleaned, text)
	}

	return cleaned, nil
}

// CleanMode normalises and checks the voting mode. An empty value defaults to
// single, so a client that predates multiple-choice keeps working.
func CleanMode(raw string) (string, map[string]string) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "":
		return models.ModeSingle, nil
	case models.ModeSingle, models.ModeMultiple:
		return mode, nil
	default:
		return "", map[string]string{"mode": "must be either single or multiple"}
	}
}

// CleanSelections validates a submitted ballot against the poll it belongs to.
//
// Every rule here is enforced server-side regardless of what the UI allowed:
// a radio group stops an honest user selecting two options, it does nothing
// about a crafted request.
func CleanSelections(selected []int, mode string, optionCount int) ([]int, map[string]string) {
	if len(selected) == 0 {
		return nil, map[string]string{"optionIndexes": "select at least one choice"}
	}

	// Single mode is the strict case: exactly one, no more.
	if mode == models.ModeSingle && len(selected) > 1 {
		return nil, map[string]string{"optionIndexes": "this poll allows only one choice"}
	}

	// Cheap upper bound before the loop: a ballot can never legitimately name
	// more options than the poll has.
	if len(selected) > optionCount {
		return nil, map[string]string{"optionIndexes": "contains more choices than this poll has"}
	}

	seen := make(map[int]struct{}, len(selected))
	cleaned := make([]int, 0, len(selected))

	for _, idx := range selected {
		if idx < 0 || idx >= optionCount {
			return nil, map[string]string{"optionIndexes": "refers to a choice that is not on this poll"}
		}
		if _, dup := seen[idx]; dup {
			// Silently de-duplicating would let one person inflate an option
			// by sending [1,1,1], so this is rejected rather than cleaned.
			return nil, map[string]string{"optionIndexes": "lists the same choice more than once"}
		}
		seen[idx] = struct{}{}
		cleaned = append(cleaned, idx)
	}

	return cleaned, nil
}

// ValidateExpiry checks an optional closing time. A nil pointer means the poll
// never expires, which is allowed.
func ValidateExpiry(expiresAt *time.Time, now time.Time) map[string]string {
	if expiresAt == nil {
		return nil
	}
	switch {
	case !expiresAt.After(now):
		return map[string]string{"expiresAt": "must be in the future"}
	case expiresAt.Sub(now) > MaxPollDuration:
		return map[string]string{"expiresAt": "must be within 30 days"}
	}
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
