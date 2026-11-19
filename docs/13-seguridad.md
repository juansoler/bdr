# Seguridad

**Documento:** 13-seguridad  
**Versión:** 1.0  
**Requisitos:** RNF-01, RF-28, RF-29, RF-30, RF-35, RF-36, RF-37, RF-38

---

## 1. Modelo de amenazas

### 1.1 Activos

- Datos de backup y sus claves.
- Credenciales de Proxmox, PBS, NAS y S3.
- Catálogo, políticas y auditoría.
- Binarios y actualizaciones de agentes.
- Disponibilidad del servicio de backup.

### 1.2 Actores

| Actor | Confianza | Riesgo |
|---|---|---|
| Administrador legítimo | Alta | Error humano, credencial comprometida |
| Agente legítimo | Media | Máquina comprometida, movimiento lateral |
| Atacante externo | Ninguna | Acceso a panel/API, ransomware |
| Atacante interno | Variable | Borrado de backups, cambio de retención |
| Proveedor S3/NAS | Variable | Acceso a datos cifrados, borrado |
| Operador de break-glass | Alta | Uso indebido de la clave |

### 1.3 Fronteras de confianza

- Internet ↔ panel/API: no expuesto por defecto.
- Agente ↔ control plane: mTLS.
- Control plane ↔ repositorio: red interna, credenciales de servicio.
- Repositorio ↔ NAS/S3: backend cifrado.
- Agente PVE ↔ PVE: token de mínimo privilegio.
- Operador ↔ ISO: clave custodiada.

### 1.4 Compromisos y límites

- Si se compromete el control plane **y** el agente PVE, el atacante puede afectar al clúster. El modelo de amenazas debe decirlo.
- Si se compromete la cuenta S3 con permisos de borrado, Object Lock es la última defensa; GOVERNANCE es reversible por un principal privilegiado, COMPLIANCE no.
- Si se pierde la clave maestra, los backups son irrecuperables. No hay puerta trasera.
- El cifrado en cliente protege el backend, no un endpoint comprometido con la clave en memoria.

---

## 2. Enrolamiento y PKI

### 2.1 Token de un solo uso

- 32 bytes aleatorios; se guarda solo el hash (argon2id) y un prefijo identificativo.
- TTL 1 h; un uso; binding opcional a hostname/labels.
- Rate limit: 5/min por IP, 20/h global.
- El token se muestra una sola vez en el panel.

### 2.2 Certificados

- El agente genera un par de claves (Ed25519) y un CSR.
- El servidor firma con `CN=<agent_uuid>`, `SAN URI=urn:bdr:agent:<uuid>`, EKU `clientAuth`, validez 90 días.
- CA intermedia propia; la raíz puede estar offline.
- El agente fija la huella del servidor en el primer contacto y rechaza cambios sin re-enrolamiento.
- Renovación automática al 60 % de la vida; solapamiento de 7 días.
- Revocación inmediata al marcar `agents.status=revoked`; se aplica también en el proxy/API y en el usuario de Kopia.

### 2.3 Rotación de CA

- Rotación anual de la intermedia con doble confianza temporal.
- Los certificados antiguos siguen válidos hasta su caducidad durante la transición.
- Documentar el procedimiento en `17-operacion-runbooks.md`.

---

## 3. mTLS y red

- Listener de agentes en `:8443` con `RequireAndVerifyClientCert`.
- Listener de panel en `:443` con TLS de navegador.
- No se mezclan.
- No se delega la verificación del certificado de cliente a un proxy que no propague la identidad.
- El Repository Server escucha en su propio puerto, con TLS; no se expone a Internet.
- Firewall: solo los puertos necesarios; los agentes inician la conexión.
- VPN o red interna para administración remota.

---

## 4. Secretos

### 4.1 Envelope encryption

- Cada secreto tiene una DEK AES-256-GCM con AAD = nombre + id.
- La DEK se envuelve con una KEK versionada.
- La KEK vive fuera de la BD: archivo 0400, TPM/systemd-creds o KMS.
- `key_version` permite rotar sin downtime.
- Los secretos nunca se devuelven por API; solo IDs.

### 4.2 Qué se cifra

- Contraseña del repositorio Kopia.
- Tokens de Proxmox/PBS.
- Credenciales de S3/NAS.
- Claves de recuperación de BitLocker, si el usuario decide custodiarlas (opcional y explícito).
- Secretos TOTP.

### 4.3 Logs

- Redacción automática de secretos, tokens, cabeceras y URLs con credenciales.
- Tests de redacción en CI.
- No se registran cuerpos completos de requests sensibles.

---

## 5. Autenticación y autorización del panel

- Contraseñas con argon2id.
- 2FA TOTP obligatorio para `admin` y `operator`.
- Códigos de recuperación de un uso.
- Sesión de 12 h; re-auth (step-up) para:
  - restaurar a VM;
  - borrar restore points;
  - rotar claves;
  - gestionar usuarios;
  - cambiar retención/Object Lock.
