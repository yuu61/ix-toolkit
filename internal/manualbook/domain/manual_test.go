package domain_test

import (
	"testing"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
)

func TestReferencePreservesSourceLocation(t *testing.T) {
	for _, tc := range []struct {
		ref  domain.Ref
		want string
		web  bool
	}{
		{domain.Ref{}, "", false},
		{domain.Ref{Page: 1057}, "p1057", false},
		{domain.Ref{Path: "cli/example.html", Anchor: "example-enable"}, "cli/example.html#example-enable", true},
		{domain.Ref{Path: "cli/example.html"}, "cli/example.html", true},
	} {
		if got := tc.ref.String(); got != tc.want {
			t.Errorf("%+v: source = %q, want %q", tc.ref, got, tc.want)
		}
		if got := tc.ref.IsWeb(); got != tc.web {
			t.Errorf("%+v: IsWeb = %v, want %v", tc.ref, got, tc.web)
		}
	}
}

// SyntaxLabels と CommandsOf は揃えたあとの綴りで見出し語を見る。揃える先がそれらと
// 食い違うと、構文の欄が地の文として整形され、索引が空になる。
func TestLabelSpellingKeepsSyntaxLabelsReachable(t *testing.T) {
	for l := range domain.SyntaxLabels {
		if got := domain.NormalizeLabel(l); got != l {
			t.Errorf("LabelSpelling が SyntaxLabels の %q を %q に写している", l, got)
		}
	}
	for from, to := range domain.LabelSpelling {
		if domain.SyntaxLabels[from] && !domain.SyntaxLabels[to] {
			t.Errorf("LabelSpelling: %q → %q で構文の欄の判定が外れる", from, to)
		}
	}
}

func TestCommandsOfJoinsWrappedLinesAndDropsNoForm(t *testing.T) {
	syntax := func(lines ...string) *domain.Entry {
		return &domain.Entry{Fields: []domain.Field{
			{Label: "説明", Lines: []string{"example enable は無視される"}},
			{Label: "入力形式", Lines: lines},
		}}
	}
	for _, tc := range []struct {
		name  string
		entry *domain.Entry
		want  []string
	}{
		{
			name:  "折り返しは直前のコマンドに繋ぐ",
			entry: syntax("  example proposal NAME [ aes-cbc-128 ]", "      [ aes-cbc-192 ]", "  no example proposal NAME"),
			want:  []string{"example proposal NAME [ aes-cbc-128 ] [ aes-cbc-192 ]"},
		},
		{
			name:  "英小文字で始まらない行も折り返し",
			entry: syntax("  example address ADDRESS [", "  MASK ]"),
			want:  []string{"example address ADDRESS [ MASK ]"},
		},
		{
			name:  "no 形が 1 行目より浅くても 1 行目は消えない (snmp-agent ip trap の形)",
			entry: syntax("      example trap HOST", "  no example trap HOST"),
			want:  []string{"example trap HOST"},
		},
		{
			name:  "no 形と重複は落とし、繋ぐ先の無い断片は載せない",
			entry: syntax("      ] 断片", "  example enable", "  example enable", "  no example enable"),
			want:  []string{"example enable"},
		},
	} {
		got := domain.CommandsOf(tc.entry)
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
				break
			}
		}
	}
}

func TestCommandProfileDoesNotRequirePDFMarker(t *testing.T) {
	web := domain.Profile{FieldLabels: []string{"入力形式"}}
	if !web.HasCommandEntries() {
		t.Fatal("Web command references have fields without a PDF entry marker")
	}
	section := domain.Profile{EntryMarker: "■"}
	if section.HasCommandEntries() {
		t.Fatal("a heading marker alone must not turn sections into command entries")
	}
}

func TestSetDiffPrefersDocumentedDifferencesAndExistingPrefixes(t *testing.T) {
	ix := []domain.IndexedCommand{
		{Cmd: "sample-agent [ NAME ]", Source: "p1"},
		{Cmd: "ip old-example enable", Source: "p2"},
		{Cmd: "ip missing-example enable", Source: "p3"},
		{Cmd: "ip missing-example disable", Source: "p4"},
	}
	ixr := []domain.IndexedCommand{{Cmd: "sample-agent ip enable"}}
	known := []domain.DiffRow{{IX: "ip old-example enable", Kind: "renamed", Source: "ch8"}}
	rows := domain.SetDiff(ix, ixr, known)
	if len(rows) != 1 {
		t.Fatalf("got %+v; only the undocumented missing family should remain", rows)
	}
	if r := rows[0]; r.Kind != "absent" || r.IX != "ip missing-example *" || r.Source != "setdiff" || r.RefIX != "crm:p3" {
		t.Fatalf("missing family lost its classification or source: %+v", r)
	}
}

func TestChapterDifferenceExpandsAlternativesAndKeepsReferences(t *testing.T) {
	table := [][]string{
		{"機能", "IXシリーズ", "IX-Rシリーズ", "備考"},
		{"例", "[ ip | ipv6 ] old-example enable", "ip new-example enable\nipv6 new-example enable", "確認"},
	}
	index := []domain.IndexedCommand{
		{Cmd: "ip old-example enable", Source: "p10"},
		{Cmd: "ipv6 old-example enable", Source: "p11"},
	}
	rows := domain.Ch8Rows("renamed", "example.html#change", table, index)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want both protocol alternatives", len(rows))
	}
	for i, prefix := range []string{"ip", "ipv6"} {
		r := rows[i]
		if r.IX != prefix+" old-example enable" || r.IXR != prefix+" new-example enable" ||
			r.RefIX != "crm:"+index[i].Source || r.RefIXR != "fd:example.html#change" || r.Source != "ch8" {
			t.Errorf("alternative %s lost its mapping or source: %+v", prefix, r)
		}
	}
}
