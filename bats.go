package infinicave

import (
	"image/color"
	"math"
	"math/rand"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const batMargin = .04

type bat struct {
	start, end         V       // world coordinates, independent of camera movement
	control1, control2 float64 // heights of the cubic route's control points
	duration, age      float64
	size, phase, weave float64
}

// position combines a broad curved route with smaller swoops. Horizontal travel
// remains monotonic, but eases between faster and slower parts of the crossing.
func (b bat) position(t float64) V {
	t = math.Max(0, math.Min(1, t))
	u := 1 - t
	travel := t + .055*math.Sin(2*math.Pi*t)
	y := u*u*u*b.start.Y + 3*u*u*t*b.control1 + 3*u*t*t*b.control2 + t*t*t*b.end.Y
	y += math.Sin(math.Pi*t) * (b.weave*math.Sin(4*math.Pi*t+b.phase) +
		.003*math.Sin(t*b.duration*5+b.phase))
	return V{X: b.start.X + (b.end.X-b.start.X)*travel, Y: y}
}

func (b bat) bank(t float64) float64 {
	direction := b.position(t + .01).Sub(b.position(t - .01))
	if direction.X == 0 {
		return 0
	}
	return math.Max(-.35, math.Min(.35, math.Atan(direction.Y/direction.X)))
}

type batFlock struct {
	rng  *rand.Rand
	next float64 // seconds until the next arrival
	bats []bat
}

func newBatFlock(seed int64) *batFlock {
	// Keep ambient animation independent of terrain generation and its RNG.
	f := &batFlock{rng: rand.New(rand.NewSource(seed))}
	f.next = 2 + f.rng.Float64()*3
	return f
}

func (f *batFlock) update(viewport Viewport) {
	tps := ebiten.TPS()
	if tps <= 0 {
		tps = ebiten.DefaultTPS
	}
	f.step(viewport, 1/float64(tps))
}

func (f *batFlock) step(viewport Viewport, seconds float64) {
	if !viewport.valid() || seconds <= 0 {
		return
	}
	alive := f.bats[:0]
	for _, b := range f.bats {
		b.age += seconds
		if b.age < b.duration {
			alive = append(alive, b)
		}
	}
	f.bats = alive
	f.next -= seconds
	if f.next > 0 {
		return
	}
	// Limit excursions in tall viewports, with room for the entire silhouette
	// above the floor and below the top of the view in ordinary window sizes.
	span := math.Min(viewport.Height, 1)
	padding := math.Min(viewport.Height*.2, .06)
	low, high := viewport.Y+padding, viewport.Y+viewport.Height-padding
	clampY := func(y float64) float64 { return math.Max(low, math.Min(high, y)) }
	startY := viewport.Y + viewport.Height*(.2+.6*f.rng.Float64())
	endY := clampY(startY + (f.rng.Float64()-.5)*span*.5)
	b := bat{
		start:    V{X: backgroundMinX - batMargin, Y: startY},
		end:      V{X: backgroundMaxX + batMargin, Y: endY},
		control1: clampY(startY + (f.rng.Float64()-.5)*span*.8),
		control2: clampY(endY + (f.rng.Float64()-.5)*span*.8),
		duration: (backgroundMaxX - backgroundMinX + 2*batMargin) / (.14 + .08*f.rng.Float64()),
		size:     .012 + .006*f.rng.Float64(),
		phase:    f.rng.Float64() * 2 * math.Pi,
		weave:    span * (.015 + .02*f.rng.Float64()),
	}
	if f.rng.Intn(2) == 0 {
		b.start.X, b.end.X = b.end.X, b.start.X
	}
	f.bats = append(f.bats, b)
	f.next = 5 + f.rng.Float64()*6
}

func (f *batFlock) draw(dst *ebiten.Image, viewport Viewport) {
	view := targetTransform(dst)
	pixels := view.pixels
	scale := float64(pixels) / Width
	cameraY := renderAlignedY(viewport.Y, pixels)
	options := &vector.DrawPathOptions{AntiAlias: true}
	for _, b := range f.bats {
		t := b.age / b.duration
		position := b.position(t)
		x, y := view.offsetX+position.X*scale, (position.Y-cameraY)*scale
		size := b.size * scale
		if x+size < 0 || x-size > float64(dst.Bounds().Dx()) || y+size < 0 || y-size > float64(dst.Bounds().Dy()) {
			continue
		}
		options.ColorScale.Reset()
		options.ColorScale.ScaleWithColor(color.NRGBA{R: 45, G: 35, B: 40, A: uint8(math.Round(255 * horizontalFade(position.X)))})
		flap := float32(math.Sin(b.age*2*math.Pi*6 + b.phase))
		path := batSilhouette(flap)
		transform := &vector.AddPathOptions{}
		transform.GeoM.Scale(size, size)
		transform.GeoM.Rotate(b.bank(t))
		transform.GeoM.Translate(x, y)
		var screenPath vector.Path
		screenPath.AddPath(&path, transform)
		vector.FillPath(dst, &screenPath, nil, options)
	}
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
