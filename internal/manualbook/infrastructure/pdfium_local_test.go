package infrastructure

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/klippa-app/go-pdfium/requests"
)

func TestLocalPDFPageWorkers(t *testing.T) {
	root := os.Getenv("IX_MANUALBOOK_PDF_ROOT")
	if root == "" {
		t.Skip("set IX_MANUALBOOK_PDF_ROOT to check local PDFs")
	}
	pdf := filepath.Join(root, "pdf", "FD-ver10.11-1.1.pdf")
	d, err := openDoc(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer CloseDoc(pdf)
	// 同じ実 PDF の先頭 8 ページで、逐次経路と並列経路を両方通す。
	sample := *d
	sample.pages = d.pages[:8]
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(0))
	for _, n := range []int{1, 4} {
		runtime.GOMAXPROCS(n)
		failure := errors.New("page conversion failed")
		if err := sample.forPages(func(worker *pdfDoc, i int) error {
			if i == 0 {
				return failure
			}
			_, err := worker.instance.GetPageSize(&requests.GetPageSize{Page: worker.page(i)})
			return err
		}); !errors.Is(err, failure) {
			t.Fatalf("workers=%d: error = %v, want %v", n, err, failure)
		}
		// 失敗後にも主インスタンスを使え、追加インスタンスを取得し直せる。
		// 取り忘れた資源や、失敗時に閉じられた主文書があればここで失敗する。
		visits := make([]int, len(sample.pages))
		sizes := make([][2]float64, len(sample.pages))
		if err := sample.forPages(func(worker *pdfDoc, i int) error {
			size, err := worker.instance.GetPageSize(&requests.GetPageSize{Page: worker.page(i)})
			if err != nil {
				return err
			}
			visits[i]++
			sizes[i] = [2]float64{size.Width, size.Height}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		for i, pg := range sample.pages {
			if visits[i] != 1 || sizes[i] != [2]float64{pg.width, pg.height} {
				t.Errorf("workers=%d page=%d: visits=%d size=%v", n, i, visits[i], sizes[i])
			}
		}
	}
}
