# Informe ejecutivo — Plataforma BDR self-hosted

**Documento:** 00-informe-ejecutivo  
**Versión:** 1.1  
**Fecha:** 19 de noviembre de 2026  
**Autor:** Diseño técnico del proyecto  
**Audiencia:** propietario del sistema, responsables de infraestructura y agente de código que implementará las fases.

---

## 1. Resumen ejecutivo

La plataforma es **viable y está bien delimitada**, pero no debe entenderse como «un panel para Kopia». Es un producto compuesto por cuatro capacidades que se integran bajo una consola común:

1. **Protección de equipos** con agente: inventario, planes, snapshots consistentes, backup de archivos e imagen.
2. **Almacenamiento incremental** con deduplicación, compresión y cifrado en cliente, en NAS y S3.
3. **Protección de VMs de Proxmox sin agente**, delegando en PBS.
4. **Recuperación automatizada**, incluida la conversión de una máquina física o virtual en una VM nueva de Proxmox.

El mayor valor diferencial —y el mayor riesgo técnico— está en la cuarta capacidad: conseguir que la VM restaurada **arranque**, no solo que los datos se hayan copiado.

La recomendación es:

- **Construir** la consola, la orquestación, los agentes, el formato de imagen y el flujo P2V.
- **Reutilizar** Kopia para repositorio, deduplicación, cifrado y backup de archivos; PBS para VMs de Proxmox; VSS/LVM/Btrfs/ZFS para consistencia; QEMU/Proxmox para virtualización.
- **No intentar** replicar antimalware, EDR, gestión de parches, escritorio remoto, archivado de correo ni multi-tenant.
- **Empezar por una PoC de restauración** (Windows Server 2025 UEFI → VM Proxmox) antes de escribir el panel o el agente completo.

El resultado esperado es una plataforma de BDR completa, especializada en backup de imagen, archivos, PBS, bases de datos SQL Server y recuperación P2V/V2V, con una arquitectura que no dependa de un único proveedor ni de un único motor.

---

## 2. El problema

El propietario administra una infraestructura propia con:

- Windows Server 2025, Windows 11 y Debian.
- Un clúster Proxmox VE con VMs y almacenamiento local/ZFS/LVM/Ceph.
- Un NAS para copias locales rápidas.
- Un destino S3 compatible para copia externa.

La solución deseada debe permitir, desde una sola consola:

1. Instalar un agente y proteger la máquina completa.
2. Programar copias incrementales con retención.
3. Restaurar archivos sueltos sin depender del equipo origen.
4. Restaurar una máquina completa como VM nueva en Proxmox.
5. Proteger VMs de Proxmox sin instalar agentes dentro de ellas.
6. Verificar que las copias son recuperables.
7. Mantener dos destinos y una copia offline de las claves.

Las suites comerciales de protección de datos cubren esto, pero imponen coste recurrente por carga de trabajo, dependencia del proveedor y módulos que no se necesitan. Los proyectos open source cubren piezas, no el producto completo: Kopia no hace imágenes de disco con arranque; UrBackup hace imágenes pero no encaja con S3 ni con P2V automatizado; PBS protege VMs de Proxmox pero no máquinas físicas; Duplicacy no es open source y no hace imagen.

La oportunidad es construir la capa que falta: **orquestación, formato de imagen, recuperación y consola**, sobre motores que ya funcionan.

---

## 3. Alcance y no alcance

### 3.1 Dentro de alcance

| Área | Incluido |
|---|---|
| Sistemas con agente | Windows Server 2025, Windows 11, Debian 12/13 |
| Sistemas sin agente | VMs de Proxmox VE vía PBS |
| Backup | Imagen completa, archivos y carpetas, incrementales, deduplicación, compresión, cifrado |
| Consistencia | VSS en Windows; LVM/Btrfs/ZFS en Linux; ext4 sin LVM en modo crash-consistent |
| Restauración | Archivos, bare-metal con ISO, P2V/V2V a Proxmox, VMs Proxmox desde PBS |
| Destinos | NAS principal y S3 compatible con réplica |
| Gestión | Panel, planes, retención, alertas, informes, auditoría, 2FA |
| Seguridad | mTLS, tokens de un solo uso, secretos cifrados, binarios firmados |
| Verificación | Integridad, restauración de archivos, reconstrucción de imagen, arranque de VM temporal |

