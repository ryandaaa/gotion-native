// Package tr says gotion's texts in the language the user reads: Bahasa
// Indonesia, the language they are written in, or English.
//
// A text is its own key: T("Batal") is "Batal" in Indonesian and "Cancel"
// in English. A text kept in a table outside a function is marked with N,
// which changes nothing, and said with T where it is shown, so that it
// follows a change of language.
package tr

import (
	"strings"
	"sync/atomic"
)

// The languages gotion is written in.
const (
	ID = "id"
	EN = "en"
)

var lang atomic.Value // string

// Set makes gotion speak lang, ID or EN; anything else is EN.
func Set(l string) {
	if l != ID {
		l = EN
	}
	lang.Store(l)
}

// Lang is the language gotion speaks now.
func Lang() string {
	if l, ok := lang.Load().(string); ok {
		return l
	}
	return EN
}

// Pick returns the language of the first preferred language gotion is
// written in (BCP 47 tags such as "id-ID", "en-GB"), else EN.
func Pick(preferred []string) string {
	for _, t := range preferred {
		t = strings.ToLower(t)
		switch {
		case t == "id" || strings.HasPrefix(t, "id-") || t == "in" || strings.HasPrefix(t, "in-"):
			return ID
		case t == "en" || strings.HasPrefix(t, "en-"):
			return EN
		}
	}
	return EN
}

// T says s in the language gotion speaks.
func T(s string) string {
	if Lang() == ID {
		return s
	}
	if e, ok := en[s]; ok {
		return e
	}
	return s
}

// N marks s as a text to say with T where it is shown.
func N(s string) string { return s }

// Has tells that s has an English text, for the tests.
func Has(s string) bool {
	_, ok := en[s]
	return ok
}
