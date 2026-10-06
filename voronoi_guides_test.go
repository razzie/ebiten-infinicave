package infinicave

import (
	"image/color"
	"math"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

func TestGuideCutsCellsWithoutAddingSites(t *testing.T) {
	seeds := []V{{0.25, 0.48}, {0.75, 0.48}, {0.25, 0.8}, {0.75, 0.8}}
	guides := []Guide{splineGuide([]V{{-0.01, 0.5}, {generationWidth + 0.01, 0.5}}, 1)}
	grid := newGuideRockGrid(seeds, guides, func(p V) color.NRGBA {
		if p.Y > .500 {
			return color.NRGBA{R: 200, A: 255}
		}
		return color.NRGBA{R: 20, A: 255}
	})
	if len(grid) != 6 {
		t.Fatalf("want only the two crossed cells split, got %d faces", len(grid))
	}
	area, seam := 0.0, 0.0
	for _, cell := range grid {
		area += faceArea(cell.Polygon)
		for i, p := range cell.Polygon {
			if (cell.Center.Y-.500)*(p.Y-.500) < -1e-13 {
				t.Fatal("cell crosses guide border")
			}
			q := cell.Polygon[(i+1)%len(cell.Polygon)]
			if math.Abs(p.Y-.500) < 1e-10 && math.Abs(q.Y-.500) < 1e-10 {
				seam += p.Sub(q).Len()
			}
		}
		if cell.Center.Y > .500 && cell.Color.R != 200 {
			t.Fatal("lit fragment inherited the original shadow site's color")
		}
	}
	if math.Abs(area-generationWidth*generationHeight) > 1e-12 || math.Abs(seam-2*generationWidth) > 1e-9 {
		t.Fatalf("faces leave gaps or fail to share the guide: area=%v seam=%v", area, seam)
	}
}

func TestCurvedGuideCutsPreserveAreaAndCurve(t *testing.T) {
	poly := []V{{0.1, 0.1}, {0.3, 0.1}, {0.3, 0.3}, {0.1, 0.3}}
	g := splineGuide([]V{{0.08, 0.18}, {0.16, 0.14}, {0.24, 0.25}, {0.32, 0.21}}, 1)
	faces := splitGuideFace(poly, &g)
	if len(faces) != 2 {
		t.Fatalf("curve produced %d faces, want two", len(faces))
	}
	assertFacesPartition(t, poly, faces)
	for _, p := range g.Pts {
		if !insideFace(p, poly) {
			continue
		}
		for _, face := range faces {
			found := false
			for i, a := range face {
				d := face[(i+1)%len(face)].Sub(a)
				q := a.Add(d.Mul(clamp(p.Sub(a).Dot(d)/d.Len2(), 0, 1)))
				found = found || p.Sub(q).Len() < 1e-10
			}
			if !found {
				t.Fatalf("curved border was replaced by a tangent or chord at %v", p)
			}
		}
	}
}

func TestGuideCrossingsAndFiniteEndpoints(t *testing.T) {
	poly := []V{{0.1, 0.1}, {0.3, 0.1}, {0.3, 0.3}, {0.1, 0.3}}
	for _, tc := range []struct {
		name  string
		knots []V
		count int
	}{
		{"curl", []V{{0.08, 0.15}, {0.18, 0.15}, {0.33, 0.17}, {0.33, 0.24}, {0.18, 0.25}, {0.08, 0.25}}, 3},
		{"interior tip", []V{{0.08, 0.2}, {0.18, 0.2}}, 1},
		{"outside segment", []V{{0.02, 0.2}, {0.08, 0.2}}, 1},
		{"boundary", []V{{0.08, 0.1}, {0.32, 0.1}}, 1},
		{"vertices", []V{{0.08, 0.08}, {0.32, 0.32}}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := splineGuide(tc.knots, 1)
			faces := splitGuideFace(poly, &g)
			if len(faces) != tc.count {
				t.Fatalf("got %d faces, want %d", len(faces), tc.count)
			}
			assertFacesPartition(t, poly, faces)
		})
	}
	a := splineGuide([]V{{0.08, 0.2}, {0.32, 0.2}}, 1)
	b := splineGuide([]V{{0.2, 0.08}, {0.2, 0.32}}, 1)
	var faces [][]V
	for _, face := range splitGuideFace(poly, &a) {
		faces = append(faces, splitGuideFace(face, &b)...)
	}
	if len(faces) != 4 {
		t.Fatalf("intersecting guides produced %d faces, want four", len(faces))
	}
	assertFacesPartition(t, poly, faces)
}

