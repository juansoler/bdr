# Changelog del producto

Formato basado en Keep a Changelog; versionado semántico mientras el producto no llegue a 1.0.

## [0.16.18] — 2026-11-22

### Corregido

- **SQL Server: el job de restauración llegaba al agente sin destino de
  almacenamiento** (hallazgo del lab): sin él, el agente no puede
  descargar la cadena del repo y el job fallaba con «el job SQL requiere
  un destino de almacenamiento». Ahora el eslabón de la cadena calculada
  expone el `target_id`, el panel lo envía y el control lo deriva del
  catálogo si la petición no lo trae.

## [0.16.17] — 2026-11-22

### Corregido

- **Panel: el asistente de restauración SQL fallaba con 400** (hallazgo
  del lab): el checkbox de confirmación recogía el visto bueno del
  operador pero el cuerpo del POST no incluía `confirm: true`, que la API
  exige para el restore destructivo. Ahora se envía.
- **Panel: tamaños menores de 1 GiB se mostraban como «0 GiB»** — ahora
  en MB con un decimal.

## [0.16.16] — 2026-11-22

### Añadido

- **SQL Server: restauración desde el catálogo con asistente (RF-42)**:
  - Constructor de la cadena mínima FULL → DIFF → LOG desde los puntos
    del catálogo, con continuidad de LSN (comparación big.Int), corte
    temporal opcional (STOPAT) y detección de huecos con error explícito.
    Base = último FULL no COPY_ONLY; un DIFF es válido si hereda el
    `database_backup_lsn` del FULL base.
  - Endpoint `POST /api/v1/admin/sql/restore-chain` que devuelve la
    cadena ordenada (URIs en orden de aplicación) o `valid=false` con el
    motivo (falta un FULL, hueco de logs…).
  - **Asistente en el panel**: botón Restaurar en los puntos SQL → modal
    con base destino (in situ u otro nombre con aviso de MOVE), punto en
    el tiempo, cadena calculada visible (tipo, fin, LSN, tamaño) y
    confirmación destructiva obligatoria antes de lanzar el job.
  - **Restauración en otra instancia**: el asistente permite elegir el
    agente/servidor/instancia de destino — la cadena se calcula sobre los
    puntos del origen y el job se ejecuta en el agente destino. Motor
    limpio: mismo nombre de base sin MOVE; destino existente: WITH
    REPLACE.

### Corregido

- **Puntos de recuperación (general)**: los puntos SQL ya no muestran el
  botón de restauración de ficheros (abría el flujo RESTORE_FILE y
  fallaba con «el destino debe ser una ruta absoluta», hallazgo del
  lab); en su lugar enlazan a la pestaña SQL Server.

## [0.16.15] — 2026-11-22

### Mejorado

- **SQL Server: el error 262 de `RESTORE VERIFYONLY` se traduce a la acción
  concreta** (hallazgo del lab P-25): VERIFYONLY crea internamente una
  base temporal y exige el rol de servidor `dbcreator` para el login del
  agente. Cuando falta, el mensaje del job explica el permiso requerido y
  la sentencia exacta `ALTER SERVER ROLE dbcreator ADD MEMBER [...]`
  en vez del error T-SQL crudo.

## [0.16.14] — 2026-11-22

### Corregido

- **SQL Server: la captura de LSNs fallaba tras un backup correcto**
  (hallazgo del lab P-26 en real): los LSN de `msdb.dbo.backupset` son
  `numeric(25,0)` — hasta 25 dígitos — y un LSN real de 21 dígitos
  (`338420000000652000001`) desborda `uint64`, matando el job con
  «value out of range» **después** de haber transferido el backup al
  repositorio. El tipo interno `connector.LSN` pasa a texto decimal
  validado con comparación `big.Int` (la validación de cadenas por LSN
  sigue siendo numérica), el catálogo PostgreSQL inserta con cast
  explícito a `numeric(25,0)` y los resultados de jobs ya los llevaban
  como texto. Sin cambio de contrato externo.

## [0.16.13] — 2026-11-22

### Corregido

- **SQL Server: el staging se pregunta a la propia instancia** — el acceso
  directo al registro de Windows (0.16.12) falló en el host de laboratorio
  (las claves `MSSQL<n>.<instancia>` no estaban donde se esperaban). Vía
  canónica e independiente de versión/instancia: `xp_instance_regread`
  sobre `BackupDirectory` ejecutado contra la instancia, con fallback a la
  ruta de datos por defecto (`SERVERPROPERTY('InstanceDefaultDataPath')`).
  Resolución final: `BDR_SQL_STAGING` → directorio reportado por la
  instancia → temp del proceso. Restore, verify y custodia usan la misma
  resolución.

## [0.16.12] — 2026-11-22

### Corregido

- **SQL Server: staging escribible por la cuenta del servicio SQL**
  (hallazgo del lab P-24): el staging por defecto caía en el temp del
  agente (`C:\WINDOWS\SystemTemp`), que la cuenta del servicio de SQL
  Server no puede escribir — el BACKUP moría con error 3201/OS 5
  «Acceso denegado». Ahora el default en Windows es el `BackupDirectory`
  de la instancia, leído del registro (`MSSQL<n>.<instancia>`), donde SQL
  escribe por definición y el agente (SYSTEM) lee sin problema.
  `BDR_SQL_STAGING` conserva la prioridad; fuera de Windows sigue el temp
  de lab. El progreso del job muestra el staging elegido.

## [0.16.11] — 2026-11-22

### Corregido

- **SQL Server: el backup fallaba con error 102 de T-SQL** (hallazgo del
  lab P-28, primer backup real contra SQL Server 2022): el motor generaba
  `BACKUP DATABASE N'nombre'`, pero el nombre de base en
  `BACKUP/RESTORE DATABASE|LOG` es un **identificador**, no un literal de
  cadena. Ahora se genera `[nombre]` con escape de corchetes (`]` doblado);
  los apóstrofos son legales dentro de los corchetes. `MOVE`, `STOPAT` y
  `DISK` siguen siendo literales (posición correcta en la sintaxis).

## [0.16.10] — 2026-11-22

### Corregido

- **Panel: el auto-inventario de SQL Server se disparaba por cada tecla**
  (hallazgo del laboratorio): al escribir `LOCALHOST` en el campo
  servidor, el efecto lanzaba un job `SQL_INVENTORY` por letra (`L`,
  `LO`, `LOC`…), fallando contra hostnames inexistentes y ensuciando el
  histórico de trabajos. Ahora espera 700 ms tras dejar de escribir y
  cada tecla cancela el intento anterior; el valor se recorta antes de
  usarse. Un job por nodo, no por tecla.

## [0.16.9] — 2026-11-22

### Añadido

- **SQL Server: verificación programada y DBCC CHECKDB (RF-44)**: campo
  `verify_cron` opcional en los planes SQL — VERIFYONLY periódico del
  último backup del plan contra el destino, con deduplicación y
  reprogramación automática. Job nuevo `SQL_CHECKDB` que ejecuta
  `DBCC CHECKDB (…) WITH NO_INFOMSGS, ALL_ERRORMSGS`: base sana → OK;
  corrupción → el job falla con los mensajes de DBCC y el control plane
  levanta alerta. Endpoint `POST /api/v1/admin/sql/checkdb` y botón en el
  panel.
- **SQL Server: cifrado nativo y custodia de certificados (RF-45)**:
  `WITH ENCRYPTION` (AES_128/192/256 por certificado de master) en
  backups y planes (`encrypt_cert`, con validación dura: cifrar sin
  custodia → rechazado). Job `SQL_CERTBACKUP` custodia el `.cer` y el
  `.pvk` (cifrado con contraseña generada por el control y guardada
  sellada) en el destino de almacenamiento. Alerta crítica
  `sql_cert_missing` cuando un backup llega cifrado sin custodia
  registrada (medio irrecuperable); se resuelve sola al custodiar.
- **SQL Server: runbook de master (RF-46)**: `bdr-sql runbook-master`
  genera el procedimiento single-user exacto (NET STOP, sqlservr -m,
  RESTORE, arranque) con el orden de restauración de instancia completa.
- **Panel: inventario de bases automático**: al elegir agente+servidor en
  la pestaña SQL Server, el panel consulta la caché del control plane
  (`GET /api/v1/admin/sql/databases`) y si no existe dispara el job de
  inventario y espera el resultado para poblar el selector de bases
  (recovery model, estado, bases de sistema).

### Corregido

- **SQL Server: `MSSQLSERVER` es la instancia por defecto y no se nombra**
  (hallazgo del lab P-24): conectar a `host\MSSQLSERVER` falla con error
  87 del ODBC. El conector normaliza ahora la instancia por defecto fuera
  del DSN y también acepta `host\instancia` escrito en el campo servidor.
- Los puntos de restauración SQL heredan el `PlanID` del job (necesario
  para la verificación programada y la correlación en el panel).

## [0.16.8] — 2026-11-22

### Corregido

- **Windows: selección de sesión interactiva** — se enumeran las sesiones
  WTS y se elige la primera ACTIVA con usuario, con fallback a la de consola
  física. Cubre el caso RDP: `WTSGetActiveConsoleSessionId` solo mira la
  consola física, y en un equipo gestionado por RDP la sesión activa (la que
  tiene el escritorio) es otra — el helper nacía en la sesión equivocada y
  GDI fallaba con «handle invalid».
- **Motivo del fallo del helper sin etiquetas canned**: nuevo mensaje
  `msgHelperError` del protocolo interno — el helper envía por el pipe el
  texto real del fallo antes de salir (su stderr va a NUL) y el agente lo
  muestra verbatim en el panel y en su log. Los mensajes por exit code
  quedan solo como fallback; el falso «captura GDI no disponible» que
  ocultaba el error concreto desaparece.

## [0.16.7] — 2026-11-22

### Corregido

- **Windows: tras autoactualizarse, el agente tardaba minutos en volver (o
  parecía seguir en la versión antigua hasta reiniciar el servicio a mano)** —
  el reinicio salía con `exit(1)` y delegaba en la recuperación del SCM, que
  cuenta cada salida como FALLO del servicio: con `restart/5000, restart/
  10000, restart/300000` y `reset= 86400`, la tercera autoactualización del
  día caía en la ranura de 5 minutos. Síntomas observados: el binario nuevo
  quedaba en disco (`bdr-agent.exe`), el panel seguía mostrando la versión
  vieja y, al reiniciar a mano, aparecía la nueva. Además, si el operador
  re-lanzaba el instalador del panel mientras esperaba, el re-enrolamiento
  creaba fichas de equipo duplicadas (cada una con una versión distinta).
  Ahora el agente para **en limpio** (`SERVICE_STOPPED`, código 0 — no
  consume la recuperación del SCM, que queda para crashes reales, y no genera
  eventos 7031/7034) y un helper interno (`bdr-agent service-restart`) espera
  la salida del proceso y arranca el servicio vía SCM en 1-2 s con el binario
  nuevo. Nota de despliegue: el fix viaja en el binario nuevo, así que la
  actualización HACIA 0.16.7 puede seguir necesitando un reinicio manual del
  servicio; a partir de ella, las siguientes son automáticas.
