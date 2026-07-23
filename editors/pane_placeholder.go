package editors

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// Pane is a placeholder for split.go's tests and rebuild() — a real
// task-owner (this package's "leaf widgets" work) replaces this file
// entirely with the real tab-bar+content+south-bar Pane implementation.
// This placeholder only needs to (a) be comparable (so split.go's
// pointer-identity comparisons work — every real *Pane is already a
// distinct pointer, so no special Equal method is needed) and (b) satisfy
// fyne.CanvasObject so rebuild() type-checks.
type Pane struct {
	widget.BaseWidget
	Name string // test-only label, not part of the real design
}

func newPlaceholderPane(name string) *Pane {
	p := &Pane{Name: name}
	p.ExtendBaseWidget(p)
	return p
}

func (p *Pane) CreateRenderer() fyne.WidgetRenderer {
	label := widget.NewLabel(p.Name)
	return widget.NewSimpleRenderer(label)
}
