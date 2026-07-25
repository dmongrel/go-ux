import {GetImageGridSpec, SelectImage, CancelImageGrid} from "../../bindings/go-ux/dialog/service";
import type {ImageGridSpec} from "../../bindings/go-ux/dialog/models";

// mountImageGrid is the Wails-side counterpart to go-ux/dialog.Service's
// ShowImageGrid: it renders whatever ImageGridSpec that call registered
// under this window's ?id= query param as a grid of image-only thumbnails
// (no labels, matching the original Fyne parchment-texture picker), then
// reports the clicked option's Key back via SelectImage — letting the
// original (still-blocked) ShowImageGrid call in Go return. Closing the
// window without clicking anything resolves via the Go side's own
// WindowClosing hook, not from here.
export function mountImageGrid(root: HTMLElement) {
    const id = new URLSearchParams(location.hash.split("?")[1] ?? "").get("id");
    if (!id) {
        root.innerHTML = `<div class="empty-pane">No image-grid id given</div>`;
        return;
    }

    (async () => {
        const spec = await GetImageGridSpec(id);
        renderGrid(root, id, spec);
    })();
}

function renderGrid(root: HTMLElement, id: string, spec: ImageGridSpec) {
    root.innerHTML = `
        <div class="settings-form" id="imagegrid-form">
          <h2>${escapeHtml(spec.Title)}</h2>
          <div id="imagegrid-cells" style="display: flex; flex-wrap: wrap; gap: 12px;"></div>
          <div class="hub-buttons" style="margin-top: 16px;">
            <button class="btn" id="imagegrid-cancel-btn">Close</button>
          </div>
        </div>
    `;
    const cellsEl = root.querySelector("#imagegrid-cells") as HTMLElement;
    (root.querySelector("#imagegrid-cancel-btn") as HTMLButtonElement).addEventListener("click", () => {
        CancelImageGrid(id);
    });

    for (const opt of spec.Options ?? []) {
        const cell = document.createElement("button");
        cell.className = "btn";
        cell.style.padding = "4px";
        cell.style.width = "120px";
        cell.style.height = "160px";
        if (opt.Key === spec.Selected) {
            cell.style.borderColor = "#3574f0";
            cell.style.borderWidth = "2px";
        }

        const img = document.createElement("img");
        // ImageData crosses the Go<->JS boundary as a base64 string (Wails'
        // JSON binding encodes a Go []byte field that way automatically) —
        // browsers accept that directly as a data: URI body. Declared as
        // image/png regardless of the option's actual encoding: WebView2
        // (Chromium) sniffs the real format from the bytes for <img> tags
        // rather than trusting the declared MIME, so this works for JPEG
        // ImageData too (e.g. Go-EPub's parchment textures) — if a
        // consumer ever hits a browser that doesn't sniff, ImageGridSpec
        // would need a per-option MIME/format field.
        img.src = "data:image/png;base64," + opt.ImageData;
        img.style.width = "100%";
        img.style.height = "100%";
        img.style.objectFit = "cover";
        cell.appendChild(img);

        cell.addEventListener("click", () => {
            SelectImage(id, opt.Key);
        });
        cellsEl.appendChild(cell);
    }

    if ((spec.Options ?? []).length === 0) {
        cellsEl.innerHTML = `<div class="empty-pane">No options</div>`;
    }
}

function escapeHtml(s: string): string {
    const div = document.createElement("div");
    div.textContent = s;
    return div.innerHTML;
}
