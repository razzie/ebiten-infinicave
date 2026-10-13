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

func TestViewerDisableBackgroundFlag(t *testing.T) {
	oldFlags, oldArgs := flag.CommandLine, os.Args
	t.Cleanup(func() { flag.CommandLine, os.Args = oldFlags, oldArgs })
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"-disable-background"}, true},
		{[]string{"-disable-background=false"}, false},
		{[]string{"-disable-background", "-output", "scene.png"}, true},
	} {
		flag.CommandLine = flag.NewFlagSet("background-options", flag.ContinueOnError)
		os.Args = append([]string{"infinicave"}, tc.args...)
		options, err := parseOptions()
		if err != nil || options.config.DisableBackground != tc.want {
			t.Fatalf("background flag: got %v, want %v, error %v", options.config.DisableBackground, tc.want, err)
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
		{nil, infinicave.DefaultConfig().MushroomDensity},
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

func TestViewerRockGrowthDensityFlags(t *testing.T) {
	oldFlags, oldArgs := flag.CommandLine, os.Args
	t.Cleanup(func() { flag.CommandLine, os.Args = oldFlags, oldArgs })
	defaults := infinicave.DefaultConfig()
	for _, tc := range []struct {
		args          []string
		moss, hanging float64
	}{
		{nil, defaults.MossDensity, defaults.HangingGrowthDensity},
		{[]string{"-moss-density", "0.2", "-hanging-growth-density", "0.1"}, .2, .1},
		{[]string{"-moss-density", "0", "-hanging-growth-density", "1"}, 0, 1},
		{[]string{"-moss-density", "1", "-hanging-growth-density", "0"}, 1, 0},
	} {
		flag.CommandLine = flag.NewFlagSet("rock-growth-density", flag.ContinueOnError)
		os.Args = append([]string{"infinicave"}, tc.args...)
		options, err := parseOptions()
		if err != nil || options.config.MossDensity != tc.moss || options.config.HangingGrowthDensity != tc.hanging {
			t.Fatalf("growth flags: moss=%g hanging=%g, want %g/%g, error %v", options.config.MossDensity, options.config.HangingGrowthDensity, tc.moss, tc.hanging, err)
		}
	}
}

func TestViewerGrassDensityFlag(t *testing.T) {
	oldFlags, oldArgs := flag.CommandLine, os.Args
	t.Cleanup(func() { flag.CommandLine, os.Args = oldFlags, oldArgs })
	for _, tc := range []struct {
		args []string
		want float64
	}{
		{nil, infinicave.DefaultConfig().GrassDensity},
		{[]string{"-grass-density", "0.2"}, .2},
		{[]string{"-grass-density", "0"}, 0},
		{[]string{"-grass-density", "1"}, 1},
	} {
		flag.CommandLine = flag.NewFlagSet("grass-density", flag.ContinueOnError)
		os.Args = append([]string{"infinicave"}, tc.args...)
		options, err := parseOptions()
		if err != nil || options.config.GrassDensity != tc.want {
			t.Fatalf("grass flag: got %g, want %g, error %v", options.config.GrassDensity, tc.want, err)
		}
	}
}

func TestViewerBranchingAndRockEdgeFlags(t *testing.T) {
	oldFlags, oldArgs := flag.CommandLine, os.Args
	t.Cleanup(func() { flag.CommandLine, os.Args = oldFlags, oldArgs })
	defaults := infinicave.DefaultConfig()
	for _, tc := range []struct {
		args              []string
		strategy          infinicave.BranchingStrategy
		spikes, roughness float64
	}{
		{nil, infinicave.BranchingRecursive, defaults.SpikeDensity, defaults.EdgeRoughness},
		{[]string{"-branching", "simple"}, infinicave.BranchingSimple, defaults.SpikeDensity, defaults.EdgeRoughness},
		{[]string{"-branching", "recursive", "-spike-density", "0", "-edge-roughness", "1"}, infinicave.BranchingRecursive, 0, 1},
		{[]string{"-branching", "loops", "-spike-density", "0.3", "-edge-roughness", "0.4"}, infinicave.BranchingLoops, .3, .4},
	} {
		flag.CommandLine = flag.NewFlagSet("rock-shape-options", flag.ContinueOnError)
		os.Args = append([]string{"infinicave"}, tc.args...)
		options, err := parseOptions()
		if err != nil || options.config.BranchingStrategy != tc.strategy || options.config.SpikeDensity != tc.spikes || options.config.EdgeRoughness != tc.roughness {
			t.Fatalf("shape flags: %+v, error %v", options.config, err)
		}
	}
	flag.CommandLine = flag.NewFlagSet("invalid-branching", flag.ContinueOnError)
	os.Args = []string{"infinicave", "-branching", "unknown"}
	if _, err := parseOptions(); err == nil {
		t.Fatal("unknown branching strategy accepted")
	}
}

func TestViewerBranchThicknessFlags(t *testing.T) {
	oldFlags, oldArgs := flag.CommandLine, os.Args
	t.Cleanup(func() { flag.CommandLine, os.Args = oldFlags, oldArgs })
	defaults := infinicave.DefaultConfig()
	for _, tc := range []struct {
		args   []string
		lo, hi float64
	}{
		{nil, defaults.BranchMinThickness, defaults.BranchMaxThickness},
		{[]string{"-branch-min-thickness", "0.04", "-branch-max-thickness", "0.09"}, .04, .09},
		{[]string{"-branch-min-thickness", "0", "-branch-max-thickness", "0"}, 0, 0},
		{[]string{"-branch-min-thickness", "0.06", "-branch-max-thickness", "0.06"}, .06, .06},
	} {
		flag.CommandLine = flag.NewFlagSet("branch-thickness", flag.ContinueOnError)
		os.Args = append([]string{"infinicave"}, tc.args...)
		options, err := parseOptions()
		if err != nil || options.config.BranchMinThickness != tc.lo || options.config.BranchMaxThickness != tc.hi {
			t.Fatalf("thickness flags: %+v, error %v", options.config, err)
		}
	}
}
