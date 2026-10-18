package render

import (
	"image"
	"image/color"
	"math"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func BenchmarkPrepareBackgroundVines(b *testing.B) {
	data := terrain.BuildSection(42, 0)
	grid := terrain.ShadeRockFaces(data.Background, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		prepareRockVines(data.Vines, grid)
	}
}

func TestBackgroundVinesFadeWithBorderRock(t *testing.T) {
	vines := []terrain.Vine{{Parent: -1, Points: []terrain.VinePoint{{P: geom.V{X: -.25, Y: .2}, Radius: .001}, {P: geom.V{X: -.25, Y: .4}, Radius: .001}}}}
	base, faded := PrepareVines(vines), prepareRockVines(vines, nil)
	for i, mesh := range faded {
		for j, v := range mesh.Vertices {
			fade := float32(horizontalFade(float64(v.DstX) / RasterPixelsPerUnit))
			old := base[i].Vertices[j]
			if math.Abs(float64(v.ColorA-old.ColorA*fade)) > 1e-6 || math.Abs(float64(v.ColorR-old.ColorR*fade)) > 1e-6 {
				t.Fatal("background vine alpha or material ignores the border fade")
			}
		}
	}
}

func TestVinesFollowLocalRockBrightness(t *testing.T) {
	for _, orientation := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		t.Run(orientation.String(), func(t *testing.T) {
			for _, foreground := range []bool{false, true} {
				name := "background"
				if foreground {
					name = "foreground"
				}
				t.Run(name, func(t *testing.T) {
					vine := terrain.Vine{Parent: -1, Foreground: foreground}
					var grid terrain.RockGrid
					// Repeat black, gray, and white cells in the left padding,
					// middle, and right padding; exercise stems and leaves.
					for i, tone := range []uint8{0, 10, 255, 0, 10, 255, 0, 10, 255} {
						x := -.45 + .75*float64(i/3) + .1*float64(i%3)
						cell := terrainRect(x, .1, .1, .6)
						cell.Raised = foreground
						cell.Color = color.NRGBA{R: tone, G: tone, B: tone, A: 255}
						cell.Normal = geom.V3{Z: 1}
						grid = append(grid, terrain.WithRockOrientation(cell, orientation))
						p := geom.V{X: x + .05, Y: .4}
						vine.Points = append(vine.Points, terrain.VinePoint{P: p, Radius: .003})
						vine.Leaves = append(vine.Leaves, terrain.VineLeaf{Anchor: p, Polygon: [4]geom.V{p, p.Add(geom.V{X: .01, Y: .01}), p.Add(geom.V{X: .02, Y: .04}), p.Add(geom.V{X: -.01, Y: .02})}})
					}
					vines := []terrain.Vine{vine}
					plain := prepareRockVines(vines, nil)
					lit := prepareRockVines(vines, grid)
					for pass := range 2 {
						previousShade := 0.0
						for i, point := range vine.Points {
							p := point.P.Add(geom.V{Y: .001})
							if pass == 1 {
								leaf := vine.Leaves[i].Polygon
								p = leaf[0].Add(leaf[1]).Add(leaf[2]).Mul(1.0 / 3)
							}
							v, base := sampleVineMesh(t, lit[pass], p), sampleVineMesh(t, plain[pass], p)
							shade := float64(v.ColorG / base.ColorG)
							black := .38
							if !foreground {
								black = 0
							}
							if i%3 == 0 && math.Abs(shade-black) > 1e-5 {
								t.Fatal("black rock did not darken the vine")
							}
							if i%3 == 2 && shade <= 1 {
								t.Fatal("light rock did not lighten the vine")
							}
							if i%3 > 0 && shade <= previousShade {
								t.Fatal("vine brightness does not follow the rock cells")
							}
							if math.Abs(float64(v.ColorA-base.ColorA)) > 1e-5 {
								t.Fatal("rock shading changed vine opacity")
							}
							previousShade = shade
						}
					}
					if !reflect.DeepEqual(prepareRockVines(vines, nil), plain) || !reflect.DeepEqual(prepareRockVines(vines, grid), lit) {
						t.Fatal("rock shading is not deterministic or changed unsupported vines")
					}

					data := terrain.SectionData{Orientation: orientation}
					part := terrain.BackgroundVinesPart
					if foreground {
						data.Foreground, data.ForegroundVines = grid, vines
						data.ForegroundBake = terrain.NewRockTopology(grid)
						data.ForegroundBake.Appearance = grid
						part = terrain.ForegroundVinesPart
					} else {
						data.Background, data.Vines = grid, vines
					}
					full := PrepareSection(data, ViewShaded)
					FinishSectionMesh(&full)
					streamed := PrepareVegetationParts(data, ViewShaded, part)
					if foreground {
						data.Foreground, data.ForegroundTopology = nil, &terrain.RockTopology{}
						replayed := PrepareVegetationParts(data, ViewShaded, part)
						if !reflect.DeepEqual(streamed.ForegroundVines, replayed.ForegroundVines) {
							t.Fatal("carving changed shading from the original rock bake")
						}
					}
					fullMeshes, streamedMeshes := full.Vines, streamed.Vines
					if foreground {
						fullMeshes, streamedMeshes = full.ForegroundVines, streamed.ForegroundVines
					}
					for i, mesh := range fullMeshes {
						for j, v := range mesh.Vertices {
							other := streamedMeshes[i].Vertices[j]
							if v.ColorR != other.ColorR || v.ColorG != other.ColorG || v.ColorB != other.ColorB || v.ColorA != other.ColorA {
								t.Fatal("streaming and synchronous preparation use different vine shading")
							}
						}
					}
				})
			}
		})
	}
}

