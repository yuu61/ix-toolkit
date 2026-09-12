package application_test

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
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

func TestScanPreservesNumericOrderAndSplitGeometry(t *testing.T) {
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			old := runtime.GOMAXPROCS(workers)
			defer runtime.GOMAXPROCS(old)
			dir := t.TempDir()
			writeScanPNG(t, filepath.Join(dir, "page010.png"))
			writeScanSpread(t, filepath.Join(dir, "page002.png"))
			if err := application.Scan(dir, "", false); err != nil {
				t.Fatal(err)
			}
			for i, width := range []int{20, 20, 40} {
				path := filepath.Join(dir, "processed", fmt.Sprintf("page%03d.png", i+1))
				f, err := os.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				img, err := png.Decode(f)
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
				if img.Bounds().Dx() != width || img.Bounds().Dy() != 36 {
					t.Fatalf("%s: size = %v", path, img.Bounds())
				}
				if i == 0 && color.GrayModel.Convert(img.At(0, 0)).(color.Gray).Y != 0 {
					t.Fatal("left page lost")
				}
				if i == 1 && color.GrayModel.Convert(img.At(19, 0)).(color.Gray).Y != 80 {
					t.Fatal("right page lost")
				}
			}
		})
	}
}

func TestScanDryRunCreatesNoOutput(t *testing.T) {
	dir := t.TempDir()
	writeScanSpread(t, filepath.Join(dir, "page002.png"))
	if err := application.Scan(dir, "", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "processed")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created output: %v", err)
	}
}

func TestScanContinuesAfterSplitWriteFailure(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	writeScanSpread(t, filepath.Join(dir, "page001.png"))
	writeScanPNG(t, filepath.Join(dir, "page002.png"))
	if err := os.Mkdir(filepath.Join(out, "page002.png"), 0700); err != nil {
		t.Fatal(err)
	}
	var reported application.ReportedError
	if err := application.Scan(dir, out, false); !errors.As(err, &reported) {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "page001.png")); !os.IsNotExist(err) {
		t.Fatalf("left half remains after right half failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "page003.png")); err != nil {
		t.Fatalf("next input was not saved: %v", err)
	}
}

func writeScanSpread(t *testing.T, path string) {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			shade := uint8(255)
			if x < 4 {
				shade = 0
			} else if x >= 36 {
				shade = 80
			}
			img.SetGray(x, y, color.Gray{Y: shade})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encodeErr := png.Encode(f, img)
	if err := errors.Join(encodeErr, f.Close()); err != nil {
		t.Fatal(err)
	}
}
