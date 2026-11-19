package control

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/contracts"
)

// Notifier envía alertas por webhook y email en segundo plano, sin bloquear
// la petición que las genera. Cada canal reintenta con backoff exponencial:
// si el webhook o el SMTP fallan, se reintenta sin perder la alerta por un
// fallo puntual de red.
type Notifier struct {
	s      *Server
	queue  chan contracts.AlertView
	testCh chan chan contracts.NotificationTestResult
	// RetryBackoff son las esperas entre intentos (n-1 reintentos). Nil →
	// 2 s y 8 s. Se sobreescribe en tests.
	RetryBackoff []time.Duration
}

// NewNotifier crea el notificador.
func NewNotifier(s *Server) *Notifier {
	return &Notifier{s: s, queue: make(chan contracts.AlertView, 64), testCh: make(chan chan contracts.NotificationTestResult, 4)}
}

// defaultRetryBackoff es la política de reintentos por defecto: hasta
// 3 intentos por canal con backoff exponencial.
var defaultRetryBackoff = []time.Duration{2 * time.Second, 8 * time.Second}

func (n *Notifier) backoff() []time.Duration {
	if len(n.RetryBackoff) > 0 {
		return n.RetryBackoff
	}
	return defaultRetryBackoff
}

// Enqueue encola una alerta para notificar (no bloquea si la cola está llena).
func (n *Notifier) Enqueue(a contracts.AlertView) {
	select {
	case n.queue <- a:
	default:
		n.s.Logger.Warn("cola de notificaciones llena; alerta no notificada", "alert", a.Key)
	}
}

// Run procesa la cola hasta que ctx se cancela.
func (n *Notifier) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case a := <-n.queue:
			n.deliver(ctx, a)
		case reply := <-n.testCh:
			reply <- n.test(ctx)
		}
	}
}

// Test envía una notificación de prueba y devuelve el resultado.
func (n *Notifier) Test(ctx context.Context) contracts.NotificationTestResult {
	reply := make(chan contracts.NotificationTestResult, 1)
	select {
	case n.testCh <- reply:
	case <-time.After(2 * time.Second):
		return contracts.NotificationTestResult{Webhook: "error: cola ocupada", Email: "error: cola ocupada"}
	}
	select {
	case r := <-reply:
		return r
	case <-time.After(60 * time.Second):
		return contracts.NotificationTestResult{Webhook: "error: tiempo agotado", Email: "error: tiempo agotado"}
	}
}

func (n *Notifier) test(ctx context.Context) contracts.NotificationTestResult {
	out := contracts.NotificationTestResult{}
	settings := n.currentSettings()
	a := contracts.AlertView{Key: "test", Kind: "test", Severity: contracts.SeverityLow,
		Title: "Notificación de prueba", Detail: "Enviada desde BDR Control", FirstAt: n.s.Now().UTC()}
	if settings.WebhookURL != "" {
		out.Webhook = n.sendWebhook(ctx, settings, a)
	} else {
		out.Webhook = "skipped: sin webhook configurado"
	}
	if settings.SMTPHost != "" && len(settings.SMTPTo) > 0 {
		out.Email = n.sendEmail(ctx, settings, a)
	} else {
		out.Email = "skipped: sin SMTP configurado"
	}
	return out
}

func (n *Notifier) deliver(ctx context.Context, a contracts.AlertView) {
	settings := n.currentSettings()
	if severityRank(a.Severity) < severityRank(settings.AlertMinSeverity) {
		return
	}
	if settings.WebhookURL != "" {
		if res := n.sendWithRetry(ctx, "webhook", a.Key, func() string {
			return n.sendWebhook(ctx, settings, a)
		}); res != "" {
			n.s.Logger.Warn("webhook no entregado tras los reintentos", "result", res, "alert", a.Key)
		}
	}
	if settings.SMTPHost != "" && len(settings.SMTPTo) > 0 {
		if res := n.sendWithRetry(ctx, "email", a.Key, func() string {
			return n.sendEmail(ctx, settings, a)
		}); res != "" {
			n.s.Logger.Warn("email no entregado tras los reintentos", "result", res, "alert", a.Key)
		}
	}
}

// sendWithRetry ejecuta send hasta que devuelva "ok" o se agoten los
// reintentos; devuelve "" si se entregó o el último resultado.
func (n *Notifier) sendWithRetry(ctx context.Context, channel, alertKey string, send func() string) string {
	var last string
	for attempt := 0; ; attempt++ {
		last = send()
		if last == "ok" {
			return ""
		}
		waits := n.backoff()
		if attempt >= len(waits) {
			return last
		}
		n.s.Logger.Warn("notificación fallida; reintentando",
			"channel", channel, "alert", alertKey, "attempt", attempt+1, "result", last,
			"retry_in", waits[attempt].String())
		select {
		case <-ctx.Done():
			return last
		case <-time.After(waits[attempt]):
		}
	}
}

