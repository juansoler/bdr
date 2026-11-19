# Especificación maestra

**Documento:** 01-especificacion-maestra  
**Versión:** 1.2  
**Estado:** normativo  
**Requisitos relacionados:** `00-informe-ejecutivo.md`, `04-arquitectura.md`, `21-matriz-trazabilidad.md`

---

## 1. Propósito y vigencia

Este documento fusiona las dos especificaciones aportadas por el propietario y el informe adicional en un único contrato normativo. Define **qué** debe hacer la plataforma, con qué prioridad, en qué fase y cómo se acepta. El **cómo** se detalla en los documentos de arquitectura y diseño.

Si hay contradicción entre este documento y otro, prevalece este para el alcance y los criterios de aceptación; los documentos técnicos prevalecen para el diseño.

---

## 2. Alcance

### 2.1 Dentro de alcance

| Tipo | Sistemas |
|---|---|
| Con agente | Windows Server 2025, Windows 11, Debian 12/13 |
| Sin agente | VMs de Proxmox VE 8.2+/9.x mediante PBS |
| Bases de datos | Microsoft SQL Server (conector dentro del agente Windows; `24-motor-sql-server.md`) |
| Destinos | NAS propio (principal) y S3 compatible (réplica) |
| Recuperación | Archivos, bare-metal, P2V/V2V a Proxmox, VMs Proxmox desde PBS, bases de datos SQL Server |

### 2.2 Fuera de alcance

Antimalware/EDR, filtrado de URL, gestión de parches, escritorio remoto, archivado de correo, macOS, móviles, Microsoft 365/Google Workspace, cintas, multi-tenant, facturación y licencias. No se implementa CBT propio en Fase 1/2.

---

## 3. Convenciones

- **Prioridad MoSCoW:** Must (obligatorio), Should (recomendado), Could (evolutivo), Won't (fuera de alcance).
- **Fase:** 0 PoC, 1 MVP archivos, 2 imagen/P2V, 3 robustez, 4 avanzado.
- **Criterio de aceptación:** verificable mediante prueba manual o automatizada, con evidencia.
- **`[VERIFICAR]`:** requiere documentación oficial o prueba real; se registra en `decisiones.md`.
- **Imagen completa:** conjunto de tabla de particiones, metadatos de disco y contenido de cada partición/volumen necesario para reconstruir el disco y arrancar.

---

## 4. Reconciliación de las especificaciones

| Tema | Especificación 1 | Especificación 2 | Decisión vigente |
|---|---|---|---|
| Servidor | Node.js/TypeScript (Fastify/NestJS) | Go | **Go** |
| Agente | Go | Go | **Go** |
| Panel | Next.js | React/Next.js | **Next.js + TypeScript** |
| ORM/BD | Prisma + PostgreSQL | PostgreSQL (SQLite aceptable fase 1) | **PostgreSQL + pgx/sqlc** |
| Cola | Redis/BullMQ | No especificada | **River (PostgreSQL); Asynq/Redis como alternativa** |
| Motor de archivos | Kopia | Kopia | **Kopia** |
| Motor de imagen | Kopia sobre snapshots | Motor propio + Kopia para flujos | **Manifiesto propio + segmentos en Kopia** |
| VMs Proxmox | API Proxmox / posible agente | PBS orquestado por API | **PBS** |
| Restauración a Proxmox | ISO de rescate (estrategia 1) | Agente PVE + escritura directa (opción A/B) | **Agente PVE + `import-from`; ISO como fallback** |
| Acceso al repositorio | Credenciales por agente | Servidor central | **Kopia Repository Server central** |
| Seguridad agente | Bearer API key | mTLS + token de un solo uso | **mTLS + token de un solo uso + API key efímera** |
| Retención | 7d/4w/6m/1a | 7d/4w/6m/1a | **7d/4w/6m/1a, configurable** |
| Idiomas | UI y logs en español | UI, logs y documentación en español | **Español** |

---

## 5. Requisitos funcionales

### 5.1 Backup

