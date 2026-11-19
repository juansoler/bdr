# Anexo B — Plan de PoC (Fase 0) y evidencias

**Documento:** anexos/b-poc-0-plan-evidencias  
**Versión:** 1.0  
**Objetivo:** convertir los supuestos críticos en evidencia antes de programar el producto.

---

## 1. Entorno

| Elemento | Requisito |
|---|---|
| Windows 11 | UEFI, TPM, Secure Boot; una instancia con BitLocker y otra sin |
| Windows Server 2025 | UEFI y BIOS si es posible |
| Debian 12 | ext4 sin LVM y LVM; opcional Btrfs/ZFS |
| Proxmox VE | 8.2+/9.x, storage dir/lvmthin/zfspool |
| PBS | 4.x, datastore local y S3 si es posible |
| NAS | Montaje local y NFS/SMB para comparar |
| S3 | Bucket con Object Lock y versioning |
| Red | 1 GbE y, si es posible, 10 GbE |
| Kopia | Versión fijada, binario verificado |

Reglas:

- Entorno aislado; nunca producción.
- Discos de prueba pequeños (20–200 GB) y uno grande (1–2 TB) si es posible.
- Documentar versiones exactas.
- Guardar comandos, salidas, capturas y tiempos.

---

## 2. Experimento G0.1 — VSS

**Pregunta:** ¿go-vss crea un shadow set atómico con ESP, MSR, recovery y C: en Windows 11/Server 2025, y se puede leer en crudo?

**Pasos:**

