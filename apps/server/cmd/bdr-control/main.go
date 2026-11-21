// bdr-control es el plano de control de BDR.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/auth"
	"github.com/juansoler/bdr/apps/server/internal/control"
	"github.com/juansoler/bdr/apps/server/internal/pki"
	"github.com/juansoler/bdr/apps/server/internal/secrets"
	"github.com/juansoler/bdr/apps/server/internal/selfupdate"
	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/packages/logging"
	"github.com/juansoler/bdr/packages/updatekey"
	"github.com/juansoler/bdr/packages/version"
)

// controlOpts agrupa la configuración: se parsea en main y la consumen
// tanto la ejecución en consola como el modo servicio de Windows.
type controlOpts struct {
	addr           string
	agentAddr      string
	dataDir        string
	storeKind      string
	dbURL          string
	importState    string
	downloadsDir   string
	dev            bool
	logLevel       string
	publicURL      string
	agentPublicURL string
	agentTLSNames  string
	kopiaBinary    string
	agentMTLS      bool
	insecureAgents bool
	tlsCert        string
	tlsKey         string
	corsOrigins    string
	updateURL      string
	updateKey      string
}

func parseOpts() controlOpts {
	o := controlOpts{}
	flag.StringVar(&o.addr, "addr", "127.0.0.1:8080", "dirección de escucha de la API de administración")
	flag.StringVar(&o.agentAddr, "agent-addr", "", "escucha TLS para agentes (mTLS opcional), p. ej. 0.0.0.0:8443")
	flag.StringVar(&o.dataDir, "data-dir", "./data/control", "directorio de datos")
	flag.StringVar(&o.storeKind, "store", "file", "almacenamiento: file|memory|postgres")
	flag.StringVar(&o.dbURL, "db-url", "", "DSN de PostgreSQL para --store postgres (o BDR_DB_URL)")
	flag.StringVar(&o.importState, "import-state", "", "importa un state.json en la base antes de arrancar (solo con --store postgres)")
	flag.StringVar(&o.downloadsDir, "downloads-dir", "", "directorio con los binarios de agentes para los instaladores (default <data-dir>/downloads)")
	flag.BoolVar(&o.dev, "dev", false, "modo desarrollo: permite un token de administración fijo si no se configura otro")
	flag.StringVar(&o.logLevel, "log-level", "info", "debug|info|warn|error")
	flag.StringVar(&o.publicURL, "public-url", "", "URL pública para los agentes (http://host:8080)")
	flag.StringVar(&o.agentPublicURL, "agent-url", "", "URL TLS para los agentes (https://host:8443)")
	flag.StringVar(&o.agentTLSNames, "agent-tls-names", "127.0.0.1,localhost", "nombres/IPs para el certificado del listener de agentes")
	flag.StringVar(&o.kopiaBinary, "kopia-binary", "kopia", "binario de Kopia para el plano de control")
	flag.BoolVar(&o.agentMTLS, "agent-mtls", true, "exigir certificado de cliente en el listener de agentes (valor por defecto; deprecado como interruptor)")
	flag.BoolVar(&o.insecureAgents, "insecure-agents", false, "transición: acepta agentes sin certificado de cliente en el listener TLS")
	flag.StringVar(&o.tlsCert, "tls-cert", "", "certificado PEM para la API de administración (HTTPS); requiere --tls-key")
	flag.StringVar(&o.tlsKey, "tls-key", "", "clave PEM de --tls-cert")
	flag.StringVar(&o.corsOrigins, "cors-origins", "", "orígenes con permiso CORS para la API (solo desarrollo: http://localhost:3000)")
	flag.StringVar(&o.updateURL, "update-url", "", "manifiesto del canal de actualizaciones (latest.json); por defecto la release estable del proyecto; vacío deshabilita")
	flag.StringVar(&o.updateKey, "update-key", "", "clave pública Ed25519 (base64) alternativa del canal; por defecto la embebida en el binario")
	showVersion := flag.Bool("version", false, "muestra la versión y sale")
	flag.Parse()

	// Argumentos posicionales no admitidos: evita accidentes como
	// `bdr-control version`, que de otro modo arrancaría el servidor con
	// valores por defecto en lugar de mostrar la versión.
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "argumento desconocido: %q (la versión se consulta con --version)\n", flag.Arg(0))
		flag.Usage()
		os.Exit(2)
	}
	if *showVersion {
		fmt.Println(version.Version)
		os.Exit(0)
	}
	return o
}

