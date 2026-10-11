package main

import (
	"flag"
	"os"
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

func TestViewerForceGuidesUpFlag(t *testing.T) {
	oldFlags, oldArgs := flag.CommandLine, os.Args
	t.Cleanup(func() { flag.CommandLine, os.Args = oldFlags, oldArgs })
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{nil, infinicave.DefaultConfig().ForceGuidesUp},
		{[]string{"-force-guides-up"}, true},
		{[]string{"-force-guides-up=false"}, false},
	} {
		flag.CommandLine = flag.NewFlagSet("guide-options", flag.ContinueOnError)
		os.Args = append([]string{"infinicave"}, tc.args...)
		options, err := parseOptions()
		if err != nil || options.config.ForceGuidesUp != tc.want {
			t.Fatalf("guide flag: got %v, want %v, error %v", options.config.ForceGuidesUp, tc.want, err)
		}
	}
}

func TestViewerSpiderWebMaximumSizeFlag(t *testing.T) {
	oldFlags, oldArgs := flag.CommandLine, os.Args
	t.Cleanup(func() { flag.CommandLine, os.Args = oldFlags, oldArgs })
	for _, tc := range []struct {
		args []string
		want float64
	}{
		{nil, .15},
		{[]string{"-spider-web-max-size", "0.08"}, .08},
		{[]string{"-spider-web-max-size", "0"}, 0},
	} {
		flag.CommandLine = flag.NewFlagSet("web-options", flag.ContinueOnError)
		os.Args = append([]string{"infinicave"}, tc.args...)
		options, err := parseOptions()
		if err != nil || options.config.SpiderWebMaxSize != tc.want {
			t.Fatalf("size flag: got %g, want %g, error %v", options.config.SpiderWebMaxSize, tc.want, err)
		}
	}
}

func TestViewerSpiderWebDensityFlag(t *testing.T) {
	oldFlags, oldArgs := flag.CommandLine, os.Args
	t.Cleanup(func() { flag.CommandLine, os.Args = oldFlags, oldArgs })
	for _, tc := range []struct {
		args []string
		want float64
	}{
		{nil, .35},
		{[]string{"-spider-web-density", "0.1"}, .1},
		{[]string{"-spider-web-density", "0"}, 0},
		{[]string{"-spider-web-density", "1"}, 1},
	} {
		flag.CommandLine = flag.NewFlagSet("web-density", flag.ContinueOnError)
		os.Args = append([]string{"infinicave"}, tc.args...)
		options, err := parseOptions()
		if err != nil || options.config.SpiderWebDensity != tc.want {
			t.Fatalf("density flag: got %g, want %g, error %v", options.config.SpiderWebDensity, tc.want, err)
		}
	}
}

func TestViewerMushroomDensityFlag(t *testing.T) {
	oldFlags, oldArgs := flag.CommandLine, os.Args
	t.Cleanup(func() { flag.CommandLine, os.Args = oldFlags, oldArgs })
	for _, tc := range []struct {
		args []string
		want float64
	}{
		{nil, 1},
		{[]string{"-mushroom-density", "0.5"}, .5},
		{[]string{"-mushroom-density", "0"}, 0},
		{[]string{"-mushroom-density", "1"}, 1},
	} {
		flag.CommandLine = flag.NewFlagSet("mushroom-density", flag.ContinueOnError)
		os.Args = append([]string{"infinicave"}, tc.args...)
		options, err := parseOptions()
		if err != nil || options.config.MushroomDensity != tc.want {
			t.Fatalf("density flag: got %g, want %g, error %v", options.config.MushroomDensity, tc.want, err)
		}
	}
}
