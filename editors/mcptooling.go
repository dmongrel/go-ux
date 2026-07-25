package editors

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/wailsapp/wails/v3/pkg/application"

	"go-ux/db"
	"go-ux/fontsettings"
)

func init() {
	application.RegisterEvent[string]("editors:filechanged")
	application.RegisterEvent[fontsettings.FontSettings]("editors:font")
}

// TabInfo is the wire representation of a Tab — Tab/Document's fields are
// unexported (see document.go/tab.go — a *Document would otherwise
// marshal as "{}"), so every Service method returns this flattened,
// JSON-serializable snapshot instead.
type TabInfo struct {
	ID          string
	Title       string
	FilePath    string
	Text        string
	Dirty       bool
	PendingDiff *string
}

func tabInfo(t *Tab) TabInfo {
	return TabInfo{
		ID:          t.ID,
		Title:       t.Title,
		FilePath:    t.FilePath,
		Text:        t.Text(),
		Dirty:       t.Dirty(),
		PendingDiff: t.pendingDiff,
	}
}

// Service is the Wails-bound replacement for go-ux/editors.Group: an
// in-memory list of open Tabs (Document/Tab reused verbatim from
// document.go/tab.go), real file I/O (OpenFile/SaveTab, ported from the
// original mcptooling.go), file watching (watch.go), and per-instance
// Ctrl+scroll font sizing (go-ux/fontsettings) — the same feature set as
// the old Group, minus pane/split-tree management, which now lives
// entirely in the frontend (frontend/src/views/editor.ts, ported from
// terminal-poc and already generalized beyond the original Fyne
// split.go's algorithm) — there is no Go-side notion of a Pane anymore.
type Service struct {
	app     *application.App
	db      *db.DB
	groupID string
	fonts   *fontsettings.State

	fileWatchMode string
	watcher       *fsnotify.Watcher
	watchedFiles  map[string]bool

	mu     sync.Mutex
	tabs   []*Tab
	nextID int
}

// NewService builds an editors Service backed by database, scoped to
// groupID (matches go-ux/editors.NewGroupFromSettings' groupID scoping —
// font size and file-watch mode are independent per instance). Seeds two
// in-memory placeholder tabs, matching terminal-poc's original demo
// content, so the demo has something to show without requiring a real
// file to open first.
func NewService(app *application.App, database *db.DB, groupID string) *Service {
	s := &Service{
		app:           app,
		db:            database,
		groupID:       groupID,
		fonts:         fontsettings.NewState(fontsettings.DefaultFontSettings),
		fileWatchMode: FileWatchModeNotify,
		watchedFiles:  make(map[string]bool),
	}

	if err := ApplyEditorSettings(database, groupID, s); err != nil {
		log.Printf("editors: read settings: %v", err)
	}

	s.addTab(NewTab(s.newTabID(), "Chapter One", "", "# Chapter One\n\nIt was a dark and stormy night...\n"))
	s.addTab(NewTab(s.newTabID(), "Notes", "", "- idea one\n- idea two\n"))

	return s
}

func (s *Service) newTabID() string {
	s.nextID++
	return strconv.Itoa(s.nextID)
}

func (s *Service) addTab(t *Tab) {
	s.mu.Lock()
	s.tabs = append(s.tabs, t)
	s.mu.Unlock()
}

func (s *Service) findTabByID(id string) (*Tab, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tabs {
		if t.ID == id {
			return t, true
		}
	}
	return nil, false
}

func (s *Service) findTabByPath(path string) (*Tab, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tabs {
		if t.FilePath == path {
			return t, true
		}
	}
	return nil, false
}

// listInfo snapshots every open tab, in order.
func (s *Service) listInfo() []TabInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TabInfo, len(s.tabs))
	for i, t := range s.tabs {
		out[i] = tabInfo(t)
	}
	return out
}

// ListTabs returns every open tab and its current state.
func (s *Service) ListTabs() []TabInfo {
	return s.listInfo()
}

// NewTab creates a blank in-memory tab and returns the updated list.
func (s *Service) NewTab() []TabInfo {
	s.addTab(NewTab(s.newTabID(), "Untitled", "", ""))
	return s.listInfo()
}

// SaveTab updates tab id's text. If the tab has a FilePath (opened via
// OpenFile, or previously SaveTabAs), also writes it to disk and marks it
// clean — the plain Ctrl+S path. A tab with no FilePath (e.g. one of the
// demo placeholders, or a NewTab that was never Saved As) just updates its
// in-memory Document; there is nothing to write to and this is not an
// error, unlike the original Fyne Group.SaveTab (which required a
// FilePath) — this Service's own OpenWindow demo seeds memory-only tabs a
// user should still be able to "Save" without first choosing a path.
func (s *Service) SaveTab(id string, text string) ([]TabInfo, error) {
	tab, ok := s.findTabByID(id)
	if !ok {
		return nil, errUnknownTab
	}
	tab.Doc.SetText(text)
	if tab.FilePath != "" {
		if err := os.WriteFile(tab.FilePath, []byte(text), 0o644); err != nil {
			return nil, err
		}
		tab.Doc.MarkClean()
	}
	return s.listInfo(), nil
}

