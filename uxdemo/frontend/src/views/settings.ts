// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

import {
    ListNodes,
    AllProperties,
    StageProperty,
    Apply,
    Cancel,
    InitialTreeState,
    SetExpanded,
    SetSelected,
} from "../../bindings/github.com/dmongrel/go-ux/settings/service";
import {PropertyType} from "../../bindings/github.com/dmongrel/go-ux/db/models";
import type {Node as SettingsNode, Property} from "../../bindings/github.com/dmongrel/go-ux/db/models";
import {Window as ThisWindow} from "@wailsio/runtime";

// mountSettings is the Wails/JS replacement for go-ux/settings.Window: a
// real nested tree (built from Node.ParentID, not a flat list) on the
// left, a generated properties form on the right, and OK/Cancel/Apply
// staging — edits go to Service.StageProperty as the user types and only
// reach the db on Apply/OK, matching the original Fyne Window's
// stage-then-flush behavior instead of terminal-poc's immediate-write POC
// shortcut.
export function mountSettings(root: HTMLElement) {
    root.innerHTML = `
        <div class="editor-view">
          <div class="settings" style="flex: 1; min-height: 0;">
            <aside class="settings-tree" id="tree" style="width: 260px; overflow-y: auto;">
              <div style="padding: 0 12px 8px;">
                <input class="input" id="search" placeholder="Search settings..." style="width: 100%;"/>
              </div>
              <div id="tree-nodes"></div>
            </aside>
            <section class="settings-form" id="form"></section>
          </div>
          <div class="editor-toolbar">
            <span style="flex: 1;"></span>
            <button class="btn" id="cancel-btn">Cancel</button>
            <button class="btn" id="apply-btn">Apply</button>
            <button class="btn btn-primary" id="ok-btn">OK</button>
            <span class="count" id="status"></span>
          </div>
        </div>
    `;

    const treeNodesEl = root.querySelector("#tree-nodes") as HTMLElement;
    const formEl = root.querySelector("#form") as HTMLElement;
    const searchEl = root.querySelector("#search") as HTMLInputElement;
    const statusEl = root.querySelector("#status") as HTMLElement;

    treeNodesEl.innerHTML = `<div class="tree-item">Loading...</div>`;

    let nodes: SettingsNode[] = [];
    let byParent = new Map<string, SettingsNode[]>();
    let allProps = new Map<string, Property[]>();
    let expanded = new Set<string>();
    let selected = "";
    let query = "";

    function rootKey(): string {
        return "";
    }

    function nodeKey(n: SettingsNode): string {
        return String(n.ID);
    }

    function parentKey(n: SettingsNode): string {
        return n.ParentID == null ? rootKey() : String(n.ParentID);
    }

    // matchSets recomputes which nodes are visible/highlighted for the
    // current search query — a direct client-side port of
    // go-ux/settings.Window's original applySearch, now running over data
    // already fetched in bulk via AllProperties instead of round-tripping
    // per keystroke.
    function matchSets(): {visible: Set<string> | null; descMatch: Set<string>; propMatch: Map<string, Set<string>>} {
        const q = query.trim().toLowerCase();
        if (!q) return {visible: null, descMatch: new Set(), propMatch: new Map()};

        const visible = new Set<string>();
        const descMatch = new Set<string>();
        const propMatch = new Map<string, Set<string>>();

        const markVisible = (uid: string) => {
            let cur: string | undefined = uid;
            while (cur !== undefined) {
                visible.add(cur);
                const n = nodes.find((n) => nodeKey(n) === cur);
                if (!n || n.ParentID == null) break;
                cur = String(n.ParentID);
            }
        };

        for (const n of nodes) {
            if (n.Description.toLowerCase().includes(q)) {
                descMatch.add(nodeKey(n));
                markVisible(nodeKey(n));
            }
        }
        for (const [uid, props] of allProps) {
            for (const p of props) {
                if (!p.Label.toLowerCase().includes(q)) continue;
                if (!propMatch.has(uid)) propMatch.set(uid, new Set());
                propMatch.get(uid)!.add(p.Key);
                markVisible(uid);
            }
        }
        return {visible, descMatch, propMatch};
    }

    function renderTree() {
        const {visible, descMatch} = matchSets();
        treeNodesEl.innerHTML = "";

        function renderLevel(parent: string, depth: number) {
            const children = byParent.get(parent) ?? [];
            for (const n of children) {
                const key = nodeKey(n);
                if (visible && !visible.has(key)) continue;

                const hasChildren = (byParent.get(key) ?? []).length > 0;
                const isExpanded = visible !== null || expanded.has(key); // search auto-expands every visible branch

                const item = document.createElement("div");
                item.className = "tree-item" + (key === selected ? " active" : "");
                item.style.paddingLeft = 16 + depth * 16 + "px";
                item.style.display = "flex";
                item.style.alignItems = "center";
                item.style.gap = "4px";

                const toggle = document.createElement("span");
                toggle.style.width = "12px";
                toggle.style.display = "inline-block";
                toggle.style.cursor = hasChildren ? "pointer" : "default";
                toggle.textContent = hasChildren ? (isExpanded ? "▾" : "▸") : "";
                if (hasChildren) {
                    toggle.addEventListener("click", (ev) => {
                        ev.stopPropagation();
                        if (expanded.has(key)) {
                            expanded.delete(key);
                            SetExpanded(key, false);
                        } else {
                            expanded.add(key);
                            SetExpanded(key, true);
                        }
                        renderTree();
                    });
                }
                item.appendChild(toggle);

                const label = document.createElement("span");
                label.textContent = n.Description;
                if (descMatch.has(key)) label.style.background = "#ffeb3b", label.style.color = "#000";
                item.appendChild(label);

                item.addEventListener("click", () => {
                    selected = key;
                    SetSelected(key);
                    renderTree();
                    renderForm();
                });

                treeNodesEl.appendChild(item);

                if (hasChildren && isExpanded) renderLevel(key, depth + 1);
            }
        }

        renderLevel(rootKey(), 0);

        if (treeNodesEl.children.length === 0) {
            treeNodesEl.innerHTML = `<div class="tree-item">No settings nodes (loaded ${nodes.length} total)</div>`;
        }
    }

    function renderForm() {
        const {propMatch} = matchSets();
        const props = allProps.get(selected) ?? [];
        const node = nodes.find((n) => nodeKey(n) === selected);
        formEl.innerHTML = node ? `<h2>${node.Description}</h2>` : "";
        if (!node) return;

        const nodeID = node.ID;
        const highlighted = propMatch.get(selected) ?? new Set();

        for (const prop of props) {
            const row = document.createElement("div");
            row.className = "prop-row";

            const label = document.createElement("label");
            label.textContent = prop.Label;
            if (highlighted.has(prop.Key)) label.style.background = "#ffeb3b", label.style.color = "#000";
            row.appendChild(label);

            let input: HTMLInputElement | HTMLSelectElement | HTMLSpanElement;
            if (prop.Type === PropertyType.PropertyReadOnly) {
                input = document.createElement("span");
                input.textContent = prop.Value;
            } else if (prop.Type === PropertyType.PropertyBool) {
                input = document.createElement("input");
                (input as HTMLInputElement).type = "checkbox";
                (input as HTMLInputElement).checked = prop.Value === "true";
                input.addEventListener("change", () => {
                    StageProperty(nodeID, prop.Key, (input as HTMLInputElement).checked ? "true" : "false");
                });
            } else if (prop.Type === PropertyType.PropertyEnum) {
                input = document.createElement("select");
                for (const opt of prop.EnumOptions) {
                    const o = document.createElement("option");
                    o.value = opt;
                    o.textContent = opt;
                    o.selected = opt === prop.Value;
                    input.appendChild(o);
                }
                input.addEventListener("change", () => {
                    StageProperty(nodeID, prop.Key, (input as HTMLSelectElement).value);
                });
            } else {
                input = document.createElement("input");
                (input as HTMLInputElement).type = prop.Type === PropertyType.PropertyInt || prop.Type === PropertyType.PropertyFloat ? "number" : "text";
                (input as HTMLInputElement).value = prop.Value;
                input.addEventListener("change", () => {
                    StageProperty(nodeID, prop.Key, (input as HTMLInputElement).value);
                });
            }
            input.className = "input";

            const valueGroup = document.createElement("div");
            valueGroup.className = "prop-value";
            valueGroup.appendChild(input);
            if (prop.Capability) {
                const capability = document.createElement("span");
                capability.className = "prop-capability";
                capability.textContent = prop.Capability;
                valueGroup.appendChild(capability);
            }
            row.appendChild(valueGroup);

            formEl.appendChild(row);
        }
    }

    searchEl.addEventListener("input", () => {
        query = searchEl.value;
        renderTree();
        renderForm();
    });

    (root.querySelector("#apply-btn") as HTMLButtonElement).addEventListener("click", async () => {
        await Apply();
        statusEl.textContent = "Applied.";
    });
    (root.querySelector("#ok-btn") as HTMLButtonElement).addEventListener("click", async () => {
        await Apply();
        void ThisWindow.Close();
    });
    (root.querySelector("#cancel-btn") as HTMLButtonElement).addEventListener("click", () => {
        Cancel();
        void ThisWindow.Close();
    });

    (async () => {
        try {
            [nodes, allProps] = await Promise.all([
                ListNodes(),
                AllProperties().then((m) => new Map(Object.entries(m))),
            ]);
            byParent = new Map();
            for (const n of nodes) {
                const key = parentKey(n);
                if (!byParent.has(key)) byParent.set(key, []);
                byParent.get(key)!.push(n);
            }

            const initial = await InitialTreeState();
            expanded = new Set(initial.Expanded ?? []);
            selected = initial.Selected ?? "";

            renderTree();
            renderForm();
        } catch (err) {
            console.error("settings init failed", err);
            treeNodesEl.innerHTML = `<div class="tree-item">Failed to load settings: ${String(err)}</div>`;
            statusEl.textContent = `Error: ${String(err)}`;
        }
    })();
}