| ID | Requisito | Prioridad | Fase | Criterio de aceptación |
|---|---|---|---|---|
| **RF-01** | Backup de imagen completa de todos los discos y particiones, incluida tabla de particiones, EFI, MSR y partición de recuperación cuando existan. | Must | 2 | Restaurar la imagen produce un disco con el mismo layout y la VM arranca. |
| **RF-02** | Backup a nivel de archivos y carpetas, con selección de rutas y exclusiones. | Must | 1 | Restaurar una carpeta y un archivo suelto reproduce contenido y metadatos. |
| **RF-03** | Tras la primera copia, las siguientes son incrementales: solo se transfieren y almacenan datos nuevos, con deduplicación por contenido. | Must | 1/2 | Un segundo backup sin cambios no aumenta apreciablemente el repositorio. |
| **RF-04** | Compresión configurable (por defecto Zstandard). | Must | 1 | El repositorio almacena datos comprimidos y la restauración los expande correctamente. |
| **RF-05** | Cifrado en el cliente antes de salir de la máquina; la clave nunca se almacena junto al repositorio. | Must | 1 | Un acceso directo al backend no permite leer los datos sin la clave. |
| **RF-06** | Consistencia en Windows mediante VSS, incluyendo writers de aplicación cuando existan (SQL Server, AD). | Must | 2 | Un backup de un servidor con escrituras concurrentes restaura una base de datos consistente o el job falla con error tipado. |
| **RF-07** | Consistencia en Linux mediante snapshots LVM, Btrfs o ZFS. Para ext4 sin LVM, imagen crash-consistent marcada como tal. | Must | 2 | La restauración de Debian ext4 y LVM arranca; la UI muestra el nivel de consistencia. |
| **RF-08** | Planes de protección reutilizables: selección, programación, retención, destinos, cifrado, compresión, límites y notificaciones; asignables a equipos o grupos. | Must | 1 | Se crea un plan, se asigna a un grupo y todos los equipos lo ejecutan sin configuración individual. |
| **RF-09** | Retención configurable diaria, semanal, mensual y anual, con limpieza automática. | Must | 1 | Tras N ejecuciones, los restore points fuera de retención se eliminan y los protegidos permanecen. |
| **RF-10** | Réplica del repositorio principal (NAS) al secundario (S3) siguiendo 3-2-1, independiente de la ejecución del backup. | Must | 3 | Un fallo de S3 no impide el backup local; la réplica se reanuda y se verifica. |
| **RF-11** | Backup de VMs de Proxmox desde el hipervisor vía PBS, sin agente dentro de la VM. | Must | 1/2 | Una VM se respalda, se lista y se restaura desde el panel. |
| **RF-12** | Inventario de hardware y sistema operativo: hostname, SO, arquitectura, CPU, RAM, discos, particiones, firmware UEFI/BIOS, Secure Boot, TPM, BitLocker/LUKS y red. | Must | 1 | El panel muestra el inventario y lo actualiza ante cambios. |
| **RF-13** | Manifiesto de imagen versionado con layout, particiones, offsets, hashes, IDs de segmentos, firmware, BitLocker y versiones de agente/Kopia. | Must | 2 | Un restaurador rechaza un manifiesto con versión desconocida y acepta el vigente. |
| **RF-14** | Reanudación de backups interrumpidos por segmento, sin reenviar segmentos completados. | Must | 2 | Matar el proceso a mitad y reanudar: no se re-suben los segmentos ya confirmados. |
| **RF-15** | El agente puede operar sin conexión al servidor: conserva la orden y el estado local, y reporta al recuperar la conexión. | Should | 1 | Un corte de red no pierde el job; al volver, el estado se reconcilia. |
| **RF-16** | Límites configurables de CPU, I/O y ancho de banda; prioridad baja por defecto. | Should | 1 | El backup no satura la máquina ni el enlace según el plan. |

### 5.2 Restauración

