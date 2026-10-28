package ledger

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"

	goaway "github.com/TwiN/go-away"
)

const MaxNameLength = 20

// Personal GitHub usernames use ASCII letters/numbers and single internal hyphens.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9]+(-[A-Za-z0-9]+)*$`)

//go:embed profane-words.txt
var profaneWords string

// Construct once and never reconfigure: readers can safely share the detectors.
// The raw detector preserves explicitly listed digit and punctuation variants;
// the standard detector also recognizes case, digit, and hyphen obfuscations.
var prohibitedNames = func() map[string]bool {
	words := make(map[string]bool)
	for _, word := range strings.Fields(profaneWords) {
		words[strings.ToLower(word)] = true
	}
	return words
}()

var nameDetector = newNameDetector()
var rawNameDetector = newNameDetector().
	WithSanitizeLeetSpeak(false).
	WithSanitizeSpecialCharacters(false).
	WithSanitizeSpaces(false)

func newNameDetector() *goaway.ProfanityDetector {
	return goaway.NewProfanityDetector().
		WithCustomDictionary(strings.Fields(strings.ToLower(profaneWords)), goaway.DefaultFalsePositives, nil).
		WithSanitizeAccents(false)
}

// ValidateName applies the current admission policy, not historical replay rules.
// Errors deliberately exclude the submitted name and any matched offensive word.
func ValidateName(name, field string) error {
	if len(name) == 0 || len(name) > MaxNameLength || !namePattern.MatchString(name) {
		return fmt.Errorf("%s must be 1–20 ASCII letters or digits, with only single internal hyphens", field)
	}
	// Explicit list entries take precedence over the library's exceptions.
	if prohibitedNames[strings.ToLower(name)] || rawNameDetector.IsProfane(name) || nameDetector.IsProfane(name) {
		return fmt.Errorf("%s contains prohibited language", field)
	}
	return nil
}
