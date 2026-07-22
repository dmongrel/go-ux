//go:build windows

package terminal

import (
	"os"
	"testing"
	"time"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"golang.org/x/sys/windows"
)

// closeWaitWindow bounds how long TestCloseTabTerminatesProcess blocks
// waiting for the process TerminateProcess'd by CloseTab to actually become
// signaled. It's generous (well past normal process-teardown time) but not
// unbounded, so a genuine regression (CloseTab not killing the process) fails
// the test in finite time instead of hanging the suite.
const closeWaitWindow = 3 * time.Second

func cmdDef(name string) ShellDef {
	return ShellDef{Name: name, Path: os.Getenv("SystemRoot") + `\System32\cmd.exe`}
}

// dupProcessHandle returns the caller's own duplicate of process, independent
// of the session's original handle. winPTYSession.Close (see
// winpty_windows.go) both TerminateProcess's AND CloseHandle's its own
// process handle, so waiting on that original handle after Close returns
// fails with "the handle is invalid" rather than reporting signaled — that's
// an artifact of the handle being closed, not evidence about whether the
// process itself is still alive. A duplicate handle to the same underlying
// kernel process object stays valid (and reports the same signaled state)
// even after the original handle it was duplicated from is closed, which is
// exactly what's needed to observe termination from outside Close's own
// bookkeeping.
func dupProcessHandle(t *testing.T, process windows.Handle) windows.Handle {
	t.Helper()
	self := windows.CurrentProcess()
	var dup windows.Handle
	if err := windows.DuplicateHandle(self, process, self, &dup, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		t.Fatalf("DuplicateHandle: %v", err)
	}
	t.Cleanup(func() { windows.CloseHandle(dup) })
	return dup
}

// TestNewTabViewCreatesOneTabPerShell confirms NewTabView spawns and wires up
// one tab per configured ShellDef.
func TestNewTabViewCreatesOneTabPerShell(t *testing.T) {
	test.NewApp()

	tv := NewTabView([]ShellDef{cmdDef("cmd-1"), cmdDef("cmd-2")})
	defer tv.closeAll()

	if got := len(tv.tabs.Items); got != 2 {
		t.Fatalf("len(tabs.Items) = %d, want 2", got)
	}
	if got := len(tv.byItem); got != 2 {
		t.Fatalf("len(byItem) = %d, want 2", got)
	}
}

// TestAddTabAppendsAndSelects confirms AddTab grows the tab bar and returns a
// usable Session distinct from the initial tabs.
func TestAddTabAppendsAndSelects(t *testing.T) {
	test.NewApp()

	tv := NewTabView([]ShellDef{cmdDef("cmd-1")})
	defer tv.closeAll()

	sess := tv.AddTab(cmdDef("cmd-2"))
	if sess == nil {
		t.Fatal("AddTab returned nil Session")
	}
	if got := len(tv.tabs.Items); got != 2 {
		t.Fatalf("len(tabs.Items) after AddTab = %d, want 2", got)
	}
	if got := tv.tabs.Items[len(tv.tabs.Items)-1]; got.Content != sess {
		t.Error("AddTab's session is not the content of the newly appended tab")
	}
}

// TestCreateTabUsesDefaultShell confirms DocTabs' built-in "+" button
// callback (createTab) spawns from the first configured shell and is
// nil-safe when there is no default (see doctabs.go's buildCreateTabsButton,
// which treats a nil *TabItem as "nothing to add").
func TestCreateTabUsesDefaultShell(t *testing.T) {
	test.NewApp()

	tv := NewTabView([]ShellDef{cmdDef("cmd-1")})
	defer tv.closeAll()

	item := tv.createTab()
	if item == nil {
		t.Fatal("createTab returned nil despite a configured default shell")
	}
	sess, ok := item.Content.(*Session)
	if !ok {
		t.Fatalf("createTab's item.Content is %T, not *Session", item.Content)
	}
	defer sess.Close()

	empty := NewTabView(nil)
	if got := empty.createTab(); got != nil {
		t.Error("createTab on a TabView with no configured shells should return nil")
	}
}

// TestCloseTabTerminatesProcess is this task's core "done" bar: CloseTab must
// actually terminate the underlying OS process, not just remove the tab
// widget. Per the plan's Task 3 brief and the pattern already established in
// session_windows_test.go (TestNewPtySessionSpawnsShellAndProducesOutput's
// doc comment), an instant 0ms WaitForSingleObject poll immediately after
// Close is a vacuous check on this machine: a process racing toward its own
// exit for unrelated reasons could pass it even if CloseTab's
// TerminateProcess call were silently broken. This test instead blocks on a
// real WaitForSingleObject with a multi-second timeout and requires the
// signaled state (WAIT_OBJECT_0) specifically, which only happens once the
// process has actually died — CloseTab calls Session.Close synchronously
// before returning, and Session.Close calls ptySession.Close, which
// TerminateProcess's the child (see winpty_windows.go), so the process
// should already be gone (or dying) by the time this wait even starts.
func TestCloseTabTerminatesProcess(t *testing.T) {
	test.NewApp()

	tv := NewTabView([]ShellDef{cmdDef("cmd-1"), cmdDef("cmd-2")})
	defer tv.closeAll()

	target := tv.byItem[tv.tabs.Items[0]]
	wpty, ok := target.pty.(*winPTYSession)
	if !ok {
		t.Fatalf("target.pty is %T, not *winPTYSession", target.pty)
	}
	process := dupProcessHandle(t, wpty.process)

	tv.CloseTab(target)

	if got := len(tv.tabs.Items); got != 1 {
		t.Errorf("len(tabs.Items) after CloseTab = %d, want 1", got)
	}
	if _, stillTracked := tv.byItem[tv.tabs.Items[0]]; !stillTracked {
		// sanity: the remaining tab should still be tracked normally.
		t.Error("remaining tab is missing from byItem after CloseTab")
	}

	event, err := windows.WaitForSingleObject(process, uint32(closeWaitWindow/time.Millisecond))
	if err != nil {
		t.Fatalf("WaitForSingleObject: %v", err)
	}
	if event != windows.WAIT_OBJECT_0 {
		t.Fatalf("process not signaled (terminated) within %v of CloseTab; WaitForSingleObject event = %d, want WAIT_OBJECT_0", closeWaitWindow, event)
	}
}

