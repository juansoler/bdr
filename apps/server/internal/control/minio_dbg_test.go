package control_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type loggingTransport struct {
	base http.RoundTripper
	t    *testing.T
}

func (l *loggingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	l.t.Log("OUT:", r.Method, r.URL.String())
	resp, err := l.base.RoundTrip(r)
	if resp != nil {
		l.t.Log("IN:", resp.StatusCode, resp.Header.Get("Location"))
	}
	return resp, err
}

func TestDebugMinioPut(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Log("fakeS3 <-", r.Method, r.URL.String())
		w.Header().Set("x-amz-bucket-region", "us-east-1")
		w.WriteHeader(http.StatusOK)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	tr := &loggingTransport{base: http.DefaultTransport, t: t}
	mc, err := minio.New(strings.TrimPrefix(ts.URL, "http://"), &minio.Options{
		Creds:     credentials.NewStaticV4("admin", "sk-test", ""),
		Secure:    false,
		Transport: tr,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = mc.PutObject(context.Background(), "bdr-test", "verify/ok.txt",
		strings.NewReader("ok"), 2, minio.PutObjectOptions{})
	t.Logf("put err=%v", err)
}
