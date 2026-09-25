package web

import (
	"fmt"
	"html/template"
	"math"
	"strings"

	"regime-sleeve/internal/backtest"
)

func equitySVG(points []backtest.Point) template.HTML {
	if len(points) < 2 {
		return ""
	}
	s := make([]float64, len(points))
	h := make([]float64, len(points))
	eq, hold := 1.0, 1.0
	for i, p := range points {
		eq *= 1 + p.Ret
		hold *= 1 + p.HoldRet
		s[i] = eq
		h[i] = hold
	}
	return lineSVG(s, h, "#1f6b45", "#8a8175", false)
}

func drawdownSVG(points []backtest.Point) template.HTML {
	if len(points) < 2 {
		return ""
	}
	dd := make([]float64, len(points))
	eq, peak := 1.0, 1.0
	for i, p := range points {
		eq *= 1 + p.Ret
		if eq > peak {
			peak = eq
		}
		dd[i] = eq/peak - 1
	}
	return lineSVG(dd, nil, "#9c2f2f", "", true)
}

func rollSVG(vals []float64) template.HTML {
	if len(vals) < 2 {
		return ""
	}
	return lineSVG(vals, nil, "#1c1915", "", true)
}

func lineSVG(a, b []float64, colorA, colorB string, zeroLine bool) template.HTML {
	const w, h = 760, 220
	const l, r, t, bot = 16, 16, 12, 18
	minV, maxV := bounds(a, b)
	if zeroLine {
		if minV > 0 {
			minV = 0
		}
		if maxV < 0 {
			maxV = 0
		}
	}
	if math.Abs(maxV-minV) < 1e-9 {
		maxV = minV + 1
	}
	var bld strings.Builder
	fmt.Fprintf(&bld, `<svg viewBox="0 0 %d %d" role="img" class="chart">`, w, h)
	if zeroLine {
		y := mapY(0, minV, maxV, t, h-bot)
		fmt.Fprintf(&bld, `<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" class="zero"/>`, l, y, w-r, y)
	}
	fmt.Fprintf(&bld, `<polyline fill="none" stroke="%s" stroke-width="1.6" points="%s"/>`, colorA, poly(a, w, h, l, r, t, bot, minV, maxV))
	if b != nil && colorB != "" {
		fmt.Fprintf(&bld, `<polyline fill="none" stroke="%s" stroke-width="1.4" points="%s"/>`, colorB, poly(b, w, h, l, r, t, bot, minV, maxV))
	}
	bld.WriteString(`</svg>`)
	return template.HTML(bld.String())
}

func poly(series []float64, w, h, l, r, t, bot int, minV, maxV float64) string {
	step := 1
	if len(series) > 280 {
		step = len(series) / 280
	}
	var b strings.Builder
	n := 0
	for i := 0; i < len(series); i += step {
		n++
	}
	if (len(series)-1)%step != 0 {
		n++
	}
	k := 0
	write := func(i int) {
		x := float64(l)
		if n > 1 {
			x = float64(l) + float64(k)*(float64(w-l-r)/float64(n-1))
		}
		y := mapY(series[i], minV, maxV, t, h-bot)
		if k > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%.1f,%.1f", x, y)
		k++
	}
	for i := 0; i < len(series); i += step {
		write(i)
	}
	if last := len(series) - 1; last%step != 0 {
		write(last)
	}
	return b.String()
}

func bounds(a, b []float64) (float64, float64) {
	minV, maxV := a[0], a[0]
	scan := func(s []float64) {
		for _, v := range s {
			if v < minV {
				minV = v
			}
			if v > maxV {
				maxV = v
			}
		}
	}
	scan(a)
	if b != nil {
		scan(b)
	}
	pad := (maxV - minV) * 0.08
	if pad < 1e-6 {
		pad = 0.01
	}
	return minV - pad, maxV + pad
}

func mapY(v, minV, maxV float64, top, bottom int) float64 {
	rel := (v - minV) / (maxV - minV)
	return float64(bottom) - rel*float64(bottom-top)
}
