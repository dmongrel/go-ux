// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

//go:build windows

// Package cpucount is a helper to count a machine's actual physical CPU
// cores — not logical threads/hyperthreads. runtime.NumCPU() and a naive
// "processor count" both report logical processors, which on a
// hyperthreaded/SMT machine is roughly double the physical core count; a
// caller sizing a thread pool to actual compute resources (e.g. GoLLM
// picking a default inference thread count) wants the physical number, not
// the logical one.
package cpucount

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// relationProcessorCore is LOGICAL_PROCESSOR_RELATIONSHIP's
// RelationProcessorCore value: each SYSTEM_LOGICAL_PROCESSOR_INFORMATION_EX
// record returned for this relationship type describes exactly one physical
// core, regardless of how many logical processors (hyperthreads) that core
// exposes.
const relationProcessorCore = 0

var (
	kernel32                             = windows.NewLazySystemDLL("kernel32.dll")
	procGetLogicalProcessorInformationEx = kernel32.NewProc("GetLogicalProcessorInformationEx")
)

// Count returns the number of physical CPU cores on the current machine.
func Count() (int, error) {
	var length uint32
	r1, _, err := procGetLogicalProcessorInformationEx.Call(
		uintptr(relationProcessorCore), 0, uintptr(unsafe.Pointer(&length)),
	)
	if r1 != 0 {
		return 0, fmt.Errorf("cpucount: GetLogicalProcessorInformationEx: unexpected success sizing the buffer")
	}
	if err != windows.ERROR_INSUFFICIENT_BUFFER {
		return 0, fmt.Errorf("cpucount: GetLogicalProcessorInformationEx: size query: %w", err)
	}

	buf := make([]byte, length)
	r1, _, err = procGetLogicalProcessorInformationEx.Call(
		uintptr(relationProcessorCore), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&length)),
	)
	if r1 == 0 {
		return 0, fmt.Errorf("cpucount: GetLogicalProcessorInformationEx: %w", err)
	}

	// Each record is variable-length (a Relationship/Size header followed
	// by a relationship-specific union); Size at offset 4 is how far to
	// advance to the next one. Since every record here was requested with
	// RelationProcessorCore, one record is exactly one physical core — the
	// union's contents don't need to be parsed at all, just walked past.
	var cores int
	for offset := 0; offset < len(buf); {
		if offset+8 > len(buf) {
			return 0, fmt.Errorf("cpucount: GetLogicalProcessorInformationEx: truncated record at offset %d", offset)
		}
		size := binary.LittleEndian.Uint32(buf[offset+4 : offset+8])
		if size == 0 {
			return 0, fmt.Errorf("cpucount: GetLogicalProcessorInformationEx: zero-size record at offset %d", offset)
		}
		cores++
		offset += int(size)
	}
	return cores, nil
}
