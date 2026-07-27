# `cpucount` package

Import path: `github.com/dmongrel/go-ux/cpucount`

A helper to count a machine's actual physical CPU cores — not logical
threads/hyperthreads. `runtime.NumCPU()` (and a naive processor count)
reports logical processors, which on a hyperthreaded/SMT machine is
roughly double the physical core count. A caller sizing a thread pool to
actual compute resources (e.g. picking a default inference thread count)
wants the physical number, not the logical one.

Windows-only (`//go:build windows`), matching this repo's existing
convention for OS-specific helpers (`fontsettings.DetectMonospaceFonts`,
`terminal`'s winpty backend) — there is no fallback implementation for
other platforms.

## Public API

```go
func Count() (int, error)
```

Implemented via `GetLogicalProcessorInformationEx` (Win32,
`RelationProcessorCore`), which returns one record per physical core
regardless of how many logical processors (hyperthreads) that core
exposes — unlike `GetSystemInfo`/`runtime.NumCPU()`, which both count
logical processors.

## Minimal usage

```go
cores, err := cpucount.Count()
if err != nil {
    cores = runtime.NumCPU() // fall back to logical count
}
```
