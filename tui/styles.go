package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Catppuccin Mocha palette.
var (
	cBase     color.Color = lipgloss.Color("#1e1e2e")
	cMantle   color.Color = lipgloss.Color("#181825")
	cCrust    color.Color = lipgloss.Color("#11111b")
	cSurface0 color.Color = lipgloss.Color("#313244")
	cSurface1 color.Color = lipgloss.Color("#45475a")
	cSurface2 color.Color = lipgloss.Color("#585b70")
	cOverlay0 color.Color = lipgloss.Color("#6c7086")
	cOverlay1 color.Color = lipgloss.Color("#7f849c")
	cSubtext0 color.Color = lipgloss.Color("#a6adc8")
	cSubtext1 color.Color = lipgloss.Color("#bac2de")
	cText     color.Color = lipgloss.Color("#cdd6f4")
	cLavender color.Color = lipgloss.Color("#b4befe")
	cBlue     color.Color = lipgloss.Color("#89b4fa")
	cSapphire color.Color = lipgloss.Color("#74c7ec")
	cTeal     color.Color = lipgloss.Color("#94e2d5")
	cGreen    color.Color = lipgloss.Color("#a6e3a1")
	cYellow   color.Color = lipgloss.Color("#f9e2af")
	cPeach    color.Color = lipgloss.Color("#fab387")
	cMaroon   color.Color = lipgloss.Color("#eba0ac")
	cRed      color.Color = lipgloss.Color("#f38ba8")
	cMauve    color.Color = lipgloss.Color("#cba6f7")
	cPink     color.Color = lipgloss.Color("#f5c2e7")
	cFlamingo color.Color = lipgloss.Color("#f2cdcd")
)

// Header / title bar.
var (
	headerStyle = lipgloss.NewStyle().
			Foreground(cSubtext0).
			Background(cBase).
			Padding(0, 1)

	headerTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(cMauve).
				Background(cBase)

	headerCountStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(cLavender).
				Background(cBase)

	headerFilterStyle = lipgloss.NewStyle().
				Foreground(cYellow).
				Background(cBase).
				Italic(true)

	headerPausedStyle = lipgloss.NewStyle().
				Foreground(cPeach).
				Background(cBase).
				Bold(true)
)

// Breadcrumb tabs.
var (
	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(cBase).
			Background(cMauve).
			Padding(0, 2)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(cSubtext0).
				Background(cSurface0).
				Padding(0, 2)

	tabSeparatorStyle = lipgloss.NewStyle().
				Foreground(cOverlay0).
				Background(cBase)
)

// Table.
var (
	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(cSubtext0).
				Background(cBase).
				Padding(0, 1)

	tableCellStyle = lipgloss.NewStyle().
			Foreground(cText).
			Background(cBase).
			Padding(0, 1)

	tableSelectedStyle = lipgloss.NewStyle().
				Foreground(cText).
				Bold(true).
				Background(cSurface0).
				Padding(0, 1)

	tableAccentSelected = lipgloss.NewStyle().
				Foreground(cMauve).
				Background(cSurface0).
				Bold(true)

	tableBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(cSurface1).
				BorderForegroundBlend(cSurface2, cMauve, cSurface2).
				BorderBackground(cBase)
)

// Status indicators with dot prefix.
var (
	statusRunningStyle    = lipgloss.NewStyle().Foreground(cGreen).Background(cBase)
	statusExitedStyle     = lipgloss.NewStyle().Foreground(cRed).Background(cBase)
	statusPausedStyle     = lipgloss.NewStyle().Foreground(cYellow).Background(cBase)
	statusRestartingStyle = lipgloss.NewStyle().Foreground(cPeach).Background(cBase)
	statusCreatedStyle    = lipgloss.NewStyle().Foreground(cSapphire).Background(cBase)
	statusDefaultStyle    = lipgloss.NewStyle().Foreground(cOverlay0).Background(cBase)
)

// Status dot characters.
const (
	dotRunning = "●"
	dotExited  = "○"
	dotPaused  = "◉"
	dotOther   = "◌"
)

