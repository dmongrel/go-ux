import {
    ListTabs,
    NewTab,
    SaveTab,
    ProposeDiff,
    AcceptDiff,
    CancelDiff,
    CloseTab,
    OpenFileDialog,
    SaveTabAsDialog,
    ReloadTab,
    CurrentFontSettings,
    SetFontSettings,
    SaveLayout,
    LoadLayout,
} from "../../bindings/github.com/dmongrel/go-ux/editors/service";
import type {LayoutNode} from "../../bindings/github.com/dmongrel/go-ux/editors/models";
import type {TabInfo} from "../../bindings/github.com/dmongrel/go-ux/editors/models";
import type {FontSettings} from "../../bindings/github.com/dmongrel/go-ux/fontsettings/models";
import {Events} from "@wailsio/runtime";

import {EditorView, basicSetup} from "codemirror";
import {markdown} from "@codemirror/lang-markdown";
import {unifiedMergeView} from "@codemirror/merge";
import {marked} from "marked";

// randomlyRearrangeLines is the Propose Diff button's own stand-in for a
// real host app's AI-assistant tooling calling Group.ProposeDiff(path,
// newText) with an actual proposed rewrite: it's a demo harness button,
// not a real menu action, so rather than prompting for manual edits
// first, it cuts a random contiguous chunk of lines out of the tab's
// current text and pastes it back in at a different random position —
// enough of a real, visible diff to exercise the @codemirror/merge
// review flow with a single click, no typing required. Falls back to
// returning text unchanged if there aren't at least 4 lines to
// meaningfully rearrange.
function randomlyRearrangeLines(text: string): string {
    const lines = text.split("\n");
    if (lines.length < 4) return text;
    const cutStart = Math.floor(Math.random() * (lines.length - 2));
    const cutLen = 1 + Math.floor(Math.random() * Math.min(3, lines.length - cutStart - 1));
    const cut = lines.splice(cutStart, cutLen);
    const insertAt = Math.floor(Math.random() * (lines.length + 1));
    lines.splice(insertAt, 0, ...cut);
    return lines.join("\n");
}

// SharedDoc is the frontend port of go-ux/editors' Document
// (document.go): the live, in-progress (possibly unsaved) text for one
// Tab, plus a listener registry so every Pane currently displaying that
// Tab stays in sync — every Pane pushes its own edits in, and gets
// every OTHER Pane's edits pushed out (excluding itself, via exceptKey).
class SharedDoc {
    text: string;
    private listeners = new Map<string, (text: string) => void>();

    constructor(text: string) {
        this.text = text;
    }

    setText(newText: string, exceptKey: string) {
        if (newText === this.text) return;
        this.text = newText;
        for (const [key, fn] of this.listeners) {
            if (key !== exceptKey) fn(newText);
        }
    }

    registerListener(key: string, fn: (text: string) => void) {
        this.listeners.set(key, fn);
    }

    unregisterListener(key: string) {
        this.listeners.delete(key);
    }
}

// Pane is the frontend port of go-ux/editors.Pane: an independent tab
// bar + content area.
interface Pane {
    key: string;
    tabIDs: string[];
    activeID: string | null;
    currentDocID: string | null;
    wrapperEl: HTMLElement;
    tabStripEl: HTMLElement;
    contentEl: HTMLElement;
    view: EditorView | null;
}

// Axis mirrors go-ux/editors/split.go's splitAxis: "row" = left/right
// (axisHorizontal), "column" = top/bottom (axisVertical).
type Axis = "row" | "column";

// TreeNode is the direct TS port of split.go's `node`: a leaf (pane) or a
// split with two children, capped at one level of nesting (a leaf
// reached through one split can never be split again) — see canSplit.
// Ported here because the same "same document shown in a live-synced
// second Pane, at most one level of PERPENDICULAR nesting" semantics
// this app already needs are exactly what that file already solved,
// tested, in go-ux; no reason to re-derive the tree algorithm from
// scratch.
interface SplitNode {
    pane: null;
    axis: Axis;
    a: TreeNode;
    b: TreeNode;
}
interface LeafNode {
    pane: Pane;
    axis: null;
    a: null;
    b: null;
}
type TreeNode = SplitNode | LeafNode;

function leaf(pane: Pane): LeafNode {
    return {pane, axis: null, a: null, b: null};
}
function isLeaf(n: TreeNode): n is LeafNode {
    return n.pane !== null;
}

// findLeaf mirrors split.go's findLeaf: locates target's leaf node and
// its depth (0 = target IS the root/no split yet, 1 = direct child of
// the outer split, 2 = nested inside one side's own inner split).
function findLeaf(root: TreeNode, target: Pane): {found: LeafNode; depth: number} | null {
    if (isLeaf(root)) {
        return root.pane === target ? {found: root, depth: 0} : null;
    }
    if (isLeaf(root.a) && root.a.pane === target) return {found: root.a, depth: 1};
    if (isLeaf(root.b) && root.b.pane === target) return {found: root.b, depth: 1};
    if (!isLeaf(root.a)) {
        const r = findLeaf(root.a, target);
        if (r) return {found: r.found, depth: 2};
    }
    if (!isLeaf(root.b)) {
        const r = findLeaf(root.b, target);
        if (r) return {found: r.found, depth: 2};
    }
    return null;
}

