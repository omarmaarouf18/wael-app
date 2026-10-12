// Package storesafety holds the single store-safety word rule: payment and
// purchase wording is forbidden in admin-composed, student-visible text
// (settings fields, broadcast titles and bodies) so the apps stay safe in
// the stores. Matching normalizes Arabic letters and requires word
// boundaries, so innocent words sharing a root are not rejected.
package storesafety

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ForbiddenWords defines the store-safety payment terms (A1). The single
// source of truth; every service checks against this list.
var ForbiddenWords = []string{
	"دفع",
	"الدفع",
	"ادفع",
	"الادفع",
	"شراء",
	"الشراء",
	"اشتري",
	"الاشتري",
	"استرداد",
	"الاسترداد",
	"اشتراك مدفوع",
	"الاشتراك المدفوع",
	"payment",
	"pay",
	"buy",
	"purchase",
	"refund",
	"instapay",
	"فودافون كاش",
	"محفظة",
	"المحفظة",
	"wallet",
}

// normalizeArabic normalizes Arabic letters (alifs, yaa, taa marbuta) and
// strips diacritics.
func normalizeArabic(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case 'أ', 'إ', 'آ':
			sb.WriteRune('ا')
		case 'ى':
			sb.WriteRune('ي')
		case 'ة':
			sb.WriteRune('ه')
		case '\u064B', '\u064C', '\u064D', '\u064E', '\u064F', '\u0650', '\u0651', '\u0652', '\u0670':
			continue
		default:
			sb.WriteRune(unicode.ToLower(r))
		}
	}
	return sb.String()
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func containsForbiddenTerm(normalizedText, normalizedTerm string) bool {
	termLen := len(normalizedTerm)
	if termLen == 0 {
		return false
	}
	start := 0
	for {
		idx := strings.Index(normalizedText[start:], normalizedTerm)
		if idx == -1 {
			return false
		}
		pos := start + idx

		// Boundary check before
		boundaryBefore := false
		if pos == 0 {
			boundaryBefore = true
		} else {
			r, _ := utf8.DecodeLastRuneInString(normalizedText[:pos])
			boundaryBefore = !isWordRune(r)
		}

		// Boundary check after
		boundaryAfter := false
		end := pos + termLen
		if end == len(normalizedText) {
			boundaryAfter = true
		} else {
			r, _ := utf8.DecodeRuneInString(normalizedText[end:])
			boundaryAfter = !isWordRune(r)
		}

		if boundaryBefore && boundaryAfter {
			return true
		}
		start = pos + 1
	}
}

// HasForbiddenWord checks if the raw text contains any forbidden payment
// term with word boundaries.
func HasForbiddenWord(text string) bool {
	norm := normalizeArabic(text)
	for _, word := range ForbiddenWords {
		normWord := normalizeArabic(word)
		if containsForbiddenTerm(norm, normWord) {
			return true
		}
	}
	return false
}

// CleanText strips control characters, trims leading/trailing spaces, and
// enforces a rune limit. It reports false for over-long text.
func CleanText(raw string, maxRunes int) (string, bool) {
	s := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, raw)
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > maxRunes {
		return "", false
	}
	return s, true
}