// Sample rendered triangles, including newly inserted cell-boundary vertices.
func sampleVineMesh(t *testing.T, mesh TriangleMesh, p geom.V) ebiten.Vertex {
	t.Helper()
	p = geom.V{X: p.X * RasterPixelsPerUnit, Y: (p.Y - terrain.GenerationMinY) * RasterPixelsPerUnit}
	for i := 0; i < len(mesh.Indices); i += 3 {
		a, b, c := mesh.Vertices[mesh.Indices[i]], mesh.Vertices[mesh.Indices[i+1]], mesh.Vertices[mesh.Indices[i+2]]
		pa, pb, pc := geom.V{X: float64(a.DstX), Y: float64(a.DstY)}, geom.V{X: float64(b.DstX), Y: float64(b.DstY)}, geom.V{X: float64(c.DstX), Y: float64(c.DstY)}
		area := geom.Cross(pb.Sub(pa), pc.Sub(pa))
		if math.Abs(area) < 1e-9 {
			continue
		}
		u, v := geom.Cross(pb.Sub(p), pc.Sub(p))/area, geom.Cross(pc.Sub(p), pa.Sub(p))/area
		w := 1 - u - v
		if min(u, v, w) < -1e-5 {
			continue
		}
		mix := func(x, y, z float32) float32 { return float32(u*float64(x) + v*float64(y) + w*float64(z)) }
		return ebiten.Vertex{ColorR: mix(a.ColorR, b.ColorR, c.ColorR), ColorG: mix(a.ColorG, b.ColorG, c.ColorG), ColorB: mix(a.ColorB, b.ColorB, c.ColorB), ColorA: mix(a.ColorA, b.ColorA, c.ColorA)}
	}
	t.Fatalf("vine mesh does not cover %v", p)
	return ebiten.Vertex{}
}