- **El relevo en consola/tarea programada heredaba stdio inválido**: en
  procesos sin consola los handles estándar son NULL/muertos y `cmd.Start`
  falla con «The handle is invalid» — el agente quedaba vivo con el binario
  viejo mientras el nuevo ya estaba en disco. Ahora el hijo solo hereda
  stdio cuando es utilizable (`Stat` correcto) y arranca desacoplado
  (`CREATE_NO_WINDOW`).

## [0.16.6] — 2026-11-22

### Corregido

- **Windows: el helper no aterriza en la sesión interactiva del usuario** —
  el log de 0.16.5 lo acusa: «escritorio interactivo no abrible» +
  «BitBlt: The handle is invalid» con DC y DIB válidos. Lanzado con el
  token de WTSQueryUserToken, el proceso puede quedar en la sesión del
  servicio (0), donde WinSta0 no tiene escritorio real (GetDC «funciona»,
  BitBlt falla). Ahora se aplica el patrón canónico de «iniciar proceso
  interactivo desde un servicio»: `DuplicateTokenEx(TokenPrimary)` +
  **`SetTokenInformation(TokenSessionId)` explícito** con la sesión de
  consola antes del `CreateProcessAsUser`.

### Añadido

- El helper registra su contexto de seguridad en el log:
  sesión del proceso, window station, escritorio del hilo y usuario
  (DOMAIN\usuario) — el diagnóstico decisivo para este tipo de fallo;
  y el fallo de BitBlt incluye ahora también el oldBmp (detecta fallos
  de SelectObject).

## [0.16.5] — 2026-11-22

### Corregido

- **Windows: panic del helper 0.16.4 en el binding de escritorio** —
  `GetThreadDesktop` y `SetThreadDesktop` son funciones de **user32.dll**
  (los escritorios son objetos USER), no de kernel32. El log de
  diagnóstico de la 0.16.4 lo localizó con stack exacto
  («Failed to find GetThreadDesktop procedure in kernel32.dll»); se
  corrige la DLL y el helper continúa hasta la captura real.

## [0.16.4] — 2026-11-22

### Corregido

- **Windows: el helper 0.16.3 moría en silencio y el motivo se perdía por
  dos vías a la vez**:
  - El `recover` del helper estaba instalado DESPUÉS del binding de
    escritorio: un panic en esa fase (stderr→NUL) mataba el proceso sin
    dejar rastro — el log solo tenía «iniciado». Ahora el recover cubre
    todo el cuerpo y cada paso registra línea en
    `%ProgramData%\bdr\screenhelper.log` (LockOSThread, binding, GDI,
    primer frame, stdin).
  - **Carrera exit-code**: el EOF del pipe del helper llega antes de que
    `WaitForSingleObject` publique el código de salida — el agente leía
    «aún vivo» y reportaba un falso «finalizó (código 0)». Ahora espera
    acotada (3 s) al exit code antes de componer el motivo, y los motivos
    por código distinguen pánico (2) de fallo GDI (1) de canal cerrado (0).
- El agente registra ahora en su propio log el ciclo del helper
  («helper de sesión lanzado» / «helper terminó, reason=…») además del
  fichero del helper: dos fuentes independientes para diagnosticar.

## [0.16.3] — 2026-11-22

### Corregido

- **Windows: «BitBlt falló: The handle is invalid» en cada sesión** —
  los DC de `GetDC` y el binding de escritorio (`SetThreadDesktop`) son
  **estado por hilo**, y las goroutines de Go migran entre hilos del SO: el
  DC de pantalla creado en un hilo se usaba en otro. Diagnóstico confirmado
  con el log del helper (0.16.2). Solución canónica:
  - El helper bloquea su goroutine de captura a un único hilo
    (`runtime.LockOSThread`) y fija ahí el escritorio interactivo
    (`OpenInputDesktop` + `SetThreadDesktop`, con el nombre del escritorio
    en el log).
  - El DC de pantalla se adquiere **fresco en cada frame** y se libera al
    terminar — inmune a cambios de escritorio activo (UAC, bloqueo), el
    mismo motivo por el que MeshCentral re-adquiere el DC por captura.
  - `BitBlt` registra en el fallo los handles y la geometría (vx/vy/wxh)
    para diagnóstico inmediato.

## [0.16.2] — 2026-11-22

### Añadido

- **Log de diagnóstico del helper de sesión Windows**: `%ProgramData%\bdr\screenhelper.log`
  registra inicio (pid/versión), resultado de la inicialización GDI, primer
  frame capturado (dimensiones), fin del canal y panics con stack — el
  stderr del helper va a NUL para no contaminar el stream de tiles, así que
  sin este fichero los fallos internos eran invisibles.

### Documentado

- El acceso por pantalla comparte el **escritorio gráfico** del equipo
  gestionado: un host sin sesión gráfica (p. ej. Proxmox headless, servidores
  solo-TTY) no puede compartir pantalla — por diseño. La prueba debe hacerse
  en equipos con escritorio (Windows con usuario logado, Linux con X11).

## [0.16.1] — 2026-11-22

### Corregido

- **Windows: la captura GDI fallaba siempre** — `GetDIBits` se llamaba con el
  bitmap aún seleccionado en el DC (MSDN lo prohíbe: comportamiento
  indefinido). Ahora se usa `CreateDIBSection`: GDI entrega los píxeles
  directamente en memoria y el `BitBlt` escribe sobre ese buffer sin pasos
  intermedios. Era la causa del «finalizó (código 0)» en Windows.
- **Motivo real de terminación visible en el panel**: los backends (helper
  Windows, X11 nativo, bridge VNC) informan ahora la causa de la muerte —
  el agente la envía como `screen_error` y el visor la muestra tal cual
  (p. ej. «el helper falló: captura GDI no disponible», «la captura X11
  falló de forma persistente: …»). El agente también lo registra en su log.
- El helper de Windows ya no comparte el pipe de stdout con su stderr: un
  panic del helper no puede corromper el stream de tiles (stderr → NUL) y
  el agente reporta el código de salida del helper como motivo.
- Linux: si la profundidad del servidor X no entrega un framebuffer BGRA de
  24/32 bpp, la sesión falla con mensaje claro en vez de pintar basura.
- Panel: comprobación previa de protocolo — si el agente aún es 0.15.x
  (RFB/noVNC), el visor avisa «actualiza el agente a 0.16.0+» en vez de
  fallar con un error confuso tras el handshake.

## [0.16.0] — 2026-11-22

### Añadido

- **Escritorio remoto nativo con protocolo de tiles (estilo MeshCentral)** —
  ya no hay que instalar ningún servicio VNC de terceros. El agente captura
  el escritorio él mismo, divide en tiles de 64 px, detecta cambios por CRC
  y envía solo los tiles modificados codificados en JPEG (calidad y ritmo
  ajustables por el visor). El panel sustituye noVNC por un visor canvas
  propio con entrada de ratón/teclado en el mismo canal WebSocket.
  - **Windows**: el servicio (sesión 0) lanza `bdr-agent screenhelper`
    dentro de la sesión interactiva del usuario (`WTSQueryUserToken` +
    `CreateProcessAsUser` → WinSta0\Default, mismo patrón que el `--slave`
    de MeshCentral). El helper captura con GDI (`BitBlt`+`GetDIBits`,
    escritorio virtual completo multi-monitor) e inyecta entrada con
    `SendInput`. Sin CGO, sin VNC externo.
  - **Linux**: captura X11 nativa (`XGetImage` + XTEST vía jezek/xgb, sin
    CGO), con resolución de `XAUTHORITY` del usuario de consola; fallback
    automático a x11vnc bajo demanda (solo loopback) si X11 no es accesible.
  - **macOS**: bridge cliente VNC contra el «Compartir pantalla» integrado
    del sistema (toggle nativo, sin instalaciones); la captura CoreGraphics
    nativa queda como mejora futura (requeriría CGO o permisos TCC).
  - **Protocolo de tiles** documentado en contracts (screen_info, tile JPEG,
    mouse, key por keysym, quality, refresh); framing interno por stdio
    para el helper de Windows. El relay del servidor no cambia (bytes opacos).
  - Panel: canvas con escala automática manteniendo aspecto, Ctrl+Alt+Supr,
    refresco, pantalla completa, liberación de modificadores al perder foco.
  - Eliminada la dependencia `@novnc/novnc` (MPL) del panel.

### Cambiado

- El stream de la sesión de pantalla transporta el protocolo de tiles en
  lugar de RFB/VNC (el visor de la 0.15.0 no es compatible con agentes
  0.16.0 y viceversa: sesiones nuevas, mismo endpoint).

## [0.15.1] — 2026-11-22

### Cambiado

- **Backups de imagen y VM mucho más ligeros con discos dispersos** — la
  captura (`BACKUP_VM` y `BACKUP_IMAGE`) ahora segmenta de forma híbrida:
  el segmento de 64 MiB se escanea en runs de 4 MiB y
  - si está todo a ceros no se sube nada (como antes),
  - si la mitad o más del segmento tiene datos se sube entero (como antes,
    en una sola petición),
  - si está disperso **solo se suben los runs con datos** (4 MiB) y los
    huecos se coalescen en streams `Zero`.
  Un solo byte no nulo en un espacio libre fragmentado pasa de costar
  64 MiB subidos a costar 4 MiB. El manifiesto no cambia de formato
  (compatible con agentes y restores anteriores); el resultado del job
  añade `bytes_uploaded` para ver lo realmente transferido frente a los
  bytes leídos del disco.

### Corregido

- **`image-write` con streams a cero**: el despliegue bare-metal fallaba al
  intentar abrir un fichero inexistente para los segmentos marcados como
  cero en el manifiesto; ahora escribe ceros en su offset sin abrir nada.

## [0.15.0] — 2026-11-22

### Añadido

