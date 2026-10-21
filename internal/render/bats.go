package render

import (
	"image/color"
	"math"
	"math/rand"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

const (
	batMargin       = .04
	batFallDuration = .65
	batFallDistance = .16 // Several wingspans in scene units.
)

type Bat struct {
	ID                 uint64
	Start, End         geom.V  // world coordinates, independent of camera movement
	control1, control2 float64 // heights of the cubic route's control points
	duration, Age      float64
	Size, phase, weave float64
	shade              float32 // fixed brightness offset for this bat
	hit                bool
	fallAge            float64
	hitPosition        geom.V
	fallDirection      geom.V
	hitAngle           float64
}

// position combines a broad curved route with smaller swoops. Horizontal travel
// remains monotonic, but eases between faster and slower parts of the crossing.
func (b Bat) Position(t float64) geom.V {
	if b.hit {
		fall := math.Min(1, b.fallAge/batFallDuration)
		return b.hitPosition.Add(b.fallDirection.Mul(batFallDistance * fall * fall))
	}
	t = math.Max(0, math.Min(1, t))
	u := 1 - t
	travel := t + .055*math.Sin(2*math.Pi*t)
	y := u*u*u*b.Start.Y + 3*u*u*t*b.control1 + 3*u*t*t*b.control2 + t*t*t*b.End.Y
	y += math.Sin(math.Pi*t) * (b.weave*math.Sin(4*math.Pi*t+b.phase) +
		.003*math.Sin(t*b.duration*5+b.phase))
	return geom.V{X: b.Start.X + (b.End.X-b.Start.X)*travel, Y: y}
}

func (b Bat) bank(t float64) float64 {
	direction := b.Position(t + .01).Sub(b.Position(t - .01))
	if direction.X == 0 {
		return 0
	}
	return math.Max(-.35, math.Min(.35, math.Atan(direction.Y/direction.X)))
}

type BatFlock struct {
	Orientation terrain.Orientation

	nextID     uint64
	rng        *rand.Rand
	PerMinute  float64
	Next       float64 // seconds until the next arrival
	Bats       []Bat
	burn       *BurnDissolve
	burnSprite *ebiten.Image
}

func NewBatFlock(seed int64, perMinute float64, orientation ...terrain.Orientation) *BatFlock {
	// Keep ambient animation independent of terrain generation and its RNG.
	f := &BatFlock{Orientation: terrain.OptionalOrientation(orientation), rng: rand.New(rand.NewSource(seed)), PerMinute: perMinute}
	f.Next = (60 / perMinute) * (.25 + f.rng.Float64()*.375)
	return f
}

// PrepareBurn creates the GPU effect on the game goroutine. Flight and hit
// simulation remain CPU-only, including when no terrain has loaded yet.
func (f *BatFlock) PrepareBurn() error {
	var err error
	f.burn, err = NewBurnDissolve()
	return err
}

func (f *BatFlock) Warm(dst *ebiten.Image) {
	if f.burn != nil {
		f.burn.Warm(dst)
	}
}

func (f *BatFlock) Reset(seed int64) {
	fresh := NewBatFlock(seed, f.PerMinute, f.Orientation)
	f.rng, f.Next, f.Bats = fresh.rng, fresh.Next, nil
}

func (f *BatFlock) Close() {
	if f.burn != nil {
		f.burn.Close()
		f.burn = nil
	}
	if f.burnSprite != nil {
		f.burnSprite.Deallocate()
		f.burnSprite = nil
	}
	f.Bats = nil
}

// Carve tests the current wing pose, including wing-only hits. Repeated cuts
// cannot restart a falling bat, but a repeated terrain cut can hit a new bat.
func (f *BatFlock) Carve(cut terrain.RockCut) {
	for i := range f.Bats {
		b := &f.Bats[i]
		if b.hit {
			continue
		}
		t := b.Age / b.duration
		position := b.Position(t)
		radius := 1.25 * b.Size // Includes the rotated wing tips.
		if position.X+radius <= cut.Min.X || position.X-radius >= cut.Max.X ||
			position.Y+radius <= cut.Min.Y || position.Y-radius >= cut.Max.Y {
			continue
		}
		if !cut.Intersects(f.silhouette(*b), 0) {
			continue
		}
		f.hit(b)
	}
}

// silhouette shares the current wing pose between carving and ray queries.
func (f *BatFlock) silhouette(b Bat) []geom.V {
	t := b.Age / b.duration
	position := b.Position(t)
	path := batSilhouette(b.flap())
	transform := &vector.AddPathOptions{}
	// Flatten curves at the reference raster scale, independent of resizing.
	transform.GeoM.Scale(b.Size*RasterPixelsPerUnit, b.Size*RasterPixelsPerUnit)
	transform.GeoM.Rotate(f.SilhouetteAngle(b, t))
	var outline vector.Path
	outline.AddPath(&path, transform)
	vertices, _ := outline.AppendVerticesAndIndicesForFilling(nil, nil)
	poly := make([]geom.V, len(vertices))
	for j, vertex := range vertices {
		poly[j] = position.Add(geom.V{X: float64(vertex.DstX) / RasterPixelsPerUnit, Y: float64(vertex.DstY) / RasterPixelsPerUnit})
	}
	return geom.CleanPolygon(poly)
}

type BatHit struct {
	ID            uint64
	Point, Normal geom.V
	Distance      float64
	StartedInside bool
}

// Raycast tests flying silhouettes in internal scene coordinates. Direction
// must be normalized or zero. Falling and burning bats are excluded.
func (f *BatFlock) Raycast(origin, direction geom.V, limit float64) (BatHit, bool) {
	var best BatHit
	found := false
	for _, b := range f.Bats {
		if b.hit {
			continue
		}
		position := b.Position(b.Age / b.duration)
		radius := geom.V{X: 1.25 * b.Size, Y: 1.25 * b.Size}
		if _, _, ok := geom.RayBox(origin, direction, limit, position.Sub(radius), position.Add(radius)); !ok {
			continue
		}
		poly := f.silhouette(b)
		hit := BatHit{ID: b.ID}
		if (terrain.CollisionGeometry{Polygons: [][]geom.V{poly}}).Contains(origin) {
			hit.Point, hit.StartedInside = origin, true
		} else {
			if direction == (geom.V{}) {
				continue
			}
			hit.Distance = math.Inf(1)
			for i, a := range poly {
				end := poly[(i+1)%len(poly)]
				if distance, ok := geom.RaySegment(origin, direction, a, end, limit); ok && distance < hit.Distance {
					hit.Distance = distance
					hit.Point = origin.Add(direction.Mul(distance))
					hit.Normal = end.Sub(a).Perp().Norm().Mul(-1)
				}
			}
			if math.IsInf(hit.Distance, 1) {
				continue
			}
		}
		if !found || hit.Distance < best.Distance-geom.QueryEpsilon ||
			(math.Abs(hit.Distance-best.Distance) <= geom.QueryEpsilon && hit.ID < best.ID) {
			best, found = hit, true
		}
	}
	return best, found
}

// HitBat starts a flying bat's fall without changing terrain or arrivals.
func (f *BatFlock) HitBat(id uint64) bool {
	for i := range f.Bats {
		b := &f.Bats[i]
		if b.ID == id && !b.hit {
			f.hit(b)
			return true
		}
	}
	return false
}

func (f *BatFlock) hit(b *Bat) {
	t := b.Age / b.duration
	b.hitAngle = f.SilhouetteAngle(*b, t)
	b.hitPosition, b.fallDirection = b.Position(t), terrain.InternalPoint(f.Orientation, geom.V{Y: 1})
	b.hit = true
}

func (f *BatFlock) Update(viewport Viewport) {
	tps := ebiten.TPS()
	if tps <= 0 {
		tps = ebiten.DefaultTPS
	}
	f.Step(viewport, 1/float64(tps))
}

func (f *BatFlock) Step(viewport Viewport, seconds float64) {
	if !ValidViewport(viewport) || seconds <= 0 {
		return
	}
	alive := f.Bats[:0]
	for _, b := range f.Bats {
		if b.hit {
			b.fallAge += seconds
			if b.fallAge < batFallDuration+burnDuration {
				alive = append(alive, b)
			}
			continue
		}
		b.Age += seconds
		if b.Age < b.duration {
			alive = append(alive, b)
		}
	}
	f.Bats = alive
	f.Next -= seconds
	for f.Next <= 0 {
		f.spawn(viewport, -f.Next)
		// Preserve elapsed time so rates above the update frequency still work.
		f.Next += (60 / f.PerMinute) * (.625 + f.rng.Float64()*.75)
	}
}

func (f *BatFlock) spawn(viewport Viewport, age float64) {
	// Limit excursions in tall viewports, with room for the entire silhouette
	// above the floor and below the top of the view in ordinary window sizes.
	span := math.Min(viewport.Height, 1)
	padding := math.Min(viewport.Height*.2, .06)
	low, high := viewport.Y+padding, viewport.Y+viewport.Height-padding
	clampY := func(y float64) float64 { return math.Max(low, math.Min(high, y)) }
	startY := viewport.Y + viewport.Height*(.2+.6*f.rng.Float64())
	endY := clampY(startY + (f.rng.Float64()-.5)*span*.5)
	b := Bat{
		Age:      age,
		Start:    geom.V{X: terrain.BackgroundMinX - batMargin, Y: startY},
		End:      geom.V{X: terrain.BackgroundMaxX + batMargin, Y: endY},
		control1: clampY(startY + (f.rng.Float64()-.5)*span*.8),
		control2: clampY(endY + (f.rng.Float64()-.5)*span*.8),
		duration: (terrain.BackgroundMaxX - terrain.BackgroundMinX + 2*batMargin) / (.14 + .08*f.rng.Float64()),
		Size:     .012 + .006*f.rng.Float64(),
		phase:    f.rng.Float64() * 2 * math.Pi,
		weave:    span * (.015 + .02*f.rng.Float64()),
	}
	if f.rng.Intn(2) == 0 {
		b.Start.X, b.End.X = b.End.X, b.Start.X
	}
	// Reuse the random phase to preserve the arrival and flight sequence.
	b.shade = float32((b.phase/(2*math.Pi) - .5) * .3)
	if b.Age < b.duration {
		f.nextID++
		b.ID = f.nextID
		f.Bats = append(f.Bats, b)
	}
}

func (f *BatFlock) Draw(dst *ebiten.Image, viewport Viewport) {
	f.DrawWithAntialiasing(dst, viewport, true)
}

func (f *BatFlock) DrawWithAntialiasing(dst *ebiten.Image, viewport Viewport, aa bool) {
	view := TargetTransform(dst)
	pixels := view.Pixels
	scale := float64(pixels) / terrain.Width
	cameraY := RenderAlignedY(viewport.Y, pixels)
	for _, b := range f.Bats {
		t := b.Age / b.duration
		position := b.Position(t)
		x, y := view.OffsetX+position.X*scale, (position.Y-cameraY)*scale
		size := b.Size * scale
		radius := 1.25 * size
		if x+radius < 0 || x-radius > float64(dst.Bounds().Dx()) || y+radius < 0 || y-radius > float64(dst.Bounds().Dy()) {
			continue
		}
		alpha := uint8(math.Round(255 * horizontalFade(position.X)))
		angle := f.SilhouetteAngle(b, t)
		if b.hit && b.fallAge > batFallDuration && f.burn != nil {
			// Reuse one small target for all bats, rasterized at the current
			// output resolution so resizing never stretches a stale snapshot.
			edge := int(math.Ceil(3*size)) + 2
			EnsureEffectImage(&f.burnSprite, edge, edge)
			f.burnSprite.Clear()
			center := float64(edge) / 2
			drawBat(f.burnSprite, b, center, center, size, angle, alpha, aa)
			f.burn.drawPatch(dst, burnPatch{
				image: f.burnSprite, min: position.Sub(geom.V{X: center / scale, Y: center / scale}),
				pixels: pixels, age: b.fallAge - batFallDuration,
			}, cameraY, view)
		} else {
			drawBat(dst, b, x, y, size, angle, alpha, aa)
		}
	}
}

func (b Bat) flap() float32 {
	return float32(math.Sin(b.Age*2*math.Pi*6 + b.phase))
}

func drawBat(dst *ebiten.Image, b Bat, x, y, size, angle float64, alpha uint8, aa bool) {
	options := &vector.DrawPathOptions{AntiAlias: aa}
	options.ColorScale.ScaleWithColor(color.NRGBA{R: 45, G: 35, B: 40, A: alpha})
	shade := 1 + b.shade
	options.ColorScale.Scale(shade, shade, shade, 1)
	path := batSilhouette(b.flap())
	transform := &vector.AddPathOptions{}
	transform.GeoM.Scale(size, size)
	transform.GeoM.Rotate(angle)
	transform.GeoM.Translate(x, y)
	var screenPath vector.Path
	screenPath.AddPath(&path, transform)
	vector.FillPath(dst, &screenPath, nil, options)
	// Keep the eyes attached to the head as the bat banks and turns.
	eyes := color.NRGBA{R: 255, G: 255, B: 255, A: alpha}
	for _, eyeX := range []float64{-.065, .065} {
		ex, ey := transform.GeoM.Apply(eyeX, -.15)
		vector.FillCircle(dst, float32(ex), float32(ey), float32(size*.045), eyes, aa)
	}
}

// Keep the silhouette upright in world coordinates while its route crosses
// the bounded axis. The landscape compositor supplies the opposite turn.
func (f *BatFlock) SilhouetteAngle(b Bat, t float64) float64 {
	if b.hit {
		return b.hitAngle + 2*math.Min(b.fallAge, batFallDuration)
	}
	angle := b.bank(t)
	if f.Orientation == terrain.Horizontal {
		angle -= math.Pi / 2
	}
	return angle
}

func batSilhouette(flap float32) vector.Path {
	// One continuous outline joins the ears, scalloped wings, and small body.
	var path vector.Path
	path.MoveTo(-.18, -.5)
	path.LineTo(-.04, -.3)
	path.LineTo(.04, -.3)
	path.LineTo(.18, -.5)
	path.LineTo(.14, -.12)
	path.QuadTo(.55, -(.3 + .3*flap), 1, -.7*flap)
	path.QuadTo(.7, .3-.35*flap, .65, .42-.25*flap)
	path.QuadTo(.45, .16, .32, .45)
	path.QuadTo(.17, .23, .12, .4)
	path.QuadTo(0, .65, -.12, .4)
	path.QuadTo(-.17, .23, -.32, .45)
	path.QuadTo(-.45, .16, -.65, .42-.25*flap)
	path.QuadTo(-.7, .3-.35*flap, -1, -.7*flap)
	path.QuadTo(-.55, -(.3 + .3*flap), -.14, -.12)
	path.Close()
	return path
}
