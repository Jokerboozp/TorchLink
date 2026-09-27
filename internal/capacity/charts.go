package capacity

import (
	"fmt"
	"html"
	"math"
	"strings"
	"time"
)

// Charts are plain SVG with their own light background so they read the same
// standalone, embedded in HTML, or pasted into a ticket. Missing data breaks
// lines and is shaded grey; it is never drawn as zero.

var chartColors = []string{"#2563eb", "#d97706", "#059669", "#9333ea", "#dc2626", "#0891b2", "#64748b", "#be185d"}

type xy struct {
	X, Y  float64
	Valid bool
}

type chartSeries struct {
	Name   string
	En     string // PNG label (ASCII)
	Points []xy
	Dashed bool
	Color  string
}

type chartLine struct {
	Label string
	En    string
	Y     float64
}

type chartBand struct {
	From, To float64
	Label    string
	Fill     string
}

type chartTick struct {
	X     float64
	Label string
}

type chartSpec struct {
	Title, XLabel, YLabel string
	ENTitle, ENYLabel     string // PNG labels (the bitmap font is ASCII only)
	Series                []chartSeries
	Lines                 []chartLine // horizontal reference lines (SLO)
	Bands                 []chartBand // shaded x ranges
	Ticks                 []chartTick // explicit x ticks; nil = numeric/time ticks
	TimeAxis              bool        // X is Unix ms
	Markers               bool
	Note                  string
}

const (
	chartW, chartH                     = 880, 360
	padL, padR, padT, padB             = 72, 24, 44, 88
	plotW, plotH                       = chartW - padL - padR, chartH - padT - padB
	chartFont                          = `font-family="system-ui,-apple-system,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif"`
	chartInk, chartMuted, chartGridCol = "#1f2937", "#6b7280", "#e5e7eb"
)

// chartLayout is the geometry shared by the SVG and PNG renderers.
type chartLayout struct {
	minX, maxX, yMax float64
	anyValid         bool
	ticks            []chartTick
}

func (l chartLayout) px(x float64) float64 { return padL + (x-l.minX)/(l.maxX-l.minX)*plotW }
func (l chartLayout) py(y float64) float64 { return padT + plotH - y/l.yMax*plotH }

func layoutChart(s chartSpec) chartLayout {
	minX, maxX := math.Inf(1), math.Inf(-1)
	maxY := 0.0
	anyValid := false
	for _, se := range s.Series {
		for _, p := range se.Points {
			minX, maxX = math.Min(minX, p.X), math.Max(maxX, p.X)
			if p.Valid {
				maxY = math.Max(maxY, p.Y)
				anyValid = true
			}
		}
	}
	for _, l := range s.Lines {
		maxY = math.Max(maxY, l.Y)
	}
	for _, t := range s.Ticks {
		minX, maxX = math.Min(minX, t.X), math.Max(maxX, t.X)
	}
	for _, b := range s.Bands {
		minX, maxX = math.Min(minX, b.From), math.Max(maxX, b.To)
	}
	if math.IsInf(minX, 1) {
		minX, maxX = 0, 1
	}
	if maxX == minX {
		minX, maxX = minX-0.5, maxX+0.5
	}
	if s.Ticks != nil {
		minX, maxX = minX-0.5, maxX+0.5
	}
	yMax := niceCeil(maxY * 1.08)
	if yMax <= 0 {
		yMax = 1
	}
	l := chartLayout{minX: minX, maxX: maxX, yMax: yMax, anyValid: anyValid, ticks: s.Ticks}
	if l.ticks == nil {
		for i := 0; i <= 5; i++ {
			x := minX + (maxX-minX)*float64(i)/5
			label := fmtAxis(x)
			if s.TimeAxis {
				label = time.UnixMilli(int64(x)).Format("15:04:05")
			}
			l.ticks = append(l.ticks, chartTick{X: x, Label: label})
		}
	}
	return l
}

