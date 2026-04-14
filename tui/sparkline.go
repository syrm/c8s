package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// sparklineChars are unicode block characters ordered from low to high.
var sparklineChars = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

const sparklineMaxHistory = 8

// metricHistory tracks recent CPU and memory values for sparkline rendering.
type metricHistory struct {
	cpu []float64
	mem []float64
}

func (h *metricHistory) push(cpu, mem float64) {
	h.cpu = append(h.cpu, cpu)
	if len(h.cpu) > sparklineMaxHistory {
		h.cpu = h.cpu[1:]
	}
	h.mem = append(h.mem, mem)
	if len(h.mem) > sparklineMaxHistory {
		h.mem = h.mem[1:]
	}
}

// sparkColor picks a color from the gradient based on a 0..1 normalized value.
func sparkColor(normalized float64, gradient []color.Color) color.Color {
	if len(gradient) == 0 {
		return lipgloss.Color("#cdd6f4")
	}
	if normalized <= 0 {
		return gradient[0]
	}
	if normalized >= 1 {
		return gradient[len(gradient)-1]
	}
	idx := normalized * float64(len(gradient)-1)
	i := int(idx)
	if i >= len(gradient)-1 {
		return gradient[len(gradient)-1]
	}
	return gradient[i]
}

// renderSparkline renders a unicode sparkline with per-character gradient color.
// bg is optional: if non-nil, each character gets that background color.
func renderSparkline(values []float64, gradient []color.Color, bg color.Color) string {
	if len(values) == 0 {
		return ""
	}

	const maxVal = 100.0
	var b strings.Builder

	for _, v := range values {
		normalized := v / maxVal
		if normalized > 1.0 {
			normalized = 1.0
		}
		if normalized < 0 {
			normalized = 0
		}
		idx := int(normalized * float64(len(sparklineChars)-1))
		if idx >= len(sparklineChars) {
			idx = len(sparklineChars) - 1
		}
		c := sparkColor(normalized, gradient)
		s := lipgloss.NewStyle().Foreground(c)
		if bg != nil {
			s = s.Background(bg)
		}
		b.WriteString(s.Render(string(sparklineChars[idx])))
	}

	s := lipgloss.NewStyle()
	if bg != nil {
		s = s.Background(bg)
	}
	b.WriteString(s.Render(strings.Repeat(" ", sparklineMaxHistory-len(values))))

	return b.String()
}
