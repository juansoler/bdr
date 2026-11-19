//go:build !windows && !linux

package inventory

import (
	"context"
	"runtime"

	"github.com/juansoler/bdr/packages/contracts"
)

// collectPlatform implementa lo mínimo para desarrollo en macOS.
func collectPlatform(_ context.Context, inv *contracts.Inventory) {
	if inv.OSVersion == "" {
		inv.OSVersion = runtime.GOOS
	}
}
