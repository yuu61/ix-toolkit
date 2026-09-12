package infrastructure

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
)

func TestTableMarkdownRoundTrip(t *testing.T) {
	want := [][]string{
		{"name", "value", "last"},
		{`<ARG> & >`, `a\|b\\|c`, `trailing\`},
		{"line one\nline two", `<br> &lt;ARG&gt; &#124;`, `\<br>\&amp;`},
		{"", "日本語\x00text", ""},
	}
	var output strings.Builder
	renderTableBlock(&output, domain.Block{Rows: want}, links{})
	var got [][]string
	for _, line := range strings.Split(output.String(), "\n") {
		if mdTableRe.MatchString(line) && !mdRuleRe.MatchString(line) {
			got = append(got, splitTableRow(line))
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestReadCh8DecodesTableEscapes(t *testing.T) {
	dir := t.TempDir()
	index := "section\ttitle\tfile\tline\tsource\n8\tIXシリーズとの差分\tch8.md\t1\tch8\n8.3.5\tその他変更があるコマンド\tch8.md\t2\tchanges\n"
	if err := os.WriteFile(filepath.Join(dir, "sections.tsv"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	command := `sample <ARG> & path\file`
	note := "literal <br> &lt;ARG&gt; & back\\slash | pipe\nnext line"
	var md strings.Builder
	md.WriteString("## 8 IXシリーズとの差分\n### 8.3.5 その他変更があるコマンド\n")
	renderTableBlock(&md, domain.Block{Rows: [][]string{{"対象コマンド", "IXシリーズからの変更内容"}, {command, note}}}, links{})
	if err := os.WriteFile(filepath.Join(dir, "ch8.md"), []byte(md.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadCh8(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].IX != command || rows[0].Note != note || rows[0].RefIXR != "fd:changes" {
		t.Fatalf("Ch8 rows = %#v", rows)
	}
	out := filepath.Join(dir, "diff.tsv")
	if err := WriteDiffTSV(out, rows); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\t"+command+"\t") || !strings.Contains(string(data), strings.ReplaceAll(note, "\n", " / ")) {
		t.Fatalf("diff.tsv = %s", data)
	}
}