func TestBackgroundVinesShadeCellsBetweenSamples(t *testing.T) {
	for _, orientation := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		var grid terrain.RockGrid
		for i, tone := range []uint8{20, 0, 20} {
			c := terrainRect(-.45+.2*float64(i), .1, .2, .7)
			c.Raised = false
			c.Color = color.NRGBA{R: tone, G: tone, B: tone, A: 255}
			grid = append(grid, terrain.WithRockOrientation(c, orientation))
		}
		// Both stem endpoints are on light cells. The leaf also bridges all
		// three cells, so sampling just its corners would miss the dark band.
		vine := terrain.Vine{Parent: -1, Points: []terrain.VinePoint{
			{P: geom.V{X: -.4, Y: .4}, Radius: .003}, {P: geom.V{X: .1, Y: .4}, Radius: .003},
		}, Leaves: []terrain.VineLeaf{{Polygon: [4]geom.V{{X: -.4, Y: .5}, {X: -.15, Y: .7}, {X: .1, Y: .5}, {X: -.15, Y: .45}}}}}
		vines := []terrain.Vine{vine}
		plain, lit := prepareRockVines(vines, nil), prepareRockVines(vines, grid)
		for pass, y := range []float64{.401, .51} {
			for _, x := range []float64{-.3, -.251, -.249, -.15, -.051, -.049, 0} {
				p := geom.V{X: x, Y: y}
				v, base := sampleVineMesh(t, lit[pass], p), sampleVineMesh(t, plain[pass], p)
				if x > -.25 && x < -.05 {
					if v.ColorR != 0 || v.ColorG != 0 || v.ColorB != 0 {
						t.Fatal("vine bridges a black cell between growth samples")
					}
				} else if v.ColorG < base.ColorG {
					t.Fatal("vine on a lit cell was globally darkened")
				}
				if math.Abs(float64(v.ColorA-base.ColorA)) > 1e-5 {
					t.Fatal("cell clipping changed border fade")
				}
			}
		}
		// Cell boundaries must not change covered area or introduce cracks.
		area := func(mesh TriangleMesh) float64 {
			sum := 0.0
			for i := 0; i < len(mesh.Indices); i += 3 {
				a, b, c := mesh.Vertices[mesh.Indices[i]], mesh.Vertices[mesh.Indices[i+1]], mesh.Vertices[mesh.Indices[i+2]]
				sum += math.Abs(float64(b.DstX-a.DstX)*float64(c.DstY-a.DstY)-float64(b.DstY-a.DstY)*float64(c.DstX-a.DstX)) * .5
			}
			return sum
		}
		for i := range lit {
			if math.Abs(area(lit[i])/area(plain[i])-1) > 1e-5 {
				t.Fatal("clipping changed vine coverage")
			}
		}
	}
}

func TestBackgroundVinesGeneratedCellContrast(t *testing.T) {
	for _, orientation := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		data := terrain.NewSectionBuilder(42, nil, orientation).Build(0)
		grid := terrain.ShadeRockFaces(data.Background, nil)
		var cells [2]terrain.RockCell
		levels := [2]float64{math.Inf(1), 0}
		for _, c := range grid {
			if c.Center.X < -.35 || c.Center.X > 1.35 || c.Center.Y < .1 || c.Center.Y > .9 {
				continue
			}
			clr := rockViewColor(c, ViewShaded)
			level := .30*float64(clr.R) + .59*float64(clr.G) + .11*float64(clr.B)
			if level < levels[0] {
				cells[0], levels[0] = c, level
			}
			if level > levels[1] {
				cells[1], levels[1] = c, level
			}
		}
		var vines []terrain.Vine
		for _, c := range cells {
			if len(c.Polygon) == 0 {
				t.Fatal("generated contrast fixture lacks dark or lit cells")
			}
			vines = append(vines, terrain.Vine{Parent: -1, Points: []terrain.VinePoint{{P: c.Center.Sub(geom.V{X: .0002}), Radius: .0001}, {P: c.Center.Add(geom.V{X: .0002}), Radius: .0001}}})
		}
		plain, lit := prepareRockVines(vines, nil), prepareRockVines(vines, grid)
		var shades [2]float64
		for i, c := range cells {
			p := c.Center
			v, base := sampleVineMesh(t, lit[i], p), sampleVineMesh(t, plain[i], p)
			shades[i] = float64(v.ColorG / base.ColorG)
		}
		t.Logf("%s: generated cell levels %v, vine multipliers %v", orientation, levels, shades)
		if shades[0] <= 0 || shades[1] < .9 || shades[0] > shades[1]*.8 {
			t.Fatal("real background cell colors collapse into nearly uniform vine lighting")
		}
	}
}