func renderChart(s chartSpec) string {
	l := layoutChart(s)
	yMax, anyValid := l.yMax, l.anyValid
	px, py := l.px, l.py
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="100%%" role="img" aria-label="%s" %s font-size="12">`, chartW, chartH, html.EscapeString(s.Title), chartFont)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#ffffff" rx="8"/>`, chartW, chartH)
	fmt.Fprintf(&b, `<text x="%d" y="24" font-size="15" font-weight="600" fill="%s">%s</text>`, padL, chartInk, html.EscapeString(s.Title))
	for _, band := range s.Bands {
		fill := band.Fill
		if fill == "" {
			fill = "#f3f4f6"
		}
		x0, x1 := px(band.From), px(band.To)
		fmt.Fprintf(&b, `<rect x="%.1f" y="%d" width="%.1f" height="%d" fill="%s"><title>%s</title></rect>`, x0, padT, math.Max(1, x1-x0), plotH, fill, html.EscapeString(band.Label))
	}
	for i := 0; i <= 4; i++ {
		y := yMax * float64(i) / 4
		fmt.Fprintf(&b, `<line x1="%d" x2="%d" y1="%.1f" y2="%.1f" stroke="%s"/>`, padL, padL+plotW, py(y), py(y), chartGridCol)
		fmt.Fprintf(&b, `<text x="%d" y="%.1f" text-anchor="end" fill="%s">%s</text>`, padL-8, py(y)+4, chartMuted, fmtAxis(y))
	}
	fmt.Fprintf(&b, `<text transform="translate(16 %d) rotate(-90)" text-anchor="middle" fill="%s">%s</text>`, padT+plotH/2, chartMuted, html.EscapeString(s.YLabel))
	for _, t := range l.ticks {
		fmt.Fprintf(&b, `<text x="%.1f" y="%d" text-anchor="middle" fill="%s">%s</text>`, px(t.X), padT+plotH+18, chartMuted, html.EscapeString(t.Label))
	}
	fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="middle" fill="%s">%s</text>`, padL+plotW/2, padT+plotH+38, chartMuted, html.EscapeString(s.XLabel))
	for i, l := range s.Lines {
		fmt.Fprintf(&b, `<line x1="%d" x2="%d" y1="%.1f" y2="%.1f" stroke="%s" stroke-dasharray="6 4"/>`, padL, padL+plotW, py(l.Y), py(l.Y), "#dc2626")
		fmt.Fprintf(&b, `<text x="%d" y="%.1f" text-anchor="end" fill="#dc2626">%s</text>`, padL+plotW-4, py(l.Y)-4-float64(i%2)*12, html.EscapeString(l.Label))
	}
	for i, se := range s.Series {
		color := se.Color
		if color == "" {
			color = chartColors[i%len(chartColors)]
		}
		dash := ""
		if se.Dashed {
			dash = ` stroke-dasharray="5 4"`
		}
		var seg []string
		flush := func() {
			if len(seg) > 1 {
				fmt.Fprintf(&b, `<polyline fill="none" stroke="%s" stroke-width="2"%s points="%s"/>`, color, dash, strings.Join(seg, " "))
			}
			seg = seg[:0]
		}
		for _, p := range se.Points {
			if !p.Valid {
				flush()
				continue
			}
			seg = append(seg, fmt.Sprintf("%.1f,%.1f", px(p.X), py(p.Y)))
			if s.Markers || len(se.Points) == 1 {
				fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="3.5" fill="%s"><title>%s: %s</title></circle>`, px(p.X), py(p.Y), color, html.EscapeString(se.Name), fmtAxis(p.Y))
			}
		}
		flush()
		lx := padL + (i%4)*200
		ly := chartH - 26 + (i/4)*16
		fmt.Fprintf(&b, `<line x1="%d" x2="%d" y1="%d" y2="%d" stroke="%s" stroke-width="3"%s/><text x="%d" y="%d" fill="%s">%s</text>`, lx, lx+18, ly-4, ly-4, color, dash, lx+24, ly, chartInk, html.EscapeString(se.Name))
	}
	if !anyValid {
		fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="middle" fill="%s" font-size="14">没有有效观测（缺测，不代表零）</text>`, padL+plotW/2, padT+plotH/2, chartMuted)
	}
	if s.Note != "" {
		fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="end" fill="%s">%s</text>`, chartW-padR, 24, chartMuted, html.EscapeString(s.Note))
	}
	b.WriteString(`</svg>`)
	return b.String()
}

func niceCeil(v float64) float64 {
	if v <= 0 {
		return 0
	}
	exp := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10} {
		if m*exp >= v {
			return m * exp
		}
	}
	return 10 * exp
}

func fmtAxis(v float64) string {
	switch {
	case math.Abs(v) >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case math.Abs(v) >= 1e4:
		return fmt.Sprintf("%.0fk", v/1e3)
	case math.Abs(v) >= 100 || v == math.Trunc(v):
		return fmt.Sprintf("%.0f", v)
	default:
		return fmt.Sprintf("%.1f", v)
	}
}
