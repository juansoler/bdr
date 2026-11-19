package inventory

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"
)

func TestCollect(t *testing.T) {
	inv, err := Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if inv.Hostname == "" {
		t.Fatal("hostname vacío")
	}
	if inv.CPUCores < 1 {
		t.Fatalf("cpu_cores inválido: %d", inv.CPUCores)
	}
	if inv.OSFamily != runtime.GOOS {
		t.Fatalf("os_family %q != %q", inv.OSFamily, runtime.GOOS)
	}
	if inv.AgentVersion == "" {
		t.Fatal("agent_version vacío")
	}
	if inv.CollectedAt.IsZero() {
		t.Fatal("collected_at vacío")
	}
	if len(inv.Network) == 0 {
		t.Fatal("network vacío")
	}
}

func TestFlexTypes(t *testing.T) {
	var v struct {
		S flexString `json:"s"`
		N flexInt64  `json:"n"`
		B flexBool   `json:"b"`
	}
	payload := []byte(`{"s":"sda","n":"1024","b":"1"}`)
	if err := json.Unmarshal(payload, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v.S.String() != "sda" || v.N.Int64() != 1024 || !v.B.Bool() {
		t.Fatalf("valores inesperados: %+v", v)
	}

	payload = []byte(`{"s":42,"n":512,"b":null}`)
	if err := json.Unmarshal(payload, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v.S.String() != "42" || v.N.Int64() != 512 || v.B.Bool() {
		t.Fatalf("valores inesperados: %+v", v)
	}
}
