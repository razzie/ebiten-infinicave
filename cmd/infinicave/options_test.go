package main

import (
	"testing"

	infinicave "github.com/razzie/ebiten-infinicave"
)

func TestViewerModesAndWindowSizing(t *testing.T) {
	for _, tc := range []struct {
		name          string
		mode          infinicave.Orientation
		width, height int
	}{
		{"portrait", infinicave.Vertical, 486, 864},
		{"landscape", infinicave.Horizontal, 1536, 864},
	} {
		mode, err := parseMode(tc.name)
		if err != nil || mode != tc.mode {
			t.Fatalf("mode %q: %v / %v", tc.name, mode, err)
		}
		w, h := windowSize(mode, 1920, 1080)
		if w != tc.width || h != tc.height {
			t.Fatalf("mode %q window: %d x %d", tc.name, w, h)
		}
	}
	for _, name := range []string{"", "horizontal", "vertical", "Landscape", "square"} {
		if _, err := parseMode(name); err == nil {
			t.Fatalf("accepted invalid mode %q", name)
		}
	}
}