// canSplit mirrors split.go's canSplit (depth < 2 eligible), PLUS one
// constraint go-ux's real menu.go enforced only by never offering the
// option in the first place rather than in this pure function: a depth-1
// pane can't split further along the SAME axis as the existing outer
// split (that would produce a 3rd column/row, not the "one level of
// PERPENDICULAR nesting, up to 4 panes total" shape — two stacked on one
// side, a quadrant, etc. — the design actually calls for).
function canSplit(root: TreeNode, target: Pane, axis: Axis): boolean {
    const r = findLeaf(root, target);
    if (!r || r.depth >= 2) return false;
    if (r.depth === 1 && !isLeaf(root) && root.axis === axis) return false;
    return true;
}

// splitTree mirrors split.go's split()+replaceLeaf(): returns a NEW root
// with target's leaf replaced by a split node (target on side "a", a
// fresh leaf(newPane) on side "b"), or null if ineligible.
function splitTree(root: TreeNode, target: Pane, axis: Axis, newPane: Pane): TreeNode | null {
    if (!canSplit(root, target, axis)) return null;
    return replaceLeaf(root, target, {pane: null, axis, a: leaf(target), b: leaf(newPane)});
}

function replaceLeaf(root: TreeNode, target: Pane, replacement: TreeNode): TreeNode {
    if (isLeaf(root)) {
        return root.pane === target ? replacement : root;
    }
    return {pane: null, axis: root.axis, a: replaceLeaf(root.a, target, replacement), b: replaceLeaf(root.b, target, replacement)};
}

// removePane mirrors split.go's removePane: promotes target's sibling up
// one level. primary can never be removed.
function removePane(root: TreeNode, target: Pane, primary: Pane): TreeNode | null {
    if (target === primary || isLeaf(root)) return null;
    if (isLeaf(root.a) && root.a.pane === target) return root.b;
    if (isLeaf(root.b) && root.b.pane === target) return root.a;
    if (!isLeaf(root.a)) {
        const inner = removePane(root.a, target, primary);
        if (inner) return {pane: null, axis: root.axis, a: inner, b: root.b};
    }
    if (!isLeaf(root.b)) {
        const inner = removePane(root.b, target, primary);
        if (inner) return {pane: null, axis: root.axis, a: root.a, b: inner};
    }
    return null;
}

function containsPane(n: TreeNode, target: Pane): boolean {
    return isLeaf(n) ? n.pane === target : containsPane(n.a, target) || containsPane(n.b, target);
}

// locateAcrossAxis walks root looking for the nearest ancestor split
// whose axis matches, and returns which side (a/b) source is on
// (sourceSide — since split.go's split() always makes the ORIGINAL pane
// side "a" and the newly created one side "b", "a" means left/top and
// "b" means right/bottom for both axes), the OTHER side's subtree, and
// — if source is itself nested one level deeper within its own side —
// which slot (a/b) it occupies there, for positional correspondence.
// Shared by adjacentPane (what to move into) and moveLabel (what to call
// the action — Left/Right or Up/Down depend on sourceSide, not just the
// axis).
function locateAcrossAxis(root: TreeNode, source: Pane, axis: Axis): {other: TreeNode; withinSide: "a" | "b" | null; sourceSide: "a" | "b"} | null {
    if (isLeaf(root)) return null;
    function locate(n: TreeNode): {other: TreeNode; withinSide: "a" | "b" | null; sourceSide: "a" | "b"} | null {
        if (isLeaf(n)) return null;
        if (n.axis === axis) {
            if (containsPane(n.a, source)) {
                const withinSide = !isLeaf(n.a) ? (n.a.a.pane === source ? "a" : n.a.b.pane === source ? "b" : null) : null;
                return {other: n.b, withinSide, sourceSide: "a"};
            }
            if (containsPane(n.b, source)) {
                const withinSide = !isLeaf(n.b) ? (n.b.a.pane === source ? "a" : n.b.b.pane === source ? "b" : null) : null;
                return {other: n.a, withinSide, sourceSide: "b"};
            }
        }
        return locate(n.a) ?? locate(n.b);
    }
    return locate(root);
}

// adjacentPane finds source's "move target" along axis, generalized
// beyond go-ux's original Fyne implementation (which only resolved a
// target when the other side of the matching split was a bare leaf, so
// e.g. a 2-pane stack's other side had no defined target at all). Real
// requirement: with 2 stacked on the left and 1 on the right, moving
// from either left pane must collect into that single right pane; with a
// 2x2 quadrant, it must go to the POSITIONALLY CORRESPONDING pane
// (top-left <-> top-right, bottom-left <-> bottom-right), not an
// arbitrary one. Works symmetrically in both directions along an axis —
// source can be on either side; see moveLabel for picking the right
// verb (Left vs Right / Up vs Down) for whichever side source is
// actually on.
function adjacentPane(root: TreeNode, source: Pane, axis: Axis): Pane | null {
    const loc = locateAcrossAxis(root, source, axis);
    if (!loc) return null;
    if (isLeaf(loc.other)) return loc.other.pane;
    return loc.withinSide === "b" ? loc.other.b.pane : loc.other.a.pane;
}

