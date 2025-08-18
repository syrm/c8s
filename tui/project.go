package tui

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/rivo/tview"

	"github/syrm/c8s/dto"
)

var _ tview.TableContent = (*ProjectTableContent)(nil)

const column = 4

// Define requests for reading table data
type TableCellRequest struct {
	Row, Column int
	RespCh      chan *tview.TableCell
}

type RowCountRequest struct {
	RespCh chan int
}

// Define messages for the actor
type ProjectRequest struct {
	// Only one field is set at a time
	Project     *dto.Project
	GetCell     *TableCellRequest
	GetRowCount *RowCountRequest
	// TODO: Add other requests (InsertRow, RemoveRow, etc)
}

type ProjectTableContent struct {
	projects      []dto.Project
	mapping       map[dto.ProjectID]int
	updateCh      chan dto.Project    // Channel for updating project
	requestCh     chan ProjectRequest // Channel for actor messages
	refreshViewCh chan<- struct{}     // Channel to trigger view refresh
	logger        *slog.Logger
}

func NewProjectTableContent(ctx context.Context, refreshViewCh chan<- struct{}, logger *slog.Logger) *ProjectTableContent {
	p := &ProjectTableContent{
		projects:      make([]dto.Project, 0),
		mapping:       make(map[dto.ProjectID]int),
		updateCh:      make(chan dto.Project),
		requestCh:     make(chan ProjectRequest),
		refreshViewCh: refreshViewCh,
		logger:        logger,
	}

	go p.actorLoop(ctx)

	return p
}

func (p *ProjectTableContent) GetUpdateCh() chan<- dto.Project {
	return p.updateCh
}

func (p *ProjectTableContent) GetCell(row, column int) *tview.TableCell {
	req := &TableCellRequest{
		Row:    row,
		Column: column,
		RespCh: make(chan *tview.TableCell),
	}
	p.requestCh <- ProjectRequest{GetCell: req}
	return <-req.RespCh
}

func (p *ProjectTableContent) doGetCell(row, column int) *tview.TableCell {
	if row < 0 || column < 0 {
		return nil
	}

	if row == 0 {
		// Return header cells
		switch column {
		case 0:
			return tview.NewTableCell("[::b]Project").SetAlign(tview.AlignCenter).SetExpansion(2).SetSelectable(false)
		case 1:
			return tview.NewTableCell("[::b]CPU").SetAlign(tview.AlignRight).SetMaxWidth(7).SetSelectable(false)
		case 2:
			return tview.NewTableCell("[::b]Memory").SetAlign(tview.AlignRight).SetMaxWidth(7).SetSelectable(false)
		case 3:
			return tview.NewTableCell("[::b]Cont.").SetAlign(tview.AlignRight).SetSelectable(false)
		}
	}

	switch column {
	case 0:
		return tview.NewTableCell(p.projects[row-1].Name)
	case 1:
		return tview.NewTableCell(fmt.Sprintf("%.2f%%", p.projects[row-1].CPUPercentage)).SetAlign(tview.AlignRight)
	case 2:
		return tview.NewTableCell(fmt.Sprintf("%.2f%%", p.projects[row-1].MemoryUsagePercentage)).SetAlign(tview.AlignRight)
	case 3:
		return tview.NewTableCell(fmt.Sprintf("%d/%d", p.projects[row-1].ContainersRunning, p.projects[row-1].ContainersTotal)).SetAlign(tview.AlignRight)
	}

	return nil
}

func (p *ProjectTableContent) GetRowCount() int {
	req := &RowCountRequest{RespCh: make(chan int)}
	p.requestCh <- ProjectRequest{GetRowCount: req}
	a := <-req.RespCh
	//p.logger.DebugContext(context.Background(), "GetRowCount called", slog.Int("row_count", a))
	return a
}

func (p *ProjectTableContent) GetColumnCount() int {
	return column
}

func (p *ProjectTableContent) SetCell(row, column int, cell *tview.TableCell) {
	//TODO implement me
	panic("implement me")
}

func (p *ProjectTableContent) RemoveRow(row int) {
	//TODO implement me
	panic("implement me")
}

func (p *ProjectTableContent) RemoveColumn(column int) {
	//TODO implement me
	panic("implement me")
}

func (p *ProjectTableContent) InsertRow(row int) {
	//TODO implement me
	panic("implement me")
}

func (p *ProjectTableContent) InsertColumn(column int) {
	//TODO implement me
	panic("implement me")
}

func (p *ProjectTableContent) Clear() {
	//TODO implement me
	panic("implement me")
}

func (p *ProjectTableContent) doUpdateProject(ctx context.Context, project dto.Project) {
	//p.logger.DebugContext(ctx, "received project message", slog.String("project_id", string(project.ID)), slog.String("project_name", project.Name))

	rowIndex, ok := p.mapping[project.ID]

	if !ok {
		rowIndex = len(p.projects)
		p.mapping[project.ID] = rowIndex
		p.projects = append(p.projects, project)
		return
	}

	p.projects[rowIndex] = project
	p.refreshViewCh <- struct{}{}
}

func (p *ProjectTableContent) actorLoop(ctx context.Context) {
	go func() {
		for {
			select {
			case project := <-p.updateCh:
				//p.logger.DebugContext(ctx, "received project update", slog.String("project_id", string(project.ID)), slog.String("project_name", project.Name))
				p.doUpdateProject(ctx, project)
			}
		}
	}()

	for {
		select {
		case msg := <-p.requestCh:
			switch {
			case msg.GetCell != nil:
				msg.GetCell.RespCh <- p.doGetCell(msg.GetCell.Row, msg.GetCell.Column)
			case msg.GetRowCount != nil:
				msg.GetRowCount.RespCh <- len(p.projects) + 1
			}
		case <-ctx.Done():
			p.logger.DebugContext(ctx, "context is done")
			return
		}
	}
}
