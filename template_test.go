package main

import (
	"strings"
	"testing"
	"text/template"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		length int
		suffix string
		want   string
	}{
		{"longer than the limit", "Long text for testing truncate", 9, "..", "Long text.."},
		{"shorter than the limit", "short", 10, "..", "short"},
		{"exactly the limit", "exact", 5, "..", "exact"},
		{"one over the limit", "exactly", 6, "…", "exactl…"},
		{"empty suffix", "Long text", 4, "", "Long"},
		{"empty string", "", 5, "..", ""},
		{"limit of zero", "unchanged", 0, "..", "unchanged"},
		{"negative limit", "unchanged", -3, "..", "unchanged"},
		// characters, not bytes: a cut must not split a character
		{"umlauts", "Freifunk München", 13, "…", "Freifunk Münc…"},
		{"emoji", "📜 scribe 📜", 3, "", "📜 s"},
		{"umlauts within the limit", "München", 7, "…", "München"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncate(tt.s, tt.length, tt.suffix); got != tt.want {
				t.Errorf("truncate(%q, %d, %q) = %q, want %q", tt.s, tt.length, tt.suffix, got, tt.want)
			}
		})
	}
}

// The function is meant to be called from a template, with a field of an
// entry or with a string.
func TestTruncateInTemplate(t *testing.T) {
	tpl, err := template.New("tpl").Funcs(template.FuncMap{"truncate": truncate}).
		Parse(`{{truncate .Description 8 "…"}} / {{truncate "markscribe" 4 ""}}`)
	if err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := tpl.Execute(&out, Repo{Description: "A description"}); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "A descri… / mark"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