// moveLabel picks the correct directional verb for pane's Move item on
// axis — "Move Right" when pane is on the left/top ("a") side of the
// matching split, "Move Left" when it's on the right/bottom ("b") side
// (and Down/Up respectively for the column axis). Falls back to the
// Right/Down default label when nothing is adjacent yet (the item is
// disabled in that case anyway — see isAdjacent).
function moveLabel(root: TreeNode, pane: Pane, axis: Axis): string {
    const loc = locateAcrossAxis(root, pane, axis);
    const forward = axis === "row" ? "Move Right" : "Move Down";
    const backward = axis === "row" ? "Move Left" : "Move Up";
    if (!loc) return forward;
    return loc.sourceSide === "a" ? forward : backward;
}

// rebuildDOM mirrors split.go's rebuild(): a full reconstruction of the
// nested-flex DOM tree matching root's shape, not an incremental patch —
// same "full rebuild, cheap because pane counts are tiny" philosophy.
function rebuildDOM(node: TreeNode): HTMLElement {
    if (isLeaf(node)) return node.pane.wrapperEl;
    const container = document.createElement("div");
    container.className = "cm-split " + (node.axis === "row" ? "cm-split-row" : "cm-split-column");
    container.appendChild(rebuildDOM(node.a));
    container.appendChild(rebuildDOM(node.b));
    return container;
}

