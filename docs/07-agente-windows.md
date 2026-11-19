# Agente Windows

**Documento:** 07-agente-windows  
**Versión:** 1.0  
**Sistemas:** Windows 11, Windows Server 2025  
**Binario:** `bdr-agent.exe` (Go, `GOOS=windows`)

---

## 1. Responsabilidades

1. Registrarse con token de un solo uso y mantener el certificado mTLS.
2. Recoger y reportar inventario de hardware, SO, discos, particiones, firmware y seguridad.
3. Recibir planes y jobs; ejecutar backups programados o bajo demanda.
4. Crear snapshots VSS consistentes.
5. Capturar archivos con Kopia y discos con el motor de imagen.
6. Reportar progreso, logs y resultado.
7. Mantener una cola local y reconciliar tras cortes de red.
8. Restaurar archivos bajo orden.
9. Actualizarse con binarios firmados.

**No hace:** shell remoto, ejecución arbitraria de comandos, gestión de parches, antimalware.

---

## 2. Servicio y privilegios

- Servicio de Windows (`golang.org/x/sys/windows/svc`) con cuenta `LocalSystem`.
- Sin puertos de escucha; solo conexiones salientes mTLS al control plane.
- Configuración en `%ProgramData%\BDR\config.json` con ACL restringida.
- Clave privada protegida con DPAPI/CNG; opcional TPM.
- Logs JSON en `%ProgramData%\BDR\logs\`.
- Estado local en `%ProgramData%\BDR\state\`.
- Reinicio automático y recuperación ante fallo.

---

## 3. Estructura de paquetes (Go)

```
cmd/agent/
internal/agent/
  service/          ciclo de vida SCM, señales, reinicio
  config/           configuración y validación
  enroll/           CSR, par de claves, token, renovación
  transport/        cliente mTLS, reintentos, cola offline
  scheduler/        planes, ventanas, jitter, solapamiento
  inventory/
    inventory_windows.go
  snapshot/
    vss/            shadow set, lectura raw, delta
    freeze/         fallback
  stream/           lector por offset, segmentos, checksum, journal
  kopia/            runner, args, parseo JSON, errores
  manifest/         construcción y validación
  progress/         contadores, muestreo, envío
  limits/           prioridad, CPU, I/O, token bucket
  restore/          restauración de archivos, preflight
  update/           firma, descarga, swap atómico
  logging/          JSON, rotación, redacción
