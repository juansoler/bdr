# Roadmap

**Documento:** 19-roadmap  
**Versión:** 1.2  
**Regla:** no se avanza de fase sin cumplir la puerta de salida con evidencia.

---

## 1. Enfoque

- La **Fase 0** reduce incertidumbre antes de programar el producto.
- La **Fase 1** entrega valor temprano con archivos y control plane.
- La **Fase 2** añade imagen y restauración a Proxmox, el núcleo del producto.
- La **Fase 3** añade robustez 3-2-1, verificación de arranque, automatización y el motor de SQL Server.
- La **Fase 4** explora funcionalidades avanzadas solo si la base es estable.

Esfuerzo relativo: **S** pequeño, **M** medio, **L** grande, **XL** muy grande. Sin plazos en semanas.

---

## 2. Fase 0 — PoC y reducción de incertidumbre (M)

### 2.1 Objetivo

Convertir los supuestos en evidencia. No se escribe el panel ni el agente completo.

### 2.2 Experimentos

| ID | Experimento | Evidencia | Decisión |
|---|---|---|---|
| G0.1 | VSS con go-vss/mxk en Win11 y Server 2025; ESP + recovery + C:; lectura raw; delta | Shadow set, tiempos, errores | Librería y límites |
| G0.2 | `--stdin-file` con 200 GB; matar a mitad; medir re-subida y checkpoint | Bytes, tiempos, logs | Segmentación y tamaño |
| G0.3 | ACL de Kopia: acceso cruzado, credencial caducada | Prueba con dos usuarios | Dominio de confianza |
| G0.4 | P2V Windows 11/Server 2025: import-from, SATA+e1000, VirtIO | VM arrancada, capturas | Procedimiento y tasa |
| G0.5 | P2V Debian ext4/LVM: VirtIO, chroot, initramfs, red | VM arrancada, consola | Procedimiento |
| G0.6 | `sync-to` + Object Lock: réplica, delete, borrado denegado | Estado S3, versiones | Modo y retención |
| G0.7 | ext4 sin LVM: blksnap/dattobd/kernel/Secure Boot | Carga de módulo, snapshot | Alcance de imagen |
| G0.8 | Custodia: shares, pérdida del servidor, restore en host limpio | Tiempo, resultado | Procedimiento |
| G0.9 | Rendimiento: 1/10 GbE, dedup, RAM del índice, paralelismo | Métricas | Dimensionamiento |

### 2.3 Entregables

- `anexos/b-poc-0-plan-evidencias.md` completo.
- `anexos/c-comandos-kopia-verificados.md`.
- `anexos/e-contrato-cli-kopia.md`.
- ADRs actualizados.
- Roadmap ajustado.

### 2.4 Puerta

- Los 9 experimentos con evidencia.
- Ningún `[VERIFICAR]` crítico abierto.
- Decisión sobre VSS, segmentación, ACL, P2V, ext4 y Object Lock.
- Riesgos revisados.

---

## 3. Fase 1 — MVP de archivos y control plane (L)

### 3.1 Alcance

- Monorepo y CI.
- PostgreSQL, migraciones y River.
- API admin y agentes; enrolamiento mTLS.
- Agente Windows/Linux: inventario, heartbeat, planes, archivos.
- Kopia Repository Server central.
- NAS principal.
- Panel mínimo: resumen, equipos, grupos, planes, jobs, restore points, restauración de archivos, alertas, auditoría.
- Modal de instalación con token de un solo uso y comando firmado.
- Resolución de planes efectivos por agente y grupo.
- 2FA, secretos cifrados, logs y métricas.
- PBS: catálogo y restore básico (si la versión está disponible).

### 3.2 No incluye

- Imagen de disco.
- P2V.
- ISO.
- S3/Object Lock.
- Verificación de arranque.

### 3.3 Entregables

- `apps/server`, `apps/agent`, `apps/web` compilables.
- Docker Compose.
- Instaladores Windows/Linux.
- Documentación de API (OpenAPI inicial).
- Runbooks de alta, backup y restore de archivos.

### 3.4 Puerta

- 10 equipos reales durante 30 días.
- 100 % de jobs con estado final correcto o error accionable.
- Restore de archivos verificado semanalmente.
- RPO del plan ≥95 %.
- Revocación de agente efectiva en <5 min.
- Auditoría sin huecos.
- Ningún secreto en logs.
- Backup de la BD y reconstrucción probados.

---

## 4. Fase 2 — Imagen y restauración a Proxmox (L)

### 4.1 Alcance

- Motor de imagen con manifiesto versionado.
- Captura VSS por volúmenes; `raw_disk` como fallback.
- Captura Linux LVM/Btrfs/ZFS; ext4 crash-consistent.
- Segmentación y reanudación.
- Agente PVE.
- Restore a VM: `import-from`, SATA+e1000, VirtIO, chroot Linux.
- Wizard de restauración con preflight, avisos y evidencia.
- Preflight P2V.
- ISO de rescate (construcción y flujo básico).
- Verificación niveles 1–2.
- PBS: backup, catálogo, verify, restore.

