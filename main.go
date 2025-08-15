package main

import (
	"context"
	"fmt"
	"log"
	_ "log"
	"os"

	"github/syrm/c7r/docker"

	"github.com/docker/docker/client"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type view int

const (
	viewComposer view = iota
	viewContainer
)

var currentView = viewComposer

type project struct {
	name     string
	cpuStats int
}

type container struct {
	id            string
	name          string
	cpuPercentage int
}

var containers map[string]container

func init() {
	tview.Borders.HorizontalFocus = tview.BoxDrawingsLightHorizontal
	tview.Borders.VerticalFocus = tview.BoxDrawingsLightVertical
	tview.Borders.TopLeftFocus = tview.BoxDrawingsLightDownAndRight
	tview.Borders.TopRightFocus = tview.BoxDrawingsLightDownAndLeft
	tview.Borders.BottomLeftFocus = tview.BoxDrawingsLightUpAndRight
	tview.Borders.BottomRightFocus = tview.BoxDrawingsLightUpAndLeft

	containers = make(map[string]container)
}

func renderViewComposer(ctx context.Context, cli *client.Client, app *tview.Application) *tview.Table {
	table := tview.NewTable().SetSelectable(true, false)
	containers := docker.Containers{
		Client: cli,
	}

	_, err := containers.GetProjects(ctx, app, table)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error retrieving docker projects: %v\n", err)
		_ = cli.Close() // Ensure client is closed before exiting
		os.Exit(1)
	}

	table.SetBorder(true).SetTitle("Docker Compose Containers (press 'q' to quit)")

	// Headers
	headers := []string{"Project", "CPU"}
	for i, h := range headers {
		table.SetCell(0, i, tview.NewTableCell(fmt.Sprintf("[::b]%s", h)).SetAlign(tview.AlignCenter))
	}

/*
	i := 0
	for _, project := range projects {
		table.SetCell(i+1, 0, tview.NewTableCell(project.Name))
		table.SetCell(i+1, 1, tview.NewTableCell(fmt.Sprintf("%.2f", project.CPUPercentage)))
		i++
	}
*/
	return table
}

func addInput(ctx context.Context, cli *client.Client, app *tview.Application, table *tview.Table) {
	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == '1' {
			table = renderViewComposer(ctx, cli, app)
			addInput(ctx, cli, app, table)
			app.SetRoot(table, true)
		}
		if event.Rune() == '2' {
			// table = renderViewContainers(ctx, cli)
			// addInput(ctx, cli, app, table)
			// app.SetRoot(table, true)
		}
		if event.Rune() == 'q' {
			app.Stop()
			return nil
		}
		return event
	})
}

func main() {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating Docker client: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	app := tview.NewApplication()

	table := renderViewComposer(ctx, cli, app)
	addInput(ctx, cli, app, table)

	if err := app.SetRoot(table, true).EnableMouse(true).Run(); err != nil {
		log.Fatalf("Error running TUI: %v", err)
	}

	// Ensure client is closed after application exits
	if err := cli.Close(); err != nil {
		log.Printf("Error closing Docker client: %v", err)
	}
}