```

---

## 4. Inventario

Se reporta al enrolar, cada 6 h y ante cambio de hash.

| Grupo | Campos |
|---|---|
| Host | hostname, FQDN, dominio, OS/build, arquitectura, zona horaria, uptime, versión de agente |
| Seguridad | UEFI/BIOS, Secure Boot y claves, TPM (versión/presente), BitLocker por volumen (estado, protectores, custodia de clave), Device Encryption |
| Discos | índice, modelo, serial, tamaño, bus (NVMe/SATA/SAS/virtio), GPT/MBR, GUID, sistema, extraíble |
| Particiones | índice, offset, tamaño, type GUID, FS, label, UUID, mountpoint, flags (ESP, MSR, recovery, hidden), BitLocker |
| Volúmenes | Storage Spaces y volúmenes dinámicos (marcados como **no soportados**), letras, puntos de montaje |
| Red | interfaces, MAC, IPs, rutas, DNS |
| Software relevante | versión de Windows, VirtIO/QEMU Guest Agent, gestor de arranque |

El inventario histórico se guarda por hash para auditoría y para el preflight de restauración.

---

## 5. VSS

### 5.1 Implementación

- Usar la API COM de VSS (`IVssBackupComponents`), no `vssadmin` como solución definitiva.
- Librería preferida: `github.com/SmithOperatingSolutions/go-vss` (COM real, snapshots atómicos multi-volumen, `fs.FS` y acceso raw). Fallback: `github.com/mxk/go-vss` (WMI `Win32_ShadowCopy`), con la limitación de atomicidad.
- Crear un shadow set con **todos** los volúmenes relevantes, incluidos ESP y recovery, referenciados por `\\?\Volume{GUID}\` (no solo los que tienen letra).
- Mantener el shadow set vivo durante la lectura; un volumen a la vez para acotar el delta.
- Leer el dispositivo de la shadow copy en crudo (`\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopyN`) con buffer alineado a 4096 bytes (ideal 1 MiB).
- Cerrar el shadow set siempre (`defer` + recuperación al arrancar si el proceso murió).

### 5.2 Writers

- Consultar el estado de los VSS writers (`vssadmin list writers` o COM).
- Si un writer de SQL Server o AD está en error, el job debe fallar con `E_VSS_WRITER` o continuar en modo crash-consistent según la política del plan.
- No se implementa un backup application-aware específico de SQL/AD en Fase 2; se usa el writer de VSS.

### 5.3 Delta de VSS

El shadow storage crece con las escrituras del volumen mientras dura la lectura. Antes de empezar:

- Consultar límite y espacio libre del shadow storage.
- Estimar el delta esperado según carga.
- Monitorizar `UsedSpace/MaxSpace` durante la captura.
- Abortar con `E_VSS_DELTA` si supera un umbral configurable (por ejemplo, 30 %) o si queda poco espacio.
- Registrar el delta observado para ajustar la ventana.

### 5.4 BitLocker y modos de imagen

| Modo | Qué captura | Consistencia | Restauración |
|---|---|---|---|
| `vss_volumes` (por defecto) | Layout del disco en crudo + contenido de cada volumen desde VSS (datos en claro) | Aplicación | No arrastra BitLocker; el volumen restaurado no está cifrado |
| `raw_disk` | Disco físico completo, incluido ciphertext y espacio no asignado | Crash | Puede exigir clave de recuperación y un vTPM compatible |

Reglas:

- Detectar BitLocker y advertir en el plan y en la UI.
- No extraer ni custodiar automáticamente claves de recuperación.
- Si el volumen está bloqueado, `vss_volumes` no puede leerlo: fallar con `E_BITLOCKER_LOCKED`.
- Un vTPM nuevo cambia el EK y rompe el sellado de BitLocker, Windows Hello y credenciales ligadas al TPM. Es un efecto inevitable del P2V y debe avisarse.

---

## 6. Captura de imagen

### 6.1 Secuencia

1. Inventario y preflight de espacio.
2. Decidir `image_mode`.
3. Crear snapshots VSS (o lectura directa si `raw_disk`).
4. Leer el layout de particiones (raw) antes y después; si cambia, marcar inconsistencia.
5. Segmentar cada disco en bloques de 1–8 GiB (adaptativo al enlace y configurable).
6. Para cada segmento:
   - leer el rango;
   - calcular sha256 en streaming;
   - ejecutar `kopia snapshot create --stdin-file=<segmento> --json` con tags;
   - reportar `segments/{n}`.
7. Cerrar el manifiesto y subirlo como snapshot `bdr:kind=manifest`.
8. Eliminar el shadow set y limpiar temporales.

### 6.2 Comando Kopia

```bash
kopia snapshot create \
  --config-file="C:\ProgramData\BDR\kopia.config" \
  --json \
  --description="rp=<run_id> disk=<n> seg=<k>" \
  --tag=bdr:kind=image --tag=bdr:run=<run_id> \
  --tag=bdr:disk=<n> --tag=bdr:seg=<k> \
  --stdin-file=disk<n>.seg<k>
```

El flujo se pasa por stdin; el nombre lógico del archivo es `--stdin-file`.

### 6.3 Segmentación y reanudación

- Tamaño por defecto: 2 GiB en enlaces < 500 Mbps, 8 GiB en 10 GbE; mínimo 1 GiB, máximo 16 GiB.
- Un segmento = una invocación de Kopia.
- Journal local `state/<run_id>.json` con segmentos completados: índice, nombre, tamaño, sha256, snapshot ID.
- Al reanudar, verificar contra el servidor que esos snapshots existen por tag; saltar los completados.
- Nombres deterministas para que un reintento no genere duplicados lógicos.
- Paralelismo: 2 segmentos por defecto, configurable por plan.

### 6.4 Archivo disperso en restauración

El lector de restauración escribe por offset en un archivo disperso del tamaño virtual del disco, evitando duplicar el espacio y permitiendo reanudar por segmento.

---

## 7. Backup de archivos

```bash
kopia snapshot create <ruta> \
  --config-file="C:\ProgramData\BDR\kopia.config" \
  --json \
  --description="plan=<id> run=<run_id>" \
  --tag=bdr:plan=<id> --tag=bdr:kind=file \
  --parallel=4 --checkpoint-interval=10m