func assertFacesPartition(t *testing.T, original []V, faces [][]V) {
	t.Helper()
	area := 0.0
	for _, face := range faces {
		area += faceArea(face)
		if !insideFace(faceCenter(face), face) {
			t.Fatal("face color sample lies outside the face")
		}
		triangles := faceTriangles(face)
		if len(triangles) != len(face)-2 {
			t.Fatalf("cannot triangulate face: %d triangles for %d vertices", len(triangles), len(face))
		}
		triArea := 0.0
		for _, tri := range triangles {
			triArea += faceArea([]V{face[tri[0]], face[tri[1]], face[tri[2]]})
		}
		if math.Abs(triArea-faceArea(face)) > 1e-12 {
			t.Fatal("triangulation loses or overlaps face area")
		}
	}
	if math.Abs(area-faceArea(original)) > 1e-12 {
		t.Fatalf("cut loses or overlaps area: %v vs %v", area, faceArea(original))
	}
	for y := 0.1013; y < 0.3; y += 0.007 {
		for x := 0.1017; x < 0.3; x += 0.007 {
			count := 0
			for _, face := range faces {
				if insideFace(V{x, y}, face) {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("point %v belongs to %d faces", V{x, y}, count)
			}
		}
	}
}

func TestConcaveCellMeshStaysInsideFace(t *testing.T) {
	poly := []V{{0.1, 0.1}, {0.3, 0.1}, {0.3, 0.3}, {0.24, 0.3}, {0.24, 0.16}, {0.16, 0.16}, {0.16, 0.3}, {0.1, 0.3}}
	vertices, indices := appendCellMesh(nil, nil, poly, faceCenter(poly), color.NRGBA{R: 200, A: 255}, V3{Z: 1})
	point := func(v ebiten.Vertex) V {
		return V{float64(v.DstX) / rasterPixelsPerUnit, float64(v.DstY)/rasterPixelsPerUnit + generationMinY}
	}
	area := 0.0
	for i := 0; i < len(indices); i += 3 {
		a, b, c := point(vertices[indices[i]]), point(vertices[indices[i+1]]), point(vertices[indices[i+2]])
		area += cross(b.Sub(a), c.Sub(a)) * .5
		if !insideFace(a.Add(b).Add(c).Mul(1.0/3), poly) {
			t.Fatal("mesh triangle paints across the guide")
		}
	}
	if math.Abs(area-faceArea(poly)) > 2e-8 {
		t.Fatalf("mesh area %v differs from face area %v", area, faceArea(poly))
	}
}

func TestGuideNormalsRemainContinuous(t *testing.T) {
	g := splineGuide([]V{{0.1, 0.4}, {0.24, 0.4}, {0.28, 0.44}, {0.24, 0.49}, {0.15, 0.5}}, 1)
	for _, s := range g.S[1 : len(g.S)-1] {
		_, _, before := g.frameAt(s - 1e-8)
		_, _, after := g.frameAt(s + 1e-8)
		if before.Dot(after) < .99999 {
			t.Errorf("normal jumps at arc length %v", s)
		}
	}
}

func TestVineTerrainPreservesConcaveGuideCut(t *testing.T) {
	poly := []V{{0.1, 0.1}, {0.3, 0.1}, {0.3, 0.3}, {0.24, 0.3}, {0.24, 0.16}, {0.16, 0.16}, {0.16, 0.3}, {0.1, 0.3}}
	background := RockGrid{{Center: V{0.5, 1.5}, Polygon: []V{{0, generationMinY}, {generationWidth, generationMinY}, {generationWidth, generationMaxY}, {0, generationMaxY}}, Color: color.NRGBA{R: 30, G: 30, B: 30, A: 255}}}
	foreground := RockGrid{{Center: faceCenter(poly), Polygon: poly, Color: color.NRGBA{R: 200, G: 200, B: 200, A: 255}}}
	field := newVineTerrain(background, foreground)
	at := func(x, y float64) int {
		return int((y-generationMinY)/vineFieldStep)*vineFieldWidth + int(x/vineFieldStep)
	}
	if field.unsupported[at(.200, .260)] != 0 {
		t.Fatal("concave guide cut filled the dark passage between the rock arms")
	}
	if field.unsupported[at(.130, .260)] == 0 || field.unsupported[at(.270, .260)] == 0 {
		t.Fatal("vine terrain omitted a lit arm of the concave face")
	}
}

func TestCellMeshHasOneContinuousShadingPlane(t *testing.T) {
	polys := [][]V{
		// A ridged crest creates short edges and narrow fan triangles.
		{{0, 0.01}, {0.02, 0.008}, {0.023, 0.009}, {0.045, 0}, {0.08, 0}, {0.08, 0.06}, {0, 0.06}},
		// Concave merged cells use ear clipping instead of a single fan.
		{{0.1, 0.1}, {0.3, 0.1}, {0.3, 0.3}, {0.24, 0.3}, {0.24, 0.16}, {0.16, 0.16}, {0.16, 0.3}, {0.1, 0.3}},
	}
	for _, poly := range polys {
		vertices, _ := appendCellMesh(nil, nil, poly, faceCenter(poly), color.NRGBA{R: 200, A: 255}, V3{.6, -.6, .5})
		type shading struct{ tilt, facet float32 }
		seen := make(map[[2]float32]shading)
		for _, v := range vertices {
			key := [2]float32{v.DstX, v.DstY}
			value := shading{v.Custom1, v.Custom2}
			if previous, ok := seen[key]; ok && previous != value {
				t.Fatal("internal triangulation introduces a tonal seam within one rock")
			}
			seen[key] = value
		}
	}
}
