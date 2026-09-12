package infrastructure

import (
	"fmt"
	"image"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReadSpreadsCanStopBeforeConsumingPrefetch(t *testing.T) {
	old := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(old)
	dir := t.TempDir()
	var paths []string
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