```

- Aplicar exclusiones del plan (por ejemplo, `C:\Windows\Temp`, `pagefile.sys`).
- Verificar espacio y permisos antes de empezar.
- Reportar progreso propio; no parsear la barra de progreso humana.
- La reanudación de archivos se delega en el checkpoint nativo de Kopia y en la deduplicación.

---

## 8. Cola local y modo offline

- Cola persistente en `state/queue.json` con jobs recibidos y no confirmados.
- Si no hay conexión:
  - los jobs en curso continúan si no requieren al servidor para escribir (el repositorio puede estar en LAN);
  - los resultados se guardan y se reintentan con backoff;
  - el heartbeat se reintenta sin bloquear el servicio.
- Al recuperar conexión, reconciliar estados por `job_id` y `lease_id`.
- No se ejecuta un job nuevo sin plan vigente; si el plan cambió, se solicita confirmación en el siguiente lease.

---

## 9. Límites y prioridad

- Proceso con `BELOW_NORMAL_PRIORITY_CLASS` y prioridad de I/O baja.
- Token bucket para ancho de banda antes de Kopia; si Kopia soporta límite nativo en la versión fijada, validar cuál es más efectivo.
- Backpressure: si el repositorio va lento, el lector se bloquea; buffer acotado.
- Modo ventana: pausar entre segmentos al acabar la ventana, no a mitad de uno; estado `paused_by_window`.
- Límite de CPU por plan; no usar más de N hilos de Kopia.

---

## 10. Instalación y actualización

- Instalador MSI o script PowerShell firmado.
- Servicio SYSTEM, inicio automático, recuperación ante fallo.
- Registro con `agent register --server <URL> --token <TOKEN>`.
- Actualizaciones: binario + firma Ed25519; la clave pública va embebida; descarga por mTLS; instalación atómica con rollback; rollout por porcentaje y labels.
- El agente rechaza un binario sin firma o con hash distinto.

---

## 11. Seguridad

- Sin shell remoto ni ejecución arbitraria.
- Solo las operaciones del conjunto definido: backup, restore, inventario, cancelación, actualización.
- Redacción de secretos en logs.
- Permisos mínimos sobre el repositorio (usuario Kopia con ACL APPEND/READ sobre su prefijo).
- Protección de la clave privada con DPAPI/CNG.
- Validación de la huella del servidor en el primer contacto.
- No se ejecuta como administrador de dominio; solo SYSTEM local.

---

## 12. Restauración de archivos

1. Recibir job con snapshot, rutas y destino.
2. Preflight de espacio y permisos.
3. `kopia snapshot restore <snapshot_id> <destino>` o restauración selectiva.
4. Verificar hashes si el plan lo exige.
5. Reportar resultado.

Para restaurar en una ruta protegida, el servicio SYSTEM tiene permisos; aun así, se valida que el destino esté permitido por el plan.

---

## 13. Preguntas abiertas (Fase 0)

1. ¿`go-vss` crea un shadow set atómico con ESP y recovery en Server 2025?
2. ¿La vista VSS de un volumen BitLocker desbloqueado es en claro?
3. ¿El delta VSS se mantiene bajo con la carga real?
4. ¿`kopia snapshot create --stdin-file` admite checkpoint o reanudación?
5. ¿El cambio SATA→VirtIO funciona de forma fiable en esta versión de PVE?
6. ¿La inyección offline de VirtIO arranca Windows 11 con Secure Boot y vTPM nuevo?
7. ¿Cuánto tarda un backup de 1 TB y cuánto ocupa el índice?