### 3.2 Fuera de alcance

Antimalware/EDR, filtrado de URL, gestión de parches, escritorio remoto, archivado de correo, macOS, móviles, Microsoft 365/Google Workspace, cintas, multi-tenant, facturación y licencias. Tampoco se implementa CBT propio en las primeras fases; queda en el backlog de Fase 3.

---

## 4. Qué vamos a construir

### 4.1 Los cinco objetivos innegociables

1. **Backup de máquina completa:** discos, particiones, tabla de particiones, EFI y arranque, no solo carpetas.
2. **Incrementales reales:** el almacenamiento y la red solo incorporan datos nuevos; sin CBT, la lectura del disco sigue siendo completa, y eso se documenta con honestidad.
3. **Doble destino:** NAS para restauración rápida en LAN y S3 para protección externa e inmutabilidad.
4. **Restauración automatizada:** seleccionar un punto de recuperación y obtener una VM arrancable en Proxmox.
5. **Independencia:** poder recuperar aunque el servidor de control, PostgreSQL y el panel estén caídos.

### 4.2 Los cinco motores

| Motor | Función | Naturaleza |
|---|---|---|
| **Kopia** | Repositorio, deduplicación, cifrado, compresión, archivos, almacén de segmentos de imagen | Open source, Apache-2.0, CLI fijada |
| **Image Engine** | Captura de discos/particiones, manifiesto, segmentación, reanudación | Desarrollo propio |
| **PBS** | Backup/restore incremental de VMs y almacenamiento Proxmox | Existente, orquestado por API |
| **SQL Server Engine** | Backups nativos de bases de datos SQL Server (Full/Diff/Log), cadenas, PITR, verificación | Motor nativo de SQL Server, orquestado por la plataforma (`24-motor-sql-server.md`) |
| **Recovery Engine** | ISO de rescate, restauración bare-metal, P2V/V2V, reparación de arranque | Desarrollo propio |

La consola y el control plane son el sexto componente, pero no un motor de datos: nunca ven el flujo de backup.

---

## 5. Análisis funcional de referencia

### 5.1 Inventario funcional relevante

Las plataformas comerciales de protección de datos combinan backup, recuperación, ciberseguridad y gestión de endpoints. Las capacidades relevantes para este proyecto son:

- **Backup de imagen completa y a nivel de archivos**, con y sin agente.
- **Copias inmutables**, recuperación rápida y protección continua de datos.
- **Validación** de la recuperabilidad, no solo de la terminación correcta del job.
- **Múltiples destinos**: local, NAS, nube y S3 compatible.
- **Recuperación ante desastres**: instant recovery, failover/failback, runbooks, RTO.
- **Proxmox VE**: backup agentless de VMs (vía PBS), recuperación cross-platform y conversión de backup a VM.
- **Retención GFS** (diaria, semanal, mensual, anual), replicación y API abiertas.
- **Módulos de ciberseguridad y gestión** que no forman parte de este alcance: antimalware, EDR, vulnerabilidades, parches, filtrado de URL, escritorio remoto, archivado M365.

### 5.2 Qué replicamos y qué no

| Capacidad de referencia | Nuestra decisión | Fase |
|---|---|---|
| Imagen completa | Imprescindible | 2 |
| Backup de archivos | Imprescindible | 1 |
| Incrementales + dedup + compresión | Imprescindible | 1/2 |
| Cifrado en cliente | Imprescindible | 1 |
| Planes de protección | Imprescindible | 1 |
| Retención GFS | Imprescindible | 1 |
| Múltiples destinos | Imprescindible | 1/3 |
| Validación de backups | Imprescindible | 2/3 |
| Bare-metal restore | Imprescindible | 2 |
| P2V/V2V a Proxmox | Imprescindible | 2 |
| Backup agentless de VMs Proxmox | Imprescindible vía PBS | 1/2 |
| Restauración granular de archivos | Imprescindible | 1 |
| Inmutabilidad / Object Lock | Imprescindible | 3 |
| Verificación de arranque en VM | Imprescindible | 3 |
| Instant restore / run VM from backup | Evolutivo | 4 |
| CDP | Futuro | 4 |
| Búsqueda global de archivos | Recomendable | 4 |
| Antimalware, EDR, parches, URL, escritorio remoto | Fuera de alcance | — |
| Archivado M365/Google, macOS, móviles | Fuera de alcance | — |

