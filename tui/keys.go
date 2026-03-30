package tui

import "charm.land/bubbles/v2/key"

// keyMap defines all key bindings for the application.
type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Enter    key.Binding
	Back     key.Binding
	Quit     key.Binding
	Help     key.Binding
	Search   key.Binding
	Clear    key.Binding
	SortN    key.Binding
	SortC    key.Binding
	SortM    key.Binding
	SortO    key.Binding
	SortS    key.Binding
	Shell    key.Binding
	Stop     key.Binding
	Restart  key.Binding
	Remove   key.Binding
	Pause    key.Binding
	Time     key.Binding
	NextHit  key.Binding
	PrevHit  key.Binding
	ClearLog key.Binding
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter", "right", "l"),
		key.WithHelp("→/enter", "select"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc", "left", "h"),
		key.WithHelp("←/esc", "back"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	Search: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "filter"),
	),
	Clear: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "clear filter"),
	),
	SortN: key.NewBinding(
		key.WithKeys("N"),
		key.WithHelp("N", "sort name"),
	),
	SortC: key.NewBinding(
		key.WithKeys("C"),
		key.WithHelp("C", "sort CPU"),
	),
	SortM: key.NewBinding(
		key.WithKeys("M"),
		key.WithHelp("M", "sort memory"),
	),
	SortO: key.NewBinding(
		key.WithKeys("O"),
		key.WithHelp("O", "sort containers"),
	),
	SortS: key.NewBinding(
		key.WithKeys("S"),
		key.WithHelp("S", "sort status"),
	),
	Shell: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "shell"),
	),
	Stop: key.NewBinding(
		key.WithKeys("x"),
		key.WithHelp("x", "stop"),
	),
	Restart: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "start/restart"),
	),
	Remove: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "remove"),
	),
	Pause: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "pause"),
	),
	Time: key.NewBinding(
		key.WithKeys("t"),
		key.WithHelp("t", "timestamps"),
	),
	NextHit: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "next match"),
	),
	PrevHit: key.NewBinding(
		key.WithKeys("shift+n"),
		key.WithHelp("shift+n", "prev match"),
	),
	ClearLog: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "clear filter"),
	),
}

// projectKeyMap implements help.KeyMap for the project list view.
type projectKeyMap struct{}

func (k projectKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{keys.Up, keys.Down, keys.Enter, keys.Search, keys.Help, keys.Quit}
}

func (k projectKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{keys.Up, keys.Down, keys.Enter, keys.Back},
		{keys.Search, keys.Clear, keys.Help, keys.Quit},
		{keys.SortN, keys.SortC, keys.SortM, keys.SortO},
	}
}

// containerKeyMap implements help.KeyMap for the container list view.
type containerKeyMap struct{}

func (k containerKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{keys.Up, keys.Down, keys.Enter, keys.Back, keys.Search, keys.Help}
}

func (k containerKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{keys.Up, keys.Down, keys.Enter, keys.Back},
		{keys.Search, keys.Clear, keys.Help, keys.Quit},
		{keys.SortN, keys.SortC, keys.SortM, keys.SortS},
		{keys.Shell, keys.Stop, keys.Restart, keys.Remove},
	}
}

// logKeyMap implements help.KeyMap for the log view.
type logKeyMap struct{}

func (k logKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{keys.Back, keys.Pause, keys.Search, keys.NextHit, keys.Help}
}

func (k logKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{keys.Back, keys.Pause, keys.Time},
		{keys.Search, keys.ClearLog, keys.NextHit, keys.PrevHit},
		{keys.Help, keys.Quit},
	}
}
