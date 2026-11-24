// Clasificación de errores S3 reintentables: los backends Ceph de
// proveedores como Hetzner Object Storage devuelven esporádicamente 400
// InvalidRequest sin mensaje ni RequestID bajo carga; el PUT es idempotente
// y se puede reintentar con espera.
package jobs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
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

// El transporte de diagnóstico guarda la respuesta del backend (cuerpo XML
// incluido) y los headers relevantes, sin la firma.
func TestS3RecordTransport(t *testing.T) {
	rec := &s3Recorder{}
	tr := &s3RecordTransport{
		recorder: rec,
		base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 400,
				Header:     http.Header{"X-Amz-Request-Id": {"tx1"}},
				Body: io.NopCloser(strings.NewReader(
					`<Error><Code>InvalidRequest</Code><Message></Message></Error>`)),
			}, nil
		}),
	}
	req, err := http.NewRequest(http.MethodPut,
		"https://hel1.your-objectstorage.com/b/backup/manifest.json?X-Amz-Signature=secreto", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Amz-Object-Lock-Mode", "GOVERNANCE")
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=AK/secreto")
	req.ContentLength = 2048

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(resp.Body); len(b) == 0 {
		t.Fatal("el cuerpo de la respuesta debe seguir disponible para el SDK")
	}
	got := rec.get()
	for _, quiero := range []string{"→ 400", "Content-Length=2048", "GOVERNANCE", "InvalidRequest", "request-id=tx1", "/b/backup/manifest.json"} {
		if !strings.Contains(got, quiero) {
			t.Errorf("detalle sin %q: %s", quiero, got)
		}
	}
	for _, prohibido := range []string{"secreto", "Authorization", "X-Amz-Signature"} {
		if strings.Contains(got, prohibido) {
			t.Errorf("detalle filtra %q: %s", prohibido, got)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fakeS3 rechaza todo PUT simple con 400 InvalidRequest y acepta el ciclo
// multipart: el comportamiento observado en Hetzner Object Storage. Con
// completaFallo simula la carrera de Ceph RGW: el complete responde 500
// "This multipart completion is already in progress" pero el objeto queda
// materializado en segundo plano (a menos que oculteObjeto).
type fakeS3 struct {
	mu            sync.Mutex
	simples       int
	inicios       int
	completa      int
	completaFallo bool
	ocultaObjeto  bool
	partes        map[string]int64 // uploadId → bytes acumulados
	objs          map[string]int64 // clave → tamaño
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.partes == nil {
		f.partes = map[string]int64{}
		f.objs = map[string]int64{}
	}
	q := r.URL.Query()
	clave := strings.TrimPrefix(r.URL.Path, "/backupbdr/")
	switch {
	case r.Method == http.MethodPut && q.Get("partNumber") != "":
		// minio-go envía las partes con encoding aws-chunked: el tamaño real
		// es el del primer chunk (hex), no el Content-Length enmarcado.
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		n := int64(len(body))
		if i := bytes.Index(body, []byte(";chunk-signature=")); i > 0 {
			if v, err := strconv.ParseInt(string(body[:i]), 16, 64); err == nil {
				n = v
			}
		}
		f.mu.Lock()
		f.partes[q.Get("uploadId")] += n
		f.mu.Unlock()
		w.Header().Set("ETag", `"e1"`)
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPut:
		f.mu.Lock()
		f.simples++
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>InvalidRequest</Code><Message></Message><RequestId>tx-test</RequestId><HostId>host</HostId></Error>`)
	case r.Method == http.MethodPost && q.Has("uploads"):
		f.mu.Lock()
		f.inicios++
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<InitiateMultipartUploadResult><Bucket>backupbdr</Bucket><Key>k</Key><UploadId>u1</UploadId></InitiateMultipartUploadResult>`)
	case r.Method == http.MethodPost:
		f.mu.Lock()
		f.completa++
		fallo := f.completaFallo
		tam := f.partes[q.Get("uploadId")]
		if !fallo || !f.ocultaObjeto {
			f.objs[clave] = tam
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		if fallo {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `<Error><Code>InternalError</Code><Message>This multipart completion is already in progress</Message></Error>`)
			return
		}
		_, _ = io.WriteString(w, `<CompleteMultipartUploadResult><Bucket>backupbdr</Bucket><Key>k</Key><ETag>"e1"</ETag></CompleteMultipartUploadResult>`)
	case r.Method == http.MethodHead:
		f.mu.Lock()
		tam, ok := f.objs[clave]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", strconv.FormatInt(tam, 10))
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.Header().Set("ETag", `"e1"`)
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && q.Has("list-type"):
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<ListBucketResult><Name>backupbdr</Name><Prefix></Prefix><KeyCount>0</KeyCount><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated></ListBucketResult>`)
	default:
		w.WriteHeader(http.StatusOK)
	}
}

func newFakeS3DestWith(t *testing.T, fs *fakeS3) (*s3UploadDest, *fakeS3) {
	t.Helper()
	srv := httptest.NewServer(fs)
	t.Cleanup(srv.Close)
	creds := &contracts.RepositoryCredentials{
		Kind: contracts.RepoKindS3, Endpoint: strings.TrimPrefix(srv.URL, "http://"),
		Bucket: "backupbdr", Region: "hel1", AccessKey: "ak", SecretKey: "sk", NoTLS: true,
	}
	dest, err := newS3UploadDest(context.Background(), creds, "lxc", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	return dest, fs
}

func newFakeS3Dest(t *testing.T) (*s3UploadDest, *fakeS3) {
	t.Helper()
	return newFakeS3DestWith(t, &fakeS3{})
}

// El manifiesto (objeto diminuto) debe acabar subiendo por multipart cuando
// el backend rechaza el PUT simple, y el error final debe llevar el detalle
// del backend si todos los caminos fallan.
func TestPutObjectEscalaAMultipartParaObjetosPequenos(t *testing.T) {
	viejo := s3RetryDelay
	s3RetryDelay = 0
	t.Cleanup(func() { s3RetryDelay = viejo })

	dest, fs := newFakeS3Dest(t)
	err := dest.putRaw(context.Background(), dest.Prefix()+"manifest-x.json", []byte(`{"a":1}`), time.Now())
	if err != nil {
		t.Fatalf("putRaw = %v", err)
	}
	fs.mu.Lock()
	simples, inicios, completa := fs.simples, fs.inicios, fs.completa
	fs.mu.Unlock()
	if simples == 0 {
		t.Error("no se intentó el PUT simple primero")
	}
	if inicios == 0 || completa == 0 {
		t.Errorf("no se usó el multipart: inicios=%d completa=%d", inicios, completa)
	}

	// Un objeto grande fija el modo multipart para el resto del backup: el
	// siguiente no vuelve a pagar los PUT simples fallidos.
	grande := make([]byte, 9<<20)
	if _, err := dest.putSegment(context.Background(), dest.Prefix()+"ct1-seg0000", grande, time.Now()); err != nil {
		t.Fatalf("putSegment grande = %v", err)
	}
	fs.mu.Lock()
	simplesTrasGrande := fs.simples
	fs.mu.Unlock()
	if dest.uploadMode != s3ModeMultipart {
		t.Errorf("uploadMode = %d; quiero s3ModeMultipart", dest.uploadMode)
	}
	if _, err := dest.putSegment(context.Background(), dest.Prefix()+"ct1-seg0001", grande, time.Now()); err != nil {
		t.Fatalf("segundo putSegment = %v", err)
	}
	fs.mu.Lock()
	simplesFinal := fs.simples
	fs.mu.Unlock()
	if simplesFinal != simplesTrasGrande {
		t.Errorf("el segundo segmento repitió PUT simples fallidos (%d → %d)", simplesTrasGrande, simplesFinal)
	}
}

// Si ningún camino funciona, el error del panel dice qué se intentó y qué
// respondió el backend.
func TestPutObjectErrorConDiagnostico(t *testing.T) {
	viejo := s3RetryDelay
	s3RetryDelay = 0
	t.Cleanup(func() { s3RetryDelay = viejo })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("list-type") {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<ListBucketResult><Name>backupbdr</Name><KeyCount>0</KeyCount><IsTruncated>false</IsTruncated></ListBucketResult>`)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `<Error><Code>InvalidRequest</Code><Message></Message></Error>`)
	}))
	defer srv.Close()
	creds := &contracts.RepositoryCredentials{
		Kind: contracts.RepoKindS3, Endpoint: strings.TrimPrefix(srv.URL, "http://"),
		Bucket: "backupbdr", Region: "hel1", AccessKey: "ak", SecretKey: "sk", NoTLS: true,
	}
	dest, err := newS3UploadDest(context.Background(), creds, "lxc", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	err = dest.putRaw(context.Background(), dest.Prefix()+"manifest.json", []byte(`{}`), time.Now())
	if err == nil {
		t.Fatal("putRaw debía fallar")
	}
	msg := err.Error()
	for _, quiero := range []string{"manifest.json", "respuesta del backend:", "→ 400", "respuesta=", "put, put-sin-firma, put-md5, multipart"} {
		if !strings.Contains(msg, quiero) {
			t.Errorf("error sin %q: %s", quiero, msg)
		}
	}
}