func TestCarveRetainsBranchesAndFamilyMaterials(t *testing.T) {
	trunk := terrain.Vine{Parent: -1, Foreground: true}
	for _, x := range []float64{.1, .3, .5, .7, .9} {
		trunk.Points = append(trunk.Points, terrain.VinePoint{P: geom.V{X: x, Y: .5}, Radius: .004})
	}
	source := []terrain.Vine{trunk}
	for joint := 1; joint <= 3; joint++ {
		source = append(source, terrain.Vine{
			Parent: 0, Joint: joint, Depth: 1, Foreground: true,
			Points: []terrain.VinePoint{{P: trunk.Points[joint].P, Radius: .002}, {P: trunk.Points[joint].P.Add(geom.V{Y: .3}), Radius: .001}},
		})
	}
	cut, _ := terrain.CutFromHole((terrain.Hole{Shape: terrain.HoleCircle, Center: geom.V{X: .5, Y: -.5}, Radius: .1}))
	geometry := &terrain.Geometry{Top: -1, Topology: &terrain.RockTopology{}, Vegetation: terrain.Vegetation{ForegroundVines: source}}
	updated, changed := cut.Plants(geometry)
	if changed || updated != geometry {
		t.Fatal("texture-only carve rebuilt CPU branches")
	}
	for i, vine := range updated.Vegetation.ForegroundVines {
		if terrain.VineStyleFamily(source, i) != 0 || vinePalette(source, i) != vinePalette(source, 0) || !vine.Foreground {
			t.Fatal("carving changed family material")
		}
		if vine.Parent >= 0 && (vine.Parent >= i || vine.Joint >= len(source[vine.Parent].Points) || vine.Points[0].P != source[vine.Parent].Points[vine.Joint].P) {
			t.Fatal("carving changed a branch attachment")
		}
	}
	for _, mesh := range PrepareForegroundVines(updated.Vegetation.ForegroundVines) {
		checkMesh(t, mesh)
	}
}

func TestForkShadingMeetsParent(t *testing.T) {
	parent := terrain.Vine{Parent: -1, Points: []terrain.VinePoint{{P: geom.V{X: 0.1, Y: 0.08}, Radius: 0.008}, {P: geom.V{X: 0.1, Y: 0.1}, Radius: 0.008}, {P: geom.V{X: 0.1, Y: 0.12}, Radius: 0.008}}}
	branch := terrain.Vine{Depth: 1, Parent: 0, Joint: 1, Points: []terrain.VinePoint{{P: geom.V{X: 0.1, Y: 0.1}, Radius: 0.005}, {P: geom.V{X: 0.13, Y: 0.16}, Radius: 0}}}
	vines := []terrain.Vine{parent, branch}
	parentPalette := vinePalette(vines, 0)
	// Even the dark outer bands of the child inherit the parent material at
	// the join instead of drawing a black cut through its highlight.
	for _, x := range []float64{.096, .100, .104} {
		p := geom.V{X: x, Y: 0.1}
		want := vineBandColorFrom(parentPalette, (.100-x)/.008)
		got := vineJoinColor(vines, 1, p, vineColors[0])
		if got != want {
			t.Fatalf("fork seam at %v: got %v, want parent material %v", p, got, want)
		}
	}
	if got := vineJoinColor(vines, 1, geom.V{X: 0.13, Y: 0.16}, vineColors[3]); got != vineColors[3] {
		t.Fatal("parent shading extends beyond the fork")
	}
}

