package infrastructure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
)

func TestLocalPDFHeadings(t *testing.T) {
	root := os.Getenv("IX_MANUALBOOK_PDF_ROOT")
	if root == "" {
		t.Skip("set IX_MANUALBOOK_PDF_ROOT to check local PDFs")
	}
	pdf := filepath.Join(root, "pdf", "FD-ver10.11-1.1.pdf")
	p, err := LoadProfile(filepath.Join(root, "profiles", "nec-ix-fd.json"))
	if err != nil {
		t.Fatal(err)
	}
	pages, err := readSectionPages(p, pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer CloseDoc(pdf)
	heads, _ := parseHeadings(p, pages)
	if len(heads) != 1295 {
		t.Fatalf("headings = %d, want 1295", len(heads))
	}
	var body strings.Builder
	found := false
	tables := 0
	for _, h := range heads {
		if h.Number == "2.43.8.17" && h.Ref.Page == 673 {
			found = true
		}
		if strings.HasPrefix(h.Number, "192.") || strings.HasPrefix(h.Number, "10.2.") {
			t.Errorf("IP address became heading: %s", h.Number)
		}
		// p280 の NAT 評価手順は節番号ではない。
		if h.Ref.Page == 280 && !strings.HasPrefix(h.Number, "2.19.5") {
			t.Errorf("step became heading: %s", h.Number)
		}
		for _, b := range h.Blocks {
			body.WriteString(strings.Join(b.Lines, "\n"))
			if b.Kind == domain.BlockTable {
				tables++
			}
		}
	}
	if !found {
		t.Error("four-part SSH/Telnet heading missing")
	}
	if tables != 945 {
		t.Errorf("tables = %d, want 945", tables)
	}
	text := headingTextKey(body.String())
	for _, want := range []string{"192.168.0.3 192.168.0.2", "1.1 NAPT キャッシュに該当する場合は、変換して終了"} {
		if !strings.Contains(text, headingTextKey(want)) {
			t.Errorf("rejected heading missing from body: %s", want)
		}
	}
}
