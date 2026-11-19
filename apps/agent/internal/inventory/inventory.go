// Package inventory recoge el inventario mínimo del equipo.
package inventory

import (
	"context"
	"net"
	"os"
	"runtime"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/version"
)

// Collect devuelve el inventario del equipo.
func Collect(ctx context.Context) (contracts.Inventory, error) {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	inv := contracts.Inventory{
		CollectedAt:  time.Now().UTC(),
		Hostname:     host,
		OSFamily:     runtime.GOOS,
		Arch:         runtime.GOARCH,
		CPUCores:     runtime.NumCPU(),
		AgentVersion: version.Version,
		Extra:        map[string]any{},
	}
	collectNetwork(&inv)
	collectPlatform(ctx, &inv)
	return inv, nil
}

func collectNetwork(inv *contracts.Inventory) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return
	}
	for _, ifc := range ifaces {
		ni := contracts.NetInterface{
			Name: ifc.Name,
			MAC:  ifc.HardwareAddr.String(),
			Up:   ifc.Flags&net.FlagUp != 0,
		}
		addrs, err := ifc.Addrs()
		if err == nil {
			for _, a := range addrs {
				ni.Addresses = append(ni.Addresses, a.String())
			}
		}
		inv.Network = append(inv.Network, ni)
	}
}
