# Almacenamiento y retención

**Documento:** 15-almacenamiento-retencion  
**Versión:** 1.1  
**Requisitos:** RF-09, RF-10, RF-36, RF-37

---

## 1. Arquitectura de almacenamiento

```
Equipos y VMs
     │
     ▼
Kopia Repository Server
     │ filesystem local
     ▼
NAS principal ──sync-to──▶ S3 compatible
     │                         │
     │                         └── Object Lock (opcional)
     │
     └── restauración rápida en LAN
```

- **NAS principal:** backend filesystem montado localmente en el host del repositorio. Prioriza latencia y restauración rápida.
- **S3:** réplica externa, con cifrado en cliente y Object Lock cuando el proveedor lo soporte.
- **PBS:** almacenamiento propio para VMs de Proxmox; puede usar S3 nativo en PBS 4.x.
- **Custodia offline:** claves y configuración, fuera de ambos repositorios.

### 1.1 Destinos de réplica: S3, ruta física o SMB (0.8)

Los destinos de almacenamiento registrados en el panel admiten tres tipos:

- **S3-compatible** (`kind: s3`): endpoint, bucket, credenciales cifradas,
  prefijo y Object Lock opcional. Réplica `kopia repository sync-to s3` y
  repositorio Kopia directo del destino (`repository connect/create s3`).
- **Ruta física** (`kind: filesystem`, ruta absoluta): disco local o
  NFS/SMB montado, con prefijo como subdirectorio. Verificación por
  escritura de prueba, réplica `kopia repository sync-to filesystem` y repo
  del destino con `repository connect/create filesystem`. La ruta debe ser
  accesible desde el plano de control y desde los agentes que la usen. No
  aplica Object Lock: la inmutabilidad de los backups de archivo depende de
  la política de retención del repo.
- **Compartida SMB** (`kind: filesystem`, ruta `//servidor/recurso[/sub]` o
  `smb://…`): cliente SMB nativo (NTLM con usuario, contraseña cifrada y
  dominio AD/workgroup), sin montar el recurso ni requerir root. Válida
  para backups de imagen, VM y **LXC** (subida directa), verificación y
  borrado de backups. La réplica del repo usa `kopia sync-to rclone`
  (requiere `rclone` en el servidor); no admite repositorio Kopia directo,
  así que los **planes de archivo** deben usar S3 o ruta local.

Los uploads directos de imagen/VM/LXC admiten los tres tipos: en S3 se
suben objetos por segmento; en filesystem/SMB se escriben ficheros
(escritura atómica temporal+rename) con el mismo layout de claves y
reanudación incremental.

### 1.2 Eliminación de backups (0.8)

`DELETE /api/v1/admin/restore-points/{id}` (rol admin, auditado):

- Puntos de archivo (`file`): `kopia snapshot delete <id> --delete` en el
  repo central o en el repo del destino según `TargetID`. Los bloques
  deduplicados se liberan con el mantenimiento (GC) del repositorio.
- Puntos de imagen/VM/LXC: borra el manifiesto y los segmentos del destino
  (con bypass de Object Lock GOVERNANCE si las credenciales lo permiten; en
  SMB borra los ficheros del recurso; los objetos retenidos no se pueden
  borrar hasta expirar la retención).

---

## 2. Repositorio Kopia

### 2.1 Layout

- Un repositorio por dominio de confianza (por defecto, uno).
- Prefijo por agente en el manifiesto y en los tags; no confundir con aislamiento de contenidos.
- Configuración del repositorio en el Repository Server, no en los agentes.
- Contraseña maestra custodiada; los agentes usan usuarios con ACL.

### 2.2 Repository Server

- Host/VM dedicado recomendado; NAS montado localmente.
- 10/25 GbE para el repositorio.
- Kopia escucha en `:51515`; no se expone a Internet.
- Supervisión: `repository status --json`, `snapshot list --json`, `maintenance info`.
- SPOF: documentar y monitorizar; la restauración break-glass no depende de él.

### 2.3 Deduplicación y compresión