1. Compilar un programa Go con `SmithOperatingSolutions/go-vss` y otro con `mxk/go-vss`.
2. Crear shadow set con todos los volúmenes, incluidos ESP/recovery por `\\?\Volume{GUID}\`.
3. Leer 1 GB de cada volumen con buffer de 1 MiB.
4. Medir tiempo, errores y delta de VSS.
5. Repetir con carga de escritura.
6. Cerrar y limpiar.

**Evidencia:** IDs de shadow copy, volúmenes incluidos, tiempos, errores, delta, captura de `vssadmin list shadows`/writers.

**Criterio:** shadow set completo, lectura estable, delta aceptable; decisión go-vss vs mxk.

---

## 3. Experimento G0.2 — Streaming

**Pregunta:** ¿`kopia snapshot create --stdin-file` permite reanudar o checkpoint? ¿Qué rendimiento y memoria consume?

**Pasos:**

1. Crear un repositorio local y uno remoto.
2. Enviar 200 GB por stdin en segmentos de 2/4/8 GiB.
3. Matar el proceso a mitad.
4. Reanudar y medir cuánto se re-sube.
5. Medir throughput, CPU y RAM.
6. Repetir sin cambios para medir dedup.

**Evidencia:** tiempos, bytes leídos/subidos, snapshots creados, logs, consumo.

**Criterio:** segmentación y tamaño óptimos; decisión sobre checkpoint.

---

## 4. Experimento G0.3 — ACL y Repository Server

**Pregunta:** ¿La ACL de Kopia aísla a un agente? ¿Cómo se conecta de forma no interactiva? ¿Qué pasa al caducar la credencial?

**Pasos:**

1. Crear repositorio y dos usuarios `agent-a@host`, `agent-b@host`.
2. `kopia server acl enable`.
3. Intentar listar/restaurar snapshots del otro usuario.
4. Intentar escribir en un prefijo ajeno.
5. Caducar/revocar la credencial a mitad de una subida.
6. Probar `kopia server refresh`/SIGHUP.

**Evidencia:** resultados de cada intento, logs, tiempo de revocación.

**Criterio:** sí/no al aislamiento; decisión de dominio de confianza o repositorios separados.

---

## 5. Experimento G0.4 — P2V Windows

**Pregunta:** ¿Una imagen de Windows 11/Server 2025 restaurada con `import-from` arranca?

**Pasos:**

1. Backup de imagen con `vss_volumes`.
2. Crear VM con SATA + e1000.
3. `import-from` al disco.
4. Arrancar y verificar.
5. Adjuntar ISO virtio-win, instalar drivers.
6. Cambiar a virtio-scsi y virtio-net.
7. Repetir con BitLocker y vTPM.
8. Probar UEFI/OVMF y BIOS/SeaBIOS.

**Evidencia:** capturas, consola, eventos, tiempos, errores, tasa de éxito.

**Criterio:** procedimiento reproducible; decisión SATA vs VirtIO inicial; aviso BitLocker.

---

## 6. Experimento G0.5 — P2V Debian

**Pregunta:** ¿Debian ext4/LVM restaurado con VirtIO arranca y tiene red?

**Pasos:**

1. Backup de imagen.
2. Crear VM VirtIO.
3. `import-from`.
4. Chroot: initramfs con virtio, fstab, red.
5. Arrancar con consola serie.
6. Verificar login y red.
7. Repetir con LVM y Btrfs.

**Evidencia:** consola, `ip a`, tiempos, errores.

**Criterio:** arranque sin intervención en ≥9/10; procedimiento documentado.

---

## 7. Experimento G0.6 — sync-to y Object Lock

**Pregunta:** ¿La réplica funciona con escritura concurrente y Object Lock? ¿Cómo crece el espacio?

**Pasos:**

1. Crear bucket con Object Lock y versioning.
2. Configurar `--retention-mode` y `--retention-period`.
3. Ejecutar `sync-to` con `--delete`.
4. Intentar borrar un objeto bloqueado.
5. Medir espacio y versiones tras varios ciclos.
6. Repetir con backend local y NFS.

**Evidencia:** estado del bucket, errores, espacio, versiones, tiempos.

**Criterio:** sí/no a la seguridad con concurrencia; modo GOVERNANCE/COMPLIANCE; coste.

---

## 8. Experimento G0.7 — ext4 sin LVM

**Pregunta:** ¿blksnap o dattobd funcionan en Debian 12 con Secure Boot? ¿O basta crash-consistent?

**Pasos:**

1. Compilar/instalar blksnap y dattobd.
2. Firmar módulos/MOK si Secure Boot.
3. Crear snapshot y leer.
4. Actualizar kernel y repetir.
5. Comparar con `fsfreeze` + lectura directa.
6. Restaurar y comprobar arranque.

**Evidencia:** carga de módulo, errores, tiempos, consistencia, arranque.

**Criterio:** decisión de alcance: crash-consistent por defecto, módulos opt-in.

---

## 9. Experimento G0.8 — Custodia

**Pregunta:** ¿Se puede recuperar sin el servidor con 3 de 5 shares?

**Pasos:**

1. Generar clave y shares.
2. Crear un snapshot de prueba.
3. Apagar el servidor de control y el Repository Server.
4. Arrancar un host limpio con la ISO.
5. Reconstruir la clave con 3 shares.
6. Conectar al repositorio y restaurar.
7. Medir tiempo y verificar.

**Evidencia:** tiempo, resultado, problemas, lecciones.

**Criterio:** procedimiento firmado y reproducible.

---

## 10. Experimento G0.9 — Rendimiento

**Pregunta:** ¿Qué throughput, dedup y RAM se obtienen con el hardware real?

**Pasos:**

1. Backup de 100 GB, 500 GB y 1 TB.
2. Medir lectura, subida, CPU, RAM, duración.
3. Repetir sin cambios y con cambios del 5 %.
4. Medir RAM del índice.
5. Probar 1 GbE y 10 GbE.
6. Probar NAS local vs NFS.

**Evidencia:** tablas de métricas, cuellos de botella, recomendaciones.

**Criterio:** dimensionamiento y decisión de host dedicado.

---

## 11. Puerta de Fase 0

- Los 9 experimentos tienen evidencia.
- `decisiones.md` actualizado: pendientes resueltos o reclasificados.
- ADRs revisados.
- Roadmap ajustado.
- Riesgos revisados.
- Ningún `[VERIFICAR]` crítico abierto.

---

## 12. Plantilla de evidencia

```
Experimento: G0.x
Fecha:
Versiones: agente / Kopia / PVE / PBS / SO
Objetivo:
Pasos:
Resultado:
Métricas:
Errores:
Decisión:
Evidencia (rutas):
Observaciones:
```
