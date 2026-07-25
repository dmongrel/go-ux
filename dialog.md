# `dialog` package

Import path: `go-ux/dialog`

A Wails v3 `Service` for modal dialogs: `ShowInfo`/`ShowError` use Wails'
native `app.Dialog` API directly (no custom rendering); `ShowCustom` opens a
real window rendering an arbitrary label+input property form, and
`ShowImageGrid` opens a real window rendering an image-only thumbnail grid —
both for cases Wails has no native equivalent for. Register the Service and
its companion frontend views (`uxdemo/frontend/src/views/dialog.ts`,
`imagegrid.ts`) to use it — see `CLAUDE.md`'s "Known limitation: frontend
distribution".

## Public API (Go)

```go
func NewService(app *application.App) *Service

func (s *Service) ShowInfo(title string, message string)
func (s *Service) ShowError(title string, message string)
func (s *Service) ShowCustom(spec CustomDialogSpec) map[string]any
func (s *Service) ShowImageGrid(spec ImageGridSpec) string

// bound methods a Custom dialog's own window calls back into — not meant
// to be called directly by a host app
func (s *Service) GetSpec(id string) CustomDialogSpec
func (s *Service) Submit(id string, result map[string]any)
func (s *Service) CancelDialog(id string)

// bound methods an ImageGrid dialog's own window calls back into — not
// meant to be called directly by a host app
func (s *Service) GetImageGridSpec(id string) ImageGridSpec
func (s *Service) SelectImage(id string, key string)
func (s *Service) CancelImageGrid(id string)
```

`ShowInfo`/`ShowError` are fire-and-forget — Wails' native dialog blocks its
own OS-level modal loop, not the calling goroutine.

`ShowCustom` blocks the calling goroutine until the user clicks a button or
closes the window (same contract the original Fyne `Dialog.Show` had) — so
call it from a goroutine other than the one running `app.Run()`. It opens a
window whose frontend (`dialog.ts`) fetches `spec` via `GetSpec`, renders the
property form, and calls `Submit`/`CancelDialog` on OK/Cancel/close, which is
what lets the blocked `ShowCustom` call return. For OK, the result is a map
keyed by each `Property`'s `Key`, typed per its `PropertyKind` (`bool`/
`string`/`int`, or `[]string` for `list`/`multiSelect`; `dropdown` is
`string`). Cancel and closing the window both return `nil`.

```go
type CustomDialogSpec struct {
	Title      string
	Buttons    []ButtonKind      // defaults to []ButtonKind{ButtonOK} if empty
	Properties []Property
	Width      int               // defaults to 800 if <= 0
	Height     int               // defaults to 800 if <= 0
}

type Property struct {
	Key      string
	Label    string
	Kind     PropertyKind
	Initial  []string // PropertyList seed items; Initial[0] pre-fills PropertyTextField
	Options  []string // PropertyDropdown/PropertyMultiSelect choice set
	Selected []string // PropertyDropdown/PropertyMultiSelect pre-selection
}
```

## Minimal usage

```go
app := application.New(application.Options{ /* ... */ })
app.RegisterService(application.NewService(dialog.NewService(app)))
```

```ts
// hub.ts or wherever
import {ShowInfo, ShowCustom} from "../../bindings/go-ux/dialog/service";

ShowInfo("Info", "This is a native Wails info dialog.");

const result = await ShowCustom({
    Title: "Preferences",
    Buttons: ["OK", "Cancel"],
    Properties: [
        {Key: "enabled", Label: "Enabled", Kind: "bool", Initial: [], Options: [], Selected: []},
        {Key: "mode", Label: "Mode", Kind: "dropdown", Initial: [], Options: ["fast", "balanced", "thorough"], Selected: ["balanced"]},
    ],
});
```

`ShowImageGrid` has the same blocking contract as `ShowCustom` — call it off
`app.Run()`'s goroutine. It opens a window whose frontend (`imagegrid.ts`)
fetches `spec` via `GetImageGridSpec`, renders each `Option` as a clickable
image-only thumbnail (no labels — matches the original Fyne parchment-
texture picker this replaces), and calls `SelectImage` on click or
`CancelImageGrid`/closes the window to cancel. Returns the clicked option's
`Key`, or `""` if cancelled/closed without a selection.

```go
type ImageGridSpec struct {
	Title    string
	Options  []ImageOption
	Selected string // currently-selected Key, highlighted; "" if none
	Width    int    // defaults to 480 if <= 0
	Height   int    // defaults to 400 if <= 0
}

type ImageOption struct {
	Key       string // identifies the option; not shown to the user
	ImageData []byte // raw image bytes (JPEG/PNG/etc.)
}
```

`ImageData` crosses the Go↔JS boundary as a base64 string — Wails' JSON
binding encodes a Go `[]byte` field that way automatically both directions,
so a caller passes raw bytes in Go and the frontend gets a base64 string it
can drop straight into a `data:` URI. See `imagegrid.ts`'s own comment on
why it hardcodes `image/png` in that URI regardless of the option's actual
encoding (WebView2 sniffs the real format from the bytes for `<img>` tags).

## Property kinds

| `PropertyKind`  | Frontend control       | Result type | Notes                                                                  |
|------------------|-------------------------|--------------|-------------------------------------------------------------------------|
| `label`          | text only               | (none)       | Not included in the result map.                                        |
| `bool`           | checkbox                | `bool`       |                                                                          |
| `textField`      | text input              | `string`     | Pre-filled from `Initial[0]` if present.                               |
| `int`            | number input            | `int`        |                                                                          |
| `list`           | editable string list    | `[]string`   | Seeded via `Initial`; add/remove edits the list client-side.           |
| `dropdown`       | select                  | `string`     | Closed set of `Options`, `Selected[0]` is the pre-chosen value.        |
| `multiSelect`    | checkbox group          | `[]string`   | Any subset of `Options`, pre-chosen via `Selected`.                    |

## Constraints for callers

- `ShowCustom` must be called off the goroutine running `app.Run()` — it
  blocks on an internal channel until the dialog window resolves.
- `Buttons` accepts at most two entries in practice (`ButtonOK`/
  `ButtonCancel`) — the frontend renders one button per entry, in order.
- `ShowCustom`'s window-close handling and an explicit `Submit`/
  `CancelDialog` call can race (e.g. `Submit` closing the window itself also
  fires the close hook) — only the first resolution wins, the rest are
  silent no-ops (see `service.go`'s `resolve`).
- `ShowImageGrid` must likewise be called off `app.Run()`'s goroutine, and
  has the identical `SelectImage`/`CancelImageGrid`-vs-window-close race
  guard (see `service.go`'s `resolveImageGrid`).
