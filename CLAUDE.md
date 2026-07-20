# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Purpose

This repository is the `ux` library: a collection of Fyne-composed dialogs and windows (`fyne.io/fyne` `app.Window`s) — e.g. a settings control panel — intended to be imported as a dependency by other Go Fyne applications.

## Research reference

When researching UX/design patterns for dialogs and control panels (e.g. settings panel layout and behavior), use the IntelliJ Community Edition source as a reference: https://github.com/jetbrains/intellij-community

## Project status

Currently a fresh scaffold: `go.mod` declares module `go-ux` on Go 1.26, but no source files, packages, tests, or tooling exist yet.

When code is added, update this file with:
- Build/test/lint commands
- Package layout and high-level architecture