### 5.3 Lecciones que adoptamos

1. **Planes de protección reutilizables**, no configuración equipo a equipo.
2. **Universal Restore**: restaurar en hardware distinto exige adaptar drivers, firmware y arranque; no basta con copiar bytes.
3. **Recuperación granular**: navegar y extraer un archivo sin restaurar la máquina.
4. **Verificación de recuperación**: un job correcto no demuestra que la copia sea recuperable.
5. **Consola única**: el valor está en la orquestación, no en un motor propietario.

Las suites comerciales pueden permitirse un ecosistema cerrado y soporte multi-tenant; nosotros no. Nuestro diseño debe ser más pequeño, más explícito y más dependiente de estándares abiertos.

---

## 6. Evaluación de motores open source

### 6.1 Kopia

**Fortalezas verificadas:** Apache-2.0; deduplicación por contenido; compresión Zstandard; cifrado AES-256-GCM/ChaCha20; backends filesystem, S3, SFTP, WebDAV y rclone; repositorio central con ACL por usuario; verificación estructural y de contenido; réplica entre repositorios; restauración granular.

**Limitación clave:** no es un motor de imagen de disco con CBT ni un producto de recuperación de arranque. Sin embargo, acepta flujos por `--stdin-file`, lo que permite ingerir una partición o un dispositivo de bloque como un snapshot. Esa es la pieza que usamos en Fase 2, con un manifiesto propio y segmentación para poder reanudar.

**Riesgo:** la CLI no es una API estable. Se fija la versión, se envuelve en un único módulo y se ejecutan contract tests.

### 6.2 UrBackup

**Fortalezas:** AGPLv3+; imagen en caliente con VSS/LVM/Btrfs/ZFS; incrementales de imagen por sectores usados; restore ISO; deduplicación por archivo; panel web.

**Limitaciones:** imagen solo NTFS/extX/XFS, MBR/GPT primarias, sin volúmenes dinámicos; sin S3 nativo en la versión open source; deduplicación por archivo, no por chunks; restaurar una imagen a Proxmox falla con frecuencia por drivers y arranque.

**Decisión:** se usa como **referencia conceptual y banco de pruebas** de captura en caliente y restore ISO. No se incorpora como dependencia del producto.

### 6.3 Duplicacy

**Fortalezas:** incrementales, deduplicación, cifrado, compresión, cloud, retención.

**Limitaciones:** no es open source; la CLI es gratuita solo para uso personal; la licencia comercial es de 50 USD por equipo y año; no hace imagen completa de máquina.

**Decisión:** descartado. No cumple el requisito de imagen ni el de licencia para un producto propio que podría evolucionar.

### 6.4 Proxmox Backup Server

**Fortalezas:** incremental por bloque sucio, deduplicación, compresión, verificación SHA-256, integración nativa con PVE, sincronización entre PBS y, desde PBS 4.x, datastore S3 nativo.

**Limitaciones:** no ingiere imágenes de máquinas físicas; el datastore S3 requiere caché local de 64–128 GiB y solo lo puede usar una instancia PBS; PBS no crea el bucket ni gestiona su ACL; Object Lock no está documentado para el datastore S3.

**Decisión:** se usa para VMs de Proxmox y se orquesta por API. No se intenta convertirlo en el repositorio universal.

### 6.5 Comparativa resumida

| Capacidad | Kopia | UrBackup | Duplicacy | PBS |
|---|---|---|---|---|
| Archivos incrementales | Excelente | Sí | Excelente | No aplica |
| Imagen completa | Vía flujos, no nativa | Sí | No | Solo VMs |
| Incremental de imagen | Dedup, sin CBT | Sectores modificados | No | Sí, por bloque sucio |
| Deduplicación | Por chunks | Por archivo | Por chunks | Por chunks |
| Cifrado en cliente | Sí | Limitado | Sí | Sí |
| S3 | Sí | No nativo | Sí | Sí (PBS 4.x) |
| Repositorio central | Sí | Sí | Sí | Sí |
| Restore ISO | No | Sí | No | No aplica |
| P2V automatizado | Desarrollo propio | Frágil | No | No aplica |
| Licencia | Apache-2.0 | AGPLv3+ | Comercial | AGPLv3 |

