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
		// El caso Hetzner: InvalidRequest sin mensaje ni RequestID → sí.
		{"invalidrequest-flaky", minio.ErrorResponse{Code: "InvalidRequest", StatusCode: 400}, true},
	}
	for _, c := range cases {
		if got := esRetryableS3(c.err); got != c.want {
			t.Errorf("esRetryableS3(%s) = %v; quiero %v", c.nombre, got, c.want)
		}
	}
}