// Carrera de Ceph RGW: el complete responde 500 "already in progress" (el
// SDK reintenta el POST duplicado) pero el objeto queda hecho en el backend:
// la subida debe darse por buena comprobando el objeto.
func TestPutMultipartRecuperaCompletePerdido(t *testing.T) {
	viejo := s3RetryDelay
	s3RetryDelay = 0
	t.Cleanup(func() { s3RetryDelay = viejo })
	viejoPoll := s3CompletePollDelay
	s3CompletePollDelay = 0
	t.Cleanup(func() { s3CompletePollDelay = viejoPoll })

	dest, _ := newFakeS3DestWith(t, &fakeS3{completaFallo: true})
	payload := []byte(`{"a":1}`)
	err := dest.putRaw(context.Background(), dest.Prefix()+"manifest-race.json", payload, time.Now())
	if err != nil {
		t.Fatalf("putRaw debía recuperar el complete perdido: %v", err)
	}
}

// Si el complete falla y el objeto NO aparece, el error llega al panel.
func TestPutMultipartSinObjetoFalla(t *testing.T) {
	viejo := s3RetryDelay
	s3RetryDelay = 0
	t.Cleanup(func() { s3RetryDelay = viejo })
	viejoPoll := s3CompletePollDelay
	s3CompletePollDelay = 0
	t.Cleanup(func() { s3CompletePollDelay = viejoPoll })
	viejoTries := s3CompletePollTries
	s3CompletePollTries = 2
	t.Cleanup(func() { s3CompletePollTries = viejoTries })

	dest, _ := newFakeS3DestWith(t, &fakeS3{completaFallo: true, ocultaObjeto: true})
	err := dest.putRaw(context.Background(), dest.Prefix()+"manifest-fail.json", []byte(`{}`), time.Now())
	if err == nil {
		t.Fatal("putRaw debía fallar: el objeto nunca apareció")
	}
	if !strings.Contains(err.Error(), "multipart") {
		t.Errorf("error sin el modo que falló: %v", err)
	}
}
