# Operación y runbooks

**Documento:** 17-operacion-runbooks  
**Versión:** 1.0  
**Uso:** procedimientos operativos para el propietario y, en el futuro, para un operador.

---

## 1. Convenciones

- `CP` = control plane (API + worker + panel).
- `RS` = Repository Server de Kopia.
- `PVE` = Proxmox VE.
- `PBS` = Proxmox Backup Server.
- `RP` = restore point.
- Toda operación destructiva exige confirmación y auditoría.

---

## 2. Instalación inicial

1. Preparar el host del CP con Docker Compose.
2. Crear el volumen del NAS y montarlo en el host del RS.
3. Levantar PostgreSQL, CP, worker, panel y proxy.
4. Inicializar la BD con migraciones.
5. Configurar TLS del panel y la CA de agentes.
6. Crear el repositorio Kopia y el usuario de mantenimiento.
7. Configurar el destino S3 (si existe) y probar la conexión.
8. Crear el usuario administrador y activar TOTP.
9. Ejecutar la ceremonia de clave maestra (`14-custodia-clave-maestra.md`).
10. Configurar alertas de email/webhook.
11. Verificar `/admin/system/health`.

---

## 3. Alta de agente

1. Panel → Agentes → Crear token (TTL 1 h, un uso).
2. Instalar el agente en el equipo.
3. Ejecutar `bdr-agent register --server https://<cp>:8443 --token <token>`.
4. Verificar que aparece `active` y con inventario.
5. Asignar plan o grupo.
6. Lanzar un backup de prueba.
7. Verificar restore de un archivo.

**Si falla:** comprobar DNS, firewall, hora, certificado y token (usado/caducado).

---

## 4. Revocación de agente

1. Panel → Agente → Revocar.
2. Se revoca el certificado y el usuario de Kopia.
3. Se fuerza `kopia server refresh` o SIGHUP.
4. Se verifica que el agente no puede conectar.
5. Se conservan los RP según retención; se decide si borrarlos.
6. Se registra en auditoría.

---

## 5. Backup manual

1. Panel → Equipo → Backup ahora.
2. Elegir plan si hay varios.
3. Confirmar.
4. Seguir el job por SSE.
5. Verificar estado `succeeded` y RP creado.
6. Si falla, revisar `error_code` y el runbook correspondiente.

---

## 6. Restauración de archivos

1. Panel → Restore points → elegir RP.
2. Navegar o introducir la ruta.
3. Elegir destino (original u otro).
4. Confirmar.
5. Verificar hashes si aplica.
6. Registrar.

---

## 7. Restauración P2V/V2V

1. Panel → Restore points → Restaurar en Proxmox.
2. Elegir clúster, nodo, storage.
3. Configurar VMID, nombre, CPU, RAM, bridge.
4. Revisar preflight: firmware, TPM, Secure Boot, BitLocker, espacio.
5. Confirmar (step-up y aprobación si producción).
6. Seguir pasos: staging → creating_vm → booting → verifying.
7. Si Windows: instalar VirtIO y cambiar bus.
8. Si Linux: verificar consola y red.
9. Adjuntar evidencia y cerrar.

**Si falla el arranque:** no borrar la VM; revisar consola, BCD/GRUB, drivers, firmware.

---

## 8. Restauración desde PBS

1. Panel → PBS → snapshots.
2. Elegir snapshot y nodo.
3. Restaurar a VM existente o nueva.
4. Confirmar.
5. Verificar arranque.
6. Registrar.

---

## 9. Réplica NAS → S3

1. Panel → Repositorio → Sincronizar.
2. Verificar que no hay backup en curso incompatible.
3. Ejecutar `sync-to` con lock.
4. Revisar `replication_run`.
5. Verificar snapshots en destino.
6. Si falla, revisar credenciales, espacio, red y Object Lock.

---

## 10. Maintenance del repositorio

1. Programar ventana.
2. Ejecutar `kopia maintenance run --full`.
3. Revisar `maintenance info`.
4. Verificar que el GC no borra datos en uso.
5. Revisar capacidad y alertas.
6. Registrar.

**No** ejecutar maintenance completo durante un backup crítico.

---

## 11. Rotación de secretos

1. Crear el nuevo secreto.
2. Actualizar el servicio/agente.
3. Verificar conexión.
4. Revocar el antiguo.
5. Rotar la KEK si aplica (recifrado por lotes).
6. Registrar.

---

## 12. Actualización de agentes

1. Publicar binario firmado con rollout 0 %.
2. Probar en un agente de laboratorio.
3. Subir rollout al 10 %, luego 50 %, luego 100 %.
4. Verificar versión y salud.
5. Si falla, rollback a la versión anterior.
6. Registrar.

---

## 13. Incidente de clave

### 13.1 Sospecha de compromiso

1. Aislar el CP y el RS.
2. Revocar credenciales y certificados.
3. Evaluar si un share pudo reconstruirse.
4. Rotar la clave maestra y re-repartir shares.
5. Revisar auditoría y accesos.
6. Verificar integridad del repositorio.

### 13.2 Pérdida de shares

1. Reunir los shares restantes.
2. Si quedan ≥3, reconstruir y re-repartir.
3. Si quedan <3, declarar irrecuperable y documentar.
4. Comunicar y registrar.

---

## 14. Pérdida del control plane

1. Confirmar que los backups en curso pueden continuar.
2. Restaurar PostgreSQL desde `pg_dump`/WAL.
3. Recrear el CP con Docker Compose.
4. Reimportar configuración y secretos.
5. Reconectar agentes (certificados).
6. Verificar catálogo contra el repositorio.
7. Si no se puede recuperar el CP, usar break-glass para los datos.
8. Registrar el incidente y el RTO.

---

## 15. Respuesta a ransomware

1. Aislar los equipos afectados.
2. No borrar ni “limpiar” el repositorio.
3. Verificar Object Lock y versiones.
4. Identificar el último RP limpio.
5. Restaurar en red aislada y verificar.
6. Rotar credenciales y claves.
7. Revisar auditoría y accesos.
8. Documentar y mejorar.

---

## 16. Break-glass

Seguir `14-custodia-clave-maestra.md` y `12-iso-rescate.md`. Registrar hora, participantes, shares usados, resultado y rotación posterior.

---

## 17. Comprobaciones periódicas

| Frecuencia | Comprobación |
|---|---|
| Diaria | Jobs fallidos, alertas, capacidad, réplica |
| Semanal | Restore de archivo aleatorio, verificación de contenido |
| Mensual | Restore de imagen, nivel 3, revisión de accesos |
| Trimestral | DR completo, rotación de secretos |
| Anual | Simulacro de clave, revisión de arquitectura |

---

## 18. Contactos y escalado

Definir:

- Propietario.
- Custodios de shares.
- Responsable de Proxmox/PBS.
- Proveedor S3/NAS.
- Procedimiento de comunicación en incidentes.

---

## 19. Registro

Cada runbook ejecutado debe dejar:

- Fecha y hora.
- Actor.
- Pasos y comandos.
- Resultado.
- Evidencia.
- Incidencias y mejoras.
