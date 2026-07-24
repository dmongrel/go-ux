package editors

import (
	"net/url"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// markdownParser is shared by every renderMarkdown call. extension.Table
// is the only goldmark extension enabled — GFM's other pieces
// (strikethrough, autolinks, task lists) aren't part of this package's
// supported subset yet, and enabling Table alone keeps the AST switch in
// renderBlock/renderInline from having to handle extension node kinds
// this package doesn't render.
var markdownParser = goldmark.New(goldmark.WithExtensions(extension.Table)).Parser()

// isMarkdownFile reports whether path looks like a Markdown file by
// extension — the signal the tab bar's preview toggle (tabbar.go) and
// pane.go's content switch use to decide whether markdown preview even
// applies to the active tab.
func isMarkdownFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown")
}

// renderMarkdown parses src as Markdown (via goldmark) and builds a
// read-only, block-level fyne.CanvasObject tree approximating its
// rendered form. This walks goldmark's AST directly rather than using
// widget.NewRichTextFromMarkdown — RichText's own built-in Markdown
// support covers too small a subset (no nested blockquotes/lists, no
// tables) for a prose-editing tool to avoid hitting believable
// limitations on real chapter text. Tables (GFM's pipe-table syntax) are
// supported via goldmark's extension.Table, rendered as a fixed-column
// grid (see renderTable) — the only goldmark extension this package
// enables.
//
// This is a snapshot render, not a live view: it does not update itself
// if the underlying Document changes elsewhere (e.g. edited in another
// split pane showing the same Document) while a pane is toggled into
// preview mode — the caller (pane.go's togglePreview) re-renders from
// scratch each time the toggle fires, but there's no live-refresh
// subscription while already in preview. Acceptable for now; revisit if
// that turns out to matter in practice.
func renderMarkdown(src []byte) fyne.CanvasObject {
	root := markdownParser.Parse(text.NewReader(src))
	return container.NewVBox(renderBlockChildren(root, src)...)
}

func renderBlockChildren(parent ast.Node, src []byte) []fyne.CanvasObject {
	var out []fyne.CanvasObject
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		out = append(out, renderBlock(n, src))
	}
	return out
}

func renderBlock(n ast.Node, src []byte) fyne.CanvasObject {
	switch n.Kind() {
	case ast.KindHeading:
		return renderHeading(n.(*ast.Heading), src)
	case ast.KindThematicBreak:
		return widget.NewSeparator()
	case ast.KindBlockquote:
		inner := container.NewVBox(renderBlockChildren(n, src)...)
		return container.NewBorder(nil, nil, widget.NewSeparator(), nil, inner)
	case ast.KindCodeBlock, ast.KindFencedCodeBlock:
		return renderCodeBlock(n, src)
	case ast.KindList:
		return renderList(n.(*ast.List), src)
	case extast.KindTable:
		return renderTable(n.(*extast.Table), src)
	case ast.KindParagraph, ast.KindTextBlock:
		return richTextFromInlines(n, src, widget.RichTextStyleInline)
	default:
		// Any unhandled block kind (HTMLBlock, LinkReferenceDefinition,
		// etc.) degrades to its raw inline text rather than disappearing
		// silently.
		return richTextFromInlines(n, src, widget.RichTextStyleInline)
	}
}

func renderHeading(h *ast.Heading, src []byte) fyne.CanvasObject {
	style := widget.RichTextStyleHeading
	switch h.Level {
	case 1:
		style = widget.RichTextStyleHeading
	case 2:
		style = widget.RichTextStyleSubHeading
	default:
		style = widget.RichTextStyleInline
		style.TextStyle.Bold = true
	}
	return richTextFromInlines(h, src, style)
}

func renderCodeBlock(n ast.Node, src []byte) fyne.CanvasObject {
	var raw string
	if lines := n.Lines(); lines != nil {
		raw = string(lines.Value(src))
	}
	label := widget.NewLabel(strings.TrimRight(raw, "\n"))
	label.TextStyle = fyne.TextStyle{Monospace: true}
	return label
}

