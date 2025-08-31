package tui

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"github/syrm/c8s/dto"
)

var keyQuit = key.NewBinding(
	key.WithKeys("q", "esc", "ctrl+c"),
	key.WithHelp("q", "quit"),
)

var moreCharacter = "·"

var keyUp = key.NewBinding(key.WithKeys("up"))
var keyDown = key.NewBinding(key.WithKeys("down"))
var keyLeft = key.NewBinding(key.WithKeys("left"))
var keyRight = key.NewBinding(key.WithKeys("right"))

type ProjectModel struct {
	projects      map[dto.ProjectID]dto.Project
	projectsMutex sync.RWMutex
	updateCh      chan dto.Project // Channel for updating project
	columns       []table.Column
	rows          []table.Row
	table         table.Model
	tableStyle    lipgloss.Style
	cursorX       int
	cursorY       int
	terminalWidth int
	logger        *slog.Logger
}

func NewProjectModel(ctx context.Context, logger *slog.Logger) *ProjectModel {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)

	columns := []table.Column{
		{Title: "Project", Width: 20},
		{Title: "CPU", Width: 20},
		{Title: "Memory", Width: 20},
		{Title: "Cont.", Width: 20},
		{Title: "CPU", Width: 20},
		{Title: "Memory", Width: 20},
		{Title: "Cont.", Width: 20},
	}

	t := table.New(table.WithColumns(columns), table.WithFocused(true), table.WithStyles(s))

	baseStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240"))

	p := &ProjectModel{
		projects:      make(map[dto.ProjectID]dto.Project),
		updateCh:      make(chan dto.Project),
		columns:       columns,
		rows:          []table.Row{},
		table:         t,
		tableStyle:    baseStyle,
		cursorX:       0,
		cursorY:       0,
		terminalWidth: 0,
		logger:        logger,
	}

	return p
}

func (pm *ProjectModel) GetUpdateCh() chan<- dto.Project {
	return pm.updateCh
}

func (pm *ProjectModel) doUpdateProject(ctx context.Context, project dto.Project) map[dto.ProjectID]dto.Project {
	//p.logger.DebugContext(ctx, "received project message", slog.String("project_id", string(project.ID)), slog.String("project_name", project.Name))

	pm.projectsMutex.Lock()
	projects := pm.projects
	projects[project.ID] = project
	pm.projectsMutex.Unlock()

	return projects
}

func (pm *ProjectModel) actorLoop(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		project := <-pm.updateCh
		//p.logger.DebugContext(ctx, "received project update", slog.String("project_id", string(project.ID)), slog.String("project_name", project.Name))
		return pm.doUpdateProject(ctx, project)
	}
}
func (pm *ProjectModel) Init() tea.Cmd {
	return pm.actorLoop(context.Background())
}

func (pm *ProjectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		pm.terminalWidth = msg.Width
		//pm.logger.Error("terminal width", slog.Int("width", msg.Width))

		pm.table.SetHeight(msg.Height - pm.tableStyle.GetHorizontalFrameSize())
		pm.table.SetWidth(msg.Width - pm.tableStyle.GetVerticalFrameSize())

		if pm.cursorX == 0 {
			pm.table.Columns()[0].Width = msg.Width - 56
		}
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keyQuit):
			return pm, tea.Quit
		case key.Matches(msg, keyUp):
			pm.cursorY = max(0, pm.cursorY-1)
			pm.table.SetCursor(pm.cursorY)
			return pm, nil
		case key.Matches(msg, keyDown):
			pm.cursorY = min(len(pm.projects)-1, pm.cursorY+1)
			pm.table.SetCursor(pm.cursorY)
			return pm, nil
		case key.Matches(msg, keyLeft):
			pm.cursorX = max(0, pm.cursorX-1)
			pm.logger.Info("cursorX", slog.Int("value", pm.cursorX))

			pm.ScrollToColumn(1)
			return pm, nil
		case key.Matches(msg, keyRight):
			pm.cursorX = min(len(pm.rows[0])-1, pm.cursorX+1)
			pm.logger.Info("cursorX", slog.Int("value", pm.cursorX))
			pm.ScrollToColumn(-1)
			return pm, nil
		}
	case map[dto.ProjectID]dto.Project:
		pm.projectsMutex.Lock()
		pm.projects = msg
		pm.projectsMutex.Unlock()
		return pm, pm.actorLoop(context.Background())
	}
	return pm, nil
}