// OpenFile is the host app's "sidebar click-to-open" entry point: if path
// is already open (matched by FilePath), returns the existing tab list
// unchanged; otherwise reads path from disk, opens it as a new tab, and
// starts watching it (watch.go).
func (s *Service) OpenFile(path string) ([]TabInfo, error) {
	if _, ok := s.findTabByPath(path); ok {
		return s.listInfo(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	tab := NewTab(s.newTabID(), filepath.Base(path), path, string(data))
	s.addTab(tab)
	s.startWatching(tab)
	return s.listInfo(), nil
}

// SaveTabAs writes tab id's current text to path, adopts path as its
// FilePath/Title, marks it clean, and starts watching path.
func (s *Service) SaveTabAs(id string, path string) ([]TabInfo, error) {
	if path == "" {
		return nil, errors.New("editors: SaveTabAs called with an empty path")
	}
	tab, ok := s.findTabByID(id)
	if !ok {
		return nil, errUnknownTab
	}
	if err := os.WriteFile(path, []byte(tab.Doc.Text()), 0o644); err != nil {
		return nil, err
	}
	tab.FilePath = path
	tab.Title = filepath.Base(path)
	tab.Doc.MarkClean()
	s.startWatching(tab)
	return s.listInfo(), nil
}

// ProposeDiff sets id's pending diff to newText — mirrors the original
// Group.ProposeDiff, minus the pane-activation half (the frontend's own
// refreshPanesShowingTab, editor.ts, handles making the diff visible
// wherever the tab is shown; there is no Go-side Pane to activate). The
// frontend switches that tab into a @codemirror/merge unifiedMergeView
// comparing Text (old) against PendingDiff (new) — this package computes
// and renders no diff itself.
func (s *Service) ProposeDiff(id string, newText string) ([]TabInfo, error) {
	tab, ok := s.findTabByID(id)
	if !ok {
		return nil, errUnknownTab
	}
	tab.pendingDiff = &newText
	return s.listInfo(), nil
}

// AcceptDiff commits finalText as tab id's real content — writing it to
// disk if the tab has a FilePath — and clears the pending diff. finalText
// comes from the frontend's merge-view editor state at the moment Accept
// is clicked, not blindly the originally proposed text, so any per-chunk
// accept/reject the user did inside CodeMirror's own merge-view controls
// is respected (mirrors the original Group.acceptDiff).
func (s *Service) AcceptDiff(id string, finalText string) ([]TabInfo, error) {
	tab, ok := s.findTabByID(id)
	if !ok {
		return nil, errUnknownTab
	}
	tab.Doc.SetText(finalText)
	if tab.FilePath != "" {
		if err := os.WriteFile(tab.FilePath, []byte(finalText), 0o644); err != nil {
			log.Printf("editors: write %s: %v", tab.FilePath, err)
		} else {
			tab.Doc.MarkClean()
		}
	}
	tab.pendingDiff = nil
	return s.listInfo(), nil
}

// CancelDiff discards id's pending diff, leaving its text untouched —
// mirrors the original Group.cancelDiff.
func (s *Service) CancelDiff(id string) ([]TabInfo, error) {
	tab, ok := s.findTabByID(id)
	if !ok {
		return nil, errUnknownTab
	}
	tab.pendingDiff = nil
	return s.listInfo(), nil
}

// CloseTab removes tab id from the open list (its file, if any, stays
// watched — matches the original Group's "a watch is never explicitly
// removed" simplification, see watch.go).
func (s *Service) CloseTab(id string) []TabInfo {
	s.mu.Lock()
	for i, t := range s.tabs {
		if t.ID == id {
			s.tabs = append(s.tabs[:i], s.tabs[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	return s.listInfo()
}

// CurrentFontSettings returns this instance's live font configuration.
func (s *Service) CurrentFontSettings() fontsettings.FontSettings {
	return s.fonts.Current()
}

// SetFontSettings replaces this instance's live font configuration
// (clamped) and persists it to the db's Editors node — the Wails
// equivalent of the original Fyne editorEntry's Ctrl+scroll handler
// (font.go, deleted) writing through fontsettings.State.Set.
func (s *Service) SetFontSettings(f fontsettings.FontSettings) error {
	f = fontsettings.ClampFontSettings(f)
	s.fonts.Set(f)
	if s.app != nil {
		s.app.Event.Emit("editors:font", f)
	}

	nodes, err := s.db.ListSettings()
	if err != nil {
		return err
	}
	node, ok := findRootNode(nodes, editorsSettingsLabel(s.groupID))
	if !ok {
		return nil // RegisterSettings was never called; nothing to persist against
	}
	return s.db.SaveProperties(node.ID, map[string]string{
		fontsettings.KeyFontFamily:  f.Family,
		fontsettings.KeyFontSize:    strconv.Itoa(f.Size),
		fontsettings.KeyLineHeight:  strconv.FormatFloat(f.LineHeight, 'f', -1, 64),
		fontsettings.KeyColumnWidth: strconv.FormatFloat(f.ColumnWidth, 'f', -1, 64),
	})
}

// ReloadTab re-reads tab id's FilePath from disk into its Document,
// discarding any unsaved in-memory edits — the "Load from Disk" half of
// the file-changed notification flow (see watch.go's handleFileChanged
// and the "editors:filechanged" event), the frontend equivalent of the
// old south bar's SouthBarFileChanged mode.
func (s *Service) ReloadTab(id string) ([]TabInfo, error) {
	tab, ok := s.findTabByID(id)
	if !ok {
		return nil, errUnknownTab
	}
	if tab.FilePath == "" {
		return nil, errors.New("editors: tab has no FilePath to reload from")
	}
	data, err := os.ReadFile(tab.FilePath)
	if err != nil {
		return nil, err
	}
	tab.Doc.SetText(string(data))
	tab.Doc.MarkClean()
	return s.listInfo(), nil
}

// LayoutTab identifies one tab within a persisted LayoutNode leaf: TabID
// is preferred on restore (works whenever this Service's tab list still
// has it — i.e. reopening the editor window within the same running app,
// memory-only tabs included, since Tab IDs are only ever stable for the
// process's lifetime); FilePath is the fallback for a tab whose ID is gone
// (e.g. after a real app restart, when the Service's in-memory tab list —
// and every ID in it — has reset) — a memory-only tab has no such
// fallback and is simply dropped from the restored layout in that case.
type LayoutTab struct {
	TabID    string
	FilePath string
}

// LayoutNode is the persisted shape of the frontend's split-pane tree — a
// recursive value type mirroring editor.ts's own TreeNode, stored as one
// JSON blob via db.SaveUIState/LoadUIState (not a relational schema — the
// original Fyne version's Go-owned Pane tree that would have mapped onto
// one no longer exists). A leaf (Axis == "") lists every tab open in that
// pane, in tab order, plus which one is active; a split node's A/B are its
// two children.
type LayoutNode struct {
	Axis        string // "row"/"column"; "" means this is a leaf pane
	SplitOffset float64
	A           *LayoutNode
	B           *LayoutNode
	Tabs        []LayoutTab
	ActiveTabID string
}

// SaveLayout persists the frontend's current split-pane tree, live — the
// frontend calls this after every structural change (split/move/close) and
// tab selection, same "write the whole current state on every change"
// philosophy as go-ux/treestate.
func (s *Service) SaveLayout(root LayoutNode) error {
	blob, err := json.Marshal(root)
	if err != nil {
		return err
	}
	return s.db.SaveUIState(s.groupID+".layout", blob)
}

// LoadLayout returns the previously persisted split-pane tree, or nil if
// nothing has been saved yet for this groupID.
func (s *Service) LoadLayout() (*LayoutNode, error) {
	blob, err := s.db.LoadUIState(s.groupID + ".layout")
	if err != nil {
		return nil, err
	}
	if blob == nil {
		return nil, nil
	}
	var root LayoutNode
	if err := json.Unmarshal(blob, &root); err != nil {
		return nil, err
	}
	return &root, nil
}

// OpenFileDialog shows a native "Open File" picker and, if the user picks
// a file (rather than cancelling), opens it via OpenFile — the Wails
// equivalent of the "host app's own file picker" OpenFile's doc comment
// says this package deliberately leaves to its caller.
func (s *Service) OpenFileDialog() ([]TabInfo, error) {
	path, err := s.app.Dialog.OpenFile().SetTitle("Open File").PromptForSingleSelection()
	if err != nil {
		return nil, err
	}
	if path == "" {
		return s.listInfo(), nil // user cancelled
	}
	return s.OpenFile(path)
}

// SaveTabAsDialog shows a native "Save As" picker and, if the user picks a
// destination, calls SaveTabAs with it — the Wails equivalent of the
// original Group.OnSaveAsRequested hook (this package has no file picker
// of its own; the host previously supplied one, this Service now shows
// Wails' native one directly).
func (s *Service) SaveTabAsDialog(id string) ([]TabInfo, error) {
	path, err := s.app.Dialog.SaveFile().SetMessage("Save File As").PromptForSingleSelection()
	if err != nil {
		return nil, err
	}
	if path == "" {
		return s.listInfo(), nil // user cancelled
	}
	return s.SaveTabAs(id, path)
}

// OpenWindow opens the editor UI in its own window.
func (s *Service) OpenWindow() {
	s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Editor",
		Width:            900,
		Height:           650,
		BackgroundColour: application.NewRGB(30, 30, 30),
		URL:              "/#editor",
	})
}

var errUnknownTab = errors.New("editors: unknown tab id")