| ID | Requisito | Prioridad | Fase | Criterio de aceptación |
|---|---|---|---|---|
| **RF-17** | Restauración granular de archivos y carpetas a su ubicación original o a otra. | Must | 1 | Se selecciona un archivo de un snapshot y se restaura sin restaurar el resto. |
| **RF-18** | Restauración bare-metal mediante ISO de rescate. | Must | 2 | Una máquina sin SO arranca desde la ISO, restaura la imagen y vuelve a arrancar. |
| **RF-19** | Restauración de una máquina con agente como VM nueva en Proxmox (P2V/V2V), creada y lanzada desde el panel. | Must | 2 | La VM se crea, restaura y arranca; se registra el resultado y las intervenciones. |
| **RF-20** | Restauración de una VM de Proxmox desde PBS a su estado anterior o a una VM nueva. | Must | 2 | La VM restaurada arranca y conserva los discos esperados. |
| **RF-21** | Restauración de prueba automática y programable, sin tocar producción. | Must | 3 | El sistema crea una VM temporal aislada, verifica el arranque y la destruye. |
| **RF-22** | Verificación de integridad del repositorio y de contenido restaurable (niveles 1–3). | Must | 2/3 | Se ejecutan verificaciones programadas y el panel muestra resultado y evidencia. |
| **RF-23** | Restauración sin el servidor de control (break-glass) desde NAS o S3 con la clave custodiada. | Must | 2 | Con PostgreSQL, River, panel y Repository Server apagados, la ISO restaura una imagen. |
| **RF-24** | Aviso previo y bloqueo de restauración ante layouts no soportados (Storage Spaces, volúmenes dinámicos, BitLocker sin clave custodiada). | Must | 2 | El preflight aborta con un mensaje accionable y no crea recursos huérfanos. |

### 5.3 Gestión y consola

| ID | Requisito | Prioridad | Fase | Criterio de aceptación |
|---|---|---|---|---|
| **RF-25** | Panel web en español: resumen, inventario, estado, último backup, histórico, errores, espacio y alertas. | Must | 1 | Un operador puede identificar un equipo sin backup y lanzar uno. |
| **RF-26** | Alertas por email y webhook ante fallo, ausencia de backup y verificación fallida. | Must | 1 | Un fallo simulado genera una alerta con deduplicación y acuse. |
| **RF-27** | Informes periódicos de estado, cumplimiento de retención y capacidad. | Should | 3 | Se genera y envía un informe con datos reales. |
| **RF-28** | Registro de agentes con token de un solo uso, caducidad y revocación desde el panel. | Must | 1 | Un token usado no se puede reutilizar; un agente revocado no puede seguir operando. |
| **RF-29** | Auditoría de acciones de administración con actor, objeto, antes/después y correlación. | Must | 1 | Cada acción destructiva queda registrada y es exportable. |
| **RF-30** | Autenticación de usuario con 2FA TOTP y step-up para operaciones destructivas. | Must | 1 | Sin TOTP no se accede; una restauración exige re-autenticación. |
| **RF-31** | Gestión de grupos (CRUD, jerarquía opcional, membresía) y asignación de planes a grupos, con resolución de conflictos frente a asignaciones directas. | Must | 1 | Añadir un equipo a un grupo aplica el plan automáticamente; una asignación directa prevalece y la UI muestra el conflicto. |
| **RF-32** | Navegación y búsqueda de snapshots para restauración granular. | Should | 2 | Se localiza un archivo por ruta o nombre en un snapshot. |
| **RF-33** | Gestión de repositorios y destinos: alta, prueba de conexión, capacidad, estado y mantenimiento. | Must | 1 | Un destino mal configurado falla con error claro y no bloquea los demás. |
| **RF-34** | API REST versionada para integraciones y para los agentes. | Should | 1 | La API documentada permite registrar un agente y consultar jobs. |
| **RF-35** | Actualizaciones firmadas de agentes con rollout y rollback. | Must | 3 | Un binario sin firma se rechaza; una actualización fallida vuelve a la versión anterior. |

### 5.4 Seguridad funcional

| ID | Requisito | Prioridad | Fase | Criterio de aceptación |
|---|---|---|---|---|
| **RF-36** | Copias inmutables cuando el destino lo soporte (S3 Object Lock). | Must | 3 | Un intento de borrado dentro de la retención es denegado. |
| **RF-37** | Separación de credenciales: las de backup no pueden borrar repositorios ni cambiar retención. | Must | 3 | Un agente comprometido no puede destruir el repositorio. |
| **RF-38** | Custodia de la clave maestra con copia offline y procedimiento de recuperación. | Must | 0/1 | Un simulacro recupera un snapshot usando los shares, sin la BD. |
| **RF-39** | Registro de eventos de seguridad y alerta ante eliminación masiva o cambios de política. | Should | 3 | Una eliminación masiva simulada genera alerta y auditoría. |

