package editors

import (
	"testing"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestIsMarkdownFile(t *testing.T) {
	cases := map[string]bool{
		"chapter1.md":    true,
		"chapter1.MD":    true,
		"notes.markdown": true,
		"chapter1.txt":   false,
		"README":         false,
		"":               false,
	}
	for path, want := range cases {
		if got := isMarkdownFile(path); got != want {
			t.Errorf("isMarkdownFile(%q) = %v, want %v", path, got, want)
		}
	}
}

func asVBox(t *testing.T, obj fyne.CanvasObject) *fyne.Container {
	t.Helper()
	c, ok := obj.(*fyne.Container)
	if !ok {
		t.Fatalf("renderMarkdown returned %T, want *fyne.Container", obj)
	}
	return c
}

func TestRenderMarkdownSingleParagraphIsOneRichTextBlock(t *testing.T) {
	fynetest.NewApp()

	root := asVBox(t, renderMarkdown([]byte("hello world")))
	if len(root.Objects) != 1 {
		t.Fatalf("got %d top-level blocks, want 1", len(root.Objects))
	}
	rt, ok := root.Objects[0].(*widget.RichText)
	if !ok {
		t.Fatalf("block is %T, want *widget.RichText", root.Objects[0])
	}
	if got := richTextPlainString(rt); got != "hello world" {
		t.Errorf("text = %q, want %q", got, "hello world")
	}
}

func TestRenderMarkdownTwoParagraphsAreTwoBlocks(t *testing.T) {
	fynetest.NewApp()

	root := asVBox(t, renderMarkdown([]byte("first paragraph\n\nsecond paragraph")))
	if len(root.Objects) != 2 {
		t.Fatalf("got %d top-level blocks, want 2", len(root.Objects))
	}
}

func TestRenderMarkdownHeadingIsBold(t *testing.T) {
	fynetest.NewApp()

	root := asVBox(t, renderMarkdown([]byte("# Chapter One")))
	rt, ok := root.Objects[0].(*widget.RichText)
	if !ok {
		t.Fatalf("block is %T, want *widget.RichText", root.Objects[0])
	}
	seg, ok := rt.Segments[0].(*widget.TextSegment)
	if !ok {
		t.Fatalf("segment is %T, want *widget.TextSegment", rt.Segments[0])
	}
	if !seg.Style.TextStyle.Bold {
		t.Errorf("heading segment is not bold")
	}
	if seg.Text != "Chapter One" {
		t.Errorf("heading text = %q, want %q", seg.Text, "Chapter One")
	}
}

func TestRenderMarkdownStrongAndEmphasis(t *testing.T) {
	fynetest.NewApp()

	root := asVBox(t, renderMarkdown([]byte("plain **bold** and *italic* text")))
	rt := root.Objects[0].(*widget.RichText)

	var sawBold, sawItalic bool
	for _, s := range rt.Segments {
		ts, ok := s.(*widget.TextSegment)
		if !ok {
			continue
		}
		if ts.Style.TextStyle.Bold && ts.Text == "bold" {
			sawBold = true
		}
		if ts.Style.TextStyle.Italic && ts.Text == "italic" {
			sawItalic = true
		}
	}
	if !sawBold {
		t.Errorf("no bold segment with text %q found", "bold")
	}
	if !sawItalic {
		t.Errorf("no italic segment with text %q found", "italic")
	}
}

func TestRenderMarkdownCodeSpanIsMonospace(t *testing.T) {
	fynetest.NewApp()

	root := asVBox(t, renderMarkdown([]byte("run `go test` now")))
	rt := root.Objects[0].(*widget.RichText)

	found := false
	for _, s := range rt.Segments {
		ts, ok := s.(*widget.TextSegment)
		if !ok {
			continue
		}
		if ts.Text == "go test" && ts.Style.TextStyle.Monospace {
			found = true
		}
	}
	if !found {
		t.Errorf("no monospace code-span segment with text %q found", "go test")
	}
}

func TestRenderMarkdownFencedCodeBlockIsMonospaceLabel(t *testing.T) {
	fynetest.NewApp()

	src := "```\nfmt.Println(\"hi\")\n```"
	root := asVBox(t, renderMarkdown([]byte(src)))
	label, ok := root.Objects[0].(*widget.Label)
	if !ok {
		t.Fatalf("block is %T, want *widget.Label", root.Objects[0])
	}
	if label.TextStyle.Monospace != true {
		t.Errorf("code block label is not monospace")
	}
	if label.Text != `fmt.Println("hi")` {
		t.Errorf("code block text = %q, want %q", label.Text, `fmt.Println("hi")`)
	}
}

func TestRenderMarkdownThematicBreakIsSeparator(t *testing.T) {
	fynetest.NewApp()

	root := asVBox(t, renderMarkdown([]byte("above\n\n---\n\nbelow")))
	if len(root.Objects) != 3 {
		t.Fatalf("got %d blocks, want 3 (paragraph, separator, paragraph)", len(root.Objects))
	}
	if _, ok := root.Objects[1].(*widget.Separator); !ok {
		t.Errorf("middle block is %T, want *widget.Separator", root.Objects[1])
	}
}

func TestRenderMarkdownUnorderedList(t *testing.T) {
	fynetest.NewApp()

	root := asVBox(t, renderMarkdown([]byte("- one\n- two\n- three")))
	list, ok := root.Objects[0].(*fyne.Container)
	if !ok {
		t.Fatalf("block is %T, want *fyne.Container (list)", root.Objects[0])
	}
	if len(list.Objects) != 3 {
		t.Fatalf("got %d list items, want 3", len(list.Objects))
	}
}

func TestRenderMarkdownOrderedListNumbersInSequence(t *testing.T) {
	fynetest.NewApp()

	root := asVBox(t, renderMarkdown([]byte("1. one\n2. two\n3. three")))
	list := root.Objects[0].(*fyne.Container)

	for i, obj := range list.Objects {
		row, ok := obj.(*fyne.Container)
		if !ok {
			t.Fatalf("list item %d is %T, want *fyne.Container", i, obj)
		}
		marker, ok := row.Objects[1].(*widget.Label) // Border: [body, marker] per container.NewBorder's Objects ordering
		if !ok {
			continue
		}
		want := []string{"1.", "2.", "3."}[i]
		if marker.Text != want {
			t.Errorf("item %d marker = %q, want %q", i, marker.Text, want)
		}
	}
}

func TestRenderMarkdownLinkIsHyperlink(t *testing.T) {
	fynetest.NewApp()

	root := asVBox(t, renderMarkdown([]byte("see [the docs](https://example.com/docs) for more")))
	rt := root.Objects[0].(*widget.RichText)

	found := false
	for _, s := range rt.Segments {
		hl, ok := s.(*widget.HyperlinkSegment)
		if !ok {
			continue
		}
		if hl.Text == "the docs" && hl.URL != nil && hl.URL.String() == "https://example.com/docs" {
			found = true
		}
	}
	if !found {
		t.Errorf("no hyperlink segment for %q -> %q found", "the docs", "https://example.com/docs")
	}
}

func TestRenderMarkdownBlockquoteNestsInner(t *testing.T) {
	fynetest.NewApp()

	root := asVBox(t, renderMarkdown([]byte("> quoted text")))
	border, ok := root.Objects[0].(*fyne.Container)
	if !ok {
		t.Fatalf("block is %T, want *fyne.Container (blockquote)", root.Objects[0])
	}
	found := false
	for _, obj := range border.Objects {
		if inner, ok := obj.(*fyne.Container); ok {
			for _, io := range inner.Objects {
				if rt, ok := io.(*widget.RichText); ok && richTextPlainString(rt) == "quoted text" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("blockquote inner content %q not found", "quoted text")
	}
}

// richTextPlainString concatenates every TextSegment's Text in rt — a
// test helper since widget.RichText has no single "give me the plain
// text" accessor.
func richTextPlainString(rt *widget.RichText) string {
	var out string
	for _, s := range rt.Segments {
		if ts, ok := s.(*widget.TextSegment); ok {
			out += ts.Text
		}
	}
	return out
}
