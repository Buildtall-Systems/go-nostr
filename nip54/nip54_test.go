package nip54

import (
	"fmt"
	"testing"
)

func TestNormalization(t *testing.T) {
	for _, vector := range []struct {
		before string
		after  string
	}{
		{" hello  ", "hello"},
		{"Goodbye", "goodbye"},
		{"the long and winding road / that leads to your door", "the-long-and-winding-road-that-leads-to-your-door"},
		{"it's 平仮名", "its-平仮名"},

		// the examples in NIP-54
		{"Wiki Article", "wiki-article"},
		{"What's Up?", "whats-up"},
		{"  Hello  World  ", "hello-world"},
		{"Article 1", "article-1"},
		{"ウィキペディア", "ウィキペディア"},
		{"Ñoño", "ñoño"},
		{"Москва", "москва"},
		{"日本語 Article", "日本語-article"},

		{"wiki-article", "wiki-article"},
		{"--a -- b--", "a-b"},
		{"tabs\tand\nnewlines", "tabs-and-newlines"},
		{"ＦＵＬＬＷＩＤＴＨ ２０２６", "fullwidth-2026"},
		{"!?/", ""},
		{"", ""},
	} {
		norm := NormalizeIdentifier(vector.before)
		if norm != vector.after {
			fmt.Println([]byte(vector.after), []byte(norm))
			t.Fatalf("%s: %s != %s", vector.before, norm, vector.after)
		}
		if again := NormalizeIdentifier(norm); again != norm {
			t.Fatalf("%s: not idempotent, %s != %s", vector.before, again, norm)
		}
	}
}