func TestVinePalettesVaryAndStayMuted(t *testing.T) {
	vines := make([]terrain.Vine, 13)
	for i := range 12 {
		vines[i].Parent = -1
	}
	vines[12] = terrain.Vine{Depth: 1, Parent: 0}
	var palettes [12][len(vineColors)]color.NRGBA
	seen := make(map[[len(vineColors)]color.NRGBA]bool, len(palettes))
	for i := range palettes {
		palettes[i] = vinePalette(vines, i)
		seen[palettes[i]] = true
	}
	if len(seen) < 4 {
		t.Fatal("vine families need variations of both pastel palettes")
	}
	if child := vinePalette(vines, 12); child != palettes[0] {
		t.Fatal("branches should retain their trunk's palette")
	}
	for i, palette := range palettes {
		c := palette[3]
		switch i % 2 {
		case 0:
			if c.G <= c.R || c.R <= c.B {
				t.Fatalf("missing sage hue: %v", c)
			}
		case 1:
			if c.R <= c.G || c.G <= c.B || int(c.G)-int(c.B) < 7 {
				t.Fatalf("missing straw hue: %v", c)
			}

		}
		vines[i].Foreground = true
		foreground := vinePalette(vines, i)
		for band, muted := range palette {
			bright := foreground[band]
			if muted.R >= bright.R || muted.G >= bright.G || muted.B >= bright.B {
				t.Fatalf("background material lost its depth: %v vs %v", muted, bright)
			}
		}
		if max(c.R, c.G, c.B) > 48 {
			t.Fatalf("background highlight is too bright: %v", c)
		}
		if max(foreground[3].R, foreground[3].G, foreground[3].B)-min(foreground[3].R, foreground[3].G, foreground[3].B) > 85 {
			t.Fatalf("foreground material is too saturated: %v", foreground[3])
		}
	}
	if meshes := PrepareVines(vines); len(meshes) != len(vines) {
		t.Fatalf("expected one matte mesh per vine, got %d for %d stems", len(meshes), len(vines))
	}
}

func TestVineWeatheringIsSubtleAndStable(t *testing.T) {
	base := color.NRGBA{R: 72, G: 18, B: 16, A: 255}
	first := weatherVineColor(base, geom.V{X: 0.12, Y: 0.34}, 2)
	if first != weatherVineColor(base, geom.V{X: 0.12, Y: 0.34}, 2) {
		t.Fatal("vine weathering is not deterministic")
	}
	second := weatherVineColor(base, geom.V{X: 0.64, Y: 0.81}, 2)
	if first == second {
		t.Fatal("vine surface has no spatial mottling")
	}
	for _, weathered := range []color.NRGBA{first, second} {
		if weathered.A != base.A || weathered.R > base.R || weathered.G > base.G || weathered.B > base.B ||
			float64(weathered.R) < float64(base.R)*.89 || float64(weathered.G) < float64(base.G)*.89 || float64(weathered.B) < float64(base.B)*.89 {
			t.Fatalf("vine weathering exceeded its subtle darkening range: %v", weathered)
		}
	}
}

func TestVineTransitionAndForkUseLocalMaterial(t *testing.T) {
	parent := terrain.Vine{
		Parent: -1, Foreground: true,
		Style:  terrain.VineStyle{From: terrain.VineSage, To: terrain.VineStraw, Start: .1, End: .7},
		Points: []terrain.VinePoint{{P: geom.V{X: .2, Y: .1}, Radius: .004}, {P: geom.V{X: .2, Y: .5}, Radius: .004}, {P: geom.V{X: .2, Y: .9}, Radius: .004}},
		Along:  []float64{0, .4, .8},
	}
	child := terrain.Vine{Parent: 0, Joint: 1, Depth: 1, Foreground: true, Points: []terrain.VinePoint{{P: parent.Points[1].P, Radius: .002}, {P: geom.V{X: .4, Y: .6}, Radius: 0}}, Along: []float64{.4, .6}}
	vines := []terrain.Vine{parent, child}
	start, middle, end := vinePaletteAt(vines, 0, 0), vinePaletteAt(vines, 0, .4), vinePaletteAt(vines, 0, .8)
	if start[3].G <= start[3].R || end[3].R <= end[3].G || middle[3] == start[3] || middle[3] == end[3] {
		t.Fatal("missing sage-to-straw transition")
	}
	if vinePaletteAt(vines, 1, .4) != middle {
		t.Fatal("child does not inherit the local material")
	}
	p := parent.Points[1].P
	if got := vineJoinColor(vines, 1, p, start[0]); got != vineBandColorFrom(middle, 0) {
		t.Fatal("fork samples the parent's starting color rather than its local color")
	}
	meshes := PrepareVines(vines)
	for _, mesh := range meshes {
		checkMesh(t, mesh)
	}
	for i := range parent.Points {
		palette := vinePaletteAt(vines, 0, parent.Along[i])
		p := parent.Points[i].P.Add(geom.V{X: -vineBands[3] * parent.Points[i].Radius})
		want := vineVertex(p, weatherVineColor(palette[3], p, 0))
		if meshes[0].Vertices[i*len(vineBands)+3] != want {
			t.Fatal("stem mesh did not evaluate material along its length")
		}
	}

}