**Conclusión:** Kopia es el núcleo de almacenamiento y archivos; PBS es el motor de VMs; UrBackup y Duplicacy no se incorporan. El valor propio está en la imagen, la recuperación y la consola.

---

## 7. Arquitectura propuesta

```
                         ┌──────────────────────────────┐
                         │  Panel Next.js (ES)          │
                         └──────────────┬───────────────┘
                                        │ HTTPS :443 + sesión/TOTP
┌───────────────────────────────────────▼───────────────────────────────────────┐
│ Control Plane (Go)                                                            │
│ API admin + agentes · AuthN/AuthZ · Scheduler · Workers · Alertas · Auditoría │
│ PostgreSQL 16 · River (cola) · Prometheus · logs JSON                         │
│                                                                               │
│  ┌─────────────────────────┐   ┌──────────────────────────────────────────┐   │
│  │ Kopia Repository Server │   │ Integración Proxmox VE API / PBS API     │   │
│  │ host/VM dedicado        │   │ tokens de mínimo privilegio              │   │
│  └────────────┬────────────┘   └──────────────────┬───────────────────────┘   │
└───────────────┼───────────────────────────────────┼───────────────────────────┘
                │ HTTPS :8443 mTLS                  │ API HTTPS
     ┌──────────┴──────────┐              ┌─────────┴──────────┐
     │ Agentes Windows/Lin │              │ PVE / PBS          │
     │ + Agente PVE        │              │ (VMs)              │
     └──────────┬──────────┘              └────────────────────┘
                │ escritura de backups
     ┌──────────┴──────────┐        ┌────────────────────┐
     │ NAS (principal)     │──sync──▶│ S3 compatible      │
     │ repo Kopia          │        │ réplica + ObjectLock│
     └─────────────────────┘        └────────────────────┘
                ▲
     ┌──────────┴──────────┐
     │ ISO de rescate      │  acceso directo break-glass al repo
     └─────────────────────┘
```

**Principios:**

- El control plane no transporta datos de backup; solo metadatos y órdenes.
- Los agentes inician la conexión; no se abren puertos en las máquinas protegidas.
- El Repository Server custodia la clave del repositorio; los agentes usan usuarios con ACL.
- La restauración puede funcionar sin el control plane mediante la ISO y la clave custodiada.
- PBS y PVE se integran por API con tokens de mínimo privilegio.

---

## 8. Decisiones clave

| Decisión | Elección | Motivo |
|---|---|---|
| Lenguaje de servidor y agentes | Go | Un solo lenguaje, binarios estáticos, mTLS y concurrencia; mismo ecosistema que Kopia |
| Panel | Next.js + TypeScript | Stack conocido por el propietario; UI rápida de construir |
| Cola | River sobre PostgreSQL | Evita Redis en el despliegue mínimo; reintentos, unicidad y periodicidad |
| Repositorio | Kopia Repository Server central | Una sola custodia de clave; ACL por agente; réplica centralizada |
| Imagen | Manifiesto propio + Kopia como almacén de segmentos | Evita acoplar la recuperación a la CLI de Kopia; permite cambiar de backend |
| VMs Proxmox | PBS | Ya hace incremental por bloque sucio y verificación; no se reimplementa |
| Restauración a Proxmox | Agente PVE + `import-from` | Menos pasos y más velocidad que arrancar una ISO; la ISO queda como fallback |
| Destinos | NAS principal + S3 réplica | 3-2-1; LAN rápida y copia externa inmutable |
| Seguridad | mTLS + TOTP + secretos cifrados | Un backup central es un objetivo de alto valor |
| Despliegue | Docker Compose + instaladores | Reproducibilidad y baja fricción |

### 8.1 Reconciliación con el informe adicional