### 5.5 Bases de datos SQL Server (motor SQL)

| ID | Requisito | Prioridad | Fase | Criterio de aceptación |
|---|---|---|---|---|
| **RF-40** | Registro y descubrimiento de instancias SQL Server desde el agente instalado en el propio servidor: instancias predeterminadas y con nombre, versión, edición, bases de datos y modelo de recuperación. | Must | 3 | Una instancia registrada muestra sus bases y estado en el panel. |
| **RF-41** | Backups nativos Full/Differential/Transaction Log mediante T-SQL (`BACKUP DATABASE`/`BACKUP LOG`) con compresión y checksum; `COPY_ONLY` cuando corresponda; transferencia al repositorio y registro de metadatos (tipo, fecha, tamaño, LSN y dependencias). | Must | 3 | Un plan genera la cadena completa y el catálogo la reconstruye. |
| **RF-42** | Restauración de una base de datos completa desde el catálogo (full + diff + logs en orden, con `NORECOVERY`/`RECOVERY` gestionado por el sistema), en el mismo servidor o en otra instancia registrada. | Must | 3 | Una base se restaura en otra instancia y queda operativa. |
| **RF-43** | Point-in-Time Recovery (`STOPAT`) sobre la cadena de logs; restauración con otro nombre y otra ubicación de archivos (`MOVE`). | Should | 3/4 | Una base se recupera a una hora concreta con evidencia. |
| **RF-44** | Verificación: `RESTORE VERIFYONLY` programado sobre cada backup; restauración de prueba en instancia aislada; `DBCC CHECKDB` opcional por base. | Must | 3 | Cada backup registra su verificación; un backup corrupto genera alerta. |
| **RF-45** | Custodia de certificados y claves de cifrado de backups nativos y de TDE como elementos de recuperación; alerta si un backup cifrado no tiene certificado registrado. | Must | 3 | Un backup cifrado se restaura en otra instancia tras recuperar el certificado. |
| **RF-46** | Inclusión de las bases de sistema (`master`, `model`, `msdb`) con tratamiento especial y restauración de instancia completa; `tempdb` excluida. | Must | 3/4 | Las bases de sistema se registran y el procedimiento de `master` está documentado y probado. |

---

## 6. Requisitos no funcionales

| ID | Requisito | Prioridad | Fase | Criterio de aceptación |
|---|---|---|---|---|
| **RNF-01** | mTLS entre agentes y servidor; CA propia; certificados cortos y revocación. | Must | 1 | Un certificado no emitido por la CA no puede conectar. |
| **RNF-02** | Integridad: checksums en escritura/lectura y verificación periódica del repositorio. | Must | 1/2 | Una corrupción simulada se detecta en la verificación. |
| **RNF-03** | Resiliencia: reanudación de backups interrumpidos y agente sin conexión. | Must | 1/2 | Corte de red y reinicio no corrompen el repositorio ni pierden el job. |
| **RNF-04** | Rendimiento: el agente corre con prioridad baja y límites configurables; el servidor no es cuello de botella no documentado. | Must | 1 | Se mide el throughput y se respetan los límites del plan. |
| **RNF-05** | Observabilidad: logs JSON estructurados, métricas Prometheus, healthchecks y trazas correlacionadas. | Must | 1 | Un job se puede seguir de extremo a extremo por `request_id`/`job_id`. |
| **RNF-06** | Reproducibilidad: servidor con Docker Compose; agentes con instalador/paquete por SO. | Must | 1 | Un entorno limpio se levanta con un comando y un agente se instala con un paquete. |
| **RNF-07** | Compatibilidad: Windows 11, Server 2025, Debian 12/13, PVE 8.2+/9.x, PBS 4.x, SQL Server 2019/2022/2025 (motor SQL). | Must | 1/2 | Matriz de pruebas ejecutada por versión. |
| **RNF-08** | Idioma: UI, logs de usuario y documentación en español; código y comentarios coherentes. | Must | 1 | Revisión de UI y docs. |
| **RNF-09** | Escalabilidad: al menos 50 agentes y 10 restauraciones concurrentes en hardware razonable. | Should | 3 | Prueba de carga documentada. |
| **RNF-10** | Mantenibilidad: versión de Kopia fijada, wrapper único, contract tests; sin importar paquetes internos. | Must | 1 | Un cambio de versión de Kopia falla en CI si cambia el contrato. |
| **RNF-11** | Recuperación de la propia plataforma: backup de PostgreSQL, configuración y claves; runbook de reconstrucción. | Must | 1 | Se reconstruye el control plane desde cero y se recupera el catálogo. |
| **RNF-12** | Licencias: solo componentes con licencia compatible; Kopia Apache-2.0 con NOTICE; UrBackup no incorporado; Duplicacy no usado. | Must | 1 | Revisión de dependencias y avisos. |

