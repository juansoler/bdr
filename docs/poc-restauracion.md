# PoC de restauración — registro de evidencias

**Documento:** poc-restauracion  
**Versión:** 1.0  
**Estado:** plantilla; se completa durante la Fase 0.  
**Procedimiento detallado:** [`anexos/b-poc-0-plan-evidencias.md`](anexos/b-poc-0-plan-evidencias.md)  
**Kit ejecutable:** [`../labs/README.md`](../labs/README.md)

---

## 1. Objetivo

Demostrar, con evidencia reproducible, que las decisiones críticas del diseño son viables en el hardware y las versiones reales antes de programar el producto.

El primer hito es **Windows Server 2025 UEFI → VM en Proxmox**, por ser el de mayor incertidumbre técnica.

---

## 2. Entorno de pruebas

| Elemento | Valor |
|---|---|
| Fecha | |
| Windows 11 build | |
| Windows Server 2025 build | |
| Debian | |
| Proxmox VE | |
| PBS | |
| Kopia | |
| NAS | |
| S3 | |
| Red | |

---

## 3. Resultados

| ID | Prueba | Resultado esperado | Resultado real | Evidencia | Decisión |
|---|---|---|---|---|---|
| G0.1 | VSS con ESP/recovery/C: | Shadow set completo y lectura raw | | | |
| G0.2 | Streaming 200 GB y corte | Reanudación medida | | | |
| G0.3 | ACL de Kopia | Acceso cruzado denegado o riesgo aceptado | | | |
| G0.4 | P2V Windows | VM arranca con SATA+e1000 | | | |
| G0.5 | P2V Debian | VM arranca con VirtIO | | | |
| G0.6 | sync-to + Object Lock | Réplica y borrado denegado | | | |
| G0.7 | ext4 sin LVM | Decisión blksnap/dattobd/crash | | | |
| G0.8 | Custodia | Restore con 3 de 5 shares | | | |
| G0.9 | Rendimiento | Throughput y RAM medidos | | | |

---

## 4. Procedimiento resumido de P2V Windows

1. Backup de imagen con `vss_volumes`.
2. Crear VM con SATA + e1000 y `import-from`.
3. Arrancar y comprobar login/red.
4. Adjuntar ISO virtio-win e instalar drivers.
5. Cambiar a virtio-scsi/virtio-net y comprobar.
6. Repetir con BitLocker y vTPM.
7. Documentar intervenciones y tasa de éxito.

## 5. Procedimiento resumido de P2V Debian

1. Backup de imagen (LVM o ext4 crash-consistent).
2. Crear VM VirtIO y `import-from`.
3. Chroot: initramfs con virtio, fstab, red.
4. Arrancar con consola serie.
5. Verificar login y red.
6. Documentar.

---

## 6. Decisiones registradas

| Fecha | Decisión | Motivo | Documento actualizado |
|---|---|---|---|
| | | | `decisiones.md` |
| | | | ADR |

---

## 7. Incidencias y lecciones

| Fecha | Incidencia | Impacto | Acción |
|---|---|---|---|
| | | | |

---

## 8. Aprobación

| Rol | Nombre | Fecha | Firma |
|---|---|---|---|
| Propietario | | | |
| Operador | | | |

---

## 9. Criterio de cierre

- Los 9 experimentos tienen evidencia.
- Las decisiones pendientes de `decisiones.md` están resueltas o reclasificadas.
- El roadmap se ajusta con lo aprendido.
- El riesgo residual se acepta explícitamente.