El informe adicional recomendaba priorizar la ISO de rescate y construir un `BlockImageRepository` propio. Se acepta la crítica de fondo —no depender del control plane y no acoplar el formato a Kopia— pero se mantiene la decisión del propietario:

- **Agente PVE + `import-from`** es la vía principal. La ISO se construye en paralelo como fallback universal y break-glass.
- **Manifiesto propio e interfaz `ImageEngine`**, con Kopia como backend de segmentos en Fase 2. Si la PoC demuestra que Kopia no es adecuado, se cambia el backend sin reescribir el panel ni el orquestador.

---

## 9. Flujos críticos

### 9.1 Registro y heartbeat

1. El administrador genera un token de un solo uso (TTL 1 h, un uso, binding opcional a hostname).
2. El agente genera un par de claves y un CSR, y se registra por TLS.
3. El servidor valida el token y emite un certificado de cliente con `CN=<agent_uuid>`, validez 90 días.
4. El agente envía inventario y mantiene heartbeat cada 30 s.
5. El servidor entrega planes, ventanas y credenciales efímeras del repositorio; nunca la clave maestra.

### 9.2 Backup de archivos

El agente ejecuta `kopia snapshot create` contra el Repository Server con su usuario y reporta el ID. Los datos van del agente al repositorio; el control plane solo registra el resultado y el manifiesto.

### 9.3 Backup de imagen

1. Inventario de discos y layout.
2. Snapshot consistente: VSS en Windows; LVM/Btrfs/ZFS en Linux; ext4 sin LVM en modo crash-consistent.
3. Lectura por segmentos de 1–8 GiB.
4. Cada segmento se envía con `kopia snapshot create --stdin-file=<segmento>`.
5. Un journal local y el estado en PostgreSQL permiten reanudar por segmento.
6. El manifiesto final se cierra y se sube como snapshot pequeño para autocontención.

### 9.4 Restauración de archivos

El agente recibe la orden, ejecuta `kopia snapshot restore`, verifica espacio y permisos, y reporta progreso. La UI permite navegar el snapshot y elegir archivos o carpetas.

### 9.5 Restauración como VM en Proxmox

1. El usuario elige restore point, nodo, storage y recursos.
2. El control plane lee el manifiesto y hace preflight: firmware, TPM, BitLocker, espacio, VMID, tipo de disco.
3. El agente PVE descarga los segmentos y escribe un archivo disperso en staging.
4. Se crea la VM con `POST /nodes/{node}/qemu` y `import-from`.
5. Windows arranca primero con SATA + e1000; después se instalan drivers VirtIO y se cambia el bus.
6. Debian se corrige con chroot (initramfs, fstab, red) antes del primer arranque.
7. Se comprueba el arranque (uptime, consola serie, guest agent, captura) y se registra el resultado.

### 9.6 Backup de VMs de Proxmox

El control plane llama a `vzdump` con storage PBS; el nodo PVE habla con PBS. El control plane cataloga snapshots, lanza verify/prune/gc y solicita restauraciones. El tráfico de datos no pasa por el control plane.

### 9.7 Break-glass

Si el control plane, PostgreSQL, River o el Repository Server no están disponibles:

1. Se arranca la ISO de rescate.
2. Se conecta directamente al NAS o al S3 con la configuración custodiada.
3. Se introduce la clave maestra desde los shares Shamir (3 de 5).
4. Se restaura la imagen y se repara el arranque.
5. Se registra el incidente y se rota la clave tras el uso.

---

## 10. Precisión sobre los incrementales

Es imprescindible no confundir tres conceptos:

| Tecnología | Qué evita |
|---|---|
| Incremental de archivos | Retransmitir archivos sin cambios |
| Deduplicación por chunks | Almacenar repetidamente contenido idéntico |
| CBT (Changed Block Tracking) | Releer del disco bloques que no han cambiado |

En este diseño:

- **Kopia** resuelve deduplicación y almacenamiento incremental de archivos y segmentos.
- **PBS** resuelve el incremental de VMs por bloque sucio.
- **El motor de imagen propio** no tiene CBT en Fase 1/2: lee el disco completo en cada ejecución, aunque solo suba y almacene los chunks nuevos. Un disco de 2 TB puede tardar horas aunque solo hayan cambiado 5 GB.
- **CBT** (USN en Windows; blksnap/dattobd/dm-era en Linux) es Fase 3, opt-in, y no se promete en el MVP.

