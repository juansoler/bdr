// El export de Next genera <ruta>.html plano; /agents debe servir agents.html
// y no caer al índice SPA (portada) hasta que el router de cliente recupere
// la página.
package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentsRouteServesAgentsHTML(t *testing.T) {
	sub := FS()
	if _, err := fs.Stat(sub, "agents.html"); err != nil {
		// El export de Next (out/agents.html) no está en el repo: se genera
		// con make web-build. En un checkout limpio (p. ej. CI) solo existe
		// el placeholder out/index.html; aquí no hay nada que verificar.
		t.Skip("export del panel no compilado en este checkout (make web-build)")
	}
	want, err := fs.ReadFile(sub, "agents.html")
	if err != nil {
		t.Fatalf("export sin agents.html: %v", err)
	}
	for _, target := range []string{"/agents", "/agents/"} {
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: código %d", target, rec.Code)
		}
		if rec.Body.String() != string(want) {
			t.Errorf("%s: no sirve agents.html (sirvió %d bytes, esperados %d)",
				target, rec.Body.Len(), len(want))
		}
	}
}

func TestRootServesIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/ : código %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("content-type: %q", rec.Header().Get("Content-Type"))
	}
}
