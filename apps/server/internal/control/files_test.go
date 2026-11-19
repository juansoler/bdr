package control_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/control"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"
)

// fakeDirLister es un snapshot en memoria para probar búsqueda y ZIP sin kopia.
type fakeDirLister struct {
	dirs  map[string][]contracts.FileEntry // ruta relativa → entradas
	opens int
}

func (f *fakeDirLister) ListDir(_ context.Context, objPath string) ([]contracts.FileEntry, error) {
	rel := strings.TrimPrefix(objPath, "root")
	entries, ok := f.dirs[rel]
	if !ok {
		return nil, errStr("no existe")
	}
	return entries, nil
}

func (f *fakeDirLister) OpenObject(_ context.Context, objPath string) (io.ReadCloser, error) {
	f.opens++
	rel := strings.TrimPrefix(objPath, "root")
	return io.NopCloser(strings.NewReader("contenido:" + rel)), nil
}

// filesEnv monta la API de administración con un dirLister falso.
func newFilesEnv(t *testing.T, dl *fakeDirLister) *httptest.Server {
	t.Helper()
	st := store.NewMemory()
	api := control.New(st, adminToken, logging.NewWithWriter(io.Discard, "error"), "test")
	api.DirLister = dl
	now := time.Now().UTC()
	err := st.Update(context.Background(), func(s *store.State) error {
		s.RestorePoints["rp1"] = &store.RestorePoint{
			ID: "rp1", AgentID: "a1", JobID: "j1", Kind: "file",
			SnapshotID: "k1234", RootObjectID: "root", SourcePath: "/data",
			CreatedAt: now, StartTime: now, EndTime: now, VerifyState: "pending",
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(api.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func sampleTree() *fakeDirLister {
	return &fakeDirLister{dirs: map[string][]contracts.FileEntry{
		"": {
			{Name: "archivo.txt", Type: "file", Size: 20},
			{Name: "sub", Type: "dir", Files: 1},
		},
		"/sub": {
			{Name: "interno.txt", Type: "file", Size: 8},
			{Name: "profundo", Type: "dir", Files: 0},
		},
		"/sub/profundo": {
			{Name: "ultimo.log", Type: "file", Size: 3},
		},
	}}
}

func TestFileSearch(t *testing.T) {
	dl := sampleTree()
	ts := newFilesEnv(t, dl)

	// Coincidencia recursiva con ruta completa.
	status, body := doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/restore-points/rp1/files/search?q=interno", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("search: %d %s", status, body)
	}
	var out contracts.FileSearchResult
	_ = json.Unmarshal(body, &out)
	if len(out.Items) != 1 || out.Items[0].Path != "sub/interno.txt" {
		t.Fatalf("resultados: %+v", out.Items)
	}
	if out.Truncated {
		t.Fatal("no debía truncarse")
	}

	// Sin q → 400.
	status, _ = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/restore-points/rp1/files/search", adminToken, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("sin q: %d", status)
	}

	// Búsqueda desde una subcarpeta.
	status, body = doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/restore-points/rp1/files/search?q=.txt&path=sub", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("search en sub: %d %s", status, body)
	}
	_ = json.Unmarshal(body, &out)
	if len(out.Items) != 1 || out.Items[0].Path != "sub/interno.txt" {
		t.Fatalf("resultados en sub: %+v", out.Items)
	}
}

func TestFileDownloadZip(t *testing.T) {
	dl := sampleTree()
	ts := newFilesEnv(t, dl)

	status, body := doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/restore-points/rp1/files/download?path=sub", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("download: %d %s", status, body)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	if len(zr.File) != 2 {
		t.Fatalf("esperaba 2 ficheros en el zip, got %d: %v", len(zr.File), zr.File)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["interno.txt"] || !names["profundo/ultimo.log"] {
		t.Fatalf("ficheros del zip: %v", names)
	}
	data := make([]byte, 64)
	rc, err := zr.Open("interno.txt")
	if err != nil {
		t.Fatalf("abrir interno.txt del zip: %v", err)
	}
	n, _ := rc.Read(data)
	rc.Close()
	if !strings.HasPrefix(string(data[:n]), "contenido:") {
		t.Fatalf("contenido inesperado: %q", string(data[:n]))
	}
}

func TestFileWalkLimits(t *testing.T) {
	// Un árbol más profundo que el límite se trunca sin colgar.
	dl := &fakeDirLister{dirs: map[string][]contracts.FileEntry{}}
	dl.dirs[""] = []contracts.FileEntry{{Name: "d1", Type: "dir"}}
	for i := 1; i < 20; i++ {
		dl.dirs[relPath(i)] = []contracts.FileEntry{{Name: "d" + strconv.Itoa(i+1), Type: "dir"}}
	}
	ts := newFilesEnv(t, dl)
	status, body := doJSON(t, http.MethodGet, ts.URL+"/api/v1/admin/restore-points/rp1/files/search?q=d", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("search profunda: %d %s", status, body)
	}
	var out contracts.FileSearchResult
	_ = json.Unmarshal(body, &out)
	if !out.Truncated {
		t.Fatal("esperaba truncado en un árbol más profundo que el límite")
	}
}

func relPath(i int) string { return "/" + joinRelChain(i) }

func joinRelChain(i int) string {
	s := "d1"
	for j := 2; j <= i; j++ {
		s += "/d" + strconv.Itoa(j)
	}
	return s
}
