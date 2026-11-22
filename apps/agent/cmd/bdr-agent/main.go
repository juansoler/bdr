// bdr-agent es el agente de copia de seguridad.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/juansoler/bdr/apps/agent/internal/agent"
	"github.com/juansoler/bdr/apps/agent/internal/client"
	"github.com/juansoler/bdr/apps/agent/internal/config"
	"github.com/juansoler/bdr/apps/agent/internal/identity"
	"github.com/juansoler/bdr/apps/agent/internal/image"
	"github.com/juansoler/bdr/apps/agent/internal/inventory"
	"github.com/juansoler/bdr/apps/agent/internal/jobs"
	"github.com/juansoler/bdr/packages/contracts"
	"github.com/juansoler/bdr/packages/logging"
	"github.com/juansoler/bdr/packages/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "enroll", "register":
		cmdEnroll(os.Args[2:])
	case "run":
		cmdRun(os.Args[2:])
	case "renew-cert":
		cmdRenewCert(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	case "image-write":
		cmdImageWrite(os.Args[2:])
	case "screenhelper": // interno: helper de sesión Windows (lo lanza el servicio)
		cmdScreenHelper()
	case "service-restart": // interno: helper de autoactualización Windows
		cmdServiceRestart(os.Args[2:])
	case "image-fetch":
		cmdImageFetch(os.Args[2:])
	case "version", "--version", "-version":
		fmt.Println(version.Version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "comando desconocido: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `bdr-agent %s

Uso:
  bdr-agent register --server URL --token TOKEN [--data-dir DIR] [--server-fingerprint sha256:HEX]
  bdr-agent enroll   (alias de register)
  bdr-agent run [--data-dir DIR] [--interval 30s] [--inventory-interval 6h]
                [--job-interval 15s] [--kopia-binary PATH] [--kopia-config FILE] [--once]
  bdr-agent renew-cert [--data-dir DIR]
  bdr-agent status [--data-dir DIR]
  bdr-agent image-fetch --endpoint E --bucket B --access-key A --secret-key S
                       [--prefix P] [--no-tls] --manifest KEY --dir DIR
  bdr-agent image-write --segments-dir DIR --target DISPOSITIVO [--yes] [--no-verify]
  bdr-agent version
`, version.Version)
}

func defaultDataDir() string {
	return filepath.Join(".", "data", "agent")
}

func cmdEnroll(args []string) {
	fs := flag.NewFlagSet("register", flag.ExitOnError)
	server := fs.String("server", "", "URL del plano de control (http:// o https://)")
	token := fs.String("token", "", "token de enrolamiento de un solo uso")
	dataDir := fs.String("data-dir", defaultDataDir(), "directorio de datos del agente")
	fingerprint := fs.String("server-fingerprint", "", "huella SHA-256 de la CA del servidor (sha256:<hex>)")
	hostnameFlag := fs.String("hostname", "", "nombre de host a reportar (por defecto el del sistema)")
	_ = fs.Parse(args)

	if *server == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "faltan --server o --token")
		fs.Usage()
		os.Exit(2)
	}

	log := logging.New("info")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	inv, err := inventory.Collect(ctx)
	if err != nil {
		log.Error("no se pudo recoger el inventario", "error", err.Error())
		os.Exit(1)
	}
	hostname := inv.Hostname
	if *hostnameFlag != "" {
		hostname = *hostnameFlag
	}

	// Generamos clave y CSR aunque el servidor no soporte mTLS: csr_pem es
	// opcional y el servidor lo ignora si no tiene listener de agentes.
	kp, err := identity.GenerateCSR(hostname)
	if err != nil {
		log.Error("no se pudo generar la identidad del agente", "error", err.Error())
		os.Exit(1)
	}

	if strings.HasPrefix(strings.ToLower(*server), "http://") {
		log.Warn("el enrolamiento usa HTTP sin TLS; use HTTPS con --server-fingerprint en producción")
		if *fingerprint != "" {
			log.Warn("--server-fingerprint se ignora porque la URL no es HTTPS")
		}
	}

	c, err := client.NewWithOptions(client.Options{
		BaseURL:           *server,
		ServerFingerprint: *fingerprint,
	})
	if err != nil {
		log.Error("URL inválida", "error", err.Error())
		os.Exit(1)
	}
	resp, err := c.Enroll(ctx, contracts.EnrollRequest{
		Token:        *token,
		Hostname:     hostname,
		OSFamily:     inv.OSFamily,
		OSVersion:    inv.OSVersion,
		Arch:         inv.Arch,
		AgentVersion: version.Version,
		CSRPEM:       string(kp.CSRPEM),
	})
	if err != nil {
		log.Error("enrolamiento fallido", "error", err.Error())
		os.Exit(1)
	}

	hb := resp.HeartbeatSeconds
	if hb <= 0 {
		hb = 30
	}
	cfg := config.Config{
		ServerURL:         *server,
		AgentURL:          resp.AgentURL,
		AgentID:           resp.AgentID,
		APIKey:            resp.APIKey,
		Hostname:          hostname,
		HeartbeatSeconds:  hb,
		EnrolledAt:        time.Now().UTC(),
		ServerFingerprint: resp.ServerFingerprint,
		CertExpiresAt:     resp.CertificateExpiry,
	}

	mtls := resp.CertificatePEM != ""
	if mtls {
		if resp.CAPEM == "" {
			log.Warn("el servidor emitió certificado pero no envió la CA; se usarán las raíces del sistema")
		}
		if err := identity.Save(*dataDir, kp.KeyPEM, []byte(resp.CertificatePEM), []byte(resp.CAPEM)); err != nil {
			log.Error("no se pudo guardar la identidad mTLS", "error", err.Error())
			os.Exit(1)
		}
	} else {
		log.Warn("el servidor no emitió certificado de cliente; se usará API key sin mTLS")
	}

	if err := config.Save(*dataDir, cfg); err != nil {
		log.Error("no se pudo guardar la configuración", "error", err.Error())
		os.Exit(1)
	}

	// El inventario inicial se envía ya por el endpoint definitivo (mTLS si
	// el servidor lo anunció).
	if authed, err := newClientFromConfig(cfg, *dataDir, log); err == nil {
		putCtx, putCancel := context.WithTimeout(ctx, 60*time.Second)
		if err := authed.PutInventory(putCtx, inv); err != nil {
			log.Warn("el agente se enroló pero el inventario no se envió", "error", err.Error())
		}
		putCancel()
	}

	log.Info("agente enrolado", "agent_id", resp.AgentID, "hostname", hostname, "mtls", mtls)
	printJSON(map[string]any{
		"agent_id":    resp.AgentID,
		"hostname":    hostname,
		"data_dir":    *dataDir,
		"enrolled_at": cfg.EnrolledAt,
		"agent_url":   cfg.AgentURL,
		"mtls":        mtls,
	})
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "directorio de datos del agente")
	interval := fs.Duration("interval", 0, "intervalo de heartbeat (por defecto el del servidor)")
	inventoryInterval := fs.Duration("inventory-interval", 6*time.Hour, "intervalo de inventario")
	jobInterval := fs.Duration("job-interval", 15*time.Second, "intervalo de sondeo de jobs")
	kopiaBinary := fs.String("kopia-binary", "kopia", "binario de Kopia")
	kopiaConfig := fs.String("kopia-config", "", "archivo de configuración de Kopia (modo heredado)")
	logLevel := fs.String("log-level", "info", "nivel de log: debug, info, warn, error")
	once := fs.Bool("once", false, "ejecuta un ciclo (heartbeat, inventario y job si existe) y termina")
	_ = fs.Parse(args)

	log := logging.New(*logLevel)
	// En modo servicio de Windows la consola no existe: los logs van a
	// <data-dir>\agent.log.
	if isWindowsService() {
		log = serviceLogger(*dataDir, *logLevel, log)
	}
	cfg, err := config.Load(*dataDir)
	if err != nil {
		log.Error("configuración no disponible", "error", err.Error())
		os.Exit(1)
	}
	c, err := newClientFromConfig(cfg, *dataDir, log)
	if err != nil {
		log.Error("no se pudo crear el cliente del plano de control", "error", err.Error())
		os.Exit(1)
	}

	runner := jobs.NewKopiaRunnerWithOptions(jobs.KopiaOptions{
		Binary:           *kopiaBinary,
		LegacyConfigFile: *kopiaConfig,
		DataDir:          *dataDir,
		Log:              log,
	})
	executor := jobs.NewExecutor(runner, log)
	executor.SetImageRunner(jobs.NewImageRunner(*dataDir, log, *kopiaBinary, *kopiaConfig))
	executor.SetVMRunner(jobs.NewVMRunner(*dataDir, log))
	executor.SetLXCRunner(jobs.NewVMRunner(*dataDir, log))
	executor.SetSQLRunner(jobs.NewSQLMotor())

	hbInterval := *interval
	leaseWait := 20
	if *once {
		leaseWait = 0
	}
	ag := agent.New(agent.Options{
		Client:            c,
		Config:            cfg,
		DataDir:           *dataDir,
		Log:               log,
		Executor:          executor,
		HeartbeatInterval: hbInterval,
		InventoryInterval: *inventoryInterval,
		JobInterval:       *jobInterval,
		LeaseWaitSeconds:  leaseWait,
		RestartSelf:       restartSelf,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("agente en marcha", "agent_id", cfg.AgentID, "endpoint", cfg.Endpoint(), "mtls", isMTLSAvailable(*dataDir), "servicio", isWindowsService())
	if err := runAgentProcess(ctx, ag, *once); err != nil {
		log.Error("el agente terminó con error", "error", err.Error())
		os.Exit(1)
	}
}

// cmdImageWrite despliega una imagen completa en un disco (bare-metal).
// Pensado para ejecutarse desde un entorno live en el equipo destino, tras
// restaurar el snapshot de staging con `kopia restore`.
func cmdImageWrite(args []string) {
	fs := flag.NewFlagSet("image-write", flag.ExitOnError)
	segmentsDir := fs.String("segments-dir", "", "directorio con manifest.json y los segmentos (staging restaurado)")
	target := fs.String("target", "", "disco destino (p. ej. /dev/sda o \\\\.\\PhysicalDrive1)")
	yes := fs.Bool("yes", false, "confirma la escritura destructiva")
	verify := fs.Bool("no-verify", false, "omite la verificación SHA-256 (no recomendado)")
	_ = fs.Parse(args)

	if *segmentsDir == "" || *target == "" {
		fmt.Fprintln(os.Stderr, "faltan --segments-dir o --target")
		fs.Usage()
		os.Exit(2)
	}
	if !*yes {
		fmt.Fprintf(os.Stderr, "PELIGRO: se escribirá %s sobre %s (DESTROY todo su contenido).\nRepite el comando con --yes para confirmar.\n", *segmentsDir, *target)
		os.Exit(2)
	}
	log := logging.New("info")
	start := time.Now()
	log.Info("escribiendo imagen", "target", *target, "segments", *segmentsDir)
	manifest, err := image.WriteImage(image.WriteOptions{
		SegmentsDir: *segmentsDir,
		Target:      *target,
		Verify:      !*verify,
		Progress: func(done, total int, bytesDone int64) {
			log.Info("progreso", "segmento", fmt.Sprintf("%d/%d", done, total), "bytes", bytesDone)
		},
	})
	if err != nil {
		log.Error("escritura de imagen fallida", "error", err.Error())
		os.Exit(1)
	}
	log.Info("imagen escrita", "disks", len(manifest.Disks), "duracion_ms", time.Since(start).Milliseconds())
	fmt.Println("despliegue completado: reinicia el equipo y comprueba que arranca")
}

// cmdRenewCert renueva el certificado mTLS bajo demanda: CSR con la clave
// local, firma por el servidor y persistencia. Útil para operación y para
// verificar el ciclo de renovación sin esperar al umbral del heartbeat.
func cmdRenewCert(args []string) {
	fs := flag.NewFlagSet("renew-cert", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "directorio de datos del agente")
	_ = fs.Parse(args)

	cfg, err := config.Load(*dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	log := logging.New("warn")
	c, err := newClientFromConfig(cfg, *dataDir, log)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	keyPEM, _, _, err := identity.Load(*dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no hay identidad mTLS local: %v\n", err)
		os.Exit(1)
	}
	csrPEM, err := identity.NewCSR(keyPEM, cfg.Hostname)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no se pudo crear la CSR: %v\n", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := c.RenewCert(ctx, string(csrPEM))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := identity.Save(*dataDir, keyPEM, []byte(resp.CertificatePEM), []byte(resp.CAPEM)); err != nil {
		fmt.Fprintf(os.Stderr, "no se pudo guardar el certificado renovado: %v\n", err)
		os.Exit(1)
	}
	cfg.CertExpiresAt = &resp.CertificateExpiry
	if err := config.Save(*dataDir, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "no se pudo actualizar la configuración: %v\n", err)
		os.Exit(1)
	}
	printJSON(map[string]any{"certificate_expires_at": resp.CertificateExpiry})
}

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "directorio de datos del agente")
	_ = fs.Parse(args)

	cfg, err := config.Load(*dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	log := logging.New("warn")
	c, err := newClientFromConfig(cfg, *dataDir, log)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	agentView, err := c.Me(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	printJSON(agentView)
}

// newClientFromConfig crea el cliente usando la identidad mTLS guardada.
// Si no hay certificado local se conserva la huella del servidor como pin.
func newClientFromConfig(cfg config.Config, dataDir string, log *logging.Logger) (*client.Client, error) {
	endpoint := cfg.Endpoint()
	opts := client.Options{BaseURL: endpoint, APIKey: cfg.APIKey}
	keyPEM, certPEM, caPEM, err := identity.Load(dataDir)
	if err == nil {
		opts.KeyPEM, opts.CertPEM, opts.CAPEM = keyPEM, certPEM, caPEM
	} else if cfg.ServerFingerprint != "" && strings.HasPrefix(strings.ToLower(endpoint), "https://") {
		opts.ServerFingerprint = cfg.ServerFingerprint
	}
	c, err := client.NewWithOptions(opts)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(strings.ToLower(endpoint), "http://") {
		log.Warn("conexión HTTP sin TLS; use HTTPS con huella del servidor en producción")
	}
	return c, nil
}

func isMTLSAvailable(dataDir string) bool {
	_, _, _, err := identity.Load(dataDir)
	return err == nil
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
