# Verificación y pruebas

**Documento:** 16-verificacion-pruebas  
**Versión:** 1.0  
**Requisitos:** RF-21, RF-22, RF-23, RNF-02, RNF-07

---

## 1. Principio

Un backup no está terminado porque el job finalice, sino porque **se ha demostrado que se puede recuperar**. La verificación es parte del producto, no una tarea manual opcional.

---

## 2. Niveles de verificación

| Nivel | Qué hace | Frecuencia | Coste | Fase |
|---|---|---|---|---|
| **1 — Estructural** | `kopia snapshot verify`: índices, blobs y metadatos | Diaria | Bajo | 2 |
| **2 — Contenido** | `--verify-files-percent=100` o muestra aleatoria: descarga, descifra y descomprime | Semanal/mensual | Medio | 2 |
| **3 — Imagen** | Reconstruye la imagen y comprueba layout/tamaño/hashes | Mensual | Alto | 2/3 |
| **4 — Arranque** | Crea VM temporal, restaura, arranca en red aislada y verifica | Mensual | Alto | 3 |
| **5 — DR completo** | Simulación de pérdida del control plane y restauración desde S3 | Trimestral | Muy alto | 3 |

### 2.1 Nivel 1

- `kopia snapshot verify` valida la estructura; no descarga ni descifra blobs.
- No detecta bit rot posterior a la subida.
- Se ejecuta en maintenance diario y explícitamente por job.

### 2.2 Nivel 2

- `kopia snapshot verify --verify-files-percent=100 --file-parallelism=10`.
- Descarga, descifra y descomprime; equivale casi a una restauración de prueba.
- Para repositorios grandes, usar una muestra (`1 %` diario) y un 100 % mensual.
- Verificar también un archivo aleatorio con comparación de hash.

### 2.3 Nivel 3

- Reconstruir la imagen en staging.
- Verificar tamaño, layout, hashes de segmentos.
- Comparar la tabla de particiones con el manifiesto.
- No requiere arrancar la VM, pero es un paso previo.

### 2.4 Nivel 4

- Crear una VM temporal `bdr-test-<id>` en un rango de VMID reservado.
- Restaurar en red aislada.
- Arrancar y verificar:
  - uptime estable;
  - consola serie (Linux) o screendump (Windows);
  - guest agent si existe;
  - login o hitos.
- Destruir la VM y registrar evidencia.
- Nunca tocar producción.

### 2.5 Nivel 5

- Simular pérdida del control plane, PostgreSQL y Repository Server.
- Restaurar desde S3 con la ISO y la clave custodiada.
- Medir RTO y documentar.
- Rotar la clave tras el ejercicio.

---

## 3. Programación

| Tipo | Frecuencia | Ventana |
|---|---|---|
| Estructural | Diaria | Maintenance |
| Contenido (muestra) | Diaria/semanal | Fuera de backup |
| Contenido (100 %) | Mensual | Ventana amplia |
| Imagen | Mensual | Ventana amplia |
| Arranque | Mensual | Red aislada |
| DR | Trimestral | Planificada |

La programación es por plan y por criticidad. Un plan `critical` puede exigir nivel 4 mensual; uno `standard`, nivel 2.

---

## 4. Matriz de pruebas

### 4.1 Origen → destino

| Origen | Destino | Debe funcionar |
|---|---|---|
| Windows 11 UEFI sin BitLocker | VM Proxmox OVMF | Arranque y login |
| Windows 11 UEFI con BitLocker | VM Proxmox OVMF + vTPM | Aviso; arranque con clave o reinstalación sin cifrado |
| Windows Server 2025 UEFI | VM Proxmox OVMF | Arranque y servicios |
| Windows Server 2025 BIOS | VM Proxmox SeaBIOS | Arranque legacy |
| Debian 12 ext4 | VM Proxmox VirtIO | Arranque y red |
| Debian 12 LVM | VM Proxmox VirtIO | Arranque y red |
| Debian 13 Btrfs | VM Proxmox VirtIO | Arranque y red |
| VM Proxmox | PBS → restore | Arranque |
| Cualquiera | NAS | Backup, incremental, restore |
| Cualquiera | S3 | Réplica, restore |
| Cualquiera | Corte de red | Reanuda sin corrupción |
| Cualquiera | Disco lleno | Error accionable, sin corrupción |