La UI y la documentación deben decir «imagen completa con deduplicación; sin CBT» para no crear expectativas falsas.

---

## 11. Seguridad y custodia de claves

Un servidor de backup central es un objetivo de alto valor: si un atacante controla el panel, puede destruir todas las copias. Controles no negociables:

- mTLS entre agentes y servidor; CA propia; certificados cortos y revocación.
- Tokens de registro de un solo uso y con caducidad.
- Secretos cifrados en reposo con envelope encryption; la KEK no vive en PostgreSQL.
- 2FA TOTP y step-up para restauraciones y operaciones destructivas.
- Auditoría append-only con hash chain.
- Binarios y actualizaciones firmadas.
- Tokens de Proxmox/PBS con mínimo privilegio y separación de funciones.
- Credenciales de backup que no puedan borrar repositorios ni cambiar retención.
- Object Lock en S3 cuando el proveedor lo soporte; GOVERNANCE por defecto.
- Clave maestra de Kopia dividida con Shamir 3-de-5 y custodiada offline.
- Simulacro anual de recuperación sin el servidor central.

**Si se pierde la clave maestra, se pierden todos los backups.** No hay recuperación mágica. El procedimiento de custodia es obligatorio y debe probarse.

---

## 12. Plan por fases

| Fase | Contenido | Puerta de salida |
|---|---|---|
| **0 — PoC** (M) | VSS, streaming, ACL de Kopia, P2V Windows/Debian, sync-to + Object Lock, ext4, custodia, rendimiento | Evidencia de los 9 experimentos; ningún `[VERIFICAR]` crítico abierto |
| **1 — MVP archivos** (L) | Enrolamiento mTLS, inventario, planes, cola, catálogo, backup/restore de archivos, NAS, UI mínima, auditoría, 2FA | 10 equipos 30 días; 100 % jobs con estado final correcto o error accionable; restore semanal verificado; RPO ≥95 %; revocación <5 min |
| **2 — Imagen + P2V** (L) | Imagen VSS por volúmenes, Linux LVM/Btrfs/ZFS y ext4 crash-consistent, manifiesto, segmentación, agente PVE, restore Windows/Debian, PBS, verificación 1-2 | 10/10 restores de archivos; ≥8/10 P2V Windows arrancan con intervención documentada; ≥9/10 Debian sin intervención; verificación 1-2 en el 100 % |
| **3 — Robustez 3-2-1** (L/XL) | Réplica S3 + Object Lock, verificación nivel 3, inyección offline de VirtIO, actualizaciones firmadas, HA, DR desde réplica, CBT opt-in | Réplica verificada semanalmente con restore real; nivel 3 ≥90 % Linux; actualización de 50 agentes sin intervención; caída del control plane no interrumpe backups |
| **4 — Avanzado** | Búsqueda global, CDP, run VM from backup, grupos/políticas avanzadas, informes | Según backlog priorizado |

**Regla:** no se avanza de fase sin cumplir la puerta. La Fase 0 no es opcional: es la que convierte supuestos en evidencia.

---

## 13. Riesgos principales

| Riesgo | Impacto | Mitigación |
|---|---|---|
| Windows no arranca tras P2V (0x7B, BCD/EFI) | Alto | SATA+e1000, ISO virtio-win, inyección offline, preflight, pruebas por build |
| BitLocker y vTPM nuevo | Alto | `vss_volumes` por defecto, aviso, custodia de clave o reinstauración sin BitLocker |
| Pérdida de la clave maestra | Crítico | Shamir 3-de-5, custodia física, simulacro anual |
| Repository Server como SPOF | Alto | Host dedicado, NAS local, 10/25 GbE, monitorización, runbook |
| `--stdin-file` sin reanudación | Alto | Segmentación + journal + reanudación por segmento |
| Sin CBT en discos grandes | Alto | Comunicarlo, programación escalonada, CBT Fase 3 |
| Delta VSS llena el disco | Alto | Límite de shadow storage, monitorización, aborto por umbral |
| Agente PVE root comprometido | Crítico | Binario firmado, sin puertos, tokens mínimos, staging acotado, auditoría |
| Object Lock vs GFS | Medio | Retención alineada, lifecycle, GOVERNANCE por defecto |
| Deriva de la CLI de Kopia | Alto | Versión fijada, wrapper, contract tests |
| DKMS ext4 (blksnap/dattobd) | Alto | Imagen crash-consistent; módulos opt-in; no prometer consistencia de aplicación |

