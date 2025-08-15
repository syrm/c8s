package main

import (
	"context"
	"fmt"
	"log"
	_ "log"
	"os"

	"github/syrm/c8s/docker"

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
var Logger *log.Logger

func init() {
	tview.Borders.HorizontalFocus = tview.BoxDrawingsLightHorizontal
	tview.Borders.VerticalFocus = tview.BoxDrawingsLightVertical
	tview.Borders.TopLeftFocus = tview.BoxDrawingsLightDownAndRight
	tview.Borders.TopRightFocus = tview.BoxDrawingsLightDownAndLeft
	tview.Borders.BottomLeftFocus = tview.BoxDrawingsLightUpAndRight
	tview.Borders.BottomRightFocus = tview.BoxDrawingsLightUpAndLeft

	containers = make(map[string]container)

	docker.Init()
}

func renderViewComposer(ctx context.Context, cli *client.Client, app *tview.Application) *tview.Table {
	table := tview.NewTable().SetSelectable(true, false)
	containers := docker.Containers{ 
		Client: cli,
	}

	table.SetBorder(true).SetTitle("Docker Compose Containers (press 'q' to quit)")

	// Headers
	table.SetCell(0, 0, tview.NewTableCell("[::b]Project").SetAlign(tview.AlignCenter).SetExpansion(2).SetSelectable(false))
	table.SetCell(0, 1, tview.NewTableCell("[::b]CPU").SetAlign(tview.AlignRight).SetMaxWidth(7).SetSelectable(false))
	table.SetCell(0, 2, tview.NewTableCell("[::b]Memory").SetAlign(tview.AlignRight).SetMaxWidth(7).SetSelectable(false))
	table.SetCell(0, 3, tview.NewTableCell("[::b]Cont.").SetAlign(tview.AlignRight).SetSelectable(false))

	_, err := containers.GetProjects(ctx, app, table)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error retrieving docker projects: %v\n", err)
		_ = cli.Close() // Ensure client is closed before exiting
		os.Exit(1)
	}

	return table
}

func addInput(ctx context.Context, cli *client.Client, app *tview.Application, table *tview.Table) {
	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == '1' {
			table = renderViewComposer(ctx, cli, app)
			addInput(ctx, cli, app, table)
			app.SetRoot(table, true)
		}
		if event.Key() == tcell.KeyEnter {
			// row, _ := table.GetSelection()
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
	

		log.Printf("yolo")
		select {}
}