func TestGeneratedVineMeshesSampleCoordinatesInsteadOfPathDistance(t *testing.T) {
	for _, orientation := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		data := terrain.NewSectionBuilder(42, nil, orientation).Build(1)
		for _, vines := range [][]terrain.Vine{data.Vines, data.ForegroundVines} {
			if len(vines) == 0 {
				t.Fatal("fixture needs generated vines")
			}
			meshes := PrepareVines(vines)
			for index, vine := range vines {
				if vine.Parent >= 0 {
					continue // Forks also inherit the parent's cross-section shading.
				}
				for i, point := range vine.Points {
					palette := vinePaletteAtPoint(vines, index, point.P, 0)
					if palette != vinePaletteAtPoint(vines, index, point.P, 123) {
						t.Fatal("generated hue still depends on distance along a stem")
					}
					before := vine.Points[max(0, i-1)].P
					after := vine.Points[min(len(vine.Points)-1, i+1)].P
					p := point.P.Add(after.Sub(before).Norm().Perp().Mul(vineBands[3] * point.Radius))
					want := vineVertex(p, weatherVineColor(palette[3], p, terrain.VineStyleFamily(vines, index)))
					if got := meshes[index].Vertices[i*len(vineBands)+3]; got != want {
						t.Fatal("stem mesh did not sample its local 2D coordinate")
					}
				}
			}
		}
	}
}

func TestLeavesFollowCrossingStemsAndRetainForegroundShadows(t *testing.T) {
	p := geom.V{X: .3, Y: .4}
	vine := terrain.Vine{Parent: -1, Foreground: true, Points: []terrain.VinePoint{{P: p, Radius: .003}, {P: p.Add(geom.V{Y: .2}), Radius: 0}}, Leaves: []terrain.VineLeaf{{Anchor: p, Polygon: [4]geom.V{p, p.Add(geom.V{X: .02, Y: .01}), p.Add(geom.V{X: .04, Y: .04}), p.Add(geom.V{X: .01, Y: .025})}}}}
	// This later stem crosses the first vine's leaf and has no leaves itself.
	crossing := terrain.Vine{Parent: -1, Foreground: true, Points: []terrain.VinePoint{
		{P: p.Add(geom.V{X: .02, Y: -.02}), Radius: .006},
		{P: p.Add(geom.V{X: .02, Y: .08}), Radius: .006},
	}}
	for _, orientation := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		t.Run(orientation.String(), func(t *testing.T) {
			for _, foreground := range []bool{false, true} {
				vines := []terrain.Vine{vine, crossing}
				vines[0].Foreground, vines[1].Foreground = foreground, foreground
				meshes := PrepareVines(vines)
				if foreground {
					meshes = PrepareForegroundVines(vines, orientation)
				}
				firstStem, wantMeshes := 0, 3
				if foreground {
					firstStem, wantMeshes = 3, 6
				}
				if len(meshes) != wantMeshes {
					t.Fatalf("got %d meshes, want %d for stems, leaves and shadows", len(meshes), wantMeshes)
				}
				for _, mesh := range meshes {
					checkMesh(t, mesh)
				}
				for _, stem := range meshes[firstStem : firstStem+2] {
					if len(stem.Vertices) != 2*len(vineBands) {
						t.Fatal("stem pass includes leaves")
					}
				}
				leaves := meshes[len(meshes)-1]
				if len(leaves.Vertices) != 6 || leaves.Vertices[0].ColorR == leaves.Vertices[3].ColorR {
					t.Fatal("leaf pass lost its two flat tones")
				}
				if foreground {
					offset := terrain.InternalPoint(orientation, geom.V{X: 1, Y: 1.5})
					for i, leaf := range leaves.Vertices {
						shadow := meshes[2].Vertices[i]
						if shadow.ColorA != .38 || shadow.DstX-leaf.DstX != float32(offset.X) || shadow.DstY-leaf.DstY != float32(offset.Y) {
							t.Fatal("leaf lost its contact shadow")
						}
					}
				}
				batch := SectionMesh{Vines: meshes}
				FinishSectionMesh(&batch)
				for _, point := range vine.Leaves[0].Polygon {
					if !image.Pt(int(point.X*RasterPixelsPerUnit), int((point.Y-terrain.GenerationMinY)*RasterPixelsPerUnit)).In(batch.VinesBounds) {
						t.Fatal("cached layer bounds omit a leaf")
					}
				}
				// Upload batching and dirty-region replay must keep the leaf last.
				for _, pixels := range []int{500, 1000, 1920} {
					scaled := ScaleSectionMesh(batch, pixels)
					last := scaled.Vines[len(scaled.Vines)-1]
					replay := RegionMesh(last, image.Rectangle{Max: scaled.VinesBounds.Size()})
					for i, index := range replay.Indices[len(replay.Indices)-6:] {
						v := replay.Vertices[index]
						leaf := leaves.Vertices[leaves.Indices[i]]
						if v.ColorR != leaf.ColorR || v.ColorG != leaf.ColorG || v.ColorB != leaf.ColorB || v.ColorA != 1 {
							t.Fatal("batching, resizing or replay moved leaves behind stems")
						}
					}
				}
			}
		})
	}
}

