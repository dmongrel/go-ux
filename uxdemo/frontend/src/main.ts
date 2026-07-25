import {mountHub} from "./views/hub";
import {mountDialog} from "./views/dialog";
import {mountSettings} from "./views/settings";
import {mountTerminal} from "./views/terminal";
import {mountEditor} from "./views/editor";

// Every window this demo opens (Hub, and — as later phases land — Dialog/
// Settings/Terminal/Editor) loads the SAME index.html/bundle: each
// component's OpenWindow() Go method just points a new window's URL at a
// different hash (#dialog, #settings, #terminal, #editor). One shared Vite
// build, routed client-side, matches the pattern already validated in
// terminal-poc.
const root = document.getElementById("root")!;

function route() {
    root.innerHTML = "";
    const hash = location.hash.split("?")[0];
    switch (hash) {
        case "#dialog":
            mountDialog(root);
            break;
        case "#settings":
            mountSettings(root);
            break;
        case "#terminal":
            mountTerminal(root);
            break;
        case "#editor":
            mountEditor(root);
            break;
        default:
            mountHub(root);
    }
}

route();
window.addEventListener("hashchange", route);