- Dedup por chunks; compresión Zstandard.
- El ratio depende de la carga; medir en Fase 0.
- El índice consume RAM; dimensionar según TB únicos y número de snapshots.

---

## 3. Réplica NAS → S3

### 3.1 Comando

```bash
kopia repository sync-to s3 \
  --bucket=<bucket> \
  --endpoint=<endpoint> \
  --region=<region> \
  --access-key=<...> \
  --secret-access-key=<...> \
  --delete \
  --parallel=8
```

Verificar los flags exactos contra la versión fijada; la sintaxis puede variar.

### 3.2 Reglas

- Ejecutar desde el host del repositorio, con el NAS montado localmente.
- Job singleton con advisory lock y ventana exclusiva.
- No sincronizar durante una ventana incompatible con el backup.
- Verificar el destino (`snapshot list`, `repository status`).
- Si `sync-to` falla, reintentar con backoff; no bloquear el backup local.
- **NFS:** el locking es frágil; preferir montaje local. Plan B: snapshot del volumen del NAS y sincronizar desde una copia congelada.

### 3.3 Delete

- `--delete` propaga borrados; con Object Lock crea delete markers y las versiones bloqueadas permanecen.
- Alinear la retención de bloqueo con la GFS.
- No usar `--delete` indiscriminadamente si el objetivo es inmutabilidad.

---

## 4. Object Lock

### 4.1 Configuración

- Bucket creado con Object Lock habilitado y versioning; Kopia no lo crea.
- `--retention-mode=GOVERNANCE|COMPLIANCE` + `--retention-period=<días>`.
- `kopia maintenance set --extend-object-locks=true`.
- Lifecycle para versiones no actuales.

### 4.2 GOVERNANCE vs COMPLIANCE

| Modo | Protección | Reversible | Uso |
|---|---|---|---|
| GOVERNANCE | Borrado bloqueado para usuarios normales | Sí, con privilegios | Predeterminado |
| COMPLIANCE | Nadie puede acortar/eliminar antes de expirar | No | Solo requisito legal |

### 4.3 Coste

- Las versiones bloqueadas no desaparecen al borrar.
- Si la retención de lock es mayor que la GFS, se paga almacenamiento no deseado.
- Medir crecimiento y definir lifecycle.
- Documentar que la inmutabilidad protege contra credenciales de API robadas, no contra un compromiso total de la cuenta.

---

## 5. Retención GFS

Política por defecto:

| Clase | Copias |
|---|---|
| Diarias | 7 |
| Semanales | 4 |
| Mensuales | 6 |
| Anuales | 1 |

Reglas:

- Configurable por plan.
- La copia más reciente siempre se conserva.
- La limpieza se ejecuta tras el backup y en maintenance.
- No se borra un restore point con verificación pendiente o fijado (`pin`).
- La retención se aplica por agente y por plan.
- Los restore points inmutables no se borran hasta que expire el lock.

---

## 6. Capacidad

Fórmulas:

- `datos_únicos = suma de datos lógicos × factor de cambio`.
- `almacenamiento ≈ datos_únicos × (1 + overhead) / ratio_dedup`.
- `índice_RAM ≈ f(TB únicos, número de snapshots)`; medir.
- `staging_restore ≈ tamaño_virtual × 1.1` (o datos únicos si sparse).
- `S3 ≈ almacenamiento_NAS + versiones bloqueadas`.

Planificar:

- Crecimiento anual.
- Retención GFS.
- Object Lock.
- Espacio de maintenance (Kopia necesita espacio temporal para GC).
- Capacidad de PBS (caché S3 64–128 GiB).

### 6.1 Ejemplo de dimensionamiento (ilustrativo)

Cifras de trabajo, **no medidas**; sirven para explicar el método. Se sustituyen por las de G0.9.