### 4.2 Entregables

- Imagen completa de Windows y Linux.
- Restore P2V Windows con intervención documentada.
- Restore P2V Debian sin intervención.
- ISO arrancable en BIOS/UEFI.
- Manifiesto y contrato de CLI documentados.
- Runbooks de restore.

### 4.3 Puerta

- 10/10 restores de archivos íntegros.
- ≥8/10 P2V Windows arrancan con el procedimiento documentado.
- ≥9/10 Debian arrancan sin intervención.
- Verificación 1–2 automática en el 100 % de los RP críticos.
- Ninguna restauración deja recursos huérfanos.
- PBS operativo con tokens mínimos.
- Break-glass probado.

---

## 5. Fase 3 — Robustez, 3-2-1 y automatización (L/XL)

### 5.1 Alcance

- Réplica S3 con Object Lock.
- Verificación nivel 3 y 4 (arranque en VM temporal).
- Inyección offline de VirtIO.
- Actualizaciones firmadas con rollout y rollback.
- HA del control plane.
- DR desde réplica.
- CBT opt-in (USN/blksnap/dattobd).
- **Motor SQL Server (RF-40–RF-46, `24-motor-sql-server.md`):** conector TDS dentro del agente Windows, descubrimiento de instancias, backups Full/Diff/Log, transferencia al repositorio, metadatos y cadenas, restauración de una base completa, `RESTORE VERIFYONLY` y custodia de certificados de cifrado/TDE.
- Límites finos de rendimiento.
- Alertas avanzadas, informes.
- Pruebas de caos.

### 5.2 Puerta

- Réplica verificada semanalmente con restore real.
- Nivel 3 ≥90 % Linux y criterio definido en Windows.
- Actualización de 50 agentes sin intervención.
- Caída del control plane no interrumpe backups en curso.
- RTO medido para pérdida del repositorio.
- Object Lock probado y coste medido.
- Simulacro de clave anual registrado.
- Motor SQL: la cadena Full/Diff/Log se restaura íntegra en otra instancia y los backups pasan `RESTORE VERIFYONLY`.

---

## 6. Fase 4 — Avanzado

- Búsqueda global de archivos.
- CDP.
- Run VM from backup / instant restore.
- Grupos y políticas avanzadas.
- Informes y cumplimiento.
- V2P si hay demanda.

Solo se aborda si las fases 1–3 están estables y con evidencia.

---

## 7. Dependencias y camino crítico

```
G0.1 VSS ─┐
G0.2 stream ─┼─▶ Fase 2 imagen ─▶ P2V Windows ─▶ verificación nivel 4
G0.4 P2V ───┘

Fase 1 control plane ─▶ Fase 2 orquestación ─▶ Fase 3 réplica/verificación
```

- La Fase 0 no depende de código de producto.
- La Fase 1 puede empezar en paralelo a G0.1/G0.2, pero no debe consumir todo el esfuerzo antes de validar P2V.
- La Fase 2 no empieza sin G0.2 y G0.4.
- La Fase 3 no empieza sin restauración estable.

---

## 8. Resumen de esfuerzo

| Fase | Esfuerzo | Riesgo |
|---|---|---|
| 0 | M | Alto (aprendizaje) |
| 1 | L | Medio |
| 2 | L | Alto |
| 3 | L/XL | Medio/alto |
| 4 | Variable | Bajo/medio |

---

## 9. Qué no hacer

- No construir el panel completo antes de la PoC.
- No prometer P2V Windows desatendido en Fase 2.
- No prometer CBT en Fase 2.
- No usar UrBackup como dependencia.
- No usar Duplicacy.
- No copiar `.mdf`/`.ldf` en caliente como sustituto del backup nativo de SQL Server.
- No deduplicar internamente los archivos `.bak`/`.trn` en el repositorio.
- No implementar antimalware/EDR/parches.
- No avanzar de fase sin evidencia.
- No probar restauraciones destructivas en producción.

---

## 10. Puntos de decisión

| Punto | Pregunta | Fase |
|---|---|---|
| D1 | ¿go-vss o mxk? | 0 |
| D2 | ¿Tamaño de segmento? | 0 |
| D3 | ¿Un repositorio o varios por grupo? | 0 |
| D4 | ¿SATA+e1000 o VirtIO directo en Windows? | 0 |
| D5 | ¿blksnap/dattobd o solo crash-consistent? | 0 |
| D6 | ¿GOVERNANCE o COMPLIANCE? | 0/3 |
| D7 | ¿River o Asynq? | 0/1 |
| D8 | ¿Servidor de repositorio dedicado? | 0/1 |
| D9 | ¿Inyección offline de VirtIO? | 2/3 |
| D10 | ¿Se implementa CBT? | 3 |
| D11 | ¿Agente local o conexión remota para SQL Server? **Resuelto: agente en el servidor (A-11)** | 3 |
| D12 | ¿Alcance de bases SQL: solo usuario o todas? **Resuelto: todas, incluidas las de sistema (A-12)** | 3 |
