# Análisis funcional del alcance

**Documento:** 02-analisis-funcional
**Objetivo:** inventariar las capacidades de una plataforma completa de backup y DR, separar lo que este proyecto implementa de lo que queda fuera de alcance y extraer lecciones de diseño.

## 1. Alcance del análisis

El punto de partida es el catálogo funcional de las plataformas comerciales de protección de datos y recuperación ante desastres. La comparación es funcional: identifica qué capacidades son imprescindibles, recomendables o evolutivas, y cómo las cubre este proyecto con componentes open source propios.

Fuera del análisis quedan los detalles comerciales de cualquier fabricante: ediciones, empaquetado y licencias no condicionan el diseño.

## 2. Bloques funcionales del producto

Una plataforma completa de BDR integra cuatro bloques que en el mercado suelen venderse por separado:

1. **Backup y recuperación** (el núcleo).
2. **Protección de VMs** (agentless o con proxy de backup).
3. **Ciberseguridad** (antimalware, EDR, parches): fuera de alcance.
4. **Gestión y monitorización** (inventario, alertas, paneles): solo la parte que rodea al backup.

Este proyecto se centra en los bloques 1, 2 y la parte de gestión necesaria para operarlos.

## 3. Catálogo de capacidades

### 3.1 Backup

| Funcionalidad | Qué hace | Relevancia |
|---|---|---|
| Image Backup | Imagen de discos y particiones | Imprescindible |
| File-Level Backup | Archivos y carpetas | Imprescindible |
| Incremental Backup | Cambios desde la última copia | Imprescindible |
| Block-Level Backup (CBT) | Solo bloques modificados | Futuro |
| Deduplication | Evita almacenar repetido | Imprescindible |
| Compression | Reduce tamaño | Imprescindible |
| Encryption | Cifrado en cliente | Imprescindible |
| Application-Aware Backup | Consistencia de SQL, AD, Exchange | Recomendable |
| Continuous Data Protection | Captura continua de cambios | Futuro |
| Immutable Backups | Protección contra borrado/ransomware | Imprescindible |
| Backup Replication | Réplica entre destinos | Imprescindible |
| Backup Search | Buscar archivos entre restore points | Recomendable |
| NAS Backup | Protección de carpetas compartidas | Recomendable |
| Tape/SAN | Cintas y almacenamiento empresarial | Fuera de alcance |

### 3.2 Recuperación

| Funcionalidad | Qué hace | Relevancia |
|---|---|---|
| File/Folder Recovery | Restauración granular | Imprescindible |
| Bare-Metal Recovery | Restauración sin SO | Imprescindible |
| Universal Restore | Recuperación en hardware distinto | Imprescindible |
| P2V | Físico a virtual | Imprescindible |
| V2V | Virtual a virtual | Imprescindible |
| V2P | Virtual a físico | Evolutivo |
| Restore to new VM | Crear VM nueva desde backup | Imprescindible |
| Instant Restore | Ejecutar una VM desde el backup mientras se recupera | Evolutivo |
| Run VM from Backup | Arrancar directamente desde el repositorio | Evolutivo |
| One-Click Recovery | Recuperación simplificada | Imprescindible |
| Granular Recovery | Archivos, carpetas, particiones, discos | Imprescindible |
| Automated Recovery Testing | Prueba automática de arranque | Imprescindible |
| Disaster Recovery / Runbooks | Failover/failback y automatización | Recomendable |
| Cross-platform Recovery | Recuperar entre hipervisores | Imprescindible para Proxmox |

### 3.3 Virtualización y Proxmox

Decisiones verificadas en `decisiones.md` (D-17 a D-19):

- El backup de VMs de Proxmox se hace **agentless vía Proxmox Backup Server (PBS)**, orquestado por API. No se reimplementa.
- La restauración de una máquina física con agente como **VM nueva en PVE** se hace con un agente en el nodo PVE y `import-from`; no dentro de cada VM.
- Sin CBT propio para Proxmox en las primeras fases: el backup de VM depende del snapshot del storage.
- LXC no se asume en las primeras fases.

Esto refuerza la decisión de tener un agente en el nodo PVE, no dentro de cada VM.

### 3.4 Ciberseguridad y gestión (fuera de alcance)

| Módulo | Qué hace | Decisión |
|---|---|---|
| Active Protection | Antiransomware en el endpoint | No implementar |
| Antimalware/EDR | Detección y respuesta | No implementar |
| Vulnerability Assessment | Evaluación de vulnerabilidades | No implementar |
| Patch Management | Gestión de parches | No implementar |
| URL Filtering | Filtrado web | No implementar |
| Remote Desktop | Administración remota | No implementar |
| Data Discovery | Clasificación de datos | No implementar |
| HDD Health | Estado de discos | Podría ser un indicador básico, no un módulo |
| Email Archiving | Archivado M365 | No implementar |
| Multi-tenant / billing | Gestión de clientes | No implementar |

No se replica ninguno de estos módulos. La consola puede mostrar salud de discos y alertas de backup, pero no pretende ser una plataforma de ciberseguridad.

---

## 4. Matriz funcional y decisión

