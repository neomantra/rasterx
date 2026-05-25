// Gradient implementation fo rasterx package
// Copyright 2018 All rights reserved.
// Created: 5/12/2018 by S.R.Wiley

package rasterx

import (
	"image/color"
	"math"
	"sort"
)

// SVG bounds paremater constants
const (
	ObjectBoundingBox GradientUnits = iota
	UserSpaceOnUse
)

// SVG spread parameter constants
const (
	PadSpread SpreadMethod = iota
	ReflectSpread
	RepeatSpread
)

const epsilonF = 1e-5

type (
	// SpreadMethod is the type for spread parameters
	SpreadMethod byte
	// GradientUnits is the type for gradient units
	GradientUnits byte
	// GradStop represents a stop in the SVG 2.0 gradient specification
	GradStop struct {
		StopColor color.Color
		Offset    float64
		Opacity   float64
	}
	// fastStop is a pre-digested GradStop for the hot per-pixel path.
	fastStop struct {
		offset  float64
		opacity float64
		r, g, b uint32
	}
	// Gradient holds a description of an SVG 2.0 gradient
	Gradient struct {
		Points   [5]float64
		Stops    []GradStop
		Bounds   struct{ X, Y, W, H float64 }
		Matrix   Matrix2D
		Spread   SpreadMethod
		Units    GradientUnits
		IsRadial bool

		stopCache []fastStop
	}
)

// ApplyOpacity sets the color's alpha channel to the given value
func ApplyOpacity(c color.Color, opacity float64) color.NRGBA {
	r, g, b, _ := c.RGBA()
	return color.NRGBA{uint8(r), uint8(g), uint8(b), uint8(opacity * 0xFF)}
}

// tColor takes the paramaterized value along the gradient's stops and
// returns a color depending on the spreadMethod value of the gradient and
// the gradient's slice of stop values.
func (g *Gradient) tColor(t, opacity float64) color.Color {
	d := len(g.stopCache)
	if d == 0 {
		return color.NRGBA{0, 0, 0, 0}
	}

	switch g.Spread {
	case PadSpread:
		if t >= 1.0 {
			s := g.stopCache[d-1]
			return color.NRGBA{uint8(s.r), uint8(s.g), uint8(s.b), uint8(s.opacity * opacity * 0xFF)}
		}
		if t <= 0.0 {
			s := g.stopCache[0]
			return color.NRGBA{uint8(s.r), uint8(s.g), uint8(s.b), uint8(s.opacity * opacity * 0xFF)}
		}
		place := sort.Search(d, func(i int) bool {
			return g.stopCache[i].offset >= t
		})
		switch place {
		case 0:
			s := g.stopCache[0]
			return color.NRGBA{uint8(s.r), uint8(s.g), uint8(s.b), uint8(s.opacity * opacity * 0xFF)}
		case d:
			s := g.stopCache[d-1]
			return color.NRGBA{uint8(s.r), uint8(s.g), uint8(s.b), uint8(s.opacity * opacity * 0xFF)}
		default:
			return blendStops(t, opacity, g.stopCache[place-1], g.stopCache[place], false)
		}
	case RepeatSpread:
		mod := math.Mod(t, 1.0)
		if mod < 0 {
			mod += 1.0
		}
		place := sort.Search(d, func(i int) bool {
			return g.stopCache[i].offset >= mod
		})
		var s1, s2 fastStop
		switch place {
		case 0, d:
			s1, s2 = g.stopCache[d-1], g.stopCache[0]
		default:
			s1, s2 = g.stopCache[place-1], g.stopCache[place]
		}
		return blendStops(mod, opacity, s1, s2, false)
	case ReflectSpread:
		mod := math.Mod(t, 2.0)
		if mod < 0 {
			mod += 2.0
		}
		place := 0
		for place != d && mod > g.stopCache[place].offset {
			place++
		}
		switch place {
		case 0:
			s := g.stopCache[0]
			return color.NRGBA{uint8(s.r), uint8(s.g), uint8(s.b), uint8(s.opacity * opacity * 0xFF)}
		case d:
			for place != d*2 && mod-1 > (1-g.stopCache[d*2-place-1].offset) {
				place++
			}
			switch place {
			case d:
				s := g.stopCache[d-1]
				return color.NRGBA{uint8(s.r), uint8(s.g), uint8(s.b), uint8(s.opacity * opacity * 0xFF)}
			case d * 2:
				s := g.stopCache[0]
				return color.NRGBA{uint8(s.r), uint8(s.g), uint8(s.b), uint8(s.opacity * opacity * 0xFF)}
			default:
				return blendStops(mod-1, opacity,
					g.stopCache[d*2-place], g.stopCache[d*2-place-1], true)
			}
		default:
			return blendStops(mod, opacity, g.stopCache[place-1], g.stopCache[place], false)
		}
	}
	return color.NRGBA{0, 0, 0, 0}
}