- **Acceso por pantalla (VNC) en el navegador** — gemelo del canal de terminal
  remota: mismo modelo de relay (agente conecta saliente, servidor solo
  autoriza y retransmite, cero puertos entrantes), visor **noVNC** embebido
  en el panel junto a xterm.js.
  - **API** (`screen:read/connect/terminate/audit`, aditiva): 
    `GET /api/v1/admin/agents/{id}/screen`,
    `POST /api/v1/admin/agents/{id}/screen/sessions`,
    `GET /api/v1/admin/screen/sessions[/{id}]`,
    `POST /api/v1/admin/screen/sessions/{id}/close`,
    `GET /api/v1/admin/screen/sessions/{id}/stream?ticket=…` (WebSocket RFB),
    `GET /api/v1/agent/screen/connect` (canal saliente del agente).
  - **Panel**: botón «Ver pantalla» en la vista de equipo, visor con escala
    automática, Ctrl+Alt+Supr y pantalla completa. La sesión se cierra al
    cerrar el visor (privacidad y ancho de banda).
  - **Agente**: atiende el escritorio local según plataforma:
    - **macOS**: «Compartir pantalla» nativo (VNC integrado del sistema).
    - **Linux**: lanza `x11vnc` bajo demanda (solo loopback, sin contraseña
      VNC — el canal agente-servidor ya autentica con mTLS + API key) o
      reutiliza un VNC existente en `127.0.0.1:5900`.
    - **Windows**: se conecta a un servidor VNC ya instalado y configurado
      para escuchar solo en localhost (TigerVNC/UltraVNC…). La integración
      de un helper propio (binario TNC embebido o servidor RFB en Go) queda
      en el backlog — ver docs/19-roadmap.md.
  - **Detalle de diseño**: el stream de pantalla transporta solo bytes RFB —
    noVNC interpreta cualquier frame de texto como datos corruptos, así que
    los estados de «sesión terminada» viajan en el frame de cierre de
    WebSocket (motivo legible para el operador), a diferencia de la terminal,
    que multiplexa mensajes de control JSON en el mismo canal.
  - Máximo de **2 sesiones de pantalla simultáneas por agente**; sesiones con
    TTL de 30 minutos y reaper propio; auditoría `screen.session.created/closed`.

## [0.14.3] — 2026-11-21

### Añadido

- **Panel: pestaña SQL Server (§23)** con la operativa completa del motor:
  - **Operaciones**: backup manual por agente/instancia/base (tipo
    FULL/DIFFERENTIAL/LOG/COPY_ONLY, destino de almacenamiento, retención
    Object Lock, verificación VERIFYONLY), inventario de bases y creación
    de planes programados.
  - **Planes**: tabla con próxima ejecución, pausar/activar y eliminar.
  - **Puntos de restauración**: fecha, base, tipo, tamaño, último LSN,
    estado de verificación y acción de verificación por punto (job
    SQL_VERIFY).
- La vista de puntos de restauración expone ahora los metadatos SQL
  (`sql`: instancia, base, tipo, LSNs como texto, verificación) — antes
  no llegaban al panel ni a la API de vistas.
- El restore destructivo sigue solo disponible por API (`confirm=true`);
  no se expone desde el panel por seguridad operativa.

## [0.14.2] — 2026-11-21

### Corregido

- **La imagen Docker llevaba agentes sin el motor SQL**: la etapa `agents`
  de `deploy/Dockerfile.server` copiaba rutas selectivas (`packages`,
  `apps/agent`) pero no el árbol `internal/`, donde viven los paquetes del
  motor que el runner SQL del agente importa (`internal/backup`,
  `internal/restore`, …). Por eso el push de la imagen fallaba en el
  release (0.14.0 y 0.14.1) y, de haberse publicado, sus agentes habrían
  respondido `E_JOB_TYPE` a los jobs SQL. Los assets de release **no**
  estaban afectados (la matriz del workflow compila desde el checkout
  completo); el problema era exclusivo de la imagen Docker.
- **CI**: nuevo job `docker-agents` que compila la etapa `agents` del
  Dockerfile en cada push — si el agente pasa a importar un árbol nuevo
  del módulo sin actualizar el COPY, ahora se detecta en CI y no en el
  release.

## [0.14.1] — 2026-11-21

### Añadido

- **Planes programados de backup SQL** (cierra la integración A-11): los
  backups SQL ya no son solo manuales.
  - `store.SQLPlan`: binding directo agente+instancia+base, cron con zona
    horaria, tipo (FULL/DIFFERENTIAL/LOG/COPY_ONLY), destino, retención y
    verificación.
  - API: `POST/GET /api/v1/admin/sql/plans` y `PUT/DELETE
    /api/v1/admin/sql/plans/{id}` con validación de agente, destino y
    expresión cron; el update es conservador (los campos ausentes heredan
    del plan existente).
  - Scheduler: cada tick lanza `SQL_BACKUP` de los planes vencidos con
    deduplicación (no solapa ejecuciones si el agente está ocupado o hay
    un job pendiente; reintenta sin avanzar la programación) y avanza
    `NextRunAt` tras cada lanzamiento.
  - Nota de distribución: esta release completa también la imagen Docker
    `ghcr.io/juansoler/bdr-control`, cuyo push falló de forma transitoria
    en la 0.14.0 (los binarios y el canal firmado de la 0.14.0 sí
    se publicaron correctamente).

## [0.14.0] — 2026-11-21

### Añadido

- **Motor SQL Server integrado en la plataforma (RF-41, A-11)**: los jobs
  del motor corren ahora dentro del agente instalado en el host SQL y el
  plano de control los orquesta y cataloga.
  - **Jobs del agente**: `SQL_BACKUP`, `SQL_RESTORE`, `SQL_VERIFY` y
    `SQL_INVENTORY` (tipos nuevos del contrato). El runner va embebido en
    el agente como los demás (imagen/VM/LXC); agentes sin soporte responden
    `E_JOB_TYPE`. El backup usa la identidad de la cuenta de servicio del
    host SQL (SSPI): nunca viajan contraseñas SQL en los payloads. El
    restore/verify descarga la cadena del destino al staging con streaming
    y la limpia al terminar. Staging configurable con `BDR_SQL_STAGING`.
  - **API del plano de control**: `POST /api/v1/admin/sql/{backup,restore,
    verify,inventory}` (patrón de los jobs ad-hoc de Proxmox; el restore
    es destructivo y exige `confirm=true`) y
    `GET /api/v1/admin/sql/restore-points`.
  - **Catálogo**: cada backup SQL exitoso se ingiere como punto de
    restauración `Kind "sql"` con metadatos propios (instancia, base,
    tipo, LSNs como texto, verificación) para reconstruir cadenas y PITR.
    Esquema PostgreSQL nuevo `sql_backups`/`sql_backup_chains` (migración
    `0002_sql_catalog.sql`; DDL idempotente también disponible para el lab
    con `metadata.PGCatalog.EnsureSchema`).
  - **Transferencia real al repositorio**: backend SMB nativo (`go-smb2`,
    NTLM, sin montaje; streaming y escritura atómica) y backend S3
    (`minio-go`, streaming, Object Lock GOVERNANCE por días) junto al
    local del lab. Los destinos usan el mismo contrato
    (`RepositoryCredentials`) que el resto de la plataforma. Nuevo `Fetch`
    (inverso de `Transfer`) para bajar ficheros del repo al staging.
  - **CLI de lab (`bdr-sql`)**: subcomando `chain` (lista y valida la
    cadena de msdb por LSN), flags `-target`
    (`s3://endpoint/bucket[/prefijo]`, `//host/recurso[/ruta]`, credenciales
    por entorno `BDR_S3_*`/`BDR_SMB_*`) y `-retain-days`.
  - Motor (ya publicado en 0.13.0) corregido: restauración real de cadenas
    (NORECOVERY/RECOVERY, `MOVE` con nombres lógicos de
    `RESTORE FILELISTONLY`, `STOPAT`, rechazo de `master`), contrato único
    de rutas staging/repositorio, captura de LSNs post-backup,
    `VerifyAfter` cableado y 27+ tests unitarios.

## [0.13.0] — 2026-11-21

### Añadido

- **Motor de backup para Microsoft SQL Server (RF-40, fase laboratorio)**:
  paquetes internos (`connector`, `backup`, `restore`, `metadata`,
  `orchestrator`, `storage`) y CLI `bdr-sql` con subcomandos `discover`,
  `inventory`, `backup`, `chain`, `restore` y `health`. La vía de producto
  (jobs del agente, catálogo PostgreSQL, transferencia SMB/S3, panel,
  scheduler) llegará en próximas versiones; esta fase habilita el lab
  (criterios P-24–P-29, requiere VM con SQL Server 2022).
  - Conector TDS (`go-mssqldb`): SQL Auth con DSN escapado (contraseñas
    con `@`/`:` no rompen la URL) y Windows Auth por SSPI; discovery de
    instancias por registro (solo Windows, build tags); inventario de
    bases que excluye `tempdb` (A-12) y cadenas desde `msdb.dbo.backupset`
    con LSNs reales (`numeric(25,0)` casteado a varchar y parseado en Go
    para no truncar).
  - Backups nativos FULL/DIFFERENTIAL/LOG/COPY_ONLY con `COMPRESSION`,
    `CHECKSUM` y `STATS`; verificación `RESTORE VERIFYONLY` tras cada
    backup (configurable con `-verify`, por defecto activada).
  - Restauración real de cadenas: `NORECOVERY` en todos los archivos
    salvo el último (que recupera), rechazo de `master` (exige single-user
    manual, ver RF-46), `WITH REPLACE`, `MOVE` con los nombres lógicos
    reales del backup (`RESTORE FILELISTONLY`, sin asumir el nombre de la
    base) y `STOPAT` para PITR solo en la restauración final; validación
    de continuidad de LSN (`ValidateChain`) y subcomando `chain` que
    lista y valida la cadena de msdb.
  - Captura de LSNs post-backup para el catálogo (antes quedaban a 0);
    contrato único de rutas staging/repositorio con nombres únicos.
  - `bdr-sql` se publica como asset por plataforma en la release, con
    attestations de procedencia y suma SHA-256 verificable.
  - 27 tests unitarios: builders T-SQL (backup/restore), ValidateChain,
    StagingManager, catálogo in-memory, parseo de LSNs y DSN.

## [0.12.10] — 2026-11-21

### Corregido

- **La CI de `main` fallaba siempre en `go test`**: el test del panel
  embebido exigía `out/agents.html`, que solo existe tras `make web-build`
  (en Git únicamente está el placeholder `index.html`). En un checkout
  limpio —como el runner de CI— el test moría con `export sin agents.html`.
  Ahora el test se salta honestamente (`t.Skip`) cuando el export no está
  compilado, y sigue verificando de verdad en los checkouts que sí lo
  tienen.
- **`govulncheck` reportaba 33 vulnerabilidades de stdlib**: la toolchain
  declarada en `go.mod` era `go 1.26.0`, cuya stdlib (`net/http`,
  `crypto/x509`, …) tiene vulnerabilidades conocidas parcheadas a partir
  de la 1.26.1. Bump a `go 1.26.11` (mínimo que las cubre todas). El
  workflow Release también usa `go-version-file: go.mod`, así que los
  binarios publicados a partir de esta versión se compilan con stdlib
  parcheada.
- **`go test` en CI usa `-count=1`**: evita que la caché de tests enmascare
  fallos de entorno.