| Funcionalidad | Nuestra plataforma | Fase | Nota |
|---|---|---|---|
| Full Image Backup | Sí, manifiesto propio + segmentos | 2 | Sin CBT; lectura completa |
| File-Level Backup | Sí, Kopia | 1 | — |
| Incremental Backup | Sí | 1/2 | Archivos: real; imagen: dedup |
| Differential Backup | No | — | No aporta sobre incremental + retención |
| Block-Level Backup (CBT) | Opt-in | 3 | USN/blksnap/dattobd |
| Deduplication | Sí, Kopia | 1 | Por chunks |
| Compression | Sí, Zstd | 1 | Configurable |
| Encryption | Sí, cliente | 1 | AES-256-GCM/ChaCha20 |
| Scheduling | Sí, planes | 1 | Cron + ventanas |
| Retention Rules | Sí, GFS | 1 | 7/4/6/1 |
| Multiple Destinations | Sí, NAS + S3 | 1/3 | Réplica |
| Backup Validation | Sí, niveles 1–3 | 2/3 | Estructural + contenido |
| Application-Aware Backup | Parcial | 2/3 | VSS writers; sin VSS SQL propio |
| Continuous Data Protection | No | 4 | Backlog |
| Immutable Backups | Sí, S3 Object Lock | 3 | GOVERNANCE |
| Backup Replication | Sí, `sync-to` | 3 | NAS → S3 |
| Backup Search | Sí, básica | 2/4 | Navegación de snapshots |
| NAS Backup | Sí, archivos | 1 | Rutas NAS montadas |
| Tape/SAN | No | — | Fuera de alcance |
| File/Folder Recovery | Sí | 1 | Kopia |
| Bare-Metal Recovery | Sí, ISO | 2 | — |
| Universal Restore | Sí, P2V/V2V | 2/3 | Windows con intervención |
| P2V | Sí | 2 | Agente PVE + import-from |
| V2V | Sí | 2 | Mismo flujo |
| V2P | No | — | Evolutivo, no prioritario |
| Restore to new VM | Sí | 2 | — |
| Instant Restore | No | 4 | Requiere integración QEMU/storage |
| Run VM from Backup | No | 4 | Backlog |
| One-Click Recovery | Sí, wizard | 2 | — |
| Granular Recovery | Sí | 1/2 | Archivos y discos |
| Automated Recovery Testing | Sí, niveles 3 | 3 | VM temporal |
| Disaster Recovery / Runbooks | Parcial | 3/4 | Runbooks documentados |
| Cross-platform Recovery | Sí, P2V | 2 | Proxmox |
| Proxmox agentless VM backup | Sí, PBS | 1/2 | No reimplementar |
| Proxmox instant restore | No | 4 | Backlog |
| LXC | No en fases tempranas | — | No asumir |
| Antimalware/EDR/parches/URL/remoto | No | — | Fuera de alcance |
| M365/Google/macOS/móviles | No | — | Fuera de alcance |

---

## 5. Lecciones de diseño

### 5.1 Protection Plans

Las plataformas del sector organizan el backup en planes reutilizables asignados a equipos o grupos. Es el modelo correcto: evita configurar cada máquina y permite aplicar cambios masivos. Nuestro modelo `backup_plans` + `plan_assignments` + `plan_runs` reproduce esta idea con prioridad, ventanas, retención, destinos, límites y verificación.

### 5.2 Universal Restore y recuperación cross-platform

Hay que separar «restaurar los datos» de «hacer que el sistema arranque». La recuperación en hardware distinto exige adaptar controladores de almacenamiento y red, firmware, TPM y, en Windows, el registro y el arranque. Nuestro flujo P2V debe tener:

- Preflight de compatibilidad.
- Arranque en dos fases (SATA + e1000 → VirtIO).
- Reparación de BCD/GRUB.
- Chroot y regeneración de initramfs en Linux.
- Aviso de BitLocker y vTPM.
- Verificación de arranque.

No se promete un P2V Windows totalmente desatendido en Fase 2. Se documenta la intervención.

### 5.3 Recuperación granular

El usuario debe poder navegar el snapshot y extraer un archivo sin restaurar la máquina. Kopia ofrece montaje/restauración de rutas; el panel debe exponerlo con permisos y auditoría. En Fase 1 se implementa la restauración de archivos; la navegación completa llega en Fase 2.

### 5.4 Verificación de recuperación

La verificación se hace por niveles, del más barato al más caro:

1. Integridad estructural del repositorio.
2. Restauración de un archivo aleatorio y comparación de hash.
3. Reconstrucción de imagen y comprobación de layout.
4. Arranque de una VM temporal en red aislada.
5. Simulación DR completa.

El nivel 4 es el que realmente demuestra recuperabilidad; en Windows puede requerir revisión humana o captura de pantalla.

### 5.5 Inmutabilidad

Las copias inmutables usan Object Lock de S3 con GOVERNANCE por defecto y COMPLIANCE solo si hay requisito legal. Hay que alinear la retención de bloqueo con la GFS y medir el coste, porque las versiones bloqueadas no desaparecen al borrar.

### 5.6 Instant restore

Arrancar una VM directamente desde el backup es atractivo, pero implica integración profunda con el almacenamiento y QEMU, y un formato de imagen adecuado. Queda en Fase 4. No se promete en el MVP.

---

## 6. Conclusiones

1. El producto debe cubrir imagen, archivos, múltiples destinos, retención, verificación y recuperación cross-platform.
2. PBS es la vía correcta para VMs de Proxmox; no se reimplementa el backup agentless.
3. Los módulos de ciberseguridad de las suites comerciales no se replican: este proyecto es un producto de backup y DR.
4. El diseño propio debe ser más pequeño y explícito que una suite completa, no un clon.
5. La diferenciación viene de: código auditable, dependencia de estándares abiertos y ausencia de coste por carga de trabajo.

---

## 7. Fuentes

- Documentación funcional pública de plataformas de backup comerciales (usada como referencia de catálogo de capacidades, sin reproducir documentación propietaria).
- Componentes técnicos: `docs/03-evaluacion-motores.md` y `docs/decisiones.md`.