export function mountEditor(root: HTMLElement) {
    root.innerHTML = `
        <div class="editor-view">
          <div class="tree-item" id="file-changed-banner" style="display: none; background: #4a3a1a;">
            <span id="file-changed-text"></span>
            <button class="btn" id="reload-from-disk-btn">Load from Disk</button>
            <button class="btn" id="keep-from-memory-btn">Keep from Memory</button>
          </div>
          <div class="cm-panes" id="cm-panes"></div>
          <div class="editor-toolbar">
            <button class="btn" id="new-tab-btn">New Tab</button>
            <button class="btn" id="open-file-btn">Open File...</button>
            <button class="btn" id="preview-btn" hidden>Preview</button>
            <button class="btn" id="propose-diff-btn">Propose Diff</button>
            <button class="btn btn-primary" id="save-btn">Save</button>
            <button class="btn" id="save-as-btn">Save As...</button>
            <button class="btn btn-primary" id="accept-diff-btn" hidden>Accept</button>
            <button class="btn" id="cancel-diff-btn" hidden>Cancel</button>
            <span class="count" id="save-status"></span>
          </div>
        </div>
    `;

    const panesEl = root.querySelector("#cm-panes") as HTMLElement;
    const saveStatus = root.querySelector("#save-status") as HTMLElement;
    const newTabBtn = root.querySelector("#new-tab-btn") as HTMLButtonElement;
    const openFileBtn = root.querySelector("#open-file-btn") as HTMLButtonElement;
    const previewBtn = root.querySelector("#preview-btn") as HTMLButtonElement;
    const proposeDiffBtn = root.querySelector("#propose-diff-btn") as HTMLButtonElement;
    const saveBtn = root.querySelector("#save-btn") as HTMLButtonElement;
    const saveAsBtn = root.querySelector("#save-as-btn") as HTMLButtonElement;
    const acceptDiffBtn = root.querySelector("#accept-diff-btn") as HTMLButtonElement;
    const cancelDiffBtn = root.querySelector("#cancel-diff-btn") as HTMLButtonElement;
    const fileChangedBanner = root.querySelector("#file-changed-banner") as HTMLElement;
    const fileChangedText = root.querySelector("#file-changed-text") as HTMLElement;

    let tabs: TabInfo[] = [];
    const docs = new Map<string, SharedDoc>();
    let nextPaneNum = 1;
    let currentFont: FontSettings = {Family: "", Size: 13, LineHeight: 1.0, ColumnWidth: 1.0};
    const previewPanes = new Set<string>(); // pane keys currently showing rendered Markdown instead of the editor
    const wrapPanes = new Set<string>(); // pane keys with soft-wrap enabled — CodeMirror's basicSetup gives line numbers by default but not wrapping
    let fileChangedTabID: string | null = null;

    function isMarkdown(tab: TabInfo): boolean {
        return /\.(md|markdown)$/i.test(tab.Title) || /\.(md|markdown)$/i.test(tab.FilePath);
    }

    function applyFont(pane: Pane) {
        pane.contentEl.style.fontSize = currentFont.Size + "px";
        pane.contentEl.style.lineHeight = String(currentFont.LineHeight);
        if (currentFont.Family) pane.contentEl.style.fontFamily = currentFont.Family;
    }

    function getDoc(id: string): SharedDoc {
        let doc = docs.get(id);
        if (!doc) {
            const tab = tabs.find((t) => t.ID === id);
            doc = new SharedDoc(tab ? tab.Text : "");
            docs.set(id, doc);
        }
        return doc;
    }

    function hasPendingDiff(tab: TabInfo): boolean {
        return tab.PendingDiff !== null && tab.PendingDiff !== undefined;
    }

    function createPane(): Pane {
        const key = "pane-" + nextPaneNum++;
        const wrapperEl = document.createElement("div");
        wrapperEl.className = "cm-pane-wrapper";
        const tabStripEl = document.createElement("div");
        tabStripEl.className = "pane-tab-strip";
        const contentEl = document.createElement("div");
        contentEl.className = "cm-pane-content";
        wrapperEl.appendChild(tabStripEl);
        wrapperEl.appendChild(contentEl);
        const pane: Pane = {key, tabIDs: [], activeID: null, currentDocID: null, wrapperEl, tabStripEl, contentEl, view: null};
        wrapperEl.addEventListener("mousedown", () => {
            focusedPane = pane;
        }, {capture: true});
        contentEl.addEventListener("wheel", (ev) => {
            if (!ev.ctrlKey) return;
            ev.preventDefault();
            const next: FontSettings = {...currentFont, Size: currentFont.Size + (ev.deltaY < 0 ? 1 : -1)};
            SetFontSettings(next);
        }, {passive: false});
        // Soft-wrap toggle lives on the line-number gutter's own right-click
        // menu (matches an IDE's own "wrap/unwrap" placement), not a
        // toolbar button — see showGutterContextMenu. Attached to
        // contentEl itself (stable across renderPaneContent's
        // rebuilds of its children) and target-filtered to the gutter,
        // so a right-click anywhere else in the editor still gets the
        // browser's normal context menu.
        contentEl.addEventListener("contextmenu", (ev) => {
            if (!(ev.target as HTMLElement).closest(".cm-gutters")) return;
            showGutterContextMenu(pane, ev);
        });
        applyFont(pane);
        return pane;
    }

    const primaryPane = createPane();
    let panesRoot: TreeNode = leaf(primaryPane);
    let focusedPane: Pane = primaryPane;

    function allPanes(): Pane[] {
        const out: Pane[] = [];
        (function walk(n: TreeNode) {
            if (isLeaf(n)) {
                out.push(n.pane);
            } else {
                walk(n.a);
                walk(n.b);
            }
        })(panesRoot);
        return out;
    }

    function rebuildLayout() {
        panesEl.innerHTML = "";
        panesEl.appendChild(rebuildDOM(panesRoot));
    }

    function unmountPaneContent(pane: Pane) {
        if (pane.currentDocID) {
            getDoc(pane.currentDocID).unregisterListener(pane.key);
            pane.currentDocID = null;
        }
        if (pane.view) {
            pane.view.destroy();
            pane.view = null;
        }
    }

    function renderPaneContent(pane: Pane) {
        unmountPaneContent(pane);
        const tab = pane.activeID ? tabs.find((t) => t.ID === pane.activeID) : undefined;
        if (!tab) {
            pane.contentEl.innerHTML = '<div class="empty-pane">No file open</div>';
            return;
        }
        pane.contentEl.innerHTML = "";
        applyFont(pane);

        // Markdown preview is a snapshot render (doesn't live-update while
        // toggled on if edited from another split pane showing the same
        // tab), matching the old Fyne version's own accepted limitation —
        // rendered client-side via `marked` instead of porting
        // markdown.go's goldmark-AST-to-Fyne-widget walk, since the
        // preview target is now HTML, not a Fyne canvas tree.
        if (previewPanes.has(pane.key) && !hasPendingDiff(tab)) {
            const preview = document.createElement("div");
            preview.style.padding = "16px";
            preview.style.overflow = "auto";
            preview.style.height = "100%";
            preview.style.boxSizing = "border-box";
            preview.innerHTML = marked.parse(getDoc(tab.ID).text, {async: false}) as string;
            pane.contentEl.appendChild(preview);
            return;
        }

        const wrapExtension = wrapPanes.has(pane.key) ? [EditorView.lineWrapping] : [];

        if (hasPendingDiff(tab)) {
            pane.view = new EditorView({
                doc: tab.PendingDiff!,
                extensions: [basicSetup, markdown(), unifiedMergeView({original: tab.Text}), ...wrapExtension],
                parent: pane.contentEl,
            });
            return;
        }

        const doc = getDoc(tab.ID);
        pane.view = new EditorView({
            doc: doc.text,
            extensions: [basicSetup, markdown(), ...wrapExtension, EditorView.updateListener.of((update) => {
                if (update.docChanged) doc.setText(update.state.doc.toString(), pane.key);
            })],
            parent: pane.contentEl,
        });
        pane.currentDocID = tab.ID;
        doc.registerListener(pane.key, (newText) => {
            if (!pane.view) return;
            pane.view.dispatch({changes: {from: 0, to: pane.view.state.doc.length, insert: newText}});
        });
    }

    function renderPaneTabStrip(pane: Pane) {
        pane.tabStripEl.innerHTML = "";
        for (const id of pane.tabIDs) {
            const tab = tabs.find((t) => t.ID === id);
            if (!tab) continue;
            const chip = document.createElement("div");
            chip.className = "tab-chip" + (id === pane.activeID ? " active" : "");
            chip.textContent = tab.Title + (hasPendingDiff(tab) ? " ●" : "");
            chip.addEventListener("click", () => {
                focusedPane = pane;
                pane.activeID = id;
                renderPaneTabStrip(pane);
                renderPaneContent(pane);
                updateToolbar();
                saveStatus.textContent = "";
            });
            chip.addEventListener("contextmenu", (ev) => showTabContextMenu(pane, id, ev));

            const close = document.createElement("span");
            close.textContent = " ×";
            close.addEventListener("click", async (ev) => {
                ev.stopPropagation();
                tabs = await CloseTab(id);
                docs.delete(id);
                for (const p of allPanes()) {
                    p.tabIDs = p.tabIDs.filter((tid) => tid !== id);
                    if (p.activeID === id) p.activeID = p.tabIDs[p.tabIDs.length - 1] ?? null;
                }
                for (const p of allPanes()) refreshPane(p);
                updateToolbar();
            });
            chip.appendChild(close);

            pane.tabStripEl.appendChild(chip);
        }
    }

    function refreshPane(pane: Pane) {
        renderPaneTabStrip(pane);
        renderPaneContent(pane);
    }

    function refreshPanesShowingTab(id: string) {
        for (const pane of allPanes()) {
            if (pane.activeID === id) refreshPane(pane);
        }
    }

    function updateToolbar() {
        const tab = focusedPane.activeID ? tabs.find((t) => t.ID === focusedPane.activeID) : undefined;
        const diffing = !!tab && hasPendingDiff(tab);
        newTabBtn.hidden = diffing;
        proposeDiffBtn.hidden = diffing || !tab;
        saveBtn.hidden = diffing || !tab;
        saveAsBtn.hidden = diffing || !tab;
        acceptDiffBtn.hidden = !diffing;
        cancelDiffBtn.hidden = !diffing;
        previewBtn.hidden = diffing || !tab || !isMarkdown(tab);
        previewBtn.textContent = previewPanes.has(focusedPane.key) ? "Edit" : "Preview";
        fileChangedBanner.style.display = fileChangedTabID && tab?.ID === fileChangedTabID ? "flex" : "none";

        persistLayout();
    }

    // serializeNode/persistLayout mirror go-ux/treestate's "write the
    // whole current state on every change" philosophy: rather than
    // tracking exactly which structural operation just happened, every
    // updateToolbar() call (already the natural end point of every
    // split/move/close/select/open/close-tab mutation below) re-derives
    // and saves the full tree. Every tab is represented, memory-only ones
    // included (FilePath "" for those) — restoring by TabID (see
    // buildTree/resolveLeaf) works for them as long as this Service's
    // process hasn't restarted since; FilePath is only the fallback for
    // surviving an actual restart, which memory-only tabs can't do either
    // way.
    function serializeNode(n: TreeNode): LayoutNode {
        if (isLeaf(n)) {
            const pane = n.pane;
            const paneTabs = pane.tabIDs
                .map((id) => tabs.find((t) => t.ID === id))
                .filter((t): t is TabInfo => !!t)
                .map((t) => ({TabID: t.ID, FilePath: t.FilePath}));
            return {
                Axis: "",
                SplitOffset: 0.5,
                A: null,
                B: null,
                Tabs: paneTabs,
                ActiveTabID: pane.activeID ?? "",
            };
        }
        return {
            Axis: n.axis,
            SplitOffset: 0.5,
            A: serializeNode(n.a),
            B: serializeNode(n.b),
            Tabs: [],
            ActiveTabID: "",
        };
    }

    function persistLayout() {
        SaveLayout(serializeNode(panesRoot));
    }

    // doSplit is split.go's split() + group.go's splitPane(): creates a
    // new Pane, grafts it into the tree, and — matching "the new pane
    // shows the SAME underlying document" — copies source's active tab
    // into it (both panes now show, and live-sync, that tab).
    function doSplit(source: Pane, axis: Axis): Pane | null {
        if (!canSplit(panesRoot, source, axis)) return null;
        const newPane = createPane();
        const newRoot = splitTree(panesRoot, source, axis, newPane);
        if (!newRoot) return null;
        panesRoot = newRoot;
        if (source.activeID && !newPane.tabIDs.includes(source.activeID)) {
            newPane.tabIDs.push(source.activeID);
        }
        newPane.activeID = source.activeID;
        return newPane;
    }

    function splitPane(source: Pane, axis: Axis) {
        const tab = source.activeID ? tabs.find((t) => t.ID === source.activeID) : undefined;
        if (!tab || hasPendingDiff(tab)) return;
        const newPane = doSplit(source, axis);
        if (!newPane) return;
        rebuildLayout();
        refreshPane(newPane);
        renderPaneTabStrip(source);
        updateToolbar();
    }

    // closePane mirrors group.go's closePane: promotes target's sibling,
    // used when a Move empties out a non-primary pane entirely.
    function closePane(target: Pane) {
        const newRoot = removePane(panesRoot, target, primaryPane);
        if (!newRoot) return;
        unmountPaneContent(target);
        panesRoot = newRoot;
        rebuildLayout();
    }

    // movePane mirrors group.go's movePane: "auto-split-then-move" — if
    // no adjacent pane exists yet in that direction, create one (via
    // doSplit, which copies the tab as a side effect), then remove the
    // tab from source, netting out to a MOVE rather than a copy. If
    // source's last tab just left, and source isn't primary, the pane
    // itself closes and its sibling is promoted.
    function movePane(source: Pane, axis: Axis) {
        const movingID = source.activeID;
        const tab = movingID ? tabs.find((t) => t.ID === movingID) : undefined;
        if (!tab || hasPendingDiff(tab) || !movingID) return;

        let target = adjacentPane(panesRoot, source, axis);
        if (!target) {
            target = doSplit(source, axis);
            if (!target) return;
        }
        if (target === source) return;

        const alreadyInTarget = target.tabIDs.includes(movingID);
        source.tabIDs = source.tabIDs.filter((id) => id !== movingID);
        const stillHasTabs = source.tabIDs.length > 0;
        if (source.activeID === movingID) {
            source.activeID = source.tabIDs[source.tabIDs.length - 1] ?? null;
        }
        if (!alreadyInTarget) target.tabIDs.push(movingID);
        target.activeID = movingID;

        if (!stillHasTabs && source !== primaryPane) {
            closePane(source);
        } else {
            rebuildLayout();
            renderPaneTabStrip(source);
            renderPaneContent(source);
        }
        renderPaneTabStrip(target);
        renderPaneContent(target);
        focusedPane = target;
        updateToolbar();
    }

// isAdjacent/isSplittable answer "is there already an adjacent pane in
// this direction" / "is this pane still eligible to split in this
// direction" — Split (Right/Down) and Split-and-Move share the same
// eligibility (both only make sense before a split exists that way);
// Move needs an adjacent pane to already exist. Used to enable/disable
// the context menu's 6 always-present items rather than hiding any of
// them (per explicit direction: menu items should always be visible,
// just greyed out when not currently applicable).
function isAdjacent(root: TreeNode, pane: Pane, axis: Axis): boolean {
    return adjacentPane(root, pane, axis) !== null;
}
function isSplittable(root: TreeNode, pane: Pane, axis: Axis): boolean {
    return !isAdjacent(root, pane, axis) && canSplit(root, pane, axis);
}

    // showTabContextMenu is this component's own right-click menu
    // (go-ux/editors' menu.go, not anything the parent Wails app
    // supplies). Always shows all 6 items (Split/Split-and-Move/Move x
    // Right/Down) per explicit direction — greyed out via a "disabled"
    // class and no click handler when not currently applicable, rather
    // than being hidden.
    // showGutterContextMenu is the line-number gutter's own right-click
    // menu — currently just the soft-wrap toggle, but its own menu (not
    // folded into showTabContextMenu's tab-chip menu) since it's a
    // content-area affordance, not a tab/pane-management one.
    function showGutterContextMenu(pane: Pane, ev: MouseEvent) {
        ev.preventDefault();

        const menu = document.createElement("div");
        menu.className = "context-menu";
        menu.style.left = ev.pageX + "px";
        menu.style.top = ev.pageY + "px";

        const item = document.createElement("div");
        item.className = "context-menu-item";
        item.textContent = wrapPanes.has(pane.key) ? "Disable Soft Wrap" : "Enable Soft Wrap";
        item.addEventListener("click", () => {
            if (wrapPanes.has(pane.key)) {
                wrapPanes.delete(pane.key);
            } else {
                wrapPanes.add(pane.key);
            }
            renderPaneContent(pane);
            menu.remove();
        });
        menu.appendChild(item);

        document.body.appendChild(menu);
        const onOutside = (e: MouseEvent) => {
            if (!menu.contains(e.target as Node)) {
                menu.remove();
                document.removeEventListener("mousedown", onOutside);
            }
        };
        setTimeout(() => document.addEventListener("mousedown", onOutside), 0);
    }

    function showTabContextMenu(pane: Pane, tabID: string, ev: MouseEvent) {
        ev.preventDefault();

        if (pane.activeID !== tabID) {
            pane.activeID = tabID;
            renderPaneTabStrip(pane);
            renderPaneContent(pane);
        }
        focusedPane = pane;
        updateToolbar();

        const menu = document.createElement("div");
        menu.className = "context-menu";
        menu.style.left = ev.pageX + "px";
        menu.style.top = ev.pageY + "px";

        function addItem(label: string, enabled: boolean, onClick: () => void) {
            const item = document.createElement("div");
            item.className = "context-menu-item" + (enabled ? "" : " disabled");
            item.textContent = label;
            if (enabled) {
                item.addEventListener("click", () => {
                    onClick();
                    menu.remove();
                });
            }
            menu.appendChild(item);
        }

        addItem("Split Right", isSplittable(panesRoot, pane, "row"), () => splitPane(pane, "row"));
        addItem("Split and Move Right", isSplittable(panesRoot, pane, "row"), () => movePane(pane, "row"));
        addItem(moveLabel(panesRoot, pane, "row"), isAdjacent(panesRoot, pane, "row"), () => movePane(pane, "row"));

        const divider = document.createElement("div");
        divider.className = "context-menu-divider";
        menu.appendChild(divider);

        addItem("Split Down", isSplittable(panesRoot, pane, "column"), () => splitPane(pane, "column"));
        addItem("Split and Move Down", isSplittable(panesRoot, pane, "column"), () => movePane(pane, "column"));
        addItem(moveLabel(panesRoot, pane, "column"), isAdjacent(panesRoot, pane, "column"), () => movePane(pane, "column"));

        document.body.appendChild(menu);
        const onOutside = (e: MouseEvent) => {
            if (!menu.contains(e.target as Node)) {
                menu.remove();
                document.removeEventListener("mousedown", onOutside);
            }
        };
        setTimeout(() => document.addEventListener("mousedown", onOutside), 0);
    }

    newTabBtn.addEventListener("click", async () => {
        try {
            // New tabs always open into the primary pane — mirrors
            // go-ux: AddTab always targets the primary pane.
            tabs = await NewTab();
            const newTab = tabs[tabs.length - 1];
            primaryPane.tabIDs.push(newTab.ID);
            focusedPane = primaryPane;
            primaryPane.activeID = newTab.ID;
            refreshPane(primaryPane);
            updateToolbar();
        } catch (err) {
            console.error("NewTab failed", err);
            saveStatus.textContent = `Failed: ${err}`;
        }
    });

    saveBtn.addEventListener("click", async () => {
        const id = focusedPane.activeID;
        if (!id) return;
        try {
            const text = getDoc(id).text;
            tabs = await SaveTab(id, text);
            saveStatus.textContent = "Saved.";
        } catch (err) {
            console.error("SaveTab failed", err);
            saveStatus.textContent = `Failed: ${err}`;
        }
    });

    saveAsBtn.addEventListener("click", async () => {
        const id = focusedPane.activeID;
        if (!id) return;
        try {
            // Push in-progress edits before the dialog switches FilePath —
            // SaveTabAs on the Go side writes the tab's current Document
            // text, which is only current if we flush this pane's edits
            // to it first.
            await SaveTab(id, getDoc(id).text);
            tabs = await SaveTabAsDialog(id);
            for (const p of allPanes()) if (p.activeID === id) renderPaneTabStrip(p);
            saveStatus.textContent = "Saved.";
        } catch (err) {
            console.error("SaveTabAsDialog failed", err);
            saveStatus.textContent = `Failed: ${err}`;
        }
    });

    openFileBtn.addEventListener("click", async () => {
        try {
            tabs = await OpenFileDialog();
            const newTab = tabs[tabs.length - 1];
            primaryPane.tabIDs.push(newTab.ID);
            focusedPane = primaryPane;
            primaryPane.activeID = newTab.ID;
            refreshPane(primaryPane);
            updateToolbar();
        } catch (err) {
            console.error("OpenFileDialog failed", err);
            saveStatus.textContent = `Failed: ${err}`;
        }
    });

    previewBtn.addEventListener("click", () => {
        if (previewPanes.has(focusedPane.key)) {
            previewPanes.delete(focusedPane.key);
        } else {
            previewPanes.add(focusedPane.key);
        }
        renderPaneContent(focusedPane);
        updateToolbar();
    });

    (root.querySelector("#reload-from-disk-btn") as HTMLButtonElement).addEventListener("click", async () => {
        const id = fileChangedTabID;
        fileChangedTabID = null;
        if (!id) return;
        try {
            tabs = await ReloadTab(id);
            const tab = tabs.find((t) => t.ID === id);
            if (tab) getDoc(id).setText(tab.Text, "");
            refreshPanesShowingTab(id);
        } catch (err) {
            console.error("ReloadTab failed", err);
            saveStatus.textContent = `Failed: ${err}`;
        }
        updateToolbar();
    });
    (root.querySelector("#keep-from-memory-btn") as HTMLButtonElement).addEventListener("click", () => {
        fileChangedTabID = null;
        updateToolbar();
    });

    proposeDiffBtn.addEventListener("click", async () => {
        const id = focusedPane.activeID;
        if (!id) return;
        try {
            const proposedText = randomlyRearrangeLines(getDoc(id).text);
            tabs = await ProposeDiff(id, proposedText);
            refreshPanesShowingTab(id);
            updateToolbar();
        } catch (err) {
            console.error("ProposeDiff failed", err);
            saveStatus.textContent = `Failed: ${err}`;
        }
    });

    acceptDiffBtn.addEventListener("click", async () => {
        const id = focusedPane.activeID;
        const view = focusedPane.view;
        if (!id || !view) return;
        try {
            const finalText = view.state.doc.toString();
            tabs = await AcceptDiff(id, finalText);
            getDoc(id).setText(finalText, "");
            refreshPanesShowingTab(id);
            updateToolbar();
            saveStatus.textContent = "Diff accepted.";
        } catch (err) {
            console.error("AcceptDiff failed", err);
            saveStatus.textContent = `Failed: ${err}`;
        }
    });

    cancelDiffBtn.addEventListener("click", async () => {
        const id = focusedPane.activeID;
        if (!id) return;
        try {
            tabs = await CancelDiff(id);
            refreshPanesShowingTab(id);
            updateToolbar();
            saveStatus.textContent = "Diff cancelled.";
        } catch (err) {
            console.error("CancelDiff failed", err);
            saveStatus.textContent = `Failed: ${err}`;
        }
    });

    const unsubFileChanged = Events.On("editors:filechanged", (ev) => {
        fileChangedTabID = ev.data as string;
        const tab = tabs.find((t) => t.ID === fileChangedTabID);
        fileChangedText.textContent = `"${tab?.Title ?? fileChangedTabID}" changed on disk.`;
        updateToolbar();
    });
    const unsubFont = Events.On("editors:font", (ev) => {
        currentFont = ev.data as FontSettings;
        for (const p of allPanes()) applyFont(p);
    });
    window.addEventListener("beforeunload", () => {
        unsubFileChanged();
        unsubFont();
    });

    // layoutHasTabs reports whether a persisted layout has anything worth
    // restoring at all.
    function layoutHasTabs(node: LayoutNode): boolean {
        if (!node.Axis) return (node.Tabs?.length ?? 0) > 0;
        return layoutHasTabs(node.A!) || layoutHasTabs(node.B!);
    }

    // resolveLeaf assigns pane's tabIDs/activeID from node's persisted
    // tabs, in order. For each: prefer TabID if a tab with that ID is
    // still in the current (already-fetched) `tabs` list — true whenever
    // this Service's process hasn't restarted since the layout was saved,
    // memory-only tabs included, since Tab IDs are only ever stable for
    // the process's lifetime. Otherwise, if FilePath is set, reopen it via
    // OpenFile (a real restart resets every Tab ID but files on disk are
    // still there). A memory-only tab whose ID is gone has no fallback and
    // is dropped. Sequential, not Promise.all: an OpenFile call replaces
    // the shared `tabs` array wholesale, so resolving out of order (or in
    // parallel) would race later lookups against an array an earlier call
    // hasn't finished updating yet.
    async function resolveLeaf(node: LayoutNode, pane: Pane) {
        for (const entry of node.Tabs ?? []) {
            let tab = tabs.find((t) => t.ID === entry.TabID);
            if (!tab && entry.FilePath) {
                try {
                    tabs = await OpenFile(entry.FilePath);
                } catch (err) {
                    console.error("OpenFile failed while restoring layout", entry.FilePath, err);
                    continue; // file moved/deleted since last save — skip it, not fatal to the rest of the layout
                }
                tab = tabs.find((t) => t.FilePath === entry.FilePath);
            }
            if (tab && !pane.tabIDs.includes(tab.ID)) pane.tabIDs.push(tab.ID);
        }
        pane.activeID = pane.tabIDs.includes(node.ActiveTabID) ? node.ActiveTabID : (pane.tabIDs[pane.tabIDs.length - 1] ?? null);
    }

    // buildTree reconstructs panesRoot from a persisted LayoutNode,
    // reusing primaryPane for whichever leaf is reached by always taking
    // side "a" from the root down — the same leaf splitTree always keeps
    // the original pane on (see its own doc comment) — and creating a
    // fresh Pane via createPane() for every other leaf.
    async function buildTree(node: LayoutNode, isPrimarySide: boolean): Promise<TreeNode> {
        if (!node.Axis) {
            const pane = isPrimarySide ? primaryPane : createPane();
            await resolveLeaf(node, pane);
            return leaf(pane);
        }
        const a = await buildTree(node.A!, isPrimarySide);
        const b = await buildTree(node.B!, false);
        return {pane: null, axis: node.Axis as Axis, a, b};
    }

    rebuildLayout();

    (async () => {
        try {
            [tabs, currentFont] = await Promise.all([ListTabs(), CurrentFontSettings()]);

            const layout = await LoadLayout();
            if (layout && layoutHasTabs(layout)) {
                panesRoot = await buildTree(layout, true);
                focusedPane = primaryPane;
                rebuildLayout();
                for (const p of allPanes()) refreshPane(p);
            } else {
                primaryPane.tabIDs = tabs.map((t) => t.ID);
                if (tabs.length > 0) primaryPane.activeID = tabs[0].ID;
                refreshPane(primaryPane);
            }
            updateToolbar();
        } catch (err) {
            console.error("ListTabs failed", err);
            primaryPane.tabStripEl.innerHTML = `<div class="tab-chip">Failed to load tabs: ${err}</div>`;
        }
    })();
}
