package infinicave

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestBranchingConfigValidation(t *testing.T) {
	if DefaultConfig().BranchingStrategy != BranchingRecursive || (Config{}).BranchingStrategy != BranchingRecursive {
		t.Fatal("recursive must be the default, including for a zero Config")
	}
	for _, strategy := range []BranchingStrategy{BranchingSimple, BranchingRecursive, BranchingLoops} {
		parsed, err := ParseBranchingStrategy(strategy.String())
		if err != nil || parsed != strategy {
			t.Fatalf("strategy round trip: %v %v", parsed, err)
		}
	}
	if _, err := ParseBranchingStrategy("random"); err == nil {
		t.Fatal("unknown strategy accepted")
	}
	if err := (Config{BranchingStrategy: BranchingStrategy(255)}).validate(); err == nil {
		t.Fatal("invalid strategy accepted")
	}
	for _, value := range []float64{-1, 1.01, math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, c := range []Config{{SpikeDensity: value}, {EdgeRoughness: value}} {
			if c.validate() == nil {
				t.Fatalf("accepted invalid shape setting: %+v", c)
			}
		}
	}
	for _, value := range []float64{0, .5, 1} {
		if (Config{SpikeDensity: value, EdgeRoughness: value}).validate() != nil {
			t.Fatalf("rejected valid setting %g", value)
		}
	}
}

func TestSceneRockShapeMatchesGenerationAndSurvivesReset(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, strategy := range []BranchingStrategy{BranchingSimple, BranchingRecursive, BranchingLoops} {
			t.Run(orientation.String()+"/"+strategy.String(), func(t *testing.T) {
				config := Config{Seed: 42, Orientation: orientation, ForceGuidesUp: true, BranchingStrategy: strategy, BranchMinThickness: .052, BranchMaxThickness: .072, SpikeDensity: .8, EdgeRoughness: .7, LoadSection: func(id int64) SectionContent {
					if id != 0 {
						return SectionContent{}
					}
					return SectionContent{Guides: []Guide{{Pts: []V{{X: .1, Y: .3}, {X: .9, Y: .3}}}}}
				}}
				if orientation == Horizontal {
					config.LoadSection = func(id int64) SectionContent {
						if id != 0 {
							return SectionContent{}
						}
						return SectionContent{Guides: []Guide{{Pts: []V{{X: .3, Y: .1}, {X: .3, Y: .9}}}}}
					}
				}
				expected, err := GenerateSectionWithConfig(config, 0)
				if err != nil {
					t.Fatal(err)
				}
				scene, err := NewScene(config)
				if err != nil {
					t.Fatal(err)
				}
				defer scene.Close()
				for attempt := 0; attempt < 2; attempt++ {
					if attempt > 0 {
						scene.Reset(config.Seed)
					}
					scene.world.request(0)
					select {
					case result := <-scene.world.terrain:
						collision := terrain.OrientedCollision(orientation, result.geometry.Collision)
						if !reflect.DeepEqual(collision, expected.Collision) {
							t.Fatal("streaming or reset changed configured rock geometry")
						}
					case <-time.After(20 * time.Second):
						t.Fatal("streamed terrain did not complete")
					}
				}
			})
		}
	}
}

func TestBranchThicknessConfigValidation(t *testing.T) {
	for _, value := range []float64{-1, 1.01, math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, c := range []Config{{BranchMinThickness: value}, {BranchMaxThickness: value}} {
			if c.validate() == nil {
				t.Fatalf("accepted invalid thickness: %+v", c)
			}
		}
	}
	for _, c := range []Config{
		{BranchMinThickness: .09, BranchMaxThickness: .05},
		{BranchMinThickness: .09},
		{BranchMaxThickness: .04},
	} {
		if c.validate() == nil {
			t.Fatalf("accepted reversed resolved bounds: %+v", c)
		}
	}
	for _, c := range []Config{
		{}, DefaultConfig(),
		{BranchMinThickness: .05}, {BranchMaxThickness: .09},
		{BranchMinThickness: .06, BranchMaxThickness: .06},
		{BranchMinThickness: .001, BranchMaxThickness: 1},
	} {
		if err := c.validate(); err != nil {
			t.Fatalf("rejected valid thickness: %+v: %v", c, err)
		}
	}
	lo, hi := (Config{}).shapeOptions().BranchThicknessBounds()
	defaults := DefaultConfig()
	if lo != defaults.BranchMinThickness || hi != defaults.BranchMaxThickness {
		t.Fatal("zero Config thickness differs from viewer defaults")
	}
}
