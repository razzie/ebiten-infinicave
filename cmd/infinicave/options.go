package main

import (
	"flag"
	"fmt"
	infinicave "github.com/razzie/ebiten-infinicave"
	"math/rand"
)

// viewerOptions translates command-line flags into library and viewer settings.
type viewerOptions struct {
	config infinicave.Config
	output string
	hover  bool
	ribbon ribbonMode
}

func parseOptions() (viewerOptions, error) {
	defaults := infinicave.DefaultConfig()
	antialiasing := flag.String("antialiasing", defaults.Antialiasing.String(), "antialiasing: full (geometry), off, fxaa (final scene filter)")
	mode := flag.String("mode", "portrait", "scrolling mode: portrait (vertical) or landscape (horizontal)")
	retainSections := flag.Int("retain-sections", defaults.RetainSections, "loaded sections to retain beyond each end of the viewport (nonnegative; immediate neighbors always stay loaded)")
	lookaheadSections := flag.Int("lookahead-sections", defaults.LookaheadSections, "extra sections to prefetch beyond the immediate neighbor in the direction of travel (0 disables lookahead)")
	forceGuidesUp := flag.Bool("force-guides-up", defaults.ForceGuidesUp, "force exposed guide sides to face upward regardless of point order")
	branching := flag.String("branching", defaults.BranchingStrategy.String(), "guide branching strategy: simple, recursive, loops")
	branchMinThickness := flag.Float64("branch-min-thickness", defaults.BranchMinThickness, "minimum nominal branch thickness in scene units (0 uses default 0.048; range 0-1)")
	branchMaxThickness := flag.Float64("branch-max-thickness", defaults.BranchMaxThickness, "maximum nominal branch thickness in scene units (0 uses default 0.08; range 0-1)")
	spikeDensity := flag.Float64("spike-density", defaults.SpikeDensity, "small rock edge spike probability (0 disables spikes, range 0-1)")
	edgeRoughness := flag.Float64("edge-roughness", defaults.EdgeRoughness, "shallow angular rock edge roughness (0 disables chips, range 0-1)")
	seed := flag.Int64("seed", rand.Int63(), "random seed (random by default)")
	output := flag.String("output", "", "save the first 2400 pixels along the scrolling axis as a PNG and exit")
	texture := flag.Float64("texture", 8, "surface texture strength (0 disables it, range 0-16)")
	disableBackground := flag.Bool("disable-background", defaults.DisableBackground, "disable background rocks, vines, blur, and shadows (fog is controlled separately)")
	backgroundBlur := flag.Float64("background-blur", defaults.BackgroundBlur, "background rock and vine blur in scene units (0 disables it, range 0-0.05)")
	shadowOpacity := flag.Float64("shadow-opacity", defaults.ShadowOpacity, "dynamic rock shadow opacity (0 disables it, range 0-1)")
	shadowBlur := flag.Float64("shadow-blur", defaults.ShadowBlur, "dynamic rock shadow softness in scene units (range 0-0.05)")
	shadowX := flag.Float64("shadow-x", defaults.ShadowOffset.X, "rock shadow horizontal offset in scene units (range -1 to 1)")
	shadowY := flag.Float64("shadow-y", defaults.ShadowOffset.Y, "rock shadow vertical offset in scene units (positive is down, range -1 to 1)")
	fog := flag.Float64("fog", defaults.Fog, "moving fog strength between background and foreground layers (0 disables it, values above 1 increase density)")
	batsPerMinute := flag.Float64("bats-per-minute", defaults.BatsPerMinute, "average bat arrivals per minute (0 disables them, interactive viewer only)")
	decorationBurn := flag.Bool("decoration-burn", defaults.DecorationBurn, "burn dissolve decorations removed by carving")
	mushroomDensity := flag.Float64("mushroom-density", defaults.MushroomDensity, "mushroom cluster occurrence probability (0 disables mushrooms, range 0-1)")
	mossDensity := flag.Float64("moss-density", defaults.MossDensity, "green and yellow moss coverage on upward-facing rocks (0 disables moss, range 0-1)")
	grassDensity := flag.Float64("grass-density", defaults.GrassDensity, "yellow or green grass tuft coverage beside guides on upward-facing rocks (0 disables grass, range 0-1)")
	hangingGrowthDensity := flag.Float64("hanging-growth-density", defaults.HangingGrowthDensity, "hanging cave bulb and lichen occurrence probability (0 disables them, range 0-1)")
	webDensity := flag.Float64("spider-web-density", defaults.SpiderWebDensity, "spider web occurrence probability per eligible pocket (0 disables webs, range 0-1)")
	webMaxSize := flag.Float64("spider-web-max-size", defaults.SpiderWebMaxSize, "maximum spider web span including attachments in scene units (0 uses the default 0.15; cave cross-axis spans 1)")
	view := flag.String("view", "shaded", "terrain view: shaded, clay, height, normals, shadows")
	hover := flag.Bool("hover", false, "highlight foreground rocks and guide lines under the mouse")
	ribbon := flag.String("ribbon", "off", "mouse ribbon trail: off, back (behind rocks), front (above rocks, mushrooms, and webs, below grass and bats); interactive viewer only")
	tolerance := flag.Float64("collision-tolerance", 0, "collision polygon simplification tolerance in scene units (one section is 1 by 1)")
	flag.Parse()
	config := defaults
	aa, err := infinicave.ParseAntialiasingMode(*antialiasing)
	if err != nil {
		return viewerOptions{}, err
	}
	config.Antialiasing = aa
	orientation, err := parseMode(*mode)
	if err != nil {
		return viewerOptions{}, err
	}
	config.Orientation = orientation
	config.RetainSections, config.LookaheadSections = *retainSections, *lookaheadSections
	config.ForceGuidesUp = *forceGuidesUp
	config.BranchingStrategy, err = infinicave.ParseBranchingStrategy(*branching)
	if err != nil {
		return viewerOptions{}, err
	}
	config.SpikeDensity, config.EdgeRoughness = *spikeDensity, *edgeRoughness
	config.BranchMinThickness, config.BranchMaxThickness = *branchMinThickness, *branchMaxThickness
	config.Seed, config.Texture = *seed, *texture
	config.DisableBackground = *disableBackground
	config.BackgroundBlur, config.ShadowOpacity, config.ShadowBlur = *backgroundBlur, *shadowOpacity, *shadowBlur
	config.ShadowOffset = infinicave.V{X: *shadowX, Y: *shadowY}
	config.Fog = *fog
	config.BatsPerMinute = *batsPerMinute
	config.DecorationBurn = *decorationBurn
	config.MushroomDensity = *mushroomDensity
	config.MossDensity = *mossDensity
	config.GrassDensity = *grassDensity
	config.HangingGrowthDensity = *hangingGrowthDensity
	config.SpiderWebMaxSize = *webMaxSize
	config.SpiderWebDensity = *webDensity
	if *output != "" {
		config.BatsPerMinute = 0
	}
	parsedView, err := infinicave.ParseView(*view)
	if err != nil {
		return viewerOptions{}, err
	}
	config.View = parsedView
	config.CollisionTolerance = *tolerance
	ribbonMode, err := parseRibbonMode(*ribbon)
	if err != nil {
		return viewerOptions{}, err
	}
	return viewerOptions{config: config, output: *output, hover: *hover, ribbon: ribbonMode}, nil
}

func parseMode(mode string) (infinicave.Orientation, error) {
	switch mode {
	case "portrait":
		return infinicave.Vertical, nil
	case "landscape":
		return infinicave.Horizontal, nil
	default:
		return infinicave.Vertical, fmt.Errorf("unknown mode %q: use portrait or landscape", mode)
	}
}

func windowSize(mode infinicave.Orientation, displayWidth, displayHeight int) (int, int) {
	if mode == infinicave.Horizontal {
		width := max(1, displayWidth*4/5)
		return width, max(1, width*9/16)
	}
	height := max(1, displayHeight*4/5)
	return max(1, height*9/16), height
}
