package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Color palette — k9s-inspired dark theme.
var (
	colorCyan      color.Color = lipgloss.Color("#00FFFF")
	colorWhite     color.Color = lipgloss.Color("#FFFFFF")
	colorBlack     color.Color = lipgloss.Color("#000000")
	colorNavy      color.Color = lipgloss.Color("#000080")
	colorGreen     color.Color = lipgloss.Color("#00FF00")
	colorRed       color.Color = lipgloss.Color("#FF0000")
	colorYellow    color.Color = lipgloss.Color("#FFFF00")
	colorFuchsia   color.Color = lipgloss.Color("#FF00FF")
	colorGray      color.Color = lipgloss.Color("#808080")
	colorDarkGray  color.Color = lipgloss.Color("#444444")
	colorBlue      color.Color = lipgloss.Color("#5555FF")
	colorTeal      color.Color = lipgloss.Color("#008B8B")
	colorDarkCyan  color.Color = lipgloss.Color("#005F5F")
	colorLightCyan color.Color = lipgloss.Color("#87FFFF")
)

// Header styles.
var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWhite).
			Background(colorNavy).
			Padding(0, 1)

	headerTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorCyan)

	headerCountStyle = lipgloss.NewStyle().
				Foreground(colorFuchsia)

	headerFilterStyle = lipgloss.NewStyle().
				Foreground(colorWhite).
				Italic(true)

	headerPausedStyle = lipgloss.NewStyle().
				Foreground(colorFuchsia).
				Bold(true)
)

// Tab styles for breadcrumb navigation.
var (
	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorCyan).
			Background(colorNavy).
			Padding(0, 2)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(colorGray).
				Background(colorDarkGray).
				Padding(0, 2)

	tabSeparatorStyle = lipgloss.NewStyle().
				Foreground(colorGray)
)

// Table styles with gradient borders.
var (
	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorCyan).
				Padding(0, 1)

	tableCellStyle = lipgloss.NewStyle().
			Foreground(colorWhite).
			Padding(0, 1)

	tableSelectedStyle = lipgloss.NewStyle().
				Background(colorNavy).
				Foreground(colorWhite).
				Bold(true).
				Padding(0, 1)

	tableBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorNavy).
				BorderForegroundBlend(colorDarkCyan, colorCyan, colorTeal)
)

// Status color styles.
var (
	statusRunningStyle    = lipgloss.NewStyle().Foreground(colorGreen)
	statusExitedStyle     = lipgloss.NewStyle().Foreground(colorRed)
	statusPausedStyle     = lipgloss.NewStyle().Foreground(colorYellow)
	statusRestartingStyle = lipgloss.NewStyle().Foreground(colorFuchsia)
	statusCreatedStyle    = lipgloss.NewStyle().Foreground(colorCyan)
	statusDefaultStyle    = lipgloss.NewStyle().Foreground(colorGray)
)

// Log level styles.
var (
	logErrorStyle = lipgloss.NewStyle().Foreground(colorRed)
	logWarnStyle  = lipgloss.NewStyle().Foreground(colorYellow)
	logInfoStyle  = lipgloss.NewStyle().Foreground(colorGreen)
	logDebugStyle = lipgloss.NewStyle().Foreground(colorGray)
	logTimeStyle  = lipgloss.NewStyle().Foreground(colorGray)
	logKeyStyle   = lipgloss.NewStyle().Foreground(colorBlue)
)

// Sort indicator styles.
var (
	sortIndicatorStyle = lipgloss.NewStyle().Foreground(colorFuchsia)
)

// Warning indicator.
var (
	warningStyle = lipgloss.NewStyle().Foreground(colorYellow)
)

// Status bar style.
var (
	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			Align(lipgloss.Center)

	refreshTimerStyle = lipgloss.NewStyle().
				Foreground(colorGray).
				Italic(true)
)

// Sparkline styles.
var (
	sparklineCPUStyle = lipgloss.NewStyle().Foreground(colorGreen)
	sparklineMEMStyle = lipgloss.NewStyle().Foreground(colorBlue)
)

// Help styles.
var (
	helpTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorCyan)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorCyan)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(colorWhite)

	helpBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorCyan).
			BorderForegroundBlend(colorTeal, colorCyan, colorLightCyan).
			Padding(1, 2)
)

// Search input style.
var (
	searchLabelStyle = lipgloss.NewStyle().
				Foreground(colorCyan)

	searchInputStyle = lipgloss.NewStyle().
				Foreground(colorWhite)
)

// Spinner style.
var (
	spinnerStyle = lipgloss.NewStyle().Foreground(colorFuchsia)
)

// Progress bar style.
var (
	progressStyle = lipgloss.NewStyle().Padding(1, 2)
)

// Log gutter styles.
var (
	gutterStyle     = lipgloss.NewStyle().Foreground(colorDarkGray)
	gutterSoftStyle = lipgloss.NewStyle().Foreground(colorDarkGray)
)

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