func blendStops(t, opacity float64, s1, s2 fastStop, flip bool) color.NRGBA {
	s1off := s1.offset
	if s1.offset > s2.offset && !flip { // happens in repeat spread mode
		s1off--
		if t > 1 {
			t--
		}
	}
	if s2.offset == s1off {
		return color.NRGBA{uint8(s2.r), uint8(s2.g), uint8(s2.b), uint8(s2.opacity * 0xFF)}
	}
	if flip {
		t = 1 - t
	}
	tp := (t - s1off) / (s2.offset - s1off)
	inv := 1 - tp
	return color.NRGBA{
		R: uint8((float64(s1.r)*inv + float64(s2.r)*tp) / 256),
		G: uint8((float64(s1.g)*inv + float64(s2.g)*tp) / 256),
		B: uint8((float64(s1.b)*inv + float64(s2.b)*tp) / 256),
		A: uint8((s1.opacity*inv + s2.opacity*tp) * opacity * 0xFF),
	}
}

//GetColorFunction returns the color function
func (g *Gradient) GetColorFunction(opacity float64) interface{} {
	return g.GetColorFunctionUS(opacity, Identity)
}

// ensureSorted sorts the stops by offset in ascending order.
// It is safe to call repeatedly; if the stops are already sorted it returns immediately.
func (g *Gradient) ensureSorted() {
	if len(g.Stops) < 2 {
		g.buildStopCache()
		return
	}
	for i := 1; i < len(g.Stops); i++ {
		if g.Stops[i].Offset < g.Stops[i-1].Offset {
			sort.Slice(g.Stops, func(i, j int) bool {
				return g.Stops[i].Offset < g.Stops[j].Offset
			})
			break
		}
	}
	g.buildStopCache()
}

// buildStopCache pre-computes fastStop values from g.Stops.
func (g *Gradient) buildStopCache() {
	if len(g.stopCache) == len(g.Stops) {
		return
	}
	g.stopCache = make([]fastStop, len(g.Stops))
	for i, s := range g.Stops {
		r, gv, b, _ := s.StopColor.RGBA()
		g.stopCache[i] = fastStop{
			offset:  s.Offset,
			opacity: s.Opacity,
			r:       r,
			g:       gv,
			b:       b,
		}
	}
}