func renderList(list *ast.List, src []byte) fyne.CanvasObject {
	var items []fyne.CanvasObject
	n := list.Start
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		marker := "•"
		if list.IsOrdered() {
			marker = strconv.Itoa(n) + "."
			n++
		}
		body := container.NewVBox(renderBlockChildren(item, src)...)
		items = append(items, container.NewBorder(nil, nil, widget.NewLabel(marker), nil, body))
	}
	return container.NewVBox(items...)
}

// renderTable renders t as a fixed-column grid: one widget.RichText cell
// per column, the header row (t's first child, a *extast.TableHeader) in
// bold, every following *extast.TableRow plain. Column count comes from
// the header row's cell count, since goldmark's table extension requires
// every row to have exactly that many cells (padding/truncating short or
// long rows itself during parsing).
func renderTable(t *extast.Table, src []byte) fyne.CanvasObject {
	header, _ := t.FirstChild().(*extast.TableHeader)
	numCols := 0
	if header != nil {
		for c := header.FirstChild(); c != nil; c = c.NextSibling() {
			numCols++
		}
	}
	if numCols == 0 {
		numCols = 1
	}

	var cells []fyne.CanvasObject
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		style := widget.RichTextStyleInline
		if row.Kind() == extast.KindTableHeader {
			style.TextStyle.Bold = true
		}
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			cells = append(cells, richTextFromInlines(cell, src, style))
		}
	}
	return container.NewGridWithColumns(numCols, cells...)
}

// richTextFromInlines walks n's inline children and returns a single
// widget.RichText, using base as every resulting segment's starting
// style (further adjusted per-node, e.g. bold for **strong**).
func richTextFromInlines(n ast.Node, src []byte, base widget.RichTextStyle) *widget.RichText {
	segs := renderInlineChildren(n, src, base)
	if len(segs) == 0 {
		segs = []widget.RichTextSegment{&widget.TextSegment{Style: base}}
	}
	return widget.NewRichText(segs...)
}

func renderInlineChildren(n ast.Node, src []byte, style widget.RichTextStyle) []widget.RichTextSegment {
	var segs []widget.RichTextSegment
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		segs = append(segs, renderInline(c, src, style)...)
	}
	return segs
}

func renderInline(n ast.Node, src []byte, style widget.RichTextStyle) []widget.RichTextSegment {
	switch v := n.(type) {
	case *ast.Text:
		text := string(v.Value(src))
		if v.SoftLineBreak() || v.HardLineBreak() {
			text += " "
		}
		return []widget.RichTextSegment{&widget.TextSegment{Text: text, Style: style}}
	case *ast.Emphasis:
		emphasized := style
		if v.Level >= 2 {
			emphasized.TextStyle.Bold = true
		} else {
			emphasized.TextStyle.Italic = true
		}
		return renderInlineChildren(n, src, emphasized)
	case *ast.CodeSpan:
		coded := style
		coded.TextStyle.Monospace = true
		return []widget.RichTextSegment{&widget.TextSegment{Text: inlineText(n, src), Style: coded}}
	case *ast.Link:
		u, _ := url.Parse(string(v.Destination))
		return []widget.RichTextSegment{&widget.HyperlinkSegment{Text: inlineText(n, src), URL: u}}
	case *ast.AutoLink:
		dest := string(v.URL(src))
		u, _ := url.Parse(dest)
		return []widget.RichTextSegment{&widget.HyperlinkSegment{Text: dest, URL: u}}
	default:
		return renderInlineChildren(n, src, style)
	}
}

// inlineText concatenates every Text descendant of n — used for CodeSpan
// and Link, whose own markdown semantics don't nest further inline
// formatting inside their label the way emphasis can.
func inlineText(n ast.Node, src []byte) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			b.Write(t.Value(src))
		} else {
			b.WriteString(inlineText(c, src))
		}
	}
	return b.String()
}