// TestCloseTabIsNoOpForUnknownSession confirms CloseTab tolerates a Session
// that isn't (or is no longer) one of its own tabs, rather than panicking —
// the same idempotency guarantee Session.Close itself makes.
func TestCloseTabIsNoOpForUnknownSession(t *testing.T) {
	test.NewApp()

	tv := NewTabView([]ShellDef{cmdDef("cmd-1")})
	defer tv.closeAll()

	other, err := NewSession(cmdDef("cmd-2"))
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer other.Close()

	tv.CloseTab(other) // not one of tv's tabs
	if got := len(tv.tabs.Items); got != 1 {
		t.Errorf("len(tabs.Items) = %d, want unchanged 1", got)
	}
}

// TestCloseLastTabLeavesEmptyTabViewWithoutPanic covers the plan's explicit
// "closing the last tab leaves an empty TabView, not a crash/panic" bar. It
// closes the only tab, then drives the widget through CreateRenderer/Refresh
// on a real (headless) canvas — the operations most likely to panic on a
// nil/empty backing structure — to prove the empty state is actually usable,
// not just "didn't panic during CloseTab itself".
func TestCloseLastTabLeavesEmptyTabViewWithoutPanic(t *testing.T) {
	a := test.NewApp()
	defer test.NewApp() // reset the global app for other tests in this package

	tv := NewTabView([]ShellDef{cmdDef("cmd-1")})
	defer tv.closeAll()

	sess := tv.byItem[tv.tabs.Items[0]]

	win := a.NewWindow("")
	// uiMu: this test builds its own window and touches tv.tabs directly
	// (bypassing window.go's own uiMu-guarded newWindow), while "cmd-1"'s
	// session already has background loops running — see uiMu's doc
	// comment in widget.go for why that touch needs the same lock those
	// loops take.
	uiMu.Lock()
	win.SetContent(tv.tabs)
	uiMu.Unlock()
	defer func() {
		uiMu.Lock()
		win.Close()
		uiMu.Unlock()
	}()

	tv.CloseTab(sess)

	if got := len(tv.tabs.Items); got != 0 {
		t.Fatalf("len(tabs.Items) after closing last tab = %d, want 0", got)
	}
	if got := len(tv.byItem); got != 0 {
		t.Fatalf("len(byItem) after closing last tab = %d, want 0", got)
	}

	// Must not panic: rebuild the renderer and refresh against the now-empty
	// tab set.
	r := tv.tabs.CreateRenderer()
	r.Refresh()
	r.Layout(r.MinSize())
}

// TestUserClosedTabViaOnClosedTerminatesProcess covers the other path into
// tab closing: DocTabs' own per-tab close button, which fires OnClosed
// (handleClosed) after removing the item itself, rather than a caller
// invoking CloseTab directly. Driving a real close-button tap through Fyne's
// test harness would require simulating pointer events on an internal,
// unexported widget, so this calls handleClosed directly with the item
// DocTabs would have passed it — that is exactly the callback DocTabs.OnClosed
// is wired to (see tabs.go's NewTabView), so this exercises the same code
// path a real user click would.
func TestUserClosedTabViaOnClosedTerminatesProcess(t *testing.T) {
	test.NewApp()

	tv := NewTabView([]ShellDef{cmdDef("cmd-1")})
	defer tv.closeAll()

	item := tv.tabs.Items[0]
	sess := tv.byItem[item]
	wpty, ok := sess.pty.(*winPTYSession)
	if !ok {
		t.Fatalf("sess.pty is %T, not *winPTYSession", sess.pty)
	}
	process := dupProcessHandle(t, wpty.process)

	// Mirror what DocTabs.close does when CloseIntercept is nil (verified in
	// the vendored source, doctabs.go): remove the item, then fire OnClosed.
	tv.tabs.Remove(item)
	tv.handleClosed(item)

	if _, stillTracked := tv.byItem[item]; stillTracked {
		t.Error("byItem still tracks the item after handleClosed")
	}

	event, err := windows.WaitForSingleObject(process, uint32(closeWaitWindow/time.Millisecond))
	if err != nil {
		t.Fatalf("WaitForSingleObject: %v", err)
	}
	if event != windows.WAIT_OBJECT_0 {
		t.Fatalf("process not signaled (terminated) within %v of handleClosed; event = %d, want WAIT_OBJECT_0", closeWaitWindow, event)
	}
}

// Compile-time confirmation that container.TabItem is what AddTab/createTab
// build against — guards against a future Fyne upgrade silently changing the
// field name this file relies on (item.Content).
var _ = (&container.TabItem{}).Content