El registro completo, con señal de detección y dueño, está en `18-riesgos.md`.

---

## 14. Construir frente a comprar

| Criterio | Comprar suite comercial | Construir esta plataforma |
|---|---|---|
| Coste inicial | Licencia por carga de trabajo | Tiempo de desarrollo |
| Coste recurrente | Suscripción | Mantenimiento propio |
| Cobertura funcional | Muy amplia | Subconjunto deliberado |
| Ciberseguridad integrada | Sí | Fuera de alcance |
| Dependencia | Proveedor, nube, licencias | Código y estándares abiertos |
| Control del dato | Limitado por el producto | Total |
| Riesgo técnico | Bajo | Alto en P2V y arranque |
| Aprendizaje | Bajo | Alto |

**Conclusión:** si el objetivo fuera únicamente «tener backups», comprar sería racional. Si el objetivo es **control, aprendizaje, independencia y recuperación P2V a medida**, construir sobre Kopia/PBS tiene sentido, siempre que se acepte que el producto es un subconjunto y que el esfuerzo se concentra en la recuperación, no en el panel.

No se recomienda construir para ahorrar licencias a corto plazo: el coste real es el mantenimiento y las pruebas de recuperación. Se recomienda construir si el valor está en la autonomía y en la integración exacta con esta infraestructura.

---

## 15. Esfuerzo relativo

Sin plazos en semanas, porque dependen de la disponibilidad del propietario y del resultado de la PoC:

| Fase | Esfuerzo | Comentario |
|---|---|---|
| 0 — PoC | M | Alta incertidumbre; mucho aprendizaje por experimento |
| 1 — MVP archivos | L | Control plane, agente, UI, seguridad; trabajo amplio pero bien conocido |
| 2 — Imagen + P2V | L | El núcleo técnico y el mayor riesgo |
| 3 — Robustez 3-2-1 | L/XL | Réplica, verificación nivel 3, actualizaciones, HA |
| 4 — Avanzado | Variable | Solo si el producto base está estable |

La Fase 0 debe presupuestarse con margen: es donde se descubre si el enfoque es viable en este hardware y con estas versiones.

---

## 16. Recomendación

1. **Aprobar este diseño documental** como contrato de trabajo.
2. **Ejecutar la Fase 0 antes de escribir el panel.** El primer experimento es Windows Server 2025 UEFI → VM Proxmox.
3. **No prometer** P2V Windows totalmente desatendido en Fase 2; documentar la intervención necesaria.
4. **No prometer** incremental físico de imagen sin CBT; hablar de deduplicación y almacenamiento incremental.
5. **Probar la custodia de la clave** antes de confiar en el repositorio.
6. **Mantener la ISO de rescate** como requisito de Fase 2, no como extra.

El éxito del proyecto se mide con una frase: *«puedo seleccionar un backup y obtener una VM que arranca en Proxmox, y puedo hacerlo aunque el servidor de control esté caído»*.

---

## 17. Fuentes principales

- Catálogo funcional de referencia de plataformas comerciales de protección de datos (análisis funcional, `docs/02-analisis-funcional.md`).
- Kopia: documentación oficial, referencia de CLI y código fuente (`cli/command_snapshot_create.go`, `cli/command_snapshot_verify.go`).
- Proxmox VE: API y documentación de `import-from`; Proxmox Backup Server 4.x (documentación 4.2.11).
- UrBackup: página oficial de funcionalidades y foro.
- Duplicacy: página oficial de compra y licencias.
- Veeam blksnap y Datto dattobd: documentación de módulos de snapshot de bloque.
- Librerías Go de VSS: `SmithOperatingSolutions/go-vss` y `mxk/go-vss`.

Las conclusiones verificadas, con fecha y matices, están en `decisiones.md`.
