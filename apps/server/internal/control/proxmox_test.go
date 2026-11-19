package control_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juansoler/bdr/apps/server/internal/control"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"
)

// newProxmoxTestAPI monta la API con caja de secretos.
func newProxmoxTestAPI(t *testing.T) *httptest.Server {
	t.Helper()
	api := control.New(store.NewMemory(), adminToken, logging.NewWithWriter(io.Discard, "error"), "test")
	api.Box = testBox(t)
	ts := httptest.NewServer(api.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// fakePVE devuelve un servidor que simula la API de Proxmox.
func fakePVE(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/cluster/resources" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"vmid": 100, "name": "debian", "node": "pve", "status": "stopped", "type": "qemu", "maxmem": 2147483648, "maxdisk": 34359738368, "maxcpu": 2},
			{"vmid": 105, "name": "win11", "node": "pve", "status": "running", "type": "qemu", "maxmem": 8589934592, "maxdisk": 34359738368, "maxcpu": 4},
		}})
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestProxmoxInstancesCRUD(t *testing.T) {
	pve := fakePVE(t)
	ts := newProxmoxTestAPI(t)

	// Creación con URL no-https → 400.
	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/proxmox/instances", adminToken,
		contracts.PVEInstanceRequest{Name: "pve", URL: "http://203.0.113.10:8006", TokenUser: "root@pam", TokenID: "bdr", TokenSecret: "s3cret"})
	if status != http.StatusBadRequest {
		t.Fatalf("url http: %d %s", status, body)
	}

	// Creación válida → 201.
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/proxmox/instances", adminToken,
		contracts.PVEInstanceRequest{Name: "pve-casa", URL: pve.URL, TokenUser: "root@pam", TokenID: "bdr", TokenSecret: "s3cret"})
	if status != http.StatusCreated {
		t.Fatalf("crear instancia: %d %s", status, body)
	}
	var created contracts.PVEVMList
	_ = json.Unmarshal(body, &created)

	// Listado: sin secretos en el cuerpo.
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/proxmox/instances", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("listar: %d", status)
	}
	if strings.Contains(string(body), "s3cret") {
		t.Fatal("el listado no debe incluir el secreto")
	}

	// Inventario de VMs vía la API falsa.
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/proxmox/instances/"+created.InstanceID+"/vms", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("vms: %d %s", status, body)
	}
	var vms contracts.PVEVMList
	_ = json.Unmarshal(body, &vms)
	if len(vms.Items) != 2 || vms.Items[0].VMID != 100 || vms.Items[1].Name != "win11" {
		t.Fatalf("inventario inesperado: %+v", vms.Items)
	}

	// Borrado y 404 posterior.
	status, _ = doJSON(t, http.MethodDelete, ts.URL+"/api/v1/admin/proxmox/instances/"+created.InstanceID, adminToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete: %d", status)
	}
	status, _ = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/proxmox/instances/"+created.InstanceID+"/vms", adminToken, nil)
	if status != http.StatusNotFound {
		t.Fatalf("vms tras delete: esperaba 404, got %d", status)
	}
}

// TestProxmoxUnreachable: instancia apuntando a un puerto cerrado → 502.
func TestProxmoxUnreachable(t *testing.T) {
	ts := newProxmoxTestAPI(t)
	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/proxmox/instances", adminToken,
		contracts.PVEInstanceRequest{Name: "pve-muerto", URL: "https://127.0.0.1:9", TokenUser: "root@pam", TokenID: "bdr", TokenSecret: "x", VerifyTLS: true})
	if status != http.StatusCreated {
		t.Fatalf("crear instancia muerta: %d", status)
	}
	var created contracts.PVEVMList
	_ = json.Unmarshal(body, &created)
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/proxmox/instances/"+created.InstanceID+"/vms", adminToken, nil)
	if status != http.StatusBadGateway || !strings.Contains(string(body), "E_PVE_UNREACHABLE") {
		t.Fatalf("instancia inalcanzable: esperaba 502, got %d %s", status, body)
	}
}
