// Package i18n is the TUI's languages (TL-604): English, the source text, and the catalogs in this
// package (nl.go). Screens wrap what they show in T or F; tickets, comments, keys and API values are
// shown as they are. Another language is one more catalog file and an entry in catalogs.
package i18n

import (
	"fmt"
	"os"
	"strings"
)

// catalogs: English text → the language's; a language not here is English.
var catalogs = map[string]map[string]string{"nl": nl, "de": de, "es": es, "pt": pt, "fr": fr, "tlh": tlh}

// hidden languages are only ever chosen in the config, never picked from the locale (an easter egg).
var hidden = map[string]bool{"tlh": true}

var current map[string]string // nil: English

// Set picks the language: en, nl, or system (the locale, else English). Unknown is English.
func Set(language string) {
	if language == "system" {
		language = Locale()
	}
	current = catalogs[language]
}

// Locale is the language the environment asks for, as POSIX reads it (LC_ALL over LC_MESSAGES over
// LANG: nl_NL.UTF-8 is nl), when there's a catalog for it; else en.
func Locale() string {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := os.Getenv(name)
		if v == "" {
			continue
		}
		lang := strings.ToLower(strings.FieldsFunc(v, func(r rune) bool { return r == '_' || r == '.' || r == '-' || r == '@' })[0])
		if _, ok := catalogs[lang]; ok && !hidden[lang] {
			return lang
		}
		return "en"
	}
	return "en"
}

// T is s in the current language.
func T(s string) string {
	if t, ok := current[s]; ok {
		return t
	}
	return s
}

// F is fmt.Sprintf with the format in the current language.
func F(format string, args ...any) string { return fmt.Sprintf(T(format), args...) }