// Log level styles.
var (
	logErrorStyle = lipgloss.NewStyle().Foreground(cRed).Background(cBase)
	logWarnStyle  = lipgloss.NewStyle().Foreground(cYellow).Background(cBase)
	logInfoStyle  = lipgloss.NewStyle().Foreground(cGreen).Background(cBase)
	logDebugStyle = lipgloss.NewStyle().Foreground(cOverlay0).Background(cBase)
	logTimeStyle  = lipgloss.NewStyle().Foreground(cOverlay1).Background(cBase)
	logKeyStyle   = lipgloss.NewStyle().Foreground(cBlue).Background(cBase)
)

// Sort indicator.
var sortIndicatorStyle = lipgloss.NewStyle().Foreground(cMauve).Background(cBase)

// Warning indicator.
var warningStyle = lipgloss.NewStyle().Foreground(cPeach).Background(cBase)

// Status bar / footer.
var (
	statusBarStyle = lipgloss.NewStyle().
			Foreground(cRed).
			Align(lipgloss.Center)

	refreshTimerStyle = lipgloss.NewStyle().
				Foreground(cOverlay0).
				Background(cBase).
				Italic(true)

	footerStyle = lipgloss.NewStyle().
			Foreground(cOverlay0).
			Background(cBase).
			Padding(0, 1)

	footerKeyStyle = lipgloss.NewStyle().
			Foreground(cMauve).
			Background(cBase).
			Bold(true)

	footerDescStyle = lipgloss.NewStyle().
			Foreground(cSubtext0).
			Background(cBase)
)

// Sparkline gradient colors (low → high).
var sparkGradient = []color.Color{
	lipgloss.Color("#a6e3a1"), // green
	lipgloss.Color("#a6e3a1"),
	lipgloss.Color("#f9e2af"), // yellow
	lipgloss.Color("#fab387"), // peach
	lipgloss.Color("#f38ba8"), // red
}

// Help styles.
var (
	helpTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(cMauve).
			Background(cBase)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(cLavender).
			Background(cBase)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(cSubtext0).
			Background(cBase)

	helpBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cSurface2).
			BorderForegroundBlend(cSurface1, cMauve, cSurface1).
			Padding(1, 3)
)

// Search input.
var (
	searchLabelStyle = lipgloss.NewStyle().
				Foreground(cMauve).
				Background(cBase)

	searchInputStyle = lipgloss.NewStyle().
				Foreground(cText).
				Background(cBase)
)

// Spinner.
var spinnerStyle = lipgloss.NewStyle().Foreground(cMauve).Background(cBase)

// Progress bar.
var progressStyle = lipgloss.NewStyle().
	Foreground(cSubtext0).
	Background(cBase).
	Padding(1, 2)

// Log gutter.
var (
	gutterStyle     = lipgloss.NewStyle().Foreground(cSurface2).Background(cBase)
	gutterSoftStyle = lipgloss.NewStyle().Foreground(cSurface1).Background(cBase)
)

// Modal overlay.
var modalStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(cSurface2).
	BorderForegroundBlend(cSurface1, cMauve, cSurface1).
	BorderBackground(cBase).
	Background(cBase).
	Padding(1, 3)

// baseBg wraps plain text with the base background to prevent transparency.
var baseBgStyle = lipgloss.NewStyle().Background(cBase).Foreground(cSubtext0)

func baseBg(s string) string {
	return baseBgStyle.Render(s)
}

// statusDot returns a colored dot for a container status.
func statusDot(status string) string {
	switch status {
	case "running":
		return statusRunningStyle.Render(dotRunning)
	case "exited", "dead", "removing":
		return statusExitedStyle.Render(dotExited)
	case "paused":
		return statusPausedStyle.Render(dotPaused)
	case "restarting":
		return statusRestartingStyle.Render(dotOther)
	case "created":
		return statusCreatedStyle.Render(dotOther)
	default:
		return statusDefaultStyle.Render(dotOther)
	}
}

// statusStyle returns the appropriate style for a container status.
func statusStyle(status string) lipgloss.Style {
	switch status {
	case "running":
		return statusRunningStyle
	case "exited", "dead", "removing":
		return statusExitedStyle
	case "paused":
		return statusPausedStyle
	case "restarting":
		return statusRestartingStyle
	case "created":
		return statusCreatedStyle
	default:
		return statusDefaultStyle
	}
}
