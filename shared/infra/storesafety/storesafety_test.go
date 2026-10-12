package storesafety

import (
	"strings"
	"testing"
)

func TestHasForbiddenWord(t *testing.T) {
	for _, word := range []string{"دفع", "ادفع الآن", "شراء", "payment", "PAY NOW", "instapay", "فودافون كاش", "wallet", "اشتراك مدفوع"} {
		if !HasForbiddenWord(word) {
			t.Errorf("expected %q to be forbidden", word)
		}
	}
	// Innocent words sharing a root or letters are not rejected.
	if HasForbiddenWord("شركة كبيرة") {
		t.Error("expected innocent Arabic text to pass")
	}
	if HasForbiddenWord("players") {
		t.Error("expected 'players' to pass (word boundary)")
	}
	if !HasForbiddenWord("إدفع") {
		t.Error("expected normalized alif form to match")
	}
}

func TestCleanText(t *testing.T) {
	s, ok := CleanText("  hello\x00world\n ", 100)
	if !ok || s != "helloworld" {
		t.Errorf("expected controls stripped and trimmed, got %q,%v", s, ok)
	}
	if _, ok := CleanText(strings.Repeat("س", 81), 80); ok {
		t.Error("expected over-long text to be refused")
	}
	if _, ok := CleanText("   ", 10); !ok {
		t.Error("expected empty-after-trim to be accepted (emptiness is the caller's check)")
	}
}