func (pm *ProjectModel) View() string {
	var rows []table.Row

	if pm.terminalWidth > 20 {

		colsWidth := 0
		for index, col := range pm.table.Columns() {
			colsWidth += col.Width + pm.tableStyle.GetHorizontalFrameSize()

			if colsWidth > pm.terminalWidth {
				if !strings.HasSuffix(col.Title, " "+moreCharacter) {

					//titleSize := min(len(col.Title)-pm.terminalWidth-colsWidth-col.Width-2, len(col.Title))

					pm.table.Columns()[index].Title = col.Title + " " + moreCharacter
				}
				break
			}

			pm.table.Columns()[index].Title = strings.TrimSuffix(col.Title, " "+moreCharacter)

			//extra := colsWidth - pm.terminalWidth
			//
			//
			//if extra <= 0 {
			//	if col.Title[len(col.Title)-3:] == " "+moreCharacter {
			//		pm.table.Columns()[index].Title = strings.TrimSuffix(col.Title, " "+moreCharacter)
			//		break
			//	}
			//
			//	continue
			//}
			//
			//if col.Title[len(col.Title)-3:] == " "+moreCharacter {
			//	break
			//}
			//
			//if len(col.Title) <= extra {
			//	col.Title += " " + moreCharacter
			//	pm.table.Columns()[index] = col
			//
			//	break
			//}
			//
			//size := len(col.Title) - extra
			//// fill with space to reach the same width
			//if size < 0 {
			//	col.Title = col.Title[:1] + "x " + moreCharacter
			//} else {
			//	col.Title = col.Title[:1] + "y " + moreCharacter
			//}
			//
			//col.Width -= extra
			//pm.table.Columns()[index] = col
			//
			//break
		}
	}

	pm.projectsMutex.RLock()
	for _, project := range pm.projects {
		row := table.Row{
			project.Name,
			fmt.Sprintf("%.2f%%", project.CPUPercentage),
			fmt.Sprintf("%.2f%%", project.MemoryUsagePercentage),
			fmt.Sprintf("%d/%d", project.ContainersRunning, project.ContainersTotal),
			fmt.Sprintf("%.2f%%", project.CPUPercentage),
			fmt.Sprintf("%.2f%%", project.MemoryUsagePercentage),
			fmt.Sprintf("%d/%d", project.ContainersRunning, project.ContainersTotal),
		}
		rows = append(rows, row)
	}
	pm.projectsMutex.RUnlock()

	slices.SortStableFunc(rows, func(a, b table.Row) int {
		if a[1] < b[1] {
			return 1
		}

		if a[1] > b[1] {
			return -1
		}

		return strings.Compare(a[0], b[0])
	})

	pm.rows = rows

	var rowsScrolled []table.Row
	for _, row := range pm.rows {
		rowsScrolled = append(rowsScrolled, row[pm.cursorX:])
	}

	pm.table.SetRows(rowsScrolled)

	return pm.tableStyle.Render(pm.table.View()) + "\n"
}

func (pm *ProjectModel) ScrollToColumn(direction int) {
	columns := pm.columns[pm.cursorX:]
	var rows []table.Row
	for _, row := range pm.rows {
		rows = append(rows, row[pm.cursorX:])
	}

	if direction == 1 {
		pm.table.SetColumns(columns)
		pm.table.SetRows(rows)
		return
	}

	pm.table.SetRows(rows)
	pm.table.SetColumns(columns)
}
