import {GetSpec, Submit, CancelDialog} from "../../bindings/go-ux/dialog/service";
import type {CustomDialogSpec, Property} from "../../bindings/go-ux/dialog/models";

// mountDialog is the Wails-side counterpart to go-ux/dialog.Service's
// ShowCustom: it renders whatever CustomDialogSpec that call registered
// under this window's ?id= query param, then reports the collected result
// back via Submit/CancelDialog — letting the original (still-blocked)
// ShowCustom call in Go return. Info/Error dialogs never route here; they
// use Wails' native app.Dialog directly and have no window/view of their
// own.
export function mountDialog(root: HTMLElement) {
    const id = new URLSearchParams(location.hash.split("?")[1] ?? "").get("id");
    if (!id) {
        root.innerHTML = `<div class="empty-pane">No dialog id given</div>`;
        return;
    }

    (async () => {
        const spec = await GetSpec(id);
        renderForm(root, id, spec);
    })();
}

function renderForm(root: HTMLElement, id: string, spec: CustomDialogSpec) {
    root.innerHTML = `
        <div class="settings-form" id="dialog-form">
          <h2>${escapeHtml(spec.Title)}</h2>
          <div id="dialog-rows"></div>
          <div class="hub-buttons" id="dialog-buttons" style="margin-top: 16px;"></div>
        </div>
    `;
    const rowsEl = root.querySelector("#dialog-rows") as HTMLElement;
    const buttonsEl = root.querySelector("#dialog-buttons") as HTMLElement;

    // fields collects one getter per property key — evaluated only when OK
    // is clicked, mirroring go-ux/dialog's original collectResult (which
    // also read widget state lazily, at Show-time, not per-keystroke).
    const fields = new Map<string, () => unknown>();

    for (const prop of spec.Properties ?? []) {
        rowsEl.appendChild(renderProperty(prop, fields));
    }

    for (const kind of spec.Buttons ?? ["OK"]) {
        const btn = document.createElement("button");
        btn.className = "btn" + (kind === "OK" ? " btn-primary" : "");
        btn.textContent = kind;
        btn.addEventListener("click", () => {
            if (kind === "OK") {
                const result: Record<string, unknown> = {};
                for (const [key, get] of fields) result[key] = get();
                Submit(id, result);
            } else {
                CancelDialog(id);
            }
        });
        buttonsEl.appendChild(btn);
    }
}

function renderProperty(prop: Property, fields: Map<string, () => unknown>): HTMLElement {
    const row = document.createElement("div");
    row.className = "prop-row";

    if (prop.Kind === "label") {
        row.textContent = prop.Label;
        return row;
    }

    const label = document.createElement("label");
    label.textContent = prop.Label;
    row.appendChild(label);

    switch (prop.Kind) {
        case "bool": {
            const input = document.createElement("input");
            input.type = "checkbox";
            fields.set(prop.Key, () => input.checked);
            row.appendChild(input);
            break;
        }
        case "textField": {
            const input = document.createElement("input");
            input.className = "input";
            input.type = "text";
            fields.set(prop.Key, () => input.value);
            row.appendChild(input);
            break;
        }
        case "int": {
            const input = document.createElement("input");
            input.className = "input";
            input.type = "number";
            fields.set(prop.Key, () => parseInt(input.value, 10) || 0);
            row.appendChild(input);
            break;
        }
        case "dropdown": {
            const select = document.createElement("select");
            select.className = "input";
            for (const opt of prop.Options ?? []) {
                const o = document.createElement("option");
                o.value = opt;
                o.textContent = opt;
                o.selected = (prop.Selected ?? [])[0] === opt;
                select.appendChild(o);
            }
            fields.set(prop.Key, () => select.value);
            row.appendChild(select);
            break;
        }
        case "multiSelect": {
            const group = document.createElement("div");
            const selected = new Set(prop.Selected ?? []);
            const boxes: HTMLInputElement[] = [];
            for (const opt of prop.Options ?? []) {
                const optLabel = document.createElement("label");
                optLabel.style.display = "block";
                const box = document.createElement("input");
                box.type = "checkbox";
                box.value = opt;
                box.checked = selected.has(opt);
                boxes.push(box);
                optLabel.appendChild(box);
                optLabel.appendChild(document.createTextNode(" " + opt));
                group.appendChild(optLabel);
            }
            fields.set(prop.Key, () => boxes.filter((b) => b.checked).map((b) => b.value));
            row.appendChild(group);
            break;
        }
        case "list": {
            const list = document.createElement("ul");
            let items = [...(prop.Initial ?? [])];
            const renderItems = () => {
                list.innerHTML = "";
                items.forEach((item, i) => {
                    const li = document.createElement("li");
                    li.textContent = item + " ";
                    const remove = document.createElement("button");
                    remove.className = "btn";
                    remove.textContent = "Remove";
                    remove.addEventListener("click", () => {
                        items = items.filter((_, j) => j !== i);
                        renderItems();
                    });
                    li.appendChild(remove);
                    list.appendChild(li);
                });
            };
            renderItems();
            const entry = document.createElement("input");
            entry.className = "input";
            const add = document.createElement("button");
            add.className = "btn";
            add.textContent = "Add";
            add.addEventListener("click", () => {
                if (!entry.value) return;
                items.push(entry.value);
                entry.value = "";
                renderItems();
            });
            fields.set(prop.Key, () => items);
            const controls = document.createElement("div");
            controls.appendChild(entry);
            controls.appendChild(add);
            row.appendChild(list);
            row.appendChild(controls);
            break;
        }
    }

    return row;
}

function escapeHtml(s: string): string {
    const div = document.createElement("div");
    div.textContent = s;
    return div.innerHTML;
}