### 4.2 Pruebas negativas

- Corte de red a mitad de backup.
- Reinicio del agente a mitad.
- Repositorio no disponible.
- S3 no disponible.
- Certificado caducado.
- Token de enrolamiento usado o caducado.
- Clave de repositorio incorrecta.
- Disco lleno en staging.
- VMID ocupado.
- Storage sin content type `import`.
- Manifiesto con `schema_version` desconocida.
- Segmento corrupto.
- VM que no arranca.
- Cancelación a mitad.
- Borrado dentro de Object Lock.
- Intento de acceso a snapshot de otro agente.

### 4.3 Pruebas de seguridad

- Certificado no emitido por la CA.
- Agente revocado.
- Token reutilizado.
- Secreto en logs (búsqueda).
- 2FA ausente.
- CSRF.
- Rate limit.
- Escalada de privilegios en PVE/PBS.
- Borrado masivo de restore points.

---

## 5. Entornos

| Entorno | Uso | Reglas |
|---|---|---|
| Laboratorio | Fase 0, pruebas destructivas | Aislado, sin producción |
| Staging | Integración | Datos sintéticos |
| Producción | Solo después de la puerta | Cambios controlados |

**Nunca** se ejecutan pruebas destructivas contra producción. Las restauraciones de prueba usan VMID, nombre y red claramente diferenciados.

---

## 6. Evidencia

Cada prueba debe producir:

- ID de caso y requisito asociado.
- Fecha, versión de agente/Kopia/PVE/PBS.
- Comandos ejecutados.
- Salida JSON o logs relevantes.
- Resultado: pass/fail.
- Captura o consola en pruebas de arranque.
- Duración y observaciones.

Se almacena en `docs/anexos/b-poc-0-plan-evidencias.md` para Fase 0 y en el sistema de CI/artefactos para las fases siguientes.

---

## 7. Pruebas automáticas

### 7.1 Unitarias

- Planes, cron, retención, ventanas.
- Máquina de estados de jobs.
- Parseo de manifiestos.
- Cálculo de segmentos y offsets.
- Errores tipados.
- Redacción de secretos.

### 7.2 Integración

- Kopia contra un repositorio temporal.
- Proxmox/PBS contra un entorno aislado.
- mTLS con CA de prueba.
- Cola River con leases y reintentos.

### 7.3 Contract tests de Kopia

- `snapshot create --json`.
- `snapshot list --json`.
- `repository status --json`.
- `snapshot verify`.
- `sync-to`.
- Falla si cambia el esquema o el código de salida en la versión fijada.

### 7.4 E2E

- Enrolar un agente de prueba.
- Backup de archivos.
- Restore de archivos.
- Backup de imagen de un disco pequeño.
- Restore P2V Linux.
- Restore P2V Windows (con intervención documentada).
- Verificación nivel 1–3.
- Break-glass.

---

## 8. Criterios de aceptación

- Nivel 1 y 2 automáticos en el 100 % de los restore points críticos.
- Nivel 3 mensual en al menos un restore point por plan.
- Nivel 4 en ≥90 % de intentos Linux y con criterio definido en Windows.
- Ningún fallo deja recursos huérfanos.
- Un backup interrumpido no corrompe el repositorio.
- Un restore interrumpido se reanuda.
- La evidencia está enlazada desde `21-matriz-trazabilidad.md`.
- Los contract tests pasan en CI.

---

## 9. Preguntas abiertas

1. ¿Cómo se verifica automáticamente el arranque de Windows sin guest agent?
2. ¿Qué porcentaje de muestra es suficiente para detectar corrupción?
3. ¿Cuánto tarda el nivel 3 en un disco de 2 TB?
4. ¿Se puede aislar la red de la VM de prueba sin afectar al nodo?
5. ¿Cómo se prueba Object Lock sin bloquear el borrado legítimo para siempre?
6. ¿Qué evidencia es aceptable para un auditor?
