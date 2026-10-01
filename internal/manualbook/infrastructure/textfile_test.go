package infrastructure

import (
	"os"
	"path/filepath"
	"testing"
)

// writeWithBOM は Windows PowerShell 5.1 の Set-Content -Encoding UTF8 と同じく、
// 先頭に BOM を付けて書く。
func writeWithBOM(t *testing.T, dir, name, text string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(utf8BOM+text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// 手で直すファイルは BOM 付きでも読める。
func TestHandEditedFilesAcceptUTF8BOM(t *testing.T) {
	dir := t.TempDir()

	m, err := ReadManifest(writeWithBOM(t, dir, "manifest.json", `{"docs": [{"series": "ix-r", "book": "crm", "version": "1.5a"}]}`))
	if err != nil || len(m.Docs) != 1 || m.Docs[0].Book != "crm" {
		t.Errorf("ReadManifest: err = %v, docs = %+v", err, m.Docs)
	}

	p, err := LoadProfile(writeWithBOM(t, dir, "profile.json", `{"name": "bom"}`))
	if err != nil || p.Name != "bom" {
		t.Errorf("LoadProfile: err = %v, profile = %+v", err, p)
	}

	// 先頭のコメント行が BOM のせいでコメントに見えないと、見出し行の検査で止まる。
	rows, err := ReadDerived(writeWithBOM(t, dir, "derived.tsv",
		"# comment\r\nkind\tix\tix-r\tref_ix\tref_ixr\tnote\r\nremoved\ta\t\t\t\tn\r\n"))
	if err != nil || len(rows) != 1 || rows[0].IX != "a" || rows[0].Note != "n" {
		t.Errorf("ReadDerived: err = %v, rows = %+v", err, rows)
	}
}
