package jobs

import (
	"fmt"
	"testing"
)

func TestDiskParamsAndSizeToG(t *testing.T) {
	p := diskParams("DATA1:vm-105-disk-1,size=64G,ssd=1,discard=on")
	if p["size"] != "64G" || p["ssd"] != "1" || p["discard"] != "on" {
		t.Fatalf("params: %v", p)
	}
	if p["volid"] != "" {
		t.Fatalf("volid no debe ser un parámetro: %v", p)
	}
	cases := map[string]string{
		"64G":  "64",
		"1T":   "1024",
		"8G":   "8",
		"512M": "1",
		"32G":  "32",
	}
	for in, want := range cases {
		if got := sizeToG(in); got != want {
			t.Errorf("sizeToG(%q) = %q, esperaba %q", in, got, want)
		}
	}
	if got := sizeToG(""); got != "" {
		t.Errorf("sizeToG vacío = %q", got)
	}
	// Argumento qm create para un disco regular con parámetros preservados.
	cfg := "DATA1:vm-105-disk-1,size=64G,ssd=1"
	params := diskParams(cfg)
	arg := fmt.Sprintf("--scsi0=DATA1:%s", sizeToG(params["size"]))
	if params["ssd"] == "1" {
		arg += ",ssd=1"
	}
	if arg != "--scsi0=DATA1:64,ssd=1" {
		t.Fatalf("arg: %q", arg)
	}
}