## [0.12.9] — 2026-11-21

### Corregido

- **Los agentes Raspberry Pi (linux/arm) no se actualizaban**:
  `scripts/build-agents.sh` —el que alimenta el directorio de descargas del
  control— no compilaba `linux/arm`, así que `/opt/bdr/agents/
  bdr-agent-linux-arm` quedaba anclado a la última compilación manual y
  tanto reinstalaciones como ofertas de autoactualización entregaban esa
  versión vieja. Ahora el script compila arm con `GOARM=7` (igual que la
  matriz del release CI).

## [0.12.8] — 2026-11-21

### Corregido

- **Carrera al abrir la consola mientras el agente se reinicia**: si el
  operador creaba la sesión cuando el agente aún no tenía canal, el `open`
  nunca se enviaba y el navegador quedaba en «conectando» indefinidamente.
  Ahora, al conectar un canal de agente, el servidor reenvía la orden de
  apertura a sus sesiones pendientes (`resumePendingSessions`).
- **Las sesiones huérfanas no expiraban**: `ReapSessions` existía pero no
  estaba cableado a ningún bucle. Ahora el plano de control la ejecuta cada
  30 s: las pendientes sin respuesta fallan a los 60 s con error explícito
  y las expiradas se cierran, en vez de dejar el spinner eterno.

## [0.12.7] — 2026-11-21

### Corregido

- **La consola seguía en «Conectando…» con el canal del agente ya conectado**:
  el agente enviaba los mensajes de control con la clave `session`, pero el
  contrato canónico (`packages/contracts.TerminalMessage`) usa `session_id`,
  así que el servidor descartaba el `opened` del agente y el navegador nunca
  recibía la señal de sesión lista. Ahora el sobre del agente usa
  `session_id`, el `opened` incluye su payload, y el servidor acepta el ID
  desde el payload o el sobre.

## [0.12.6] — 2026-11-21

### Corregido

- **Todo handshake WebSocket respondía 501** («Unexpected response code: 501»):
  el middleware `logRequests` envuelve cada respuesta en `statusRecorder`,
  que no delegaba `http.Hijacker`, y `coder/websocket` rechaza el upgrade
  cuando el `ResponseWriter` no lo implementa. Ahora `statusRecorder` delega
  `Hijack()` y `Flush()`. Afectaba tanto al stream del navegador como —de
  forma crítica— al canal del agente (`/api/v1/agent/terminal/connect`),
  que nunca pudo establecerse por este motivo.
- Test de regresión del handshake (crear sesión → dial con ticket → 101;
  ticket inválido → 401).

## [0.12.5] — 2026-11-21

### Corregido

- **El WebSocket del stream fallaba con 401** («HTTP Authentication failed;
  no valid credentials available»): los WebSockets del navegador no pueden
  enviar la cabecera `Authorization`, y la sesión del panel va por Bearer.
  Ahora la creación de la sesión devuelve un **ticket de stream** impredecible
  (`stream_ticket`, 24 bytes aleatorios) y el endpoint `GET …/{id}/stream` lo
  acepta como credencial alternativa vía `?ticket=…` (comparación en tiempo
  constante, ligado a la sesión y destruido al cerrarla). La autenticación
  estándar (cookie/bearer) sigue válida para clientes con cabeceras.
- El panel adjunta el ticket a la URL del WebSocket.

## [0.12.4] — 2026-11-21

### Corregido

- **`POST …/terminal/sessions` con cuerpo vacío devolvía 400
  `E_VALIDATION`** («El cuerpo está vacío»): el handler exigía JSON válido,
  pero la petición tiene todos los campos opcionales. Ahora un cuerpo vacío
  equivale a los valores por defecto y el panel envía `{}` explícito.

## [0.12.3] — 2026-11-21

### Corregido

- **La consola remota se quedaba en «Conectando…»**:
  - Panel: el contenedor de xterm.js solo se montaba tras salir del estado
    «conectando», por lo que el efecto que abre la sesión nunca llegaba a
    ejecutarse (el `ref` era nulo en el primer render). Ahora el contenedor
    está siempre montado y los estados de espera/error son overlays.
  - Servidor: los handlers de terminal parseaban el ID con `pathID(prefijo)`,
    que devuelve el resto completo de la ruta (`{id}/terminal/sessions`),
    produciendo 404 en `POST …/agents/{id}/terminal/sessions`, en
    `GET …/agents/{id}/terminal` y en el WebSocket del stream
    (`{id}/stream`). Ahora leen el ID con `r.PathValue("id")` de las rutas
    con wildcard del mux.

## [0.12.2] — 2026-11-21

### Corregido

- **Compilación multiplataforma del agente** (fallaba el release para
  Windows): `terminal_unix.go` no tenía restricción de build y se compilaba
  también en Windows, chocando con el stub (`StartShell redeclared`), y
  `signalFromName` usaba señales inexistentes allí (`SIGUSR1`, `SIGUSR2`,
  `SIGKILL`, `SIGHUP`). Ahora la terminal Unix lleva `//go:build unix`, el
  stub de Windows declara sus propios imports y cada plataforma define su
  mapeo de señales (en Windows solo `INT` → `os.Interrupt`).
- `gofmt` en ficheros con formato desactualizado (Go 1.26).

## [0.12.1] — 2026-11-21

Correcciones del acceso remoto publicado en 0.12.0: el canal del agente no
llegaba a establecerse y el panel no incluía la consola.

### Corregido

- **Canal de terminal del agente**: el runtime de `remoteaccess` nunca se
  arrancaba desde el bucle del agente, por lo que el servidor nunca veía al
  agente conectado y toda sesión fallaba con `E_AGENT_OFFLINE`. Ahora el
  agente mantiene el canal WebSocket hacia `/api/v1/agent/terminal/connect`
  con reconexión automática (10 s entre intentos, 3 s tras pérdida).
- **Autenticación del dial WebSocket**: `ConnectTerminal` no enviaba el mTLS
  (CA del control + cert de cliente) ni la API key; ahora reutiliza el
  Transport del cliente HTTP del agente y envía `Authorization: Bearer`.
- **Bomba de salida PTY**: faltaba el bucle que lee el maestro de la
  pseudoterminal y lo envía al servidor (frames binarios multiplexados); la
  terminal se quedaba en blanco. Ahora también notifica `exit` con el código
  del shell y cierra las sesiones locales al perder el canal.
- **Panel embebido**: el binario 0.12.0 incluía el export del panel anterior
  al botón «Abrir consola» (el export nuevo no se había copiado a
  `apps/server/internal/webui/out` antes de compilar).

### Añadido

- **Consola web en la ficha del equipo**: botón «Abrir consola» que abre una
  terminal xterm.js conectada por WebSocket al stream de la sesión
  (`/api/v1/admin/terminal/sessions/{id}/stream`), con resize al cambiar de
  ventana y envío de teclas como frames binarios.

## [0.12.0] — 2026-11-21 — Acceso remoto

### Añadido

- **Acceso remoto a terminales (Remote Access, Phase 1)**: subsistema de
  administración remota vía agente con conexión saliente mTLS. El panel crea
  sesiones de terminal y el agente abre un PTY con shell (unix; ConPTY queda en
  fase posterior); el flujo de I/O multiplexa terminal con control vía WebSocket
  (control JSON en texto, datos binarios con prefijo de longitud).
- **RBAC extendido**: capacidades `terminal:read`, `terminal:connect`,
  `terminal:elevated`, `terminal:terminate`, `terminal:audit`; el rol admin
  admite elevación con TOTP como paso adicional de seguridad.
- **Documentación**: ADR `docs/adr/0012-remote-access.md` y spec técnica
  `docs/25-remote-access.md`.

## [0.11.4] — 2026-11-21

### Corregido

- Aprovisionamiento de repos: los errores de Kopia ya no se truncan a 3
  líneas antes de envolverse en el error, lo que ocultaba la causa real del
  fallo (credenciales, región SigV4, lock del proveedor, cuota, etc.).
  Ahora se propaga el detalle completo y el llamador aplica su límite (8).

### Añadido

- Detección automática de la región S3 del destino a partir del endpoint
  (Hetzner Object Storage, DigitalOcean Spaces, AWS S3, Wasabi, B2,
  Scaleway, Backblaze): se aplica a verify, sync-to, connect/create de
  repositorios S3 y a las credenciales entregadas al agente cuando el
  destino no tiene región registrada.

### Cambiado

- El plano de control ejecuta Kopia con `--no-use-keychain` /
  `--no-persist-credentials` (la contraseña del repo la gestiona el control
  vía env `KOPIA_PASSWORD`; keychain/persist eran no determinados en
  servicios con PATH mínimo).

## [0.11.10] — 2026-11-21

### Corregido

- **Auto-actualización del agente definitivamente arreglada**: la descarga
  del binario usaba el cliente HTTP por defecto, sin la CA del control ni el
  certificado de cliente; contra el listener mTLS de agentes fallaba con
  `tls: failed to verify certificate` y la oferta nunca se aplicaba (aunque
  llegara — así se veía en el log del agente). Ahora el agente descarga con
  su identidad mTLS (CA del control + cert de cliente), igual que en el
  heartbeat.
- La oferta apunta al listener público (`--public-url`) cuando existe: los
  agentes ya desplegados (con el cliente roto) pueden actualizarse solos
  esta última vez; la integridad queda garantizada por el SHA-256 que viaja
  en la oferta por el canal mTLS autenticado. Sin `--public-url`, la URL es
  relativa y la resuelve el agente contra su listener mTLS.

## [0.11.9] — 2026-11-21

### Añadido

- **Adoptar repositorio existente**: al crear/editar un destino se acepta la
  contraseña del repo Kopia (`repo_password`). Con ella, el control CONECTA
  al repositorio ya existente (creado por otra instancia) y nunca formatea:
  si la contraseña no corresponde, el error lo dice claro. Nuevo endpoint
  break-glass `GET /admin/storage/targets/{id}/repo-password` (admin,
  auditado) para custodiar la contraseña offline o adoptarla en otra
  instancia. Con esto, un control X perdido puede ser sucedido por uno Y
  sobre los mismos datos (bucket+prefijo+contraseña).
- **Forzar actualización de agente**: nuevo endpoint
  `POST /admin/agents/{id}/upgrade` y botón «Forzar actualización» en la
  ficha del equipo. El próximo heartbeat entrega la oferta marcada como
  forzada; el agente la aplica al quedar libre (no interrumpe un job) y el
  flag se consume al entregarla. La ficha muestra «solicitada hace…».
- Instaladores (macOS/Linux/Windows) copian `agents.json` junto al binario
  del control: sin él no había ofertas de autoactualización (causa raíz de
  «los agentes no se actualizan solos»).

### Corregido