| Paso | Cálculo | Resultado |
|---|---|---|
| Datos lógicos | 50 equipos × 1 TB | 50 TB |
| Datos únicos en el origen | 35 % de 50 TB | 17,5 TB |
| Deduplicación entre equipos y snapshots | ratio 1,5× | 11,7 TB |
| Overhead de repositorio (índice, packs) | +10 % | 12,9 TB |
| Retención GFS y versiones | +20 % | 15,5 TB |
| Crecimiento anual | +25 % | ~19,4 TB a 12 meses |
| Réplica S3 | almacenamiento + versiones bloqueadas | ≥15,5 TB |
| Staging de una restauración de 2 TB | sparse hasta 2 TB; archivo completo 2,2 TB | 2–2,2 TB |

Lecciones:

- El ratio de deduplicación es la variable más sensible; medirlo por carga, no asumirlo.
- Object Lock puede duplicar el almacenamiento si la retención de bloqueo excede la GFS.
- El staging necesita tanto espacio como el disco restaurado, salvo que se use sparse con cuidado.
- El repositorio necesita margen para maintenance, no solo para backups.

### 6.2 Compatibilidad con proveedores S3

No se asume soporte de Object Lock por marca. Antes de elegir, verificar con el proveedor:

| Comprobación | Por qué importa |
|---|---|
| API S3 compatible con los backends de Kopia | Evita adaptadores propios |
| Versioning activable | Requisito de Object Lock |
| Object Lock **en la creación del bucket** | No se puede habilitar después |
| Modos GOVERNANCE y COMPLIANCE | Determina la reversibilidad |
| Lifecycle de versiones no actuales | Controla el coste |
| Límites de tamaño de objeto y número de objetos | Kopia crea muchos objetos pequeños |
| Coste de operaciones (PUT/GET/LIST) | Puede superar el coste de almacenamiento |
| Egress y latencia | Afecta a la restauración |
| Región y residencia del dato | Cumplimiento |
| Borrado por credenciales de administrador | La inmutabilidad no protege una cuenta comprometida |

Si el proveedor no soporta Object Lock, la réplica sigue siendo útil como copia externa, pero no como defensa anti-ransomware. Documentarlo así en el panel.

---

## 7. PBS S3

- PBS 4.x; documentación 4.2.11.
- Caché local persistente de 64–128 GiB; no se puede usar un datastore regular preexistente.
- Un datastore S3 solo lo usa una instancia PBS.
- PBS no crea el bucket ni gestiona su ACL.
- Reconstrucción con `reuse-datastore` + `overwrite-in-use` y el mismo nombre.
- Object Lock no documentado para el datastore S3; usar política del bucket o réplica PBS.
- Si se necesita inmutabilidad de VMs, evaluar un segundo PBS o una réplica a S3 con otro mecanismo.

---

## 8. 3-2-1

| Copia | Dónde | Protección |
|---|---|---|
| 1 | NAS principal | Acceso LAN, snapshots del NAS |
| 2 | S3 réplica | Cifrado cliente, Object Lock |
| 3 | Offline | Claves, configuración y, si se desea, una copia del repositorio |

La tercera copia debe estar en un medio o ubicación independiente, no en el mismo dominio administrativo ni con las mismas credenciales.

---

## 9. Verificación del almacenamiento

- `kopia repository status` en origen y destino.
- `kopia snapshot list` para confirmar snapshots replicados.
- Verificación de integridad programada.
- Restauración de prueba desde S3, no solo desde NAS.
- Alerta si la réplica se atrasa más que el RPO.
- Alerta si el repositorio supera el 80 % de capacidad.

---

## 10. Criterios de aceptación

- El backup local funciona con S3 caído.
- La réplica se reanuda y se verifica.
- Un borrado dentro de Object Lock es denegado.
- La retención GFS elimina lo esperado y conserva lo protegido.
- La restauración desde S3 funciona.
- El repositorio no se corrompe tras un corte.
- El crecimiento con Object Lock es medido y aceptable.

---

## 11. Preguntas abiertas

- ¿Qué proveedor S3 y qué soporte real de Object Lock?
- ¿Se usa GOVERNANCE o COMPLIANCE?
- ¿Cuál es el RPO/RTO objetivo?
- ¿El NAS permite snapshots y montaje local?
- ¿Se replica también PBS a S3 o solo Kopia?
- ¿Cuánto espacio se reserva para maintenance?
