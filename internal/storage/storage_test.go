package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTargetURI(t *testing.T) {
	env := map[string]string{
		"BDR_S3_ACCESS_KEY": "ak",
		"BDR_S3_SECRET_KEY": "sk",
		"BDR_SMB_USER":      "nas\\svc",
		"BDR_SMB_PASSWORD":  "pw",
	}
	getenv := func(k string) string { return env[k] }

	t.Run("local", func(t *testing.T) {
		tr, err := ParseTargetURI("/srv/bdr-repo", getenv, 0)
		if err != nil {
			t.Fatalf("destino local válido rechazado: %v", err)
		}
		if _, ok := tr.(*localTransferer); !ok {
			t.Fatalf("esperaba localTransferer, got %T", tr)
		}
		_ = tr.Close()
	})

	t.Run("s3 con prefijo", func(t *testing.T) {
		tr, err := ParseTargetURI("s3://minio.local:9000/bdr-backups/sql/", getenv, 30)
		if err != nil {
			t.Fatalf("destino S3 válido rechazado: %v", err)
		}
		s, ok := tr.(*s3Transferer)
		if !ok {
			t.Fatalf("esperaba s3Transferer, got %T", tr)
		}
		if s.bucket != "bdr-backups" {
			t.Errorf("bucket = %q; esperaba bdr-backups", s.bucket)
		}
		if s.prefix != "sql/" {
			t.Errorf("prefix = %q; esperaba sql/", s.prefix)
		}
		if s.retainDays != 30 {
			t.Errorf("retainDays = %d; esperaba 30", s.retainDays)
		}
		_ = tr.Close()
	})

	t.Run("s3 sin bucket", func(t *testing.T) {
		if _, err := ParseTargetURI("s3://minio.local", getenv, 0); err == nil {
			t.Fatal("s3://endpoint sin bucket debe fallar")
		}
	})

	t.Run("s3 sin credenciales", func(t *testing.T) {
		if _, err := ParseTargetURI("s3://minio.local:9000/bdr", func(string) string { return "" }, 0); err == nil {
			t.Fatal("S3 sin BDR_S3_*_KEY debe fallar")
		}
	})

	t.Run("smb sin usuario", func(t *testing.T) {
		if _, err := ParseTargetURI("//nas/backup", func(string) string { return "" }, 0); err == nil {
			t.Fatal("SMB sin BDR_SMB_USER debe fallar (antes de intentar conexión)")
		}
	})

	t.Run("vacío", func(t *testing.T) {
		if _, err := ParseTargetURI("", getenv, 0); err == nil {
			t.Fatal("destino vacío debe fallar")
		}
	})
}

func TestS3PrefixYKey(t *testing.T) {
	if got := s3Prefix("sql"); got != "sql/" {
		t.Errorf("s3Prefix(sql) = %q; esperaba sql/", got)
	}
	if got := s3Prefix("/sql/"); got != "sql/" {
		t.Errorf("s3Prefix(/sql/) = %q; esperaba sql/", got)
	}
	if got := s3Prefix(""); got != "" {
		t.Errorf("s3Prefix(vacío) = %q; esperaba vacío", got)
	}

	s := &s3Transferer{bucket: "bdr", prefix: "sql/"}
	k, err := s.s3Key("/staging/app_FULL_2026.bak")
	if err != nil {
		t.Fatalf("s3Key: %v", err)
	}
	if k != "sql/app_FULL_2026.bak" {
		t.Errorf("s3Key = %q", k)
	}
	if _, err := s.s3Key(".."); err == nil {
		t.Error("s3Key(..) debe fallar")
	}
}

func TestS3UriToKeyGuardas(t *testing.T) {
	s := &s3Transferer{bucket: "bdr", prefix: "sql/"}

	k, err := s.uriToKey("s3://bdr/sql/app.bak")
	if err != nil || k != "sql/app.bak" {
		t.Fatalf("uriToKey válida: %q, %v", k, err)
	}

	if _, err := s.uriToKey("s3://otro-bucket/sql/app.bak"); err == nil {
		t.Error("bucket distinto debe rechazarse")
	}
	if _, err := s.uriToKey("s3://bdr/otro/app.bak"); err == nil {
		t.Error("clave fuera del prefijo debe rechazarse")
	}
	// prefijo vacío: cualquier clave del bucket vale
	s2 := &s3Transferer{bucket: "bdr"}
	if _, err := s2.uriToKey("s3://bdr/raiz/app.bak"); err != nil {
		t.Errorf("sin prefijo no debe filtrar: %v", err)
	}
}

func TestSMBJoin(t *testing.T) {
	if got := smbJoin("", "sql/", "app.bak"); got != "sql/app.bak" {
		t.Errorf("smbJoin = %q", got)
	}
	if got := smbJoin("root"); got != "root" {
		t.Errorf("smbJoin = %q", got)
	}
	if got := smbJoin("", ""); got != "" {
		t.Errorf("smbJoin vacío = %q", got)
	}
}

func TestLocalTransferRoundtrip(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	staging := t.TempDir()
	src := filepath.Join(staging, "app_FULL_1.bak")
	if err := os.WriteFile(src, []byte("contenido-de-backup"), 0o640); err != nil {
		t.Fatalf("crear src: %v", err)
	}

	tr := NewTransferer(Repository{Type: "nas", Path: repo})
	uri, err := tr.Transfer(context.Background(), src)
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	if uri != filepath.Join(repo, "app_FULL_1.bak") {
		t.Fatalf("uri = %s", uri)
	}
	data, err := os.ReadFile(uri)
	if err != nil || string(data) != "contenido-de-backup" {
		t.Fatalf("contenido destino: %q, %v", data, err)
	}
	// escritura atómica: no queda residual .bdr-tmp
	if ents, _ := os.ReadDir(repo); len(ents) != 1 {
		t.Fatalf("el repo debe tener solo el objeto final: %d entradas", len(ents))
	}

	lst, err := tr.List(context.Background(), "app_")
	if err != nil || len(lst) != 1 {
		t.Fatalf("List: %v entradas=%v", err, lst)
	}
	if err := tr.Cleanup(context.Background(), uri); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(uri); !os.IsNotExist(err) {
		t.Error("Cleanup no eliminó el objeto")
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// La interfaz debe seguir cumpliéndose por los tres backends.
func TestBackendsImplementanTransferer(t *testing.T) {
	var _ Transferer = &localTransferer{}
	var _ Transferer = &s3Transferer{}
	var _ Transferer = &smbTransferer{}
}

func TestParseTargetURIEnvsDocumentados(t *testing.T) {
	// Los nombres de variables de entorno son contrato del lab: si cambian,
	// este test debe actualizarse junto con la doc del comando.
	for _, k := range []string{"BDR_S3_ACCESS_KEY", "BDR_S3_SECRET_KEY", "BDR_S3_REGION", "BDR_S3_NO_TLS", "BDR_SMB_USER", "BDR_SMB_PASSWORD", "BDR_SMB_DOMAIN"} {
		if !strings.HasPrefix(k, "BDR_") {
			t.Fatalf("variable %s sin prefijo BDR_", k)
		}
	}
}
