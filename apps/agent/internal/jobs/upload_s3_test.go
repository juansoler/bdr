// Clasificación de errores S3 reintentables: los backends Ceph de
// proveedores como Hetzner Object Storage devuelven esporádicamente 400
// InvalidRequest sin mensaje ni RequestID bajo carga; el PUT es idempotente
// y se puede reintentar con espera.
package jobs

import (
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func TestEsRetryableS3(t *testing.T) {
	cases := []struct {
		nombre string
		err    error
		want   bool
	}{
		{"nil", nil, false},
		// Fallos de transporte: sí se reintentan.
		{"transporte", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, true},
		{"envuelto", fmt.Errorf("subir: %w", &net.OpError{Op: "write", Err: errors.New("reset by peer")}), true},
		// 5xx del servidor S3: reintentables.
		{"internal", minio.ErrorResponse{Code: "InternalError", Message: "oops", RequestID: "tx1", StatusCode: 500}, true},
		{"slowdown", minio.ErrorResponse{Code: "SlowDown", Message: "muchas peticiones", StatusCode: 503}, true},
		// 400 con detalle: definitivos, NO reintentar.
		{"firma", minio.ErrorResponse{Code: "SignatureDoesNotMatch", Message: "firma", RequestID: "tx2", StatusCode: 403}, false},
		{"invalidarg", minio.ErrorResponse{Code: "InvalidArgument", Message: "mal", RequestID: "tx3", StatusCode: 400}, false},
		{"invalidrequest-detallado", minio.ErrorResponse{Code: "InvalidRequest", Message: "aws-chunked malformado", RequestID: "tx4", StatusCode: 400}, false},
		// El caso Hetzner: InvalidRequest sin mensaje (RequestID SIEMPRE
		// poblado en S3) → flaky, sí reintentar.
		{"invalidrequest-flaky", minio.ErrorResponse{Code: "InvalidRequest", StatusCode: 400, RequestID: "tx5"}, true},
		{"invalidrequest-flaky-con-request-id", minio.ErrorResponse{Code: "InvalidRequest", Message: "", RequestID: "tx0000090f494650ea4f46c-006b0442b1-28f0c71d-hel1-prod1-ceph4", StatusCode: 400}, true},
	}
	for _, c := range cases {
		if got := esRetryableS3(c.err); got != c.want {
			t.Errorf("esRetryableS3(%s) = %v; quiero %v", c.nombre, got, c.want)
		}
	}
}

// Los segmentos van en PUT único: el ciclo multipart de Hetzner/Ceph RGW
// falla de forma determinista a partir de ~1,3 GB de un backup.
func TestPutOptsSinMultipart(t *testing.T) {
	u := &s3UploadDest{retain: true, retainDays: 7}
	opts := u.putOpts(time.Now())
	if !opts.DisableMultipart {
		t.Fatal("putOpts debe desactivar multipart: el backend rechaza el ciclo multipart")
	}
	if opts.Mode != minio.Governance {
		t.Fatalf("Mode = %v; quiero GOVERNANCE con retención", opts.Mode)
	}
	if opts.RetainUntilDate.IsZero() {
		t.Fatal("RetainUntilDate vacío con retención configurada")
	}
}

func TestS3KeyName(t *testing.T) {
	if got := s3KeyName("backup/nodo/ct/fecha/lxc/ct104-seg0021-abc"); got != "ct104-seg0021-abc" {
		t.Fatalf("s3KeyName = %q", got)
	}
	if got := s3KeyName("manifiesto.json"); got != "manifiesto.json" {
		t.Fatalf("s3KeyName sin barra = %q", got)
	}
}

// La firma de rechazo del PUT único en Hetzner/Ceph es 400 InvalidRequest
// (con o sin mensaje): dispara el fallback a multipart.
func TestEsInvalidRequest(t *testing.T) {
	si := []error{
		minio.ErrorResponse{Code: "InvalidRequest", StatusCode: 400},
		minio.ErrorResponse{Code: "InvalidRequest", Message: "", RequestID: "tx1", StatusCode: 400},
		fmt.Errorf("%w (HTTP 400)", minio.ErrorResponse{Code: "InvalidRequest", StatusCode: 400}),
	}
	for i, err := range si {
		if !esInvalidRequest(err) {
			t.Errorf("caso %d: esInvalidRequest = false; quiero true", i)
		}
	}
	no := []error{
		nil,
		minio.ErrorResponse{Code: "InvalidArgument", Message: "mal", StatusCode: 400},
		minio.ErrorResponse{Code: "InternalError", StatusCode: 500},
	}
	for i, err := range no {
		if esInvalidRequest(err) {
			t.Errorf("caso no-%d: esInvalidRequest = true; quiero false", i)
		}
	}
}
