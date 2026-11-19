// Package client implementa el cliente HTTP del agente.
package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/version"
)

const maxResponse = 4 << 20

// Códigos de error relevantes del catálogo de la API.
const (
	CodeRepoUnconfigured = "E_REPO_UNCONFIGURED"
)

// APIError representa un error devuelto por el servidor.
type APIError struct {
	Status int
	Code   string
	Detail string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("servidor %d (%s): %s", e.Status, e.Code, e.Detail)
	}
	return fmt.Sprintf("servidor %d: %s", e.Status, e.Detail)
}

// Options configura el cliente HTTP del agente.
type Options struct {
	BaseURL string
	APIKey  string
	// CAPEM son las raíces de confianza del servidor (ca.crt).
	CAPEM []byte
	// CertPEM y KeyPEM forman el certificado de cliente mTLS.
	CertPEM []byte
	KeyPEM  []byte
	// ServerFingerprint fija la CA del servidor por huella SHA-256
	// ("sha256:<hex>" o solo <hex>). Se usa al enrolar, antes de conocer la CA.
	ServerFingerprint string
	Timeout           time.Duration
}

// Client habla con el plano de control.
type Client struct {
	base   *url.URL
	apiKey string

	mu   sync.RWMutex
	http *http.Client
	opts Options
}

// New valida la URL y crea un cliente sin mTLS (compatibilidad).
func New(baseURL, apiKey string) (*Client, error) {
	return NewWithOptions(Options{BaseURL: baseURL, APIKey: apiKey})
}

