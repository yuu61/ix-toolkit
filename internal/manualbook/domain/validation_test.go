package domain_test

import (
	"testing"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
)

func TestMatchesWebVersion(t *testing.T) {
	for _, tc := range []struct {
		title, version string
		want           bool
	}{
		{"Example command reference 1.5a ドキュメント", "1.5a", true},
		{"Example manual 1.5a版 ドキュメント", "1.5a", true},
		{"Example manual (1.5a)", "1.5a", true},
		{"Example manual 1.5ab版", "1.5a", false},
		{"Example manual 11.5a版", "1.5a", false},
		{"Example manual 1.50版", "1.5", false},
		{"Example manual 1.5a.1版", "1.5a", false},
		{"Example manual 1.5a-beta版", "1.5a", false},
		{"Example manual 1.5a+build版", "1.5a", false},
		{"Example manual", "1.5a", false},
		{"Example manual 1.5a版", "", false},
	} {
		t.Run(tc.title+"/"+tc.version, func(t *testing.T) {
			if got := domain.MatchesWebVersion(tc.title, tc.version); got != tc.want {
				t.Errorf("MatchesWebVersion(%q, %q) = %v, want %v", tc.title, tc.version, got, tc.want)
			}
		})
	}
}

func TestCheckDocBooks(t *testing.T) {
	for _, series := range []string{"ix", "ix-r"} {
		for _, book := range []string{"crm", "fd", "ex", "slog"} {
			if err := domain.CheckDoc(domain.Doc{Series: series, Book: book}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, d := range []domain.Doc{{Series: "ix"}, {Series: "ix", Book: "other"}, {Book: "ex"}} {
		if domain.CheckDoc(d) == nil {
			t.Errorf("accepted incomplete or unknown book: %+v", d)
		}
	}
}
