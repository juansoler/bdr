package control_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
)

// El flujo completo: reconexión conserva la identidad del agente (mismo ID,
// misma fecha de alta, planes intactos), rota la API key y el token es de
// un solo uso igual que los de alta.
func TestAgentReconnect(t *testing.T) {
	ts := newTestAPI(t)

	enrolled := enrollAgent(t, ts, createToken(t, ts, "lab-01").Token, "lab-01")

	// Plan asignado al agente: debe sobrevivir a la reconexión.
	plan := createPlan(t, ts, "diario")
	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/backup-plans/"+plan.ID+"/assign", adminToken,
		contracts.AssignmentRequest{AgentID: enrolled.AgentID})
	if status != http.StatusCreated {
		t.Fatalf("assign: %d %s", status, body)
	}

	// Ficha antes de la reconexión (para comparar la fecha de alta).
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/agents/"+enrolled.AgentID, adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("detail: %d %s", status, body)
	}
	var before contracts.AgentDetail
	if err := json.Unmarshal(body, &before); err != nil {
		t.Fatal(err)
	}

	// Token de reconexión.
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/agents/"+enrolled.AgentID+"/reconnect-token", adminToken, nil)
	if status != http.StatusCreated {
		t.Fatalf("reconnect-token: %d %s", status, body)
	}
	var rt contracts.ReconnectTokenResponse
	if err := json.Unmarshal(body, &rt); err != nil {
		t.Fatal(err)
	}
	if rt.Token == "" || rt.AgentID != enrolled.AgentID {
		t.Fatalf("reconnect-token incompleto: %+v", rt)
	}
	if !strings.Contains(rt.OneLiner, "/api/v1/download/installer/linux-amd64/") {
		t.Fatalf("one-liner sin instalador linux: %q", rt.OneLiner)
	}
	if rt.ExpiresAt.Sub(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)) != 15*time.Minute {
		t.Fatalf("TTL del token de reconexión: %s", rt.ExpiresAt)
	}

	// Reconexión: misma identidad, nueva API key, 200 (no 201).
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/agent/enroll", "", contracts.EnrollRequest{
		Token: rt.Token, Hostname: "lab-01", OSFamily: "linux", Arch: "amd64",
	})
	if status != http.StatusOK {
		t.Fatalf("reconnect enroll: %d %s", status, body)
	}
	var re contracts.EnrollResponse
	if err := json.Unmarshal(body, &re); err != nil {
		t.Fatal(err)
	}
	if re.AgentID != enrolled.AgentID {
		t.Fatalf("agentID distinto: %q vs %q", re.AgentID, enrolled.AgentID)
	}
	if re.APIKey == "" || re.APIKey == enrolled.APIKey {
		t.Fatal("la API key no se rotó")
	}

	// La API key vieja queda invalidada; la nueva funciona.
	status, _ = doJSON(t, http.MethodPost, ts.URL+"/api/v1/agent/heartbeat", enrolled.APIKey,
		contracts.HeartbeatRequest{AgentVersion: "1", State: contracts.StateIdle})
	if status != http.StatusUnauthorized {
		t.Fatalf("heartbeat con API key vieja: %d", status)
	}
	status, _ = doJSON(t, http.MethodPost, ts.URL+"/api/v1/agent/heartbeat", re.APIKey,
		contracts.HeartbeatRequest{AgentVersion: "1", State: contracts.StateIdle})
	if status != http.StatusOK {
		t.Fatalf("heartbeat con API key nueva: %d", status)
	}

	// Planes y fecha de alta intactos; hostname actualizado si cambia.
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/agents/"+enrolled.AgentID, adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("detail tras reconnect: %d %s", status, body)
	}
	var after contracts.AgentDetail
	if err := json.Unmarshal(body, &after); err != nil {
		t.Fatal(err)
	}
	if !after.Agent.EnrolledAt.Equal(before.Agent.EnrolledAt) {
		t.Fatalf("enrolled_at cambió: %s vs %s", after.Agent.EnrolledAt, before.Agent.EnrolledAt)
	}
	if len(after.EffectivePlans) != 1 || after.EffectivePlans[0].Plan.ID != plan.ID {
		t.Fatalf("planes tras reconnect: %+v", after.EffectivePlans)
	}

	// El token de reconexión es de un solo uso.
	status, _ = doJSON(t, http.MethodPost, ts.URL+"/api/v1/agent/enroll", "", contracts.EnrollRequest{
		Token: rt.Token, Hostname: "lab-01", OSFamily: "linux",
	})
	if status != http.StatusGone {
		t.Fatalf("reutilización del token de reconexión: %d, quiero 410", status)
	}

	// El listado de tokens refleja el agente que lo consumió.
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/enrollment-tokens", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("tokens: %d", status)
	}
	var toks contracts.EnrollmentTokenList
	if err := json.Unmarshal(body, &toks); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range toks.Items {
		if item.ID == rt.ID {
			found = true
			if item.UsedByAgentID != enrolled.AgentID {
				t.Fatalf("UsedByAgentID = %q, quiero %q", item.UsedByAgentID, enrolled.AgentID)
			}
		}
	}
	if !found {
		t.Fatal("token de reconexión no aparece en el listado")
	}

	// No se creó un agente nuevo.
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/agents", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("agents: %d", status)
	}
	var list contracts.AgentList
	_ = json.Unmarshal(body, &list)
	if len(list.Items) != 1 {
		t.Fatalf("agentes tras reconnect: %d, quiero 1 (sin duplicados)", len(list.Items))
	}
}

