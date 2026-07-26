// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

import {ShowInfo, ShowError, ShowCustom, ShowImageGrid, PickFile} from "../../bindings/github.com/dmongrel/go-ux/dialog/service";
import {OpenWindow as OpenSettingsWindow} from "../../bindings/github.com/dmongrel/go-ux/settings/service";
import {OpenWindow as OpenTerminalWindow} from "../../bindings/github.com/dmongrel/go-ux/terminal/service";
import {OpenWindow as OpenEditorWindow} from "../../bindings/github.com/dmongrel/go-ux/editors/service";

// hub mirrors go-ux's old per-package Fyne demos (test_settings.go,
// dialogdemo/, terminaldemo/, editorsdemo/), folded into the one runnable
// window this Wails app needs — buttons here exercise each component as
// its Go Service lands (see CLAUDE.md's migration-phase notes).
export function mountHub(root: HTMLElement) {
    root.innerHTML = `
        <div class="hub">
          <h1>go-ux</h1>
          <p class="sub">Wails v3 component demo</p>
          <div class="hub-buttons" id="hub-buttons">
            <button class="btn" id="info-btn">Show Info Dialog</button>
            <button class="btn" id="error-btn">Show Error Dialog</button>
            <button class="btn" id="custom-btn">Show Custom Dialog</button>
            <button class="btn" id="imagegrid-btn">Show Image Grid Dialog</button>
            <button class="btn" id="settings-btn">Open Settings</button>
            <button class="btn" id="terminal-btn">Open Terminal</button>
            <button class="btn" id="editor-btn">Open Editor</button>
            <button class="btn" id="pick-file-btn">Open File Dialog</button>
          </div>
        </div>
    `;

    (root.querySelector("#info-btn") as HTMLButtonElement).addEventListener("click", () => {
        ShowInfo("Info", "This is a native Wails info dialog, replacing go-ux/dialog.NewInfo.");
    });

    (root.querySelector("#error-btn") as HTMLButtonElement).addEventListener("click", () => {
        ShowError("Error", "This is a native Wails error dialog, replacing go-ux/dialog.NewError.");
    });

    (root.querySelector("#custom-btn") as HTMLButtonElement).addEventListener("click", async () => {
        const result = await ShowCustom({
            Title: "Rename",
            Buttons: ["OK", "Cancel"],
            Width: 420,
            Height: 220,
            Properties: [
                {Key: "name", Label: "New name", Kind: "textField", Initial: [], Options: [], Selected: []},
                {Key: "confirm", Label: "I understand this can't be undone", Kind: "bool", Initial: [], Options: [], Selected: []},
            ],
        });
        if (result) {
            ShowInfo("Result", JSON.stringify(result));
        }
    });

    (root.querySelector("#imagegrid-btn") as HTMLButtonElement).addEventListener("click", async () => {
        // Two 8x8 solid-color PNGs — this button only exercises the
        // ShowImageGrid wiring (spec round-trip, click-to-select,
        // window-closing-cancels), not real artwork.
        const red = "iVBORw0KGgoAAAANSUhEUgAAAAgAAAAICAYAAADED76LAAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAAJcEhZcwAADsMAAA7DAcdvqGQAAAAWSURBVChTYzhgqf0fH2ZAF0DHw0MBAHJCiMF37vwEAAAAAElFTkSuQmCC";
        const blue = "iVBORw0KGgoAAAANSUhEUgAAAAgAAAAICAYAAADED76LAAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAAJcEhZcwAADsMAAA7DAcdvqGQAAAAWSURBVChTY9Bs2PkfH2ZAF0DHw0MBAC86mEFg9+y2AAAAAElFTkSuQmCC";
        const key = await ShowImageGrid({
            Title: "Choose an image",
            Selected: "red",
            Width: 420,
            Height: 300,
            Options: [
                {Key: "red", ImageData: red},
                {Key: "blue", ImageData: blue},
            ],
        });
        ShowInfo("Result", key || "(cancelled)");
    });

    (root.querySelector("#settings-btn") as HTMLButtonElement).addEventListener("click", () => {
        OpenSettingsWindow();
    });

    (root.querySelector("#terminal-btn") as HTMLButtonElement).addEventListener("click", () => {
        OpenTerminalWindow();
    });

    (root.querySelector("#editor-btn") as HTMLButtonElement).addEventListener("click", () => {
        OpenEditorWindow();
    });

    (root.querySelector("#pick-file-btn") as HTMLButtonElement).addEventListener("click", async () => {
        const path = await PickFile();
        ShowInfo("Picked File", path || "(cancelled)");
    });
}