func severityRank(sev string) int {
	switch sev {
	case contracts.SeverityCritical:
		return 3
	case contracts.SeverityHigh:
		return 2
	case contracts.SeverityMedium:
		return 1
	default:
		return 0
	}
}

func (n *Notifier) currentSettings() store.Settings {
	var out store.Settings
	_ = n.s.Store.View(context.Background(), func(st *store.State) error {
		out = st.Settings
		return nil
	})
	return out
}

// sendWebhook publica la alerta firmada con HMAC-SHA256.
func (n *Notifier) sendWebhook(ctx context.Context, settings store.Settings, a contracts.AlertView) string {
	body, _ := json.Marshal(map[string]any{
		"type": "bdr.alert", "alert": a, "environment": settings.EnvironmentName,
		"sent_at": n.s.Now().UTC(),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, settings.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return "error: " + err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	if secret, err := n.s.Box.Open(settings.WebhookSecretEnc); err == nil && secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		req.Header.Set("X-BDR-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "error: " + err.Error()
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 4096)
	if resp.StatusCode >= 300 {
		return fmt.Sprintf("error: webhook respondió %d", resp.StatusCode)
	}
	return "ok"
}

// sendEmail envía la alerta por SMTP con STARTTLS opcional.
func (n *Notifier) sendEmail(ctx context.Context, settings store.Settings, a contracts.AlertView) string {
	password, err := n.s.Box.Open(settings.SMTPPasswordEnc)
	if err != nil {
		password = ""
	}
	addr := fmt.Sprintf("%s:%d", settings.SMTPHost, settings.SMTPPort)
	from := settings.SMTPFrom
	if from == "" {
		from = "bdr@localhost"
	}
	subject := fmt.Sprintf("[BDR][%s] %s", strings.ToUpper(a.Severity), a.Title)
	body := fmt.Sprintf(
		"Alerta BDR en %s\n\nSeveridad: %s\nEstado: %s\nDetalle: %s\nEquipo: %s\nPrimera vez: %s\nÚltima vez: %s (%d veces)\n\nID: %s\n",
		settings.EnvironmentName, a.Severity, a.State, a.Detail, a.AgentID,
		a.FirstAt.Format(time.RFC3339), a.LastAt.Format(time.RFC3339), a.Count, a.ID,
	)
	msg := bytes.Join([][]byte{
		[]byte("From: " + from),
		[]byte("To: " + strings.Join(settings.SMTPTo, ", ")),
		[]byte("Subject: " + subject),
		[]byte("MIME-Version: 1.0"),
		[]byte("Content-Type: text/plain; charset=UTF-8"),
		[]byte(""),
		[]byte(body),
	}, []byte("\r\n"))

	var auth smtp.Auth
	if settings.SMTPUsername != "" {
		auth = smtp.PlainAuth("", settings.SMTPUsername, password, settings.SMTPHost)
	}
	_ = ctx // net/smtp no soporta contexto; el timeout del cliente SMTP es fijo
	if settings.SMTPStartTLS {
		c, err := smtp.Dial(addr)
		if err != nil {
			return "error: " + err.Error()
		}
		defer c.Close()
		if err := c.Hello("bdr.local"); err != nil {
			return "error: " + err.Error()
		}
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(nil); err != nil {
				return "error: " + err.Error()
			}
		}
		if auth != nil {
			if err := c.Auth(auth); err != nil {
				return "error: " + err.Error()
			}
		}
		if err := c.Mail(from); err != nil {
			return "error: " + err.Error()
		}
		for _, to := range settings.SMTPTo {
			if err := c.Rcpt(to); err != nil {
				return "error: " + err.Error()
			}
		}
		w, err := c.Data()
		if err != nil {
			return "error: " + err.Error()
		}
		if _, err := w.Write(msg); err != nil {
			_ = w.Close()
			return "error: " + err.Error()
		}
		if err := w.Close(); err != nil {
			return "error: " + err.Error()
		}
		if err := c.Quit(); err != nil {
			return "error: " + err.Error()
		}
		return "ok"
	}
	err = smtp.SendMail(addr, auth, from, settings.SMTPTo, msg)
	if err != nil {
		return "error: " + err.Error()
	}
	return "ok"
}
