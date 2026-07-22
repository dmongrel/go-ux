package terminal

import (
	"log"
	"sync"

	"fyne.io/fyne/v2/container"
)

// TabView is a multi-tab container of terminal Sessions, one shell process
// per tab. It wraps Fyne's container.DocTabs rather than container.AppTabs —
// confirmed against the vendored Fyne v2.8.0 source (see doctabs.go): AppTabs
// has no per-tab close support in this version, and DocTabs is the only one
// that provides both a built-in "+" button (CreateTab) and a per-tab close
// button (OnClosed/CloseIntercept) out of the box, so both are used directly
// here instead of hand-rolling either.
//
// AddTab and CloseTab are expected to be called on the Fyne UI goroutine,
// same as every other exported method on go-ux's settings.Window and
// dialog.Dialog — they touch DocTabs directly (a CanvasObject) with no
// fyne.Do wrapping.
type TabView struct {
	tabs *container.DocTabs

	// defaultShell is what DocTabs' built-in "+" button spawns: the first
	// entry in the shells slice NewTabView was given. Empty (zero value) if
	// no shells were configured, in which case the "+" button is a no-op
	// (createTab returns nil, which DocTabs treats as "nothing to add" —
	// confirmed in doctabs.go's buildCreateTabsButton).
	defaultShell ShellDef
	hasDefault   bool

	// closeOnExit mirrors the close_on_exit settings property (see
	// settings_schema.go): when true, every tab this TabView creates
	// auto-closes as soon as its shell process exits, instead of leaving a
	// dead tab sitting open. Set at construction time via newTabView so it
	// applies uniformly to initial tabs and to ones later created through
	// the "+" button (createTab) or AddTab — false (off) for plain
	// NewTabView callers, who get NewWindow's pre-Task-4 behavior unchanged.
	closeOnExit bool

	mu     sync.Mutex // guards byItem, which callbacks below can touch from Fyne's own dispatch
	byItem map[*container.TabItem]*Session
}

// NewTabView builds a tab container with one initial tab per shell in
// shells. shells may be empty; the resulting TabView is still usable (a
// caller can still add tabs via AddTab with an explicit ShellDef), just with
// its "+" button doing nothing until AddTab is called at least once.
func NewTabView(shells []ShellDef) *TabView {
	return newTabView(shells, false)
}

// newTabView is NewTabView plus the closeOnExit flag — unexported because
// only this package's own db-aware window constructor (NewWindowFromSettings,
// see window.go) needs to set it; NewTabView's public signature stays
// exactly as Task 3 produced it.
func newTabView(shells []ShellDef, closeOnExit bool) *TabView {
	t := &TabView{
		byItem:      make(map[*container.TabItem]*Session),
		closeOnExit: closeOnExit,
	}
	if len(shells) > 0 {
		t.defaultShell = shells[0]
		t.hasDefault = true
	}

	t.tabs = container.NewDocTabs()
	t.tabs.CreateTab = t.createTab
	t.tabs.OnClosed = t.handleClosed

	for _, def := range shells {
		t.AddTab(def)
	}

	return t
}

// AddTab spawns def's shell in a new tab, selects it, and returns the
// resulting Session. If the shell fails to spawn (e.g. its executable is
// missing), the error is logged and AddTab returns nil rather than adding a
// broken tab — mirroring how settings.Window logs rather than panics on a
// registry error it can't usefully propagate through a UI callback.
func (t *TabView) AddTab(def ShellDef) *Session {
	sess, item, err := t.newTabItem(def)
	if err != nil {
		log.Printf("terminal: add tab for %q: %v", def.Name, err)
		return nil
	}

	t.tabs.Append(item)
	t.tabs.SelectIndex(len(t.tabs.Items) - 1)
	return sess
}

// CloseTab terminates s's shell process and removes its tab. It is a no-op
// if s is not (or is no longer) a tab of this TabView — safe to call twice,
// same idempotency guarantee Session.Close itself makes.
func (t *TabView) CloseTab(s *Session) {
	t.mu.Lock()
	var item *container.TabItem
	for it, sess := range t.byItem {
		if sess == s {
			item = it
			break
		}
	}
	if item != nil {
		delete(t.byItem, item)
	}
	t.mu.Unlock()

	if item == nil {
		return
	}

	_ = s.Close()
	t.tabs.Remove(item)
}

// closeAll terminates every open tab's session, in no particular order.
// Used by Window on its own close so shell processes don't outlive the
// window that spawned them (see conpty_windows.go's Close doc comment on why
// process termination has to be explicit rather than assumed).
func (t *TabView) closeAll() {
	t.mu.Lock()
	sessions := make([]*Session, 0, len(t.byItem))
	for _, sess := range t.byItem {
		sessions = append(sessions, sess)
	}
	t.mu.Unlock()

	for _, sess := range sessions {
		_ = sess.Close()
	}
}

// createTab is DocTabs.CreateTab: the built-in "+" button's callback. It
// spawns defaultShell. Returning nil (when there's no default shell, or the
// spawn fails) is DocTabs' documented signal for "nothing to add" — verified
// in the vendored source, buildCreateTabsButton only calls Append/SelectIndex
// when the returned *TabItem is non-nil.
func (t *TabView) createTab() *container.TabItem {
	if !t.hasDefault {
		return nil
	}
	_, item, err := t.newTabItem(t.defaultShell)
	if err != nil {
		log.Printf("terminal: create tab for %q: %v", t.defaultShell.Name, err)
		return nil
	}
	return item
}

// newTabItem spawns def's shell and wraps it in a *container.TabItem, or
// returns an error if the spawn failed. It records the item->Session mapping
// but does not append the item to t.tabs — callers differ on whether DocTabs
// itself appends it (createTab, via the "+" button machinery) or this
// package must (AddTab).
func (t *TabView) newTabItem(def ShellDef) (*Session, *container.TabItem, error) {
	sess, err := NewSession(def)
	if err != nil {
		return nil, nil, err
	}

	if t.closeOnExit {
		// OnExit's fn runs via fyne.Do (see widget.go), so CloseTab here —
		// which touches DocTabs, a CanvasObject — is on the UI goroutine by
		// the time it runs, same regime as every other exported TabView
		// method.
		sess.OnExit(func() { t.CloseTab(sess) })
	}

	item := container.NewTabItem(sess.Title(), sess)

	t.mu.Lock()
	t.byItem[item] = sess
	t.mu.Unlock()

	return sess, item, nil
}

// handleClosed is DocTabs.OnClosed: fired after the user clicks a tab's own
// close button and DocTabs has already removed the item from Items (see
// doctabs.go's close() — OnClosed only fires when CloseIntercept is nil,
// which it is here, and fires after, not instead of, the removal). This is
// the UI-driven mirror of CloseTab: same underlying process-termination
// step, triggered from the opposite direction (widget event vs. direct call).
func (t *TabView) handleClosed(item *container.TabItem) {
	t.mu.Lock()
	sess := t.byItem[item]
	delete(t.byItem, item)
	t.mu.Unlock()

	if sess != nil {
		_ = sess.Close()
	}
}