// GetColorFunctionUS returns the color function using the User Space objMatrix
func (g *Gradient) GetColorFunctionUS(opacity float64, objMatrix Matrix2D) interface{} {
	switch len(g.Stops) {
	case 0:
		return ApplyOpacity(color.RGBA{0, 0, 0, 255}, opacity) // default error color for gradient w/o stops.
	case 1:
		return ApplyOpacity(g.Stops[0].StopColor, opacity) // Illegal, I think, should really should not happen.
	}

	g.ensureSorted()

	w, h := float64(g.Bounds.W), float64(g.Bounds.H)
	oriX, oriY := float64(g.Bounds.X), float64(g.Bounds.Y)
	gradT := Identity.Translate(oriX, oriY).Scale(w, h).
		Mult(g.Matrix).Scale(1/w, 1/h).Translate(-oriX, -oriY).Invert()

	if g.IsRadial {
		cx, cy, fx, fy, rx, ry := g.Points[0], g.Points[1], g.Points[2], g.Points[3], g.Points[4], g.Points[4]
		if g.Units == ObjectBoundingBox {
			cx = g.Bounds.X + g.Bounds.W*cx
			cy = g.Bounds.Y + g.Bounds.H*cy
			fx = g.Bounds.X + g.Bounds.W*fx
			fy = g.Bounds.Y + g.Bounds.H*fy
			rx *= g.Bounds.W
			ry *= g.Bounds.H
		} else {
			cx, cy = g.Matrix.Transform(cx, cy)
			fx, fy = g.Matrix.Transform(fx, fy)
			rx, ry = g.Matrix.TransformVector(rx, ry)
			cx, cy = objMatrix.Transform(cx, cy)
			fx, fy = objMatrix.Transform(fx, fy)
			rx, ry = objMatrix.TransformVector(rx, ry)
		}

		if cx == fx && cy == fy {
			// When the focus and center are the same things are much simpler;
			// t is just distance from center
			// scaled by the bounds aspect ratio times r
			if g.Units == ObjectBoundingBox {
				return ColorFunc(func(xi, yi int) color.Color {
					x, y := gradT.Transform(float64(xi)+0.5, float64(yi)+0.5)
					dx := float64(x) - cx
					dy := float64(y) - cy
					return g.tColor(math.Sqrt(dx*dx/(rx*rx)+(dy*dy)/(ry*ry)), opacity)
				})
			}
			return ColorFunc(func(xi, yi int) color.Color {
				x := float64(xi) + 0.5
				y := float64(yi) + 0.5
				dx := x - cx
				dy := y - cy
				return g.tColor(math.Sqrt(dx*dx/(rx*rx)+(dy*dy)/(ry*ry)), opacity)
			})
		}
		fx /= rx
		fy /= ry
		cx /= rx
		cy /= ry

		dfx := fx - cx
		dfy := fy - cy

		if dfx*dfx+dfy*dfy > 1 { // Focus outside of circle; use intersection
			// point of line from center to focus and circle as per SVG specs.
			nfx, nfy, intersects := RayCircleIntersectionF(fx, fy, cx, cy, cx, cy, 1.0-epsilonF)
			fx, fy = nfx, nfy
			if intersects == false {
				return color.RGBA{255, 255, 0, 255} // should not happen
			}
		}
		if g.Units == ObjectBoundingBox {
			return ColorFunc(func(xi, yi int) color.Color {
				x, y := gradT.Transform(float64(xi)+0.5, float64(yi)+0.5)
				ex := x / rx
				ey := y / ry

				t1x, t1y, intersects := RayCircleIntersectionF(ex, ey, fx, fy, cx, cy, 1.0)
				if intersects == false { //In this case, use the last stop color
					s := g.stopCache[len(g.stopCache)-1]
					return color.NRGBA{uint8(s.r), uint8(s.g), uint8(s.b), uint8(s.opacity * opacity * 0xFF)}
				}
				tdx, tdy := t1x-fx, t1y-fy
				dx, dy := ex-fx, ey-fy
				if tdx*tdx+tdy*tdy < epsilonF {
					s := g.stopCache[len(g.stopCache)-1]
					return color.NRGBA{uint8(s.r), uint8(s.g), uint8(s.b), uint8(s.opacity * opacity * 0xFF)}
				}
				return g.tColor(math.Sqrt(dx*dx+dy*dy)/math.Sqrt(tdx*tdx+tdy*tdy), opacity)
			})
		}
		return ColorFunc(func(xi, yi int) color.Color {
			x := float64(xi) + 0.5
			y := float64(yi) + 0.5
			ex := x / rx
			ey := y / ry

			t1x, t1y, intersects := RayCircleIntersectionF(ex, ey, fx, fy, cx, cy, 1.0)
			if intersects == false { //In this case, use the last stop color
				s := g.stopCache[len(g.stopCache)-1]
				return color.NRGBA{uint8(s.r), uint8(s.g), uint8(s.b), uint8(s.opacity * opacity * 0xFF)}
			}
			tdx, tdy := t1x-fx, t1y-fy
			dx, dy := ex-fx, ey-fy
			if tdx*tdx+tdy*tdy < epsilonF {
				s := g.stopCache[len(g.stopCache)-1]
				return color.NRGBA{uint8(s.r), uint8(s.g), uint8(s.b), uint8(s.opacity * opacity * 0xFF)}
			}
			return g.tColor(math.Sqrt(dx*dx+dy*dy)/math.Sqrt(tdx*tdx+tdy*tdy), opacity)
		})
	}
	p1x, p1y, p2x, p2y := g.Points[0], g.Points[1], g.Points[2], g.Points[3]
	if g.Units == ObjectBoundingBox {
		p1x = g.Bounds.X + g.Bounds.W*p1x
		p1y = g.Bounds.Y + g.Bounds.H*p1y
		p2x = g.Bounds.X + g.Bounds.W*p2x
		p2y = g.Bounds.Y + g.Bounds.H*p2y

		dx := p2x - p1x
		dy := p2y - p1y
		d := (dx*dx + dy*dy) // self inner prod
		return ColorFunc(func(xi, yi int) color.Color {
			x, y := gradT.Transform(float64(xi)+0.5, float64(yi)+0.5)
			dfx := x - p1x
			dfy := y - p1y
			return g.tColor((dx*dfx+dy*dfy)/d, opacity)
		})
	}

	p1x, p1y = g.Matrix.Transform(p1x, p1y)
	p2x, p2y = g.Matrix.Transform(p2x, p2y)
	p1x, p1y = objMatrix.Transform(p1x, p1y)
	p2x, p2y = objMatrix.Transform(p2x, p2y)
	dx := p2x - p1x
	dy := p2y - p1y
	d := (dx*dx + dy*dy)
	// if d == 0.0 {
	// 	fmt.Println("zero delta")
	// }
	return ColorFunc(func(xi, yi int) color.Color {
		x := float64(xi) + 0.5
		y := float64(yi) + 0.5
		dfx := x - p1x
		dfy := y - p1y
		return g.tColor((dx*dfx+dy*dfy)/d, opacity)
	})
}
