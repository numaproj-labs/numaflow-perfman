package report

import "image/color"

// Baseline and candidate colors are shared across PNG and HTML/SVG output.
var (
	ColorBaseline       = color.RGBA{R: 37, G: 99, B: 235, A: 255}
	ColorCandidate      = color.RGBA{R: 220, G: 38, B: 38, A: 255}
	ColorBaselineFaint  = color.RGBA{R: 37, G: 99, B: 235, A: 70}
	ColorCandidateFaint = color.RGBA{R: 220, G: 38, B: 38, A: 70}
	ColorBaselineBand   = color.RGBA{R: 37, G: 99, B: 235, A: 45}
	ColorCandidateBand  = color.RGBA{R: 220, G: 38, B: 38, A: 45}
	ColorAxis           = color.RGBA{R: 30, G: 30, B: 30, A: 255}
	ColorGrid           = color.RGBA{R: 220, G: 220, B: 220, A: 255}
)

const (
	svgBaseline       = "#2563eb"
	svgCandidate      = "#dc2626"
	svgBaselineFaint  = "rgba(37,99,235,0.35)"
	svgCandidateFaint = "rgba(220,38,38,0.35)"
	svgBaselineBand   = "rgba(37,99,235,0.18)"
	svgCandidateBand  = "rgba(220,38,38,0.18)"
)
