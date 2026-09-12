package application_test

import (
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/yuu61/ix-toolkit/internal/manualbook/application"
)

func TestScanReportsImageFailures(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		for _, mixed := range []bool{false, true} {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "page001.png"), []byte("broken PNG"), 0600); err != nil {
				t.Fatal(err)
			}
			if mixed {
				writeScanPNG(t, filepath.Join(dir, "page002.png"))
			}
			err := application.Scan(dir, "", dryRun)
			var reported application.ReportedError
			if !errors.As(err, &reported) || reported.ExitCode() != 1 {
				t.Errorf("dryRun=%v mixed=%v: error = %v, want ReportedError(1)", dryRun, mixed, err)
			}
			if mixed && !dryRun {
				if _, err := os.Stat(filepath.Join(dir, "processed", "page001.png")); err != nil {
					t.Errorf("valid input was not processed: %v", err)
				}
			}
		}
	}
}

func TestScanSuccessfulImage(t *testing.T) {
	dir := t.TempDir()
	writeScanPNG(t, filepath.Join(dir, "page001.png"))
	if err := application.Scan(dir, "", false); err != nil {
		t.Fatal(err)
	}
}

func TestScanReportsOutputFailure(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	writeScanPNG(t, filepath.Join(dir, "page001.png"))
	if err := os.Mkdir(filepath.Join(out, "page001.png"), 0700); err != nil {
		t.Fatal(err)
	}
	err := application.Scan(dir, out, false)
	var reported application.ReportedError
	if !errors.As(err, &reported) || reported.ExitCode() != 1 {
		t.Errorf("error = %v, want ReportedError(1)", err)
	}
}

func writeScanPNG(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encodeErr := png.Encode(f, image.NewGray(image.Rect(0, 0, 40, 40)))
	closeErr := f.Close()
	if err := errors.Join(encodeErr, closeErr); err != nil {
		t.Fatal(err)
	}
}
