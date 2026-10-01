package infrastructure

import (
	"errors"
	"fmt"
	"image"
	"path/filepath"
	"runtime"
	"testing"
)

type failedPNGFile struct {
	writeErr error
	closeErr error
	closed   bool
}

func (f *failedPNGFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *failedPNGFile) Close() error {
	f.closed = true
	return f.closeErr
}

func TestPNGReportsFlushAndCloseFailures(t *testing.T) {
	writeFailure := errors.New("disk full during flush")
	closeFailure := errors.New("disk failed during close")
	for _, file := range []*failedPNGFile{
		{closeErr: closeFailure},
		{writeErr: writeFailure},
		{writeErr: writeFailure, closeErr: closeFailure},
	} {
		err := writePNG(file, image.NewGray(image.Rect(0, 0, 40, 40)))
		if !file.closed || (file.writeErr != nil && !errors.Is(err, file.writeErr)) ||
			(file.closeErr != nil && !errors.Is(err, file.closeErr)) {
			t.Errorf("writePNG = %v, closed = %v", err, file.closed)
		}
	}
}

func TestReadSpreadsCanStopBeforeConsumingPrefetch(t *testing.T) {
	old := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(old)
	dir := t.TempDir()
	paths := make([]string, 0, 6)
	for i := range 6 {
		path := filepath.Join(dir, fmt.Sprintf("page%d.png", i))
		if err := savePNG(path, image.NewGray(image.Rect(0, 0, 40, 40))); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	visits := 0
	for spread, err := range ReadSpreads(paths) {
		if err != nil || spread.Pages() != 1 {
			t.Fatalf("spread = %v, %v", spread, err)
		}
		visits++
		break
	}
	if visits != 1 {
		t.Fatalf("visits = %d", visits)
	}
}