---

## 7. Reglas de datos y formatos

1. **Manifiesto de imagen inmutable y versionado.** `schema_version` obligatorio; un restaurador rechaza versiones desconocidas.
2. **Restore points inmutables** salvo `verification_state` y `expires_at`.
3. **Bytes en `bigint`**; nunca en coma flotante.
4. **Secretos cifrados** con envelope encryption; nunca en claro en PostgreSQL ni en logs.
5. **Auditoría append-only** con hash chain; la aplicación no puede actualizar ni borrar.
6. **Idempotencia** en la creación de jobs y recursos; `Idempotency-Key` obligatorio.
7. **Retención por defecto:** 7 diarias, 4 semanales, 6 mensuales, 1 anual, configurable.
8. **Nombres deterministas** de segmentos por `run_id` y índice, para permitir reintentos sin duplicados lógicos.

---

## 8. Criterios de aceptación globales

Un release se considera aceptable solo si:

- Un segundo backup sin cambios **no aumenta apreciablemente** el tamaño del repositorio.
- Un backup interrumpido **no deja el repositorio inconsistente** y se reanuda.
- Una restauración completa produce una VM que **arranca**, salvo la excepción documentada de BitLocker.
- Los datos pueden recuperarse **sin el servidor de control** mediante la ISO y la clave custodiada.
- La réplica NAS→S3 es **verificable** y no propaga borrados de forma indiscriminada.
- **Ningún secreto** aparece en logs ni en la base de datos en claro.
- Cada requisito Must tiene **prueba y evidencia** en `21-matriz-trazabilidad.md`.
- La UI distingue **consistencia de aplicación** de **crash-consistent** y **deduplicación** de **CBT**.

---

## 9. Supuestos y dependencias

- El NAS permite un montaje local en el host del repositorio; NFS no se usa para el backend primario salvo prueba concluyente.
- Proxmox VE y PBS están en versiones soportadas y accesibles por API.
- El propietario dispone de una cuenta S3 con permisos para crear un bucket con Object Lock si se activa inmutabilidad.
- Windows 11 y Server 2025 permiten instalar un servicio con privilegios de SYSTEM.
- Debian permite cargar módulos DKMS o usar LVM/Btrfs/ZFS; si no, se acepta imagen crash-consistent.
- El propietario acepta custodia física de los shares Shamir y la regla de dos personas para break-glass.

---

## 10. Definición del MVP

El MVP (Fase 1) incluye:

- Registro de agentes con token, mTLS, inventario y heartbeat.
- Planes, asignación a grupos, programación y retención.
- Backup y restauración de archivos con Kopia en NAS.
- Panel mínimo: resumen, equipos, planes, jobs, restore points, restauración de archivos, alertas y auditoría.
- 2FA, secretos cifrados, logs y métricas.
- Backup de VMs de Proxmox vía PBS (catálogo y restore básico), si la versión de PBS está disponible.

No incluye imagen de disco, P2V, ISO, S3/Object Lock, verificación de arranque ni el motor de SQL Server; esos son Fase 2/3 (RF-40–RF-46). El diseño del motor SQL se reserva desde este documento: sus interfaces, entidades y gestión de tareas forman parte del alcance general.

---

## 11. Control de cambios

- Cambios de alcance: actualizar este documento y `21-matriz-trazabilidad.md`.
- Cambios de arquitectura: ADR nuevo en `adr/` y actualización de `decisiones.md`.
- Cambios de formato de imagen: incrementar `schema_version` y documentar compatibilidad.
- Un requisito no se marca como cumplido sin evidencia enlazada.