// NewWithOptions valida la URL y crea un cliente con la configuración TLS dada.
func NewWithOptions(opts Options) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(opts.BaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("URL inválida: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("la URL debe empezar por http:// o https://")
	}
	if u.Host == "" {
		return nil, errors.New("la URL no tiene host")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	hc, err := buildHTTPClient(u, opts, timeout)
	if err != nil {
		return nil, err
	}
	return &Client{base: u, apiKey: opts.APIKey, http: hc, opts: opts}, nil
}

// SetTLS reemplaza el certificado de cliente y las raíces de confianza.
// Es seguro llamarlo mientras hay peticiones en curso.
func (c *Client) SetTLS(certPEM, keyPEM, caPEM []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	opts := c.opts
	opts.CertPEM, opts.KeyPEM, opts.CAPEM = certPEM, keyPEM, caPEM
	// Una vez conocida la CA ya no hace falta fijar la huella.
	opts.ServerFingerprint = ""
	hc, err := buildHTTPClient(c.base, opts, opts.Timeout)
	if err != nil {
		return err
	}
	c.http, c.opts = hc, opts
	return nil
}

// BaseURL devuelve la URL base efectiva.
func (c *Client) BaseURL() string { return c.base.String() }

// Enroll registra el agente con un token de un solo uso.
func (c *Client) Enroll(ctx context.Context, req contracts.EnrollRequest) (contracts.EnrollResponse, error) {
	var out contracts.EnrollResponse
	err := c.do(ctx, http.MethodPost, "/api/v1/agent/enroll", req, &out, false)
	return out, err
}

// RenewCert renueva el certificado mTLS del agente.
func (c *Client) RenewCert(ctx context.Context, csrPEM string) (contracts.RenewCertResponse, error) {
	var out contracts.RenewCertResponse
	err := c.do(ctx, http.MethodPost, "/api/v1/agent/renew-cert",
		contracts.RenewCertRequest{CSRPEM: csrPEM}, &out, true)
	return out, err
}

// Heartbeat informa del estado del agente.
func (c *Client) Heartbeat(ctx context.Context, req contracts.HeartbeatRequest) (contracts.HeartbeatResponse, error) {
	var out contracts.HeartbeatResponse
	err := c.do(ctx, http.MethodPost, "/api/v1/agent/heartbeat", req, &out, true)
	return out, err
}

// PutInventory envía el inventario.
func (c *Client) PutInventory(ctx context.Context, inv contracts.Inventory) error {
	return c.do(ctx, http.MethodPut, "/api/v1/agent/inventory", inv, nil, true)
}

// Me consulta la ficha del agente.
func (c *Client) Me(ctx context.Context) (contracts.AgentView, error) {
	var out contracts.AgentView
	err := c.do(ctx, http.MethodGet, "/api/v1/agent/me", nil, &out, true)
	return out, err
}

// Plans consulta los planes efectivos del agente.
func (c *Client) Plans(ctx context.Context) (contracts.AgentPlansResponse, error) {
	var out contracts.AgentPlansResponse
	err := c.do(ctx, http.MethodGet, "/api/v1/agent/plans", nil, &out, true)
	return out, err
}

// LeaseJob pide un job. Devuelve nil si no hay trabajo (204).
func (c *Client) LeaseJob(ctx context.Context, waitSeconds int) (*contracts.JobView, error) {
	var out contracts.JobLeaseResponse
	err := c.do(ctx, http.MethodPost, "/api/v1/agent/jobs/lease",
		map[string]int{"wait_seconds": waitSeconds}, &out, true)
	if err != nil {
		return nil, err
	}
	return out.Job, nil
}

// JobProgress informa del progreso de un job. Los servidores antiguos
// respondían 204; en ese caso la respuesta queda vacía.
func (c *Client) JobProgress(ctx context.Context, jobID string, req contracts.JobProgressRequest) (contracts.JobProgressResponse, error) {
	var out contracts.JobProgressResponse
	err := c.do(ctx, http.MethodPost, "/api/v1/agent/jobs/"+url.PathEscape(jobID)+"/progress",
		req, &out, true)
	return out, err
}

// JobLog envía un lote de líneas de log de un job.
func (c *Client) JobLog(ctx context.Context, jobID string, req contracts.JobLogRequest) error {
	return c.do(ctx, http.MethodPost, "/api/v1/agent/jobs/"+url.PathEscape(jobID)+"/log",
		req, nil, true)
}

// CancelStatus consulta si el administrador pidió cancelar el job.
func (c *Client) CancelStatus(ctx context.Context, jobID string) (contracts.JobCancelStatus, error) {
	var out contracts.JobCancelStatus
	err := c.do(ctx, http.MethodGet, "/api/v1/agent/jobs/"+url.PathEscape(jobID)+"/cancel",
		nil, &out, true)
	return out, err
}

// CompleteJob cierra un job.
func (c *Client) CompleteJob(ctx context.Context, jobID string, req contracts.JobCompleteRequest) error {
	return c.do(ctx, http.MethodPost, "/api/v1/agent/jobs/"+url.PathEscape(jobID)+"/complete",
		req, nil, true)
}

// RepositoryCredentials obtiene las credenciales del repositorio asignadas al
// agente. Devuelve (nil, nil) si el servidor responde 404 E_REPO_UNCONFIGURED.
func (c *Client) RepositoryCredentials(ctx context.Context) (*contracts.RepositoryCredentials, error) {
	var out contracts.RepositoryCredentials
	err := c.do(ctx, http.MethodGet, "/api/v1/agent/repository-credentials", nil, &out, true)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound &&
			(apiErr.Code == CodeRepoUnconfigured || apiErr.Code == "") {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

// RepositoryCredentialsFor obtiene las credenciales del repositorio de un
// destino de almacenamiento concreto (plan con storage_target_id).
func (c *Client) RepositoryCredentialsFor(ctx context.Context, targetID string) (*contracts.RepositoryCredentials, error) {
	var out contracts.RepositoryCredentials
	err := c.do(ctx, http.MethodGet,
		"/api/v1/agent/repository-credentials?target="+url.QueryEscape(targetID), nil, &out, true)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any, auth bool) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	u := *c.base
	// La ruta puede incluir query (?target=...): hay que separarla, porque
	// u.Path no admite "?" (el servidor vería una ruta inexistente → 404).
	ref, perr := url.Parse(path)
	if perr != nil {
		return fmt.Errorf("ruta de API inválida %q: %w", path, perr)
	}
	u.Path = strings.TrimRight(u.Path, "/") + ref.Path
	u.RawQuery = ref.RawQuery

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "bdr-agent/"+version.Version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	c.mu.RLock()
	hc := c.http
	c.mu.RUnlock()

	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return parseError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponse))
		return nil
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxResponse))
	return dec.Decode(out)
}