func main() {
	opts := parseOpts()

	// Servicio de Windows: el SCM arranca el proceso y exige respuesta en
	// los primeros 30 s; el bucle corre dentro de svc.Run.
	if isWindowsService() {
		if err := runWindowsService(opts); err != nil {
			fmt.Fprintf(os.Stderr, "bdr-control servicio: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runControl(ctx, opts); err != nil {
		fmt.Fprintf(os.Stderr, "bdr-control: %v\n", err)
		os.Exit(1)
	}
}

// runControl ejecuta el plano de control hasta que se cancele el contexto.
// En modo servicio, ctx lo cancela el SCM (Stop/Shutdown).
func runControl(ctx context.Context, o controlOpts) error {
	logger := logging.New(o.logLevel)
	// En modo servicio de Windows no hay consola: los logs van a
	// <data-dir>\control.log.
	if isWindowsService() {
		logger = serviceLogger(o.dataDir, o.logLevel, logger)
	}

	token := os.Getenv("BDR_ADMIN_TOKEN")
	if token == "" && o.dev {
		token = "dev-admin-token"
		logger.Warn("modo desarrollo: usando token de administración por defecto", "admin_token", token)
	}
	if token == "" {
		return errors.New("falta BDR_ADMIN_TOKEN; expórtalo o usa --dev solo en local")
	}

	if err := os.MkdirAll(filepath.Clean(o.dataDir), 0o700); err != nil {
		return fmt.Errorf("no se pudo crear el directorio de datos: %w", err)
	}

	// Caja de secretos (cifrado en reposo).
	box, keyGenerated, err := secrets.Load(os.Getenv("BDR_SECRET_KEY"),
		filepath.Join(filepath.Clean(o.dataDir), "secret.key"))
	if err != nil {
		return fmt.Errorf("no se pudo preparar la clave de secretos: %w", err)
	}
	if keyGenerated {
		logger.Warn("clave de secretos generada",
			"path", filepath.Join(o.dataDir, "secret.key"),
			"nota", "cópiala a un lugar seguro; sin ella los secretos cifrados no se pueden recuperar")
	}

	// CA interna para mTLS de agentes.
	ca, caErr := pki.LoadOrCreate(filepath.Join(filepath.Clean(o.dataDir), "pki"))
	if caErr != nil {
		logger.Warn("sin CA interna: el enrolamiento no emitirá certificados", "error", caErr.Error())
	}

	var st store.Store
	switch o.storeKind {
	case "memory":
		st = store.NewMemory()
		logger.Warn("almacenamiento en memoria: los datos se pierden al reiniciar")
	case "file":
		statePath := filepath.Join(filepath.Clean(o.dataDir), "state.json")
		fs, err := store.NewFile(statePath)
		if err != nil {
			return fmt.Errorf("no se pudo abrir el estado (%s): %w", statePath, err)
		}
		st = fs
		logger.Info("estado persistente", "path", statePath)
	case "postgres":
		dsn := o.dbURL
		if dsn == "" {
			dsn = os.Getenv("BDR_DB_URL")
		}
		if dsn == "" {
			return errors.New("--store postgres exige --db-url o la variable BDR_DB_URL")
		}
		ctxOpen, cancel := context.WithTimeout(ctx, 30*time.Second)
		ps, err := store.OpenPostgres(ctxOpen, dsn)
		cancel()
		if err != nil {
			return fmt.Errorf("no se pudo abrir PostgreSQL: %w", err)
		}
		if o.importState != "" {
			ctxImp, cancelImp := context.WithTimeout(ctx, 30*time.Second)
			err := ps.ImportState(ctxImp, o.importState)
			cancelImp()
			if err != nil {
				_ = ps.Close()
				return fmt.Errorf("no se pudo importar el estado (%s): %w", o.importState, err)
			}
			logger.Info("estado importado en PostgreSQL", "path", o.importState)
		}
		st = ps
		logger.Info("estado persistente en PostgreSQL")
	default:
		return fmt.Errorf("valor de --store no soportado: %s", o.storeKind)
	}

	// Conexión directa del plano de control al repositorio Kopia (opcional):
	// el operador la crea con el CLI estándar de Kopia usando el mismo
	// kopia.config; el servidor la usa para navegar y verificar.
	// Autodetección de kopia junto al binario: en los layouts /opt/bdr y
	// Docker kopia vive junto al control, pero launchd/servicios tienen un
	// PATH mínimo que no lo encuentra. Si el operador fijó --kopia-binary
	// explícito, se respeta.
	if o.kopiaBinary == "kopia" {
		if exe, err := os.Executable(); err == nil {
			name := "kopia"
			if runtime.GOOS == "windows" {
				name = "kopia.exe"
			}
			if local := filepath.Join(filepath.Dir(exe), name); fileExists(local) {
				o.kopiaBinary = local
				logger.Info("kopia autodetectado junto al binario del control", "path", local)
			}
		}
	}
	kopiaConfig := filepath.Join(filepath.Clean(o.dataDir), "kopia.config")
	kopia := control.NewKopiaControl(o.kopiaBinary, kopiaConfig, os.Getenv("BDR_KOPIA_PASSWORD"),
		os.Getenv("BDR_KOPIA_KIND"), os.Getenv("BDR_KOPIA_REPO_PATH"),
		os.Getenv("BDR_KOPIA_SERVER_URL"), os.Getenv("BDR_KOPIA_SERVER_FP"),
		os.Getenv("BDR_KOPIA_SERVER_CONTROL_USER"), os.Getenv("BDR_KOPIA_SERVER_CONTROL_PASSWORD"))
	if kopia.ServerSide() {
		logger.Info("repositorio Kopia accesible por el plano de control", "kind", kopia.KindName)
	} else if kopia.KindName != "" {
		logger.Warn("repositorio Kopia declarado pero sin conexión directa; navegación y verificación deshabilitadas",
			"detalle", kopia.LastConnError)
	}

	api := control.New(st, token, logger, version.Version)
	api.Box = box
	api.CA = ca
	api.Kopia = kopia
	api.PublicURL = strings.TrimRight(o.publicURL, "/")
	api.AgentPublicURL = strings.TrimRight(o.agentPublicURL, "/")
	if strings.TrimSpace(o.corsOrigins) != "" {
		for _, origin := range strings.Split(o.corsOrigins, ",") {
			if strings.TrimSpace(origin) != "" {
				api.AllowedOrigins = append(api.AllowedOrigins, origin)
			}
		}
		logger.Info("CORS de desarrollo activo", "origins", o.corsOrigins)
	}
	api.DownloadsDir = o.downloadsDir
	if api.DownloadsDir == "" {
		api.DownloadsDir = filepath.Join(filepath.Clean(o.dataDir), "downloads")
	}
	_ = os.MkdirAll(api.DownloadsDir, 0o755)
	if ca != nil {
		api.CertFingerprint = ca.Fingerprint()
	}

	// Canal de actualizaciones: URL por defecto de la release estable y
	// clave pública embebida. Sin clave pública el chequeo queda
	// deshabilitado (fallo cerrado).
	api.UpdateURL = strings.TrimSpace(o.updateURL)
	if api.UpdateURL == "" {
		api.UpdateURL = selfupdate.ManifestURL
	}
	api.UpdatePublicKey = strings.TrimSpace(o.updateKey)
	if api.UpdatePublicKey == "" {
		api.UpdatePublicKey = updatekey.PublicKeyBase64
	}

	api.Start(ctx)

	// Usuario administrador inicial desde el entorno (opcional).
	bootstrapAdmin(api, logger)

	ln, err := net.Listen("tcp", o.addr)
	if err != nil {
		return fmt.Errorf("no se pudo abrir el puerto %s: %w", o.addr, err)
	}

	srv := &http.Server{
		Addr:              o.addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	// TLS nativo de la API de administración: en producción, o esto o un
	// reverse proxy delante (deploy/docker-compose.prod.yml usa Caddy).
	tlsEnabled := false
	if (o.tlsCert != "") != (o.tlsKey != "") {
		return errors.New("--tls-cert y --tls-key deben ir juntos")
	}
	if o.tlsCert != "" {
		if _, err := tls.LoadX509KeyPair(o.tlsCert, o.tlsKey); err != nil {
			return fmt.Errorf("no se pudo cargar el certificado de la API: %w", err)
		}
		tlsEnabled = true
		logger.Info("API de administración en HTTPS", "cert", o.tlsCert)
	} else {
		logger.Warn("API de administración en HTTP claro; usa --tls-cert/--tls-key o un reverse proxy en producción")
	}

	var agentSrv *http.Server
	if o.agentAddr != "" {
		if ca == nil {
			return errors.New("--agent-addr exige CA interna; revisa el error de pki anterior")
		}
		names := strings.Split(o.agentTLSNames, ",")
		pair, err := ca.ServerTLS(names)
		if err != nil {
			return fmt.Errorf("no se pudo preparar el certificado TLS de agentes: %w", err)
		}
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{pair},
			MinVersion:   tls.VersionTLS12,
		}
		if !o.agentMTLS && !o.insecureAgents {
			logger.Warn("--agent-mtls=false está deprecado; usa --insecure-agents para la transición")
		}
		if o.insecureAgents {
			// Transición: verifica el certificado si viene, pero no lo exige.
			tlsCfg.ClientAuth = tls.VerifyClientCertIfGiven
			tlsCfg.ClientCAs = ca.Pool()
			logger.Warn("listener de agentes sin mTLS obligatorio (--insecure-agents); solo para migraciones")
		} else {
			// mTLS estricto: certificado de cliente obligatorio y vigente.
			// Sin reutilización de sesión para que la política de
			// certificados (revocación, renovación) se aplique en cada
			// conexión nueva sin reiniciar.
			tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
			tlsCfg.ClientCAs = ca.Pool()
			tlsCfg.SessionTicketsDisabled = true
			tlsCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				if len(rawCerts) == 0 {
					return errors.New("sin certificado de cliente")
				}
				leaf, err := x509.ParseCertificate(rawCerts[0])
				if err != nil {
					return err
				}
				agentID, serial, ok := pki.AgentIdentity(leaf)
				if !ok {
					return errors.New("certificado sin identidad de agente")
				}
				return api.VerifyAgentHandshake(agentID, serial)
			}
		}
		agentLn, err := tls.Listen("tcp", o.agentAddr, tlsCfg)
		if err != nil {
			return fmt.Errorf("no se pudo abrir el listener de agentes %s: %w", o.agentAddr, err)
		}
		agentSrv = &http.Server{
			Handler:           api.Handler(),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    1 << 20,
		}
		go func() {
			logger.Info("listener de agentes TLS activo", "addr", o.agentAddr, "mtls", o.agentMTLS)
			if err := agentSrv.Serve(agentLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("listener de agentes terminó con error", "error", err.Error())
			}
		}()
	}

	go func() {
		logger.Info("bdr-control escuchando", "addr", o.addr, "version", version.Version, "tls", tlsEnabled)
		var err error
		if tlsEnabled {
			err = srv.ServeTLS(ln, o.tlsCert, o.tlsKey)
		} else {
			err = srv.Serve(ln)
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("servidor HTTP terminó con error", "error", err.Error())
		}
	}()

	<-ctx.Done()
	logger.Info("apagando bdr-control")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("apagado no limpio", "error", err.Error())
	}
	if agentSrv != nil {
		_ = agentSrv.Shutdown(shutdownCtx)
	}
	if err := st.Close(); err != nil {
		logger.Error("no se pudo cerrar el almacenamiento limpiamente", "error", err.Error())
	}
	return nil
}

// fileExists comprueba que existe un fichero regular.
func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// bootstrapAdmin crea el primer usuario si BDR_ADMIN_EMAIL y BDR_ADMIN_PASSWORD están definidos.
func bootstrapAdmin(api *control.Server, logger *logging.Logger) {
	email := strings.ToLower(strings.TrimSpace(os.Getenv("BDR_ADMIN_EMAIL")))
	password := os.Getenv("BDR_ADMIN_PASSWORD")
	if email == "" || password == "" {
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		logger.Error("no se pudo crear el usuario inicial", "error", err.Error())
		return
	}
	now := time.Now().UTC()
	err = api.CreateInitialUser(email, hash, now)
	if err != nil {
		logger.Info("usuario inicial no creado", "reason", err.Error())
		return
	}
	logger.Info("usuario administrador inicial creado desde el entorno", "email", email)
}