// Agente desconocido y agente revocado no admiten token de reconexión.
func TestAgentReconnectGuards(t *testing.T) {
	ts := newTestAPI(t)

	status, _ := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/agents/no-existe/reconnect-token", adminToken, nil)
	if status != http.StatusNotFound {
		t.Fatalf("reconnect-token de agente inexistente: %d, quiero 404", status)
	}

	enrolled := enrollAgent(t, ts, createToken(t, ts, "lab-02").Token, "lab-02")
	status, _ = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/agents/"+enrolled.AgentID+"/revoke", adminToken,
		contracts.RevokeRequest{Reason: "prueba"})
	if status != http.StatusNoContent {
		t.Fatalf("revoke: %d", status)
	}
	status, body := doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/agents/"+enrolled.AgentID+"/reconnect-token", adminToken, nil)
	if status != http.StatusForbidden {
		t.Fatalf("reconnect-token de agente revocado: %d %s, quiero 403", status, body)
	}

	// Un token creado antes de la revoción tampoco sirve para reenlazar.
	enrolled2 := enrollAgent(t, ts, createToken(t, ts, "lab-03").Token, "lab-03")
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/agents/"+enrolled2.AgentID+"/reconnect-token", adminToken, nil)
	if status != http.StatusCreated {
		t.Fatalf("reconnect-token: %d %s", status, body)
	}
	var rt contracts.ReconnectTokenResponse
	_ = json.Unmarshal(body, &rt)
	status, _ = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/agents/"+enrolled2.AgentID+"/revoke", adminToken,
		contracts.RevokeRequest{Reason: "prueba"})
	if status != http.StatusNoContent {
		t.Fatalf("revoke 2: %d", status)
	}
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/agent/enroll", "", contracts.EnrollRequest{
		Token: rt.Token, Hostname: "lab-03", OSFamily: "linux",
	})
	if status != http.StatusForbidden {
		t.Fatalf("reconnect de agente revocado: %d %s, quiero 403", status, body)
	}
}

// El instalador descargado con un token de RECONEXIÓN no lleva el guard
// anti-duplicados (el equipo ya tiene config: es su propósito); el de alta
// sí lo lleva.
func TestInstallerReconnectFlavor(t *testing.T) {
	ts := newTestAPI(t)

	enrolled := enrollAgent(t, ts, createToken(t, ts, "lab-04").Token, "lab-04")

	// Alta: guard presente.
	tok := createToken(t, ts, "lab-05")
	status, body := doJSON(t, http.MethodGet, ts.URL+"/api/v1/download/installer/linux-amd64/"+tok.Token, adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("installer alta: %d %s", status, body)
	}
	if !strings.Contains(string(body), "BDR_FORCE_REENROLL") {
		t.Fatal("installer de alta sin guard anti-duplicados")
	}
	if strings.Contains(string(body), "Reconexion BDR") {
		t.Fatal("installer de alta marcado como reconexión")
	}

	// Reconexión: sin guard, cabecera de reconexión.
	status, body = doJSON(t, http.MethodPost, ts.URL+"/api/v1/admin/agents/"+enrolled.AgentID+"/reconnect-token", adminToken, nil)
	if status != http.StatusCreated {
		t.Fatalf("reconnect-token: %d %s", status, body)
	}
	var rt contracts.ReconnectTokenResponse
	_ = json.Unmarshal(body, &rt)
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/download/installer/linux-amd64/"+rt.Token, adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("installer reconnect: %d %s", status, body)
	}
	if strings.Contains(string(body), "BDR_FORCE_REENROLL") {
		t.Fatal("installer de reconexión lleva el guard anti-duplicados")
	}
	if !strings.Contains(string(body), "Reconexion BDR") {
		t.Fatal("installer de reconexión sin cabecera de reconexión")
	}
}