func parseError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	var p contracts.Problem
	_ = json.Unmarshal(b, &p)
	detail := p.Detail
	if detail == "" {
		detail = strings.TrimSpace(string(b))
	}
	return &APIError{Status: resp.StatusCode, Code: p.Code, Detail: detail}
}

func buildHTTPClient(u *url.URL, opts Options, timeout time.Duration) (*http.Client, error) {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if u.Scheme == "https" {
		tlsCfg, err := buildTLSConfig(u, opts)
		if err != nil {
			return nil, err
		}
		tr.TLSClientConfig = tlsCfg
	}
	return &http.Client{Timeout: timeout, Transport: tr}, nil
}

func buildTLSConfig(u *url.URL, opts Options) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	if len(opts.CAPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(opts.CAPEM) {
			return nil, errors.New("ca_pem no contiene certificados válidos")
		}
		cfg.RootCAs = pool
	}

	if len(opts.CertPEM) > 0 || len(opts.KeyPEM) > 0 {
		if len(opts.CertPEM) == 0 || len(opts.KeyPEM) == 0 {
			return nil, errors.New("el certificado de cliente y la clave deben ir juntos")
		}
		cert, err := tls.X509KeyPair(opts.CertPEM, opts.KeyPEM)
		if err != nil {
			return nil, fmt.Errorf("certificado de cliente inválido: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	if pin := NormalizeFingerprint(opts.ServerFingerprint); pin != "" {
		// La huella identifica a la CA interna. Verificamos manualmente que
		// algún certificado presentado coincida y que la hoja encadene a ella.
		cfg.InsecureSkipVerify = true //nolint:gosec // se sustituye por la verificación manual
		host := u.Hostname()
		cfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return VerifyPinnedChain(rawCerts, pin, host)
		}
	}
	return cfg, nil
}

// NormalizeFingerprint limpia el prefijo "sha256:" y normaliza a minúsculas.
func NormalizeFingerprint(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "sha256:")
	s = strings.ReplaceAll(s, ":", "")
	return s
}

// FingerprintDER devuelve la huella SHA-256 en hex de un certificado DER.
func FingerprintDER(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// VerifyPinnedChain exige que algún certificado de la cadena tenga la huella
// indicada y que la hoja verifique contra ese certificado como CA.
func VerifyPinnedChain(rawCerts [][]byte, pin, host string) error {
	pin = NormalizeFingerprint(pin)
	var pinned *x509.Certificate
	for _, raw := range rawCerts {
		if FingerprintDER(raw) != pin {
			continue
		}
		cert, err := x509.ParseCertificate(raw)
		if err != nil {
			return fmt.Errorf("certificado fijado inválido: %w", err)
		}
		pinned = cert
		break
	}
	if pinned == nil {
		return errors.New("la huella del servidor no coincide con ningún certificado presentado")
	}
	if len(rawCerts) == 0 {
		return errors.New("el servidor no presentó certificados")
	}
	leaf, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("certificado del servidor inválido: %w", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(pinned)
	intermediates := x509.NewCertPool()
	for _, raw := range rawCerts[1:] {
		cert, err := x509.ParseCertificate(raw)
		if err != nil {
			continue
		}
		intermediates.AddCert(cert)
	}
	_, err = leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		DNSName:       host,
	})
	return err
}
