import {Events} from "@wailsio/runtime";
import {
    Start,
    WriteInput,
    Resize,
    CloseSession,
    DetectShells,
    CurrentFontSettings,
    SetFontSettings,
    CloseOnExit,
} from "../../bindings/github.com/dmongrel/go-ux/terminal/service";
import type {ShellDef, FontSettings} from "../../bindings/github.com/dmongrel/go-ux/terminal/models";

import {Terminal} from "@xterm/xterm";
import {FitAddon} from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";

// mountTerminal is the Wails/JS replacement for go-ux/terminal.Window/
// TabView: multiple PTY-backed tabs (one xterm.js instance each — xterm.js
// owns all VT100 parsing/rendering; Go only owns the PTY process and
// shuttles raw bytes, see terminal/terminal.go), a shell picker on the "+"
// button (go-ux/terminal.DetectShells), and Ctrl+scroll font sizing shared
// live across every open terminal window via the "terminal:font" event —
// the two gaps terminal-poc's single-session POC explicitly left open.
export function mountTerminal(root: HTMLElement) {
    root.innerHTML = `
        <div class="editor-view">
          <div class="pane-tab-strip" id="tab-strip"></div>
          <div id="sessions" style="flex: 1; min-height: 0; position: relative;"></div>
        </div>
    `;
    const tabStripEl = root.querySelector("#tab-strip") as HTMLElement;
    const sessionsEl = root.querySelector("#sessions") as HTMLElement;

    interface SessionTab {
        id: string;
        shellName: string;
        term: Terminal;
        fitAddon: FitAddon;
        container: HTMLElement;
        tabEl: HTMLElement;
    }

    const tabs = new Map<string, SessionTab>();
    let activeID: string | null = null;
    let shells: ShellDef[] = [];
    let currentFont: FontSettings = {Family: "", Size: 13, LineHeight: 1.0, ColumnWidth: 1.0};
    let closeOnExit = false;

    function applyFont(term: Terminal, f: FontSettings) {
        term.options.fontSize = f.Size;
        term.options.lineHeight = f.LineHeight;
        if (f.Family) term.options.fontFamily = f.Family;
    }

    function selectTab(id: string) {
        activeID = id;
        for (const t of tabs.values()) {
            const active = t.id === id;
            t.container.style.display = active ? "block" : "none";
            t.tabEl.className = "tab-chip" + (active ? " active" : "");
        }
        const t = tabs.get(id);
        if (t) {
            t.fitAddon.fit();
            Resize(id, t.term.cols, t.term.rows);
            t.term.focus();
        }
    }

    function renderTabStrip() {
        tabStripEl.innerHTML = "";
        for (const t of tabs.values()) {
            const chip = document.createElement("div");
            chip.className = "tab-chip" + (t.id === activeID ? " active" : "");
            chip.textContent = t.shellName + " ";
            const close = document.createElement("span");
            close.textContent = "×";
            close.style.marginLeft = "6px";
            close.addEventListener("click", (ev) => {
                ev.stopPropagation();
                closeTab(t.id);
            });
            chip.appendChild(close);
            chip.addEventListener("click", () => selectTab(t.id));
            tabStripEl.appendChild(chip);
        }
        const newTabBtn = document.createElement("select");
        newTabBtn.className = "input";
        const placeholder = document.createElement("option");
        placeholder.textContent = "+ New Tab";
        placeholder.value = "";
        newTabBtn.appendChild(placeholder);
        for (const shell of shells) {
            const opt = document.createElement("option");
            opt.value = shell.Name;
            opt.textContent = shell.Name;
            newTabBtn.appendChild(opt);
        }
        newTabBtn.addEventListener("change", () => {
            if (newTabBtn.value) openTab(newTabBtn.value);
            newTabBtn.value = "";
        });
        tabStripEl.appendChild(newTabBtn);
    }

    async function openTab(shellName: string) {
        const container = document.createElement("div");
        container.style.width = "100%";
        container.style.height = "100%";
        container.style.padding = "8px";
        container.style.boxSizing = "border-box";
        container.style.display = "none";
        sessionsEl.appendChild(container);

        const term = new Terminal({cursorBlink: true, convertEol: true});
        applyFont(term, currentFont);
        const fitAddon = new FitAddon();
        term.loadAddon(fitAddon);
        term.open(container);
        fitAddon.fit();

        container.addEventListener("wheel", (ev) => {
            if (!ev.ctrlKey) return;
            ev.preventDefault();
            const next: FontSettings = {...currentFont, Size: currentFont.Size + (ev.deltaY < 0 ? 1 : -1)};
            SetFontSettings(next);
        }, {passive: false});

        const id = await Start(shellName, term.cols, term.rows);

        const tabEl = document.createElement("div"); // placeholder; renderTabStrip rebuilds real chips
        const tab: SessionTab = {id, shellName, term, fitAddon, container, tabEl};
        tabs.set(id, tab);

        term.onData((data) => {
            WriteInput(id, data);
        });

        renderTabStrip();
        selectTab(id);
    }

    function closeTab(id: string) {
        const t = tabs.get(id);
        if (!t) return;
        CloseSession(id);
        t.term.dispose();
        t.container.remove();
        tabs.delete(id);
        if (activeID === id) {
            const next = tabs.keys().next();
            activeID = next.done ? null : next.value;
        }
        renderTabStrip();
        if (activeID) selectTab(activeID);
    }

    const unsubData = Events.On("terminal:data", (ev) => {
        const payload = ev.data as {SessionID: string; Data: string};
        tabs.get(payload.SessionID)?.term.write(payload.Data);
    });
    const unsubExit = Events.On("terminal:exit", (ev) => {
        // A session that exits on its own (e.g. the shell process quit)
        // is auto-closed only when close_on_exit is set — matching the
        // original Fyne TabView's behavior. closeTab already no-ops via
        // CloseSession on an already-exited session, so this is safe even
        // though the Go side already removed the session before emitting.
        if (closeOnExit) closeTab(ev.data as string);
    });
    const unsubFont = Events.On("terminal:font", (ev) => {
        currentFont = ev.data as FontSettings;
        for (const t of tabs.values()) applyFont(t.term, currentFont);
    });

    const onResize = () => {
        const t = activeID ? tabs.get(activeID) : undefined;
        if (!t) return;
        t.fitAddon.fit();
        Resize(t.id, t.term.cols, t.term.rows);
    };
    window.addEventListener("resize", onResize);

    window.addEventListener("beforeunload", () => {
        unsubData();
        unsubExit();
        unsubFont();
        window.removeEventListener("resize", onResize);
        for (const t of tabs.values()) CloseSession(t.id);
    });

    (async () => {
        [shells, currentFont, closeOnExit] = await Promise.all([DetectShells(), CurrentFontSettings(), CloseOnExit()]);
        renderTabStrip();
        await openTab(shells[0]?.Name ?? "");
    })();
}
