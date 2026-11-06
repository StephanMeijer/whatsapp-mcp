// SPDX-License-Identifier: MPL-2.0

package main

import (
	"os"
	"testing"
)

func TestRunHelpFlagIsNotAnError(t *testing.T) {
	orig := os.Args
	t.Cleanup(func() { os.Args = orig })
	os.Args = []string{"whatsapp-mcp", "-h"}
	if err := run(); err != nil {
		t.Fatalf("run() with -h = %v, want nil", err)
	}
}