- RBAC: `admin`, `operator`, `viewer`.
- CSRF, CSP estricta, HSTS, cookies `Secure`/`HttpOnly`.
- Rate limit y bloqueo progresivo.
- Registro de login, MFA y step-up.

---

## 6. Auditoría

- `audit_log` append-only con hash chain.
- Actor, objeto, antes/después, IP, user-agent, request_id.
- Exportación periódica a S3 con Object Lock.
- Alertas ante:
  - eliminación masiva de restore points;
  - cambio de retención;
  - desactivación de 2FA;
  - creación/revocación de agentes;
  - cambio de credenciales;
  - restauración a VM en producción.

---

## 7. Mínimo privilegio en Proxmox/PBS

| Sistema | Token | Privilegios |
|---|---|---|
| PVE backup | `bdr-backup` | `VM.Backup`, `Datastore.Audit` |
| PVE restore | `bdr-restore` | `VM.Allocate`, `VM.Config.*`, `VM.PowerMgmt`, `Datastore.AllocateSpace`, `Datastore.Audit`, `VM.Monitor` |
| PBS backup | `bdr-pbs-backup` | `DatastoreBackup` |
| PBS read | `bdr-pbs-read` | `DatastoreReader` |
| PBS operator | `bdr-pbs-operator` | `DatastoreOperator` |

- Separación de privilegios activada en los tokens PVE.
- Paths restringidos a `/vms` y al storage destino.
- Sin `Datastore.Allocate`, `Sys.Modify` ni `Permissions.Modify`.
- Allowlist de IP y verificación de huella TLS.
- Rotación periódica.

---

## 8. Repositorio Kopia

- Un usuario por agente (`agent-<uuid>`), contraseña de 32 bytes.
- ACL limitada a su prefijo; READ/APPEND, sin FULL salvo mantenimiento.
- El servidor de control usa un usuario de mantenimiento separado.
- Revocación en el proxy/API y en Kopia; forzar `kopia server refresh`/SIGHUP.
- **Límite:** las ACL no aíslan contenidos; los agentes del mismo repositorio se consideran del mismo dominio de confianza. Si no es aceptable, separar repositorios por grupo.
- El agente PVE usa un usuario de solo lectura limitado a los prefijos que debe restaurar.

---

## 9. Ransomware e inmutabilidad

- Object Lock en S3 con GOVERNANCE por defecto; COMPLIANCE solo con requisito legal.
- Bucket con versioning y lock desde su creación.
- `maintenance set --extend-object-locks=true`.
- Lifecycle para versiones no actuales.
- Credenciales de backup sin permiso de borrado ni de cambio de retención.
- Réplica que no propague borrados de forma indiscriminada.
- Alertas ante eliminación masiva.
- Copia offline de la clave y de la configuración.
- Separación de credenciales de backup y administración.

---

## 10. Binarios y actualizaciones

- Binarios firmados con Ed25519; clave pública embebida en agente y servidor.
- Descarga solo por mTLS.
- Instalación atómica con rollback.
- Rollout por porcentaje y labels.
- El agente rechaza binarios sin firma o con hash distinto.
- Reproducibilidad: versiones fijadas y hashes publicados.

---

## 11. Escenarios de compromiso

| Escenario | Impacto | Mitigación |
|---|---|---|
| Panel comprometido | Alto | 2FA, step-up, RBAC, auditoría, Object Lock, credenciales separadas |
| Agente comprometido | Medio | mTLS, ACL, sin shell remoto, rotación, revocación |
| Agente PVE comprometido | Crítico | Token mínimo, paths, staging acotado, firma, auditoría |
| S3 comprometido | Alto | Cifrado cliente, Object Lock, credenciales limitadas |
| NAS comprometido | Alto | Cifrado cliente, snapshots, réplica |
| Clave maestra comprometida | Crítico | Rotación, re-reparto, investigación, reinstalación |
| Clave maestra perdida | Crítico | Shamir, custodia, simulacro; si se pierde todo, irrecuperable |

---

## 12. Backup de la propia plataforma

- `pg_dump` diario y WAL archiving si se requiere RPO bajo.
- Configuración y secretos cifrados.
- Copia de la clave maestra offline.
- Runbook de reconstrucción.
- Prueba periódica de restaurar el control plane desde cero.

---

## 13. Criterios de aceptación

- Un certificado no emitido por la CA no conecta.
- Un token usado no se reutiliza.
- Un agente revocado deja de operar en <5 min.
- Ningún secreto aparece en logs ni en la BD en claro.
- Un borrado dentro de Object Lock es denegado.
- La auditoría detecta una eliminación masiva.
- La restauración break-glass funciona sin el control plane.
- La clave maestra se recupera con 3 de 5 shares en un simulacro.

---

## 14. Preguntas abiertas

- ¿La CA raíz estará offline o en el mismo host?
- ¿Se usa TPM para la KEK o un archivo con permisos?
- ¿Se exige aprobación de dos personas para toda restauración a VM o solo para producción?
- ¿Se rota la clave maestra con qué frecuencia?
- ¿Se audita también el acceso al NAS/S3 a nivel de infraestructura?
