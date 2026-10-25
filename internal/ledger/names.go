package ledger

import (
	"fmt"
	"regexp"

	goaway "github.com/TwiN/go-away"
)

const MaxNameLength = 10

// Personal GitHub usernames use ASCII letters/numbers and single internal hyphens.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9]+(-[A-Za-z0-9]+)*$`)

// Construct once and never reconfigure: readers can safely share the detector.
// Defaults recognize case, digit substitutions, and hyphen-separated profanity,
// while retaining the library's known false-positive exceptions.
var nameDetector = newNameDetector()

func newNameDetector() *goaway.ProfanityDetector {
	// The upstream profanity/slur list omits some common insults. Extend its
	// dictionary through the supported API so the same evasion handling applies.
	words := append([]string{}, goaway.DefaultProfanities...)
	words = append(words, "idiot", "moron", "stupid", "loser")
	return goaway.NewProfanityDetector().
		WithCustomDictionary(words, goaway.DefaultFalsePositives, goaway.DefaultFalseNegatives).
		WithSanitizeAccents(false)
}

// ValidateName applies the current admission policy, not historical replay rules.
// Errors deliberately exclude the submitted name and any matched offensive word.
func ValidateName(name, field string) error {
	if len(name) == 0 || len(name) > MaxNameLength || !namePattern.MatchString(name) {
		return fmt.Errorf("%s must be 1–10 ASCII letters or digits, with only single internal hyphens", field)
	}
	if nameDetector.IsProfane(name) {
		return fmt.Errorf("%s contains prohibited language", field)
	}
	return nil
}
