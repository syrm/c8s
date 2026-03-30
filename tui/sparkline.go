package tui

import (
	"charm.land/lipgloss/v2"
)

// sparklineChars are unicode block characters ordered from low to high.
var sparklineChars = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

const sparklineMaxHistory = 20

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

// renderSparkline renders a unicode sparkline from a slice of percentage values.
func renderSparkline(values []float64, style lipgloss.Style) string {
	if len(values) == 0 {
		return ""
	}

	const maxVal = 100.0

	result := make([]rune, len(values))
	for i, v := range values {
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
		result[i] = sparklineChars[idx]
	}
	return style.Render(string(result))
}