func TestRepeatedVineColorsStayDarkInBackground(t *testing.T) {
	style := terrain.VineStyle{From: terrain.VineSage, To: terrain.VineStraw, Start: .03, End: .10, Period: .18}
	vines := []terrain.Vine{{Parent: -1, Style: style}, {Parent: -1, Style: style, Foreground: true}}
	seen := map[[len(vineBands)]color.NRGBA]bool{}
	for cycle := range 6 {
		distance := style.End + float64(cycle)*style.Period + .001
		background, foreground := vinePaletteAt(vines, 0, distance), vinePaletteAt(vines, 1, distance)
		seen[foreground] = true
		for i, c := range background {
			bright := foreground[i]
			for _, pair := range [][2]uint8{{c.R, bright.R}, {c.G, bright.G}, {c.B, bright.B}} {
				if float64(pair[0]) > float64(pair[1])*.21+1 {
					t.Fatal("background material is too bright")
				}
			}
		}
	}
	if len(seen) != 2 {
		t.Fatal("renderer does not alternate green and yellow along a vine")
	}
}

func TestForegroundVinesUsePastelInheritedColor(t *testing.T) {
	vines := []terrain.Vine{
		{Parent: -1, Foreground: true, Points: []terrain.VinePoint{{P: geom.V{X: 0.1, Y: 0.08}, Radius: 0.003}, {P: geom.V{X: 0.1, Y: 0.1}, Radius: 0.003}, {P: geom.V{X: 0.1, Y: 0.12}, Radius: 0}}},
		{Parent: 0, Joint: 1, Depth: 1, Foreground: true, Points: []terrain.VinePoint{{P: geom.V{X: 0.1, Y: 0.1}, Radius: 0.002}, {P: geom.V{X: 0.12, Y: 0.12}, Radius: 0}}},
	}
	palette := vinePalette(vines, 0)
	if palette[3].G <= palette[3].R || palette[3].R <= palette[3].B || palette[3].G > 200 || vinePalette(vines, 1) != palette {
		t.Fatal("foreground stems need sage highlights inherited by their branches")
	}
	if got := vineJoinColor(vines, 1, geom.V{X: 0.1, Y: 0.1}, palette[0]); got != vineBandColorFrom(palette, 0) {
		t.Fatal("foreground fork has a material discontinuity")
	}
	meshes := PrepareForegroundVines(vines)
	if len(meshes) != 2*len(vines) {
		t.Fatal("foreground vines lost their contact shadows")
	}
	for _, mesh := range meshes {
		checkMesh(t, mesh)
	}
	if meshes[0].Vertices[0].ColorA >= 1 || meshes[len(vines)].Vertices[0].ColorA != 1 {
		t.Fatal("contact shadows must precede the opaque stems")
	}
}
