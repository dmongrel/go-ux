// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

//go:build windows

package cpucount

import (
	"runtime"
	"testing"
)

func TestCountReturnsPositiveAndAtMostLogicalCount(t *testing.T) {
	n, err := Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n <= 0 {
		t.Fatalf("Count() = %d, want > 0", n)
	}
	if logical := runtime.NumCPU(); n > logical {
		t.Errorf("Count() = %d physical cores, want <= %d logical processors (runtime.NumCPU)", n, logical)
	}
}

func TestCountIsStableAcrossCalls(t *testing.T) {
	first, err := Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	second, err := Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if first != second {
		t.Errorf("Count() not stable across calls: %d then %d", first, second)
	}
}
