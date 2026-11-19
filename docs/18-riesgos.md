# Registro de riesgos

**Documento:** 18-riesgos  
**Versión:** 1.0  
**Revisión:** en cada puerta de fase.

---

## 1. Escala

| Probabilidad | Valor |
|---|---|
| Muy baja | 1 |
| Baja | 2 |
| Media | 3 |
| Alta | 4 |
| Muy alta | 5 |

| Impacto | Valor |
|---|---|
| Bajo | 1 |
| Medio | 2 |
| Alto | 3 |
| Crítico | 4 |

`Severidad = probabilidad × impacto`.

---

## 2. Registro

| ID | Riesgo | Prob. | Imp. | Sev. | Mitigación | Señal de detección | Fase |
|---|---|---|---|---|---|---|---|
| R-01 | Windows no arranca tras P2V (0x7B, BCD/EFI) | 4 | 4 | 16 | SATA+e1000, ISO virtio-win, inyección offline, preflight, pruebas por build | `E_BOOT_TIMEOUT`, `E_DRIVER_MISSING` | 2/3 |
| R-02 | BitLocker impide arrancar o exige clave no disponible | 3 | 4 | 12 | `vss_volumes` por defecto, aviso, custodia de clave o reinstalación sin cifrado | `E_BITLOCKER_LOCKED` | 0/2 |
| R-03 | vTPM nuevo rompe Hello/PIN/credenciales | 3 | 3 | 9 | Aviso, re-enrolamiento documentado | Incidencias post-restore | 2 |
| R-04 | Secure Boot con claves propias o módulos sin firmar | 3 | 3 | 9 | `pre-enrolled-keys` según origen, detectar claves, fallar claro | `E_BOOT_TIMEOUT` | 2 |
| R-05 | DKMS ext4 (blksnap/dattobd) rompe con kernel nuevo | 4 | 3 | 12 | Imagen crash-consistent, módulos opt-in, matriz de kernels | Fallo de carga DKMS | 0/3 |
| R-06 | Pérdida de la clave maestra | 2 | 4 | 8 | Shamir 3-de-5, custodia física, simulacro anual | Simulacro fallido | 0 |
| R-07 | Repository Server SPOF / cuello de banda | 4 | 3 | 12 | Host dedicado, NAS local, 10/25 GbE, monitorización, runbook | Latencia, cola, alertas | 0–3 |
| R-08 | `--stdin-file` sin reanudación | 4 | 3 | 12 | Segmentación, journal, reanudación por segmento | Relectura completa tras corte | 0/2 |
| R-09 | Sin CBT, ventana de backup inviable | 4 | 3 | 12 | Comunicarlo, programación escalonada, CBT Fase 3 | Backups que exceden ventana | 2/3 |
| R-10 | Delta VSS llena el disco del sistema | 3 | 3 | 9 | Límite de shadow storage, monitorización, aborto por umbral | `E_VSS_DELTA`, espacio bajo | 2 |
| R-11 | Agente PVE root comprometido | 2 | 4 | 8 | Firma, sin puertos, token mínimo, staging acotado, auditoría | Anomalías de API/auditoría | 2 |
| R-12 | Object Lock vs GFS: espacio/coste | 3 | 2 | 6 | Retención alineada, lifecycle, GOVERNANCE | Crecimiento anómalo | 0/3 |
| R-13 | Deriva de la CLI de Kopia | 4 | 3 | 12 | Versión fijada, wrapper, contract tests | CI falla al actualizar | 0–3 |
| R-14 | ACL de Kopia no aísla contenidos | 3 | 3 | 9 | Dominio de confianza o repositorios por grupo | Acceso cruzado en prueba | 0 |
| R-15 | `sync-to` sobre NFS/concurrencia | 3 | 3 | 9 | Montaje local, ventana exclusiva, verificación | Réplica inconsistente | 0/3 |
| R-16 | Verificación nivel 3 Windows no automatizable | 3 | 2 | 6 | Uptime + screendump + revisión | Prueba manual | 3 |
| R-17 | Deriva de versiones PVE/PBS | 3 | 2 | 6 | Matriz de compatibilidad, detección, features gateadas | Fallo de API | 0–3 |
| R-18 | Fuga de secretos en logs | 2 | 4 | 8 | Redacción, tests, permisos, revisión | Búsqueda de secretos | 1 |
| R-19 | Restore de archivos lento por catálogo no indexado | 3 | 2 | 6 | Catálogo por snapshot, `snapshot ls` bajo demanda | Latencia de UI | 1/2 |
| R-20 | Espacio de staging insuficiente | 3 | 2 | 6 | Preflight, cuota, archivo disperso | `E_PVE_SPACE` | 2 |
| R-21 | Dependencia excesiva del proveedor S3 | 2 | 3 | 6 | Cifrado cliente, copia offline, portabilidad | Cambio de proveedor | 3 |
| R-22 | Complejidad operativa subestimada | 3 | 3 | 9 | Runbooks, automatización, fases | Incidentes repetidos | 1–3 |

---

## 3. Riesgos de producto

| ID | Riesgo | Mitigación |
|---|---|---|
| P-01 | Prometer P2V Windows desatendido y no cumplirlo | Comunicar intervención en Fase 2; inyección offline en Fase 3 |
| P-02 | Confundir dedup con CBT | UI y docs explícitas; métricas de bytes leídos vs subidos |
| P-03 | Panel demasiado ambicioso antes de validar restore | Fase 0 primero; MVP mínimo |
| P-04 | Multi-tenant/seguridad empresarial fuera de alcance | Documentar límites |
| P-05 | Coste de mantenimiento mayor que comprar | Revisión construir/comprar en cada puerta |

---

## 4. Riesgos de seguridad

| ID | Riesgo | Mitigación |
|---|---|---|
| S-01 | Robo de credenciales de panel | 2FA, step-up, rate limit, auditoría |
| S-02 | Robo de credenciales S3 | Object Lock, permisos limitados, cifrado |
| S-03 | Agente comprometido borra backups | Credenciales sin FULL, Object Lock, alertas |
| S-04 | Supply chain de binarios | Firma, hashes, versiones fijadas |
| S-05 | CA comprometida | Raíz offline, rotación, revocación |
| S-06 | Break-glass sin control | Dos personas, registro, rotación posterior |

---

## 5. Riesgos aceptados

- Sin CBT en Fase 1/2: se acepta lectura completa; se mitiga con ventanas y comunicación.
- Repositorio único: se acepta el dominio de confianza; se revisa si se necesita aislamiento.
- Windows P2V con intervención: se acepta en Fase 2; se automatiza en Fase 3.
- Object Lock GOVERNANCE: reversible por administrador; se acepta salvo requisito legal.
- PBS S3 sin Object Lock documentado: se acepta; se usa política del bucket o réplica.

---

## 6. Revisión

- En cada puerta de fase.
- Tras cada incidente.
- Tras cada cambio de versión mayor de Kopia/PVE/PBS.
- El propietario aprueba los riesgos aceptados.