- Ofertas de autoactualización sin `agents.json`: si el manifiesto local no
  existe, la oferta se construye desde el canal firmado (último
  `latest.json` verificado) comprobando el SHA-256 del binario servido —
  sin riesgo de bucles ni dependencia de `--public-url`.

## [0.11.8] — 2026-11-21

### Corregido

- Detección de actualizaciones del plano de control: la cadencia del chequeo
  del canal pasa de 6 h a 1 h — una release publicada minutos después de un
  chequeo tardaba hasta ~7 h en aparecer en el panel, lo que obligaba a
  reiniciar el servidor para forzarla.

### Añadido

- Chequeo de actualizaciones bajo demanda: nuevo endpoint
  `POST /api/v1/admin/system/update/check` (admin) que consulta el canal al
  instante y devuelve el estado fresco. El panel ahora fuerza un chequeo al
  abrirse y añade el botón «Comprobar» en el pie del menú (muestra "al día"
  con la hora o el error del canal).

## [0.11.7] — 2026-11-21

### Corregido

- Endpoints S3 con esquema y puerto (p. ej. `http://10.10.9.58:9000`): el
  puerto indicado se respeta en verificación, réplica (`sync-to`),
  aprovisionamiento y credenciales entregadas al agente. El esquema ya no
  llega a kopia (minio-go interno lo rechaza con "Endpoint url cannot have
  fully qualified paths"): se normaliza a host:puerto y el TLS lo decide
  `no_tls`/el esquema registrado.

## [0.11.6] — 2026-11-21

### Corregido

- Aprovisionamiento de repos S3: el plano de control (launchd/systemd) se
  ejecuta sin la variable HOME, lo que rompía la inicialización de
  repositorios Kopia (`unable to set up caching: unable to determine cache
  directory: $HOME is not defined`). Se fija HOME en el entorno del proceso
  de kopia; connect/create de repositorios S3 ahora pueden completar la
  inicialización.

## [0.11.5] — 2026-11-21

### Corregido

- Aprovisionamiento de repos: el error de Kopia ya no se trunca a 300
  caracteres (`firstLines`), de modo que el motivo real del fallo (conexión
  storage/SigV4, credenciales, lock del proveedor, cuota…) llega completo al
  error del job y a los logs.

### Añadido

- El heartbeat del agente adopta la `AgentURL` vigente del plano de control
  (campo `agent_url` en `HeartbeatResponse`): si el operador cambió el
  `--agent-url` (p. ej. de LAN a NetBird) tras un enrolamiento, el agente
  reconfigura su endpoint y persiste el cambio sin re-enrolarse.

## [0.9.5] — 2026-11-20

### Añadido

- Auto-actualización de agentes coherente de una vez: la oferta lleva URL
  RELATIVA (el agente la resuelve contra el servidor con el que ya hace
  heartbeat — NetBird, LAN — sin depender de --public-url) y el SHA-256 se
  toma de agents.json: el manifiesto de SHAs de los binarios de agente de
  ESTA versión del control, generado en el release, en el build de Docker y
  escrito por el propio self-update junto al binario. La oferta solo se
  emite si el fichero servido coincide con ese SHA — fin del bucle de
  binarios desincronizados.

## [0.10.8] — 2026-11-20

### Corregido

- Instalador de macOS: la plist de launchd se genera con plistlib (XML
  válido y valores de entorno escapados) en vez de heredoc+sed, y el
  arranque se verifica con healthz antes de dar el ok.

## [0.10.7] — 2026-11-20

### Corregido

- La conexión Kopia del control registra el error real
  (kopia repository status) en el aviso "sin conexión directa" — antes se
  tragaba y la depuración era a ciegas.

## [0.11.2] — 2026-11-21

### Corregido

- Aprovisionamiento de repos en S3: pasa --region=<región> del destino
  (la guía oficial de Hetzner la exige) — el verify y el sync-to ya la
  llevaban, el create/connect del aprovisionamiento no.

## [0.11.1] — 2026-11-21

### Corregido

- Aprovisionamiento de repos: el error del create muestra el detalle
  completo de kopia (8 líneas) — antes se truncaba a 2 y la causa real
  (quotas, lock del proveedor, región…) quedaba invisible.

## [0.11.0] — 2026-11-21

### Añadido

- **Autodetección de kopia junto al binario del control**: si `kopia` vive en
  el directorio del propio binario (layout /opt/bdr, Docker) se usa aunque
  launchd/systemd no lo vean en el PATH — el flag `--kopia-binary` queda
  como override explícito. Elimina la clase de fallos "sin conexión directa"
  por PATH mínimo de servicios.

## [0.10.6] — 2026-11-20

### Corregido

- Imagen Docker: layout unificado de verdad (control y kopia en
  /opt/bdr/bin con --kopia-binary en el ENTRYPOINT) — el paso agents.json
  fallaba escribiendo en un directorio inexistente y la imagen no llevaba
  el kopia-binary que las ofertas de auto-actualización necesitan.
- Instalador Linux: /opt/bdr/agents propiedad del usuario del servicio
  para que el self-update pueda refrescar los agentes.

## [0.10.5] — 2026-11-20

### Añadido

- Destinos S3: campo Región (opcional) — el verify la pasa al cliente
  MinIO y la réplica sync-to ya la incluía. Útil en proveedores que exigen
  la región en la firma (hel1 en Hetzner).
- Mensaje de fallo de escritura con pista accionable cuando el Object Lock
  es la causa (bucket creado sin lock — InvalidRequest en Hetzner).

## [0.10.4] — 2026-11-20

### Corregido

- Destinos S3: validación del nombre de bucket — un "/" o espacios son
  inválidos en S3 y la escritura de prueba falla con InvalidRequest sin
  pista (en S3 no hay carpetas: usa buckets distintos o el campo prefix).

## [0.10.3] — 2026-11-20

### Añadido

- Soporte de agentes en Raspberry Pi de 32 bits: plataforma linux/arm
  (GOARM 7) en la matriz de release, en el manifiesto de agentes y en el
  selector del panel.

## [0.10.2] — 2026-11-20

### Corregido

- Instalador de macOS: verifica que el servicio responde tras el arranque
  (bootstrap + load heredado) y FALLA con el log del daemon si no — antes
  imprimía éxito aunque el control viejo siguiera corriendo.

## [0.10.1] — 2026-11-20

> Publicada sobre la v0.10.0 y sustituye también a la v0.9.5 (misma fecha,
> numeración incorrecta — este es el contenido real del fix de
> auto-actualización de agentes).

### Añadido

- Auto-actualización de agentes coherente de una vez: la oferta lleva URL
  RELATIVA (el agente la resuelve contra el servidor con el que ya hace
  heartbeat — NetBird, LAN — sin depender de --public-url) y el SHA-256 se
  toma de agents.json: el manifiesto de SHAs de los binarios de agente de
  ESTA versión del control, generado en el release, en el build de Docker y
  escrito por el propio self-update junto al binario. La oferta solo se
  emite si el fichero servido coincide con ese SHA — fin del bucle de
  binarios desincronizados.

## [0.10.0] — 2026-11-20

### Añadido

- Instalador de servidor interactivo: pregunta en qué interfaz publicar el
  servicio (LAN, NetBird… detectadas automáticamente) y si usar IP, hostname
  o un nombre DNS propio. Prioridad: --public-host > BDR_PUBLIC_HOST >
  pregunta (solo con terminal; sin terminal publica el hostname con aviso).
- Instalador de Windows: parámetro -PublicHost equivalente, y ahora fija
  --public-url y --agent-tls-names coherentes.

## [0.9.4] — 2026-11-20

### Corregido

- Instaladores de agente: detienen el `bdr-agent` en marcha ANTES de
  descargar el binario — Linux prohíbe reescribir el binario de un proceso
  en ejecución (`ETXTBSY`) y el curl fallaba con (23) escribiendo sobre él.
- Mensaje del fallo de descarga de binario: menciona también las causas
  locales (disco lleno, permisos del destino), no solo el servidor.

## [0.9.3] — 2026-11-20

### Corregido

- One-liner del panel en dos pasos (`curl … -o /tmp/bdr-install.sh && sudo
  bash /tmp/bdr-install.sh`): la sustitución de proceso con sudo daba
  "Bad file descriptor" en macOS (sudo cierra los fds extra).

## [0.9.2] — 2026-11-20

### Corregido

- La descarga del binario de agente ya no depende del estado del
  directorio de descargas del control: si el binario local no está, el
  endpoint REDIRIGE a la release pública de la misma versión, y el
  instalador generado tiene fallback explícito a GitHub. Fin de los
  E_NO_BINARY en los enrolamientos.

## [0.9.1] — 2026-11-20

### Corregido

- Instalador de agente Linux: al re-instalar sobre un agente ya activo,
  reinicia el servicio — sin esto el proceso seguía con la config vieja
  (heartbeats al control anterior) tras re-enrolar.

## [0.9.0] — 2026-11-20

### Añadido

- La actualización del servidor refresca también los binarios de agente del
  directorio de descargas: se descargan de la release firmada (verificación
  SHA-256) en staging ANTES de tocar el binario en marcha — los agentes que
  se enrolen después reciben la versión nueva sin pasos manuales y la oferta
  de actualización no entra en bucle.

### Corregido

- Panel embebido: `/agents` servía la portada del panel (fallback SPA) hasta
  que el router de cliente recuperaba la página — el handler resuelve ahora
  `<ruta>.html` (export plano de Next), reescribe la URL para
  `http.FileServer` y añade tests de regresión.
- Instaladores: mensajes accionables cuando falta el binario del agente en
  el servidor o falla el enrolamiento (antes se enmascaraban con `curl: (23)`).

### Cambios de CI

- `govulncheck` fijado a v1.10.0 (no reproducible con `@latest`).

### Versiones intermedias no publicadas con entrada propia

- 0.8.7: `--kopia-binary` en el launchd de macOS.
- 0.8.8: one-liner `bash <(curl …)` y mensajes accionables del instalador.

## [0.8.6] — 2026-11-19

### Corregido

- Instaladores nativos: inicializan el repositorio Kopia (kopia-init nativo,
  idempotente) y exportan BDR_KOPIA_KIND y BDR_KOPIA_REPO_PATH — sin esto
  los jobs fallaban con 502 E_REPO_PROVISION.
- Unidades: --kopia-binary /opt/bdr/bin/kopia (systemd no ve /opt/bdr/bin
  en el PATH).

## [0.8.5] — 2026-11-19

### Cambiado

- Layout nativo unificado con Docker: binarios en /opt/bdr/bin y agentes en
  /opt/bdr/agents, propiedad del usuario del servicio — la auto-actualización
  del servidor funciona sin ajustes manuales de permisos.

### Corregido

- selfupdate: error accionable cuando el directorio del binario no es
  escribible por el usuario del servicio.

## [0.8.4] — 2026-11-19

### Corregido

- Instaladores: la unidad systemd incluye --downloads-dir — los agentes
  para los one-liners del panel se sirven desde /usr/local/lib/bdr/agents
  sin pasos manuales.
- Bundle de release: incluye bdr-control.service (unidad completa) en lugar
  de la mínima de reserva.

## [0.8.3] — 2026-11-19

### Corregido

- Instaladores: patrón $( [[ ] && echo ) en el bucle de agentes —
  mataba el script en silencio con set -e en plataformas no-Windows.

## [0.8.2] — 2026-11-19

### Corregido

- CI de release: fijar GOOS y GOARCH en la compilación multi-plataforma — todos
  los binarios salían para linux/amd64 aunque el nombre dijera darwin/arm64/windows.

## [0.8.1] — 2026-11-19

### Corregido

- Instaladores: formato del URL de descarga de la release (`/releases/download/<tag>/`).
- Instaladores: los binarios de agente se descargan y verifican con SHA256SUMS.
- Instalador: extracción de la versión del canal con latest.json indentado.
- Build Docker: el panel se cruza entre fases con COPY --from; fuentes autoalojadas (builds herméticos).
- CI: permiso attestations: write.
- Dependencias: klauspost/compress v1.20.2 (GO-2026-6865).

## [0.8.0] — 2026-11-19 — primera versión pública

Almacenamiento flexible (S3 o ruta física) y eliminación de backups.

### Añadido

- **Selección de discos en planes de imagen**: los planes `kind: image`
  aceptan `disks` (índices de disco separados por comas en el panel, array
  en la API) para limitar la captura a discos concretos — p. ej. excluir un
  disco de datos y capturar solo el del sistema. Vacío = todos los discos
  fijos (comportamiento anterior). El filtro viaja en el payload del job
  (`BACKUP_IMAGE.disks`) y lo aplica el agente al enumerar discos; índices
  que no coinciden con ningún disco abortan el job con `E_VALIDATION`.
  PATCH con `disks: []` limpia la selección. La validación de planes
  (`/validate`) es ahora consciente del tipo: no exige rutas en imagen y
  avisa de qué discos quedan fuera.
- **Destinos SMB con autenticación AD**: las rutas físicas aceptan
  compartidas SMB (`//servidor/recurso[/subdir]` o `smb://…`) con cliente
  SMB nativo (go-smb2, sin montar ni root) y credenciales NTLM cifradas en
  el store (usuario, contraseña y dominio AD/workgroup). Válidas para
  backups de imagen, VM y LXC (subida directa por segmentos), verificación,
  borrado de backups y lectura de manifiestos desde el panel. La réplica
  del repositorio a SMB usa `kopia sync-to rclone` (requiere rclone
  instalado). Los planes de archivo no admiten destinos SMB (kopia no
  escribe en compartidas sin montar); S3 o ruta local sí.
- **Backup y restauración de contenedores LXC de Proxmox**: job
  `BACKUP_LXC` — `pct config` → `vzdump --stdout` (snapshot, con fallback a
  stop si el storage no soporta snapshots) → compresión zstd en el agente →
  subida por segmentos al destino → manifiesto con la config cruda del CT.
  Job `RESTORE_LXC`: descarga verificando SHA-256 por segmento y `pct
  restore` — en sitio (stop → destroy → restore → start, mismo VMID) o CT
  nuevo (VMID libre, hostname opcional, storage del manifiesto o elegido).
  Puntos de restauración `Kind: lxc`; panel Proxmox con botón Backup y
  restauración para LXC. Requiere agente actualizado en el nodo PVE.
- **Destinos de almacenamiento con ruta física**: además de buckets
  S3-compatible, un destino puede ser una ruta absoluta local o NAS montado
  (`Kind: filesystem`). Verificación (crea y borra un archivo de prueba),
  réplica del plano de control (`kopia repository sync-to filesystem`) y
  aprovisionamiento del repo Kopia del destino (`repository connect/create
  filesystem`, contraseña cifrada en el store). El prefijo actúa como
  subdirectorio de la ruta. Los planes de imagen aceptan ambos tipos.
- **Backups de imagen/VM sobre ruta física**: el destino de subida del agente
  (`newUploadDest`) acepta repositorios `filesystem`; los segmentos y
  manifiestos se escriben como ficheros (escritura atómica temporal+rename,
  reanudación incremental por listing de claves). Cada repo destino del
  agente usa su propia config de Kopia (`kopia-<id>.config`).
- **Eliminación de backups**: `DELETE /api/v1/admin/restore-points/{id}`
  (rol admin, auditado). Puntos de archivo (`file`): `kopia snapshot delete
  --delete` en el repo central o en el repo del destino. Puntos de
  imagen/VM: borra el manifiesto y sus segmentos del destino (S3 con bypass
  de Object Lock GOVERNANCE, ficheros en la ruta física). El registro sale
  del catálogo; los restore points de archivo registran ahora su destino
  (`TargetID`) para borrar en el repositorio correcto.
- Panel: página de Almacenamiento con selector de tipo (S3 / ruta física) y
  campos condicionales; botón **Eliminar** con confirmación en Puntos de
  recuperación; textos de planes y Proxmox actualizados a "S3 o ruta física".

---

## [0.7.0-dev] — 2026-11-19

Proxmox: backup y restauración de VMs desde el panel (Fase 2 — PVE).

### Añadido

- **Backup de VMs de Proxmox**: job `BACKUP_VM` ejecutado por el agente
  instalado en el nodo PVE. `qm config` → discos vía `pvesm path` →
  `qemu-nbd` read-only → subida en streaming por segmentos al bucket del
  destino (misma tubería que imagen: SHA-256, incremental, salto de ceros,
  Object Lock opcional). El manifiesto v1 lleva la configuración cruda de
  la VM (`Kind: vm`).
- **Restauración de VMs** (`RESTORE_VM`): en sitio (detiene la VM, reescribe
  los volúmenes vía NBD verificando SHA-256, arranca) o **instancia nueva**
  (`qm create` con la configuración del manifiesto —cores, RAM, BIOS,
  machine, red, discos con sus tamaños, efidisk y TPM— y escritura de los
  datos). VMID nuevo automático o elegido.
- Instancias Proxmox: **vinculación de agente** por instancia (PUT), que
  es quién ejecuta los jobs de esa instancia; endpoints
  `POST /instances/{id}/vms/{vmid}/backup`,
  `GET /instances/{id}/vms/{vmid}/restore-points` y
  `POST /admin/proxmox/restore`. Los backups de VM crean puntos de
  restauración (`Kind: vm`) con la clave del manifiesto y el destino.
- Panel Proxmox: selector de destino por instancia, **Backup** por VM,
  listado de puntos de restauración por VM y diálogo de restauración
  (en sitio / nueva VM con nombre). Edición de instancias con binding de
  agente. Guía en `docs/proxmox.md`.

---

## [0.6.0-dev] — 2026-11-18

Agente de Windows como servicio real, autoactualización de agentes y **planes de imagen completa (bare-metal)**.

### Añadido

- **Elección de almacenamiento por plan (repositorio Kopia S3 por destino)**:
  - Cada destino de la sección Almacenamiento puede albergar su **propio
    repositorio Kopia** (contraseña generada y cifrada; el repo se crea
    automáticamente en el bucket al primer uso, `repository connect/create
    s3` con config aislada por destino).
  - El plan elige su destino (`storage_target_id`, selector en el panel);
    por defecto sigue el repositorio central con sus réplicas.
  - El agente pide las credenciales del repo del destino
    (`/agent/repository-credentials?target=`) **solo si tiene un plan
    asignado que use ese destino**; su configuración Kopia por repo vive en
    `<data-dir>/kopia/kopia-<target>.config` sin pisar la primaria.
  - Imprescindible para equipos remotos: un agente Windows ya puede escribir
    imagen/archivos directamente en el bucket S3 del destino.
- **Planes de imagen completa del equipo (Fase 2)**: kind=image — captura
  íntegra de todos los discos fijos (tabla de particiones, ESP, sistema y
  datos) para desplegar el equipo en otro hardware que arranque igual:
  - El job `BACKUP_IMAGE` usa el motor de segmentos con journal y
    reanudación (G0.2): staging local + snapshot Kopia con manifiesto v1
    (particiones, roles, firmware, SHA-256 por segmento).
  - Enumeración de discos por plataforma: `lsblk` (Linux) y
    `Get-Disk/Get-Partition` (Windows). Coherencia crash-consistent.
  - **Despliegue**: `bdr-agent image-write --segments-dir DIR --target
    /dev/sdX --yes` reconstruye el disco byte a byte verificando el SHA-256
    de cada segmento contra el manifiesto. Runbook completo en
    `docs/recuperacion-bare-metal.md`.
  - Panel: selector de tipo de plan (Archivos / Imagen completa) con campos
    condicionales; los planes de imagen se listan sin rutas («equipo
    completo»).
  - El programador y el disparo manual generan `BACKUP_IMAGE` según el kind
    del plan; agentes sin soporte (≤ 0.5) reciben un error explícito de
    tipo de job, no un fallo genérico.
- **Autoactualización de agentes desde el servidor**:
  - El heartbeat del agente reporta su versión; si difiere de la del plano de
    control y hay binario publicado en el directorio de descargas, la
    respuesta incluye `upgrade` con URL y **SHA-256** del artefacto.
  - El agente solo se actualiza **sin un job en curso**: descarga, verifica
    el SHA-256 en streaming y reemplaza su ejecutable por renombrado atómico
    (el anterior queda como `<exe>.old` para rollback manual). Un fallo deja
    el binario intacto y se reintenta en el siguiente heartbeat.
  - Reinicio: en Unix re-ejecuta el binario nuevo in-place (`syscall.Exec`,
    mismo PID, systemd ni se entera); en Windows sale sin reportar `Stopped`
    y la recuperación del SCM lo levanta con el binario nuevo.
  - Agentes antiguos que no conocen el campo `upgrade` lo ignoran (JSON
    tolerante): la oferta no rompe nada.

### Corregido

- **Crear un plan inválido desde el panel devolvía 200 con cuerpo vacío**
  (el panel mostraba «Cannot read properties of undefined (reading 'name')»).
  `withIdempotency` no propagaba los errores de validación (`errProblem`):
  ahora responde 400 con problema RFC 7807 y no cachea el fallo.
- **El agente de Windows ya funciona como servicio**: el binario no hablaba
  el protocolo del Service Control Manager, así que Windows lo mataba al
  ~30 s (error 1053) — síntoma: «conectado» al instalar y `Stopped` minutos
  después. Ahora `bdr-agent run` detecta si lo arrancó el SCM (`svc.Run` con
  handler StartPending/Running/StopPending) y en consola sigue igual.
  En modo servicio los logs van a `<data-dir>\agent.log`.
- Instalador Windows: ruta del binario entre comillas en `BinaryPathName`
  (Program Files tiene espacios) y **acciones de recuperación** del servicio
  (`sc failure`, reinicio 5 s/10 s/5 min) para que un crash se recupere solo.
- `verify` de destinos de almacenamiento: la clave de prueba empezaba por
  `/` sin prefix (URL con doble barra → 307) y minio-go usaba addressing
  virtual-host; ahora path-style y clave limpia.

---

## [0.5.0-dev] — 2026-11-17

Estado del plano de control en PostgreSQL.

### Añadido

- **`store.Postgres`: estado duradero en PostgreSQL** (`--store postgres` con `--db-url` o `BDR_DB_URL`):
  - El documento `State` completo se persiste como JSONB en `control_state` (fila única transaccional); la auditoría encadenada, en la tabla append-only `audit_events` (docs/adr/0007).
  - Migraciones versionadas embebidas (`store/migrations/`) aplicadas al arrancar, con registro en `schema_migrations`.
  - `Update` persiste en la misma llamada; `UpdateLazy` difiere al flusher/`Close` (misma semántica que el fichero); `Flush` y `Close` vuelven lo pendiente.
  - `--import-state <path>` importa un `state.json` (formato 0.2 en adelante) sobre una base vacía; se niega si ya hay estado.
  - Copia de seguridad y restauración de todo el estado con `pg_dump`/`pg_restore`.
- **Batería de contrato del store** (`contract_test.go`): los comportamientos exigibles a todo `Store` (persistencia, error sin persistir, lazy/flush, auditoría que sobrevive al reinicio, continuidad de secuencias) se ejecutan contra `memory`, `file` y `postgres` sin cambios. Los de PostgreSQL se activan con `BDR_TEST_POSTGRES`.
- **Smoke E2E con PostgreSQL**: `make smoke-postgres` (o `BDR_SMOKE_STORE=postgres`) ejecuta el mismo E2E completo con el estado en una base efímera de Docker, incluidos reinicio con persistencia y verificación de la cadena de auditoría.
- **Compose**: servicio `db` (PostgreSQL 16 con healthcheck) bajo el perfil `postgres` y ejemplo de `command` para `control`; guía de migración y copia de seguridad en `deploy/README.md`.

- **Kopia Repository Server: aislamiento por agente** (`BDR_KOPIA_KIND=server` con `BDR_KOPIA_SERVER_URL`/`BDR_KOPIA_SERVER_FP`, docs/adr/0002):
  - Al entregar credenciales (`GET /agent/repository-credentials`) el plano de control **crea o actualiza el usuario del agente en el repositorio** (`kopia server users add/set`); la entrega es idempotente y converge si hubo deriva (`E_REPO_PROVISION` si el repositorio no acepta el usuario).
  - **Rotación sin re-enrolar**: `POST /api/v1/admin/agents/{id}/repo-credentials/rotate` genera contraseña nueva, incrementa la versión y la aplica en el repositorio; `GET .../repo-credentials` expone el estado sin contraseña (`PendingApply` si quedó pendiente de aplicar).
  - **Revocación**: al revocar un agente se borra su usuario del repositorio (best-effort).
  - Normalización a minúsculas del identificador `user@hostname` (exigida por el CLI de Kopia) con auto-curación de credenciales 0.4.
  - **Smoke `make smoke-krs`**: E2E completo contra un Kopia Repository Server real con TLS y pin por huella, más verificación de aislamiento (usuario↔usuario, credenciales robadas limitadas al propio agente) y rotación (la antigua deja de aceptarse, la nueva funciona).
  - Compose: servicio `kopia-server` (perfil `server`) y guía de certificado/huella en `deploy/README.md`.
- **Smoke más robusto**: el agente 2 del flujo de alertas se identifica por su ID de enrolamiento (antes se elegía por orden de lista, no estable); los asertos de conteo de agentes admiten el tercer agente del aislamiento.
- **«Añadir equipo»: instaladores preconfigurados** (POST /admin/installers):
  - El panel genera un instalador (bash para Linux, PowerShell para Windows) con
    la URL del servidor y un token de enrolamiento de un solo uso **embebidos**:
    al ejecutarlo en el equipo destino, el binario se descarga del servidor, el
    agente se auto-enrola y queda conectado como servicio (systemd/Windows) sin
    intervención. One-liner copiable y descarga directa desde el panel.
  - `GET /api/v1/download/agent/{osarch}` sirve los binarios (linux/windows,
    amd64/arm64) desde el directorio de descargas del servidor
    (`--downloads-dir`, por defecto `<data>/downloads`); colócalos con
    `scripts/build-agents.sh`.
  - Panel: modal «Añadir equipo» rediseñado con modo instalador (por defecto)
    y modo token manual.
- **Sección Proxmox en el panel** (nueva página): registro de instancias con
  token de API (secreto cifrado AES-256-GCM), verificación de conexión e
  inventario de VMs/LXC en vivo vía la API de PVE (`cluster/resources`),
  con estado, RAM, disco y cores por máquina. Endpoint
  `GET /api/v1/admin/proxmox/instances/{id}/vms`. La ejecución de
  backup/restore de VMs requiere el agente en el nodo PVE (siguiente slice).
- **Destinos de almacenamiento S3 desde el panel** (página «Almacenamiento»):
  alta de targets S3-compatible con credenciales cifradas (AES-256-GCM),
  Object Lock GOVERNANCE con retención en días, intervalo de réplica
  programada y verificación manual. El plano de control replica su repositorio
  Kopia con `kopia repository sync-to s3` (sin `--delete`: inmutabilidad) y
  verifica cada destino con minio-go (bucket, credenciales y escritura de
  prueba con lock). Endpoints CRUD + `verify` + `sync`; loop de réplica cada
  minuto para los destinos con intervalo > 0. El listado nunca expone el
  secreto; el PUT sin secret conserva el cifrado existente.
- **Explorador: búsqueda y descarga de directorios**:
  - `GET /admin/restore-points/{id}/files/search?q=&path=&limit=`: búsqueda recursiva por nombre (subcadena, sin mayúsculas/minúsculas) con límites de profundidad, directorios visitados y resultados (`Truncated` si se corta). Auditada.
  - `GET .../files/download?path=`: descarga del directorio (o raíz) como **ZIP en streaming**, con límites de ficheros y bytes. Auditada.
  - `FileEntry.path` se rellena ahora en el listado: la navegación de subdirectorios del panel funcionaba solo en la raíz.
  - Panel: caja de búsqueda en el explorador (con «volver al directorio»), botón **ZIP** para el directorio actual y descarga directa desde los resultados.
- **Notificaciones con reintentos**: webhook y email reintentan hasta 3 veces con backoff exponencial (2 s, 8 s; configurable para tests). Un fallo puntual de red ya no pierde la alerta.
- **Puerta «backup de la BD probado»**: en modo postgres, el smoke E2E ahora hace `pg_dump` → `pg_restore` a una base nueva y **reinicia el plano de control contra la base restaurada**, validando persistencia y cadena de auditoría sobre la copia.
- **Panel con TLS en Compose**: servicio `caddy` (perfil `tls`) con certificado interno y HSTS para el despliegue de referencia.
- **mTLS obligatorio y revocación inmediata de agentes** (puerta del roadmap: revocación efectiva < 5 min):
  - El listener de agentes (`--agent-addr`) pasa a **exigir certificado de cliente válido y vigente** en cada handshake: `RequireAndVerifyClientCert` + verificación de política (agente existe, no revocado y certificado el actual) sin reutilización de sesión TLS, de modo que revocar o renovar surte efecto en la conexión siguiente, sin reiniciar.
  - **Ligadura identidad ↔ API key** (docs/06 §2.2): el SAN `urn:bdr:agent:<id>` del certificado presentado debe coincidir con el agente de la API key; en caso contrario `401 E_AUTH_CERT`.
  - Transición: `--insecure-agents` acepta agentes sin certificado (con aviso); `--agent-mtls=false` está deprecado.
  - **`bdr-agent renew-cert`**: renovación bajo demanda de la identidad mTLS (la automática por heartbeat ya existía); útil para operación y para verificar el ciclo.
  - **Smoke E2E sobre el listener TLS**: el flujo completo (heartbeat, jobs, restore) transcurre por mTLS estricto; comprueba que el enrolamiento entrega certificado y `agent_url`, la renovación E2E y la revocación inmediata (el agente revocado pierde el acceso en la conexión siguiente).
  - Tests unitarios de la política end-to-end: listener TLS de pruebas con certificados de cliente (handshake válido, sin cert, ligadura, renovación que invalida el certificado antiguo, revocación instantánea).

### Cambiado

- El instalador Windows arranca el servicio con error visible: antes
  `Start-Service -ErrorAction SilentlyContinue` tragaba el fallo y el agente
  quedaba enrolado pero parado (síntoma: «conectado» al instalar y
  «desconectado» minutos después).
- `pgx/v5` como única dependencia externa nueva (Go ≥ 1.25; `Dockerfile.server` actualizado a `golang:1.25-alpine`).
- `AuditLog` generaliza su persistencia detrás de un sink (archivo, tabla PostgreSQL o memoria); si el sink falla, el evento no se registra ni en memoria, de modo que nunca divergen.
- El smoke sondea la alerta `job_failed` unos segundos en lugar de un `sleep` fijo (elimina un flake de timing).

### Limitaciones

- Un solo escritor por proceso: el documento JSONB da durabilidad y atomicidad, no HA multi-nodo (evolución en docs/adr/0007, cola River en docs/adr/0006).
- El esquema relacional de `docs/05-modelo-datos.md` sigue siendo el objetivo cuando haya consultas SQL reales.

---

## [0.4.0-dev] — 2026-11-17

Fase 1 del MVP: identidad, alertas, programación, restore points y restauración de archivos con Kopia real.

### Añadido

- **Identidad y seguridad del panel:**
  - Bootstrap del primer usuario administrador (`/auth/bootstrap`, o `BDR_ADMIN_EMAIL`/`BDR_ADMIN_PASSWORD`).
  - Sesiones con cookie `HttpOnly` + CSRF (`X-CSRF-Token`), caducidad configurable y revocación al cambiar contraseña o logout.
  - TOTP RFC 6238: alta con QR (`otpauth://`), verificación con tolerancia ±30 s, anti-replay, códigos de recuperación de un solo uso, step-up para acciones sensibles.
  - Contraseñas PBKDF2-SHA256 (600 000 iteraciones), política mínima de 12 caracteres.
  - Tokens de integración `bdt_*` con rol y caducidad; revocación inmediata.
  - RBAC con tres roles: `admin`, `operator`, `viewer`; capacidades verificadas en cada endpoint.
  - Secretos en reposo con AES-256-GCM (`BDR_SECRET_KEY` o `secret.key` 0600 en el directorio de datos).
- **Auditoría encadenada:** registro append-only (`audit.jsonl`) donde cada evento lleva el hash del anterior; `GET /admin/audit`, `/verify`, `/export` (NDJSON). Un hueco o una alteración rompe la cadena y se detecta.
- **Planes completos:** cron de 5 campos (parser propio con alias y tests), zona horaria, ventanas `HH:MM–HH:MM` con cruce de medianoche, retención GFS aplicada como política de Kopia, `rpo_hours`, validación, PATCH y DELETE.
- **Grupos con jerarquía:** miembros, grupos anidados con detección de ciclos (`E_GROUP_CYCLE`); la asignación a un grupo padre cubre a los miembros de los subgrupos.
- **Asignaciones a agente o grupo** (`target_type`/`target_id`, compatible con el `{agent_id}` antiguo); los planes efectivos muestran la vía (agent/group) y qué asignación de grupo queda sombreada por una directa.
- **Programador:** crea jobs vencidos respetando ventanas y ocupación del agente, recalcula `next_run_at`, auto-reintenta jobs con lease perdido mientras queden intentos y alerta cuando un equipo deja de reportar contacto.
- **Jobs ampliados:** idempotencia por `Idempotency-Key` (24 h, 409 si cambia el cuerpo), cancelación cooperativa visible en heartbeat/progreso/`GET cancel`, reintentos, eventos por job con SSE (`text/event-stream`) y JSON paginado, progreso con bytes y mensajes.
- **Puntos de recuperación:** registrados automáticamente al completar un backup (uno por snapshot de Kopia), con tamaño, ficheros, errores y estado de verificación; listado con filtros y cursor.
- **Verificación de restore points:** `kopia snapshot verify` estructural (nivel 1) o con lectura de contenido por porcentaje (nivel 2), en segundo plano; resultado y detalle en el propio punto.
- **Explorador de snapshots:** navegación de directorios vía `kopia show` (JSON interno de Kopia) con validación anti-traversal, y descarga de archivos por streaming (`kopia show` a la respuesta), auditadas ambas.
- **Restauración de archivos:** job `RESTORE_FILE` hacia el equipo de origen u otro agente (mismo SO), con destino absoluto o `in_place`, validación de rutas y resultado por elemento.
- **Alertas:** deduplicadas por clave, severidad (critical/high/medium/low), ack y resolución, re-notificación periódica configurable; webhook firmado con HMAC-SHA256 (`X-BDR-Signature`) y email por SMTP con STARTTLS; prueba de notificaciones.
- **Agente (Kopia real verificado):**
  - Conexión al repositorio desde `GET /agent/repository-credentials` (filesystem: contraseña del repositorio; server: usuario por agente); políticas de exclusión y retención; snapshot con resumen real (tamaño, ficheros, errores); restore.
  - Ciclo con heartbeat concurrente al job, cancelación cooperativa, logs por lotes (`POST /jobs/{id}/log`), heartbeat `RUNNING_BACKUP`/`RESTORING`.
  - mTLS: CSR ECDSA P-256 en el enrolamiento, CA interna propia, pin del servidor (`--server-fingerprint`), renovación de certificado; compatible con `agent.json` 0.2/0.3.
  - Inventario amplio: discos/particiones/LVM/LUKS (lsblk o /sys), red, firmware UEFI/Secure Boot, TPM, BitLocker por PowerShell; histórico por hash en el servidor.
- **Instalación de agentes:** `bdr-agent register`, instaladores Linux (systemd endurecido + `install.sh` + `.deb` generado sin dpkg) y Windows (`install.ps1` con ACL de datos, tarea programada como SYSTEM), `scripts/build-agents.sh` con sumas SHA-256.
- **Despliegue:** Compose con servicio `kopia-init` para crear el repositorio, documentación de secretos y mTLS en `deploy/README.md`.
- **Smoke E2E con Kopia real:** backup con exclusiones → restore point → verify estructural → navegación → descarga → restore de archivo con comparación de contenido → alerta de fallo provocado → SSE → reinicio con persistencia → cadena de auditoría → resumen.

### Cambiado

- `store` rediseñado: documento `State` tipado con transacciones `View`/`Update`; migración automática del `state.json` 0.2/0.3 (con copia `.v1.bak`).
- El plano de control puede conectarse directamente al repositorio Kopia (kopia.config en su directorio de datos) para navegar y verificar; si no, esas funciones se marcan como no disponibles en lugar de fallar.
- `os_family` acepta `darwin` para desarrollo.

### Limitaciones

- Sin PostgreSQL ni River; sin mTLS obligatorio (API key + TLS opcional por listener).
- El webhook/email no reintentan con backoff; los fallos quedan en el log.
- En modo repositorio filesystem todos los agentes comparten la contraseña; el aislamiento por agente exige el modo `server` (ya disponible, ver arriba). Kopia no liga el hostname en el handshake: las credenciales robadas se revocan rotando.
- La verificación de contenido (nivel 2) es muestreada por porcentaje.
- El panel se actualiza a las pantallas nuevas en `apps/web` (ver 0.4.0 del panel).

---

## [0.3.0-dev] — 2026-11-16

Panel web operativo.

### Añadido

- **Panel Next.js 15** en `apps/web` con App Router, TypeScript y Tailwind:
  - Login con token de administración validado contra la API y guardado solo en `sessionStorage`.
  - Resumen con cobertura, planes, trabajos recientes y accesos rápidos.
  - Equipos: estado, SO, hardware, último contacto, último job, búsqueda y **Backup ahora**.
  - Modal de instalación con token de un solo uso, caducidad, comandos Windows/Linux y avisos de seguridad.
  - Planes: creación con rutas/exclusiones y asignación a equipos.
  - Trabajos: filtros por estado, progreso, errores y refresco automático.
- **Proxy runtime** en route handlers de Next:
  - `/api/v1/[...path]` y `/healthz` reenvían a `BDR_API_URL` sin recompilar el panel.
  - Cabeceras de autenticación y errores RFC 7807 preservados.
- **Diseño visual propio**: consola industrial, tipografía Barlow Condensed + IBM Plex, rejilla técnica, acento lima y estados accesibles.
- **Docker**: `deploy/Dockerfile.web` y servicio `web` en Compose.
- **Smoke web**: `scripts/smoke-web.sh` arranca API y panel, valida el proxy y el HTML.
- **Makefile**: `web-install`, `web-build`, `web-dev`, `smoke-web`.

### Limitaciones

- Sin TOTP ni sesiones de servidor; el token vive en la pestaña.
- Sin HTTPS.
- Sin explorador de snapshots ni wizard de restauración.
- El panel no se ha probado en un navegador real automatizado; sí su build, proxy y HTML.

---

## [0.2.0-dev] — 2026-11-16

Planes, jobs y persistencia.

### Añadido

- **Almacenamiento persistente en fichero** (`store.File`):
  - Agentes, tokens, planes, asignaciones y jobs en `state.json`.
  - Escritura con temporal + `fsync` y permisos 0600.
  - Fallback no atómico en Windows cuando `Rename` sobre un destino existente falla.
  - Recarga al arrancar y reconstrucción de índices (hash de API key, asignaciones).
- **Planes de protección**:
  - `POST/GET /api/v1/admin/backup-plans`.
  - Rutas y exclusiones; `kind=file` por ahora.
- **Asignaciones**:
  - `POST /api/v1/admin/backup-plans/{id}/assign`, idempotente por plan y agente.
- **Jobs**:
  - `POST /api/v1/admin/agents/{id}/backup` crea un job `BACKUP_FILE`.
  - Lease con expiración y recuperación de jobs cuyo lease caduca.
  - Progreso y cierre (`succeeded`/`failed`) desde el agente.
  - Listado y detalle administrativo.
- **Ejecución en el agente**:
  - `kopia snapshot create <ruta> --json` por cada ruta del plan.
  - Códigos `E_KOPIA_MISSING`, `E_KOPIA_FAILED`, `E_KOPIA_OUTPUT`, `E_CANCELED`, `E_PLAN_EMPTY`.
  - Runner sustituible; el smoke usa un Kopia falso.
- **Smoke test ampliado**: enrolamiento, plan, asignación, job, ejecución, reinicio persistente y token reutilizado.
- **Tests**: store en memoria y fichero, lease/caducidad, ejecutor, wrapper ausente y flujo API de planes/jobs.

### Cambiado

- El servidor usa `--store file` por defecto (`state.json` en `--data-dir`); `--store memory` sigue disponible.
- `README.md` y `DEVELOPMENT.md` reflejan el estado real.

### Limitaciones

- Sin PostgreSQL ni cola River.
- El wrapper de Kopia no se ha probado contra un repositorio real.
- Un job por agente y heartbeat bloqueado durante la ejecución.
- Sin panel.

---

## [0.1.0-dev] — 2026-11-16

Primer vertical slice funcional.

### Añadido

- **Servidor `bdr-control`** en Go, sin dependencias externas.
  - `/healthz` y `/readyz`.
  - Logs JSON con redacción de secretos.
  - Errores `application/problem+json` (RFC 7807) con `code` y `request_id`.
  - Límites de cuerpo, timeouts y apagado ordenado.
- **Enrolamiento** con token de un solo uso:
  - El administrador crea el token; solo se guarda su SHA-256.
  - El agente recibe una API key una sola vez; solo se guarda su hash.
  - Caducidad, binding opcional a hostname y rechazo de reutilización (410).
- **Heartbeat e inventario**:
  - API key por agente.
  - Estado `pending`/`online`/`offline`/`revoked` calculado por último contacto.
  - Inventario mínimo: hostname, SO, arquitectura, CPU, RAM y disco del sistema.
- **Agente `bdr-agent`**:
  - Comandos `enroll`, `run --once`, `run`, `status`.
  - Configuración local `agent.json` con permisos 0600 y escritura atómica.
  - Inventario específico de Windows (kernel32) y Linux (`/proc`, `os-release`, `statfs`).
  - Compilación cruzada `GOOS=linux` verificada.
- **Tests**: control, store, cliente, configuración e inventario.
- **Smoke test E2E**: servidor + agente reales, token de un solo uso y listado online.
- **Despliegue**: Makefile, Dockerfile y Docker Compose de desarrollo.
- **Documentación**: `DEVELOPMENT.md` y estado de implementación en el README.

### Limitaciones conocidas

- Almacenamiento en memoria; se pierde al reiniciar.
- HTTP sin TLS y API key en lugar de mTLS.
- Sin panel Next.js.
- Sin Kopia, jobs, backup, imagen, P2V ni ISO.
- El inventario no incluye todavía discos/particiones completos, VSS ni BitLocker.

### Seguridad

- No usar fuera de localhost o una red de laboratorio.
- El token de administración se toma de `BDR_ADMIN_TOKEN`; `--dev` usa un valor fijo solo para desarrollo.
