# Panel web: diseño de interfaz y experiencia

**Documento:** 22-panel-ui-ux  
**Versión:** 1.1  
**Requisitos:** RF-25, RF-26, RF-28, RF-30, RF-31, RF-32, RF-33, RF-39, RF-40–RF-46  
**Implementación prevista:** Next.js App Router + TypeScript + Tailwind + shadcn/ui

---

## 1. Principios

1. **Español claro.** La UI, los errores y las ayudas están en español; los términos técnicos se explican la primera vez.
2. **El estado importa más que el dato.** Cada pantalla responde a «¿está protegido?», «¿cuándo fue la última copia?» y «¿puedo recuperarlo?».
3. **Nada destructivo sin confirmación.** Restaurar a VM, borrar restore points y rotar claves exigen step-up; los casos irreversibles se marcan como tales.
4. **Los secretos se muestran una vez.** Tokens de enrolamiento y contraseñas no se vuelven a mostrar; la UI lo advierte antes de cerrar.
5. **Honestidad técnica.** Se distingue «deduplicación» de «CBT», «consistencia de aplicación» de «crash-consistent» y «backup correcto» de «recuperación verificada».
6. **Accesible por defecto.** Contraste AA, navegación por teclado, foco visible, estados no solo por color.

---

## 2. Arquitectura de información

```
Acceso (login + TOTP)
└── Consola
    ├── Resumen
    ├── Equipos
    │   ├── Lista
    │   ├── Ficha de equipo
    │   └── Grupos
    ├── Planes de protección
    ├── Puntos de recuperación
    │   ├── Lista
    │   ├── Explorador de archivos
    │   └── Wizard de restauración
    ├── Almacenamiento
    ├── Proxmox / PBS
    ├── Microsoft SQL Server (doc 24)
    ├── Trabajos
    ├── Restauraciones
    ├── Alertas
    ├── Informes
    └── Configuración
        ├── Usuarios y 2FA
        ├── Tokens de API
        ├── Tokens de enrolamiento
        ├── Secretos y rotación
        ├── Reglas de alerta
        └── Auditoría
```

**Barra superior:** nombre del entorno (por ejemplo, «Producción»), estado global del sistema (salud de API, base de datos, repositorio), buscador global, alertas pendientes, menú de usuario.

**Navegación lateral:** secciones anteriores, con contador de alertas y de jobs activos.

---

## 3. Convenciones visuales

| Elemento | Convención |
|---|---|
| Estado de equipo | Punto + texto: `En línea`, `Sin conexión`, `Copiando`, `Error`, `Revocado` |
| Estado de job | `Pendiente`, `En cola`, `Ejecutando`, `Correcto`, `Fallido`, `Cancelado`, `Expirado` |
| Severidad | `Crítica`, `Alta`, `Media`, `Baja` |
| Consistencia | Insignia `Aplicación` o `Crash-consistent`, con ayuda emergente |
| Verificación | `Sin verificar`, `Estructural OK`, `Contenido OK`, `Arranque OK`, `Fallida` |
| Cifrado | Candado con texto `Cifrado en cliente` |
| Destino | Etiqueta `NAS`, `S3`, `PBS` |
| Acciones destructivas | Botón con confirmación tipada (`Escriba RESTAURAR`) y step-up |
| Irreversible | Aviso ámbar/rojo explícito, sin acción por defecto |

Tokens de Tailwind/shadcn: se usan variables semánticas (`success`, `warning`, `destructive`, `muted`), no colores sueltos. Las tablas usan `DataTable` con columnas configurables y persistencia local.

---

## 4. Acceso

### 4.1 Login

- Email y contraseña.
- Error genérico: «Credenciales incorrectas» (no revela si el usuario existe).
- Rate limit visible: «Demasiados intentos. Vuelve a intentarlo en 60 s».
- Enlace a recuperación con códigos de un solo uso.

### 4.2 Segundo factor

- Código TOTP de 6 dígitos.
- Opción «Usar código de recuperación».
- Tras 5 fallos, se vuelve al login.
- No se muestra el secreto TOTP salvo en el alta, y solo una vez.

### 4.3 Step-up

Para operaciones sensibles se pide el código TOTP de nuevo en un diálogo modal que indica la acción exacta. Si el usuario no lo completa, la acción no se ejecuta.

---

## 5. Resumen

### 5.1 Bloques

| Bloque | Contenido | Definición |
|---|---|---|
| Equipos protegidos | `N de M` | Equipo con plan activo y restore point correcto dentro del RPO |
| Éxito de backups | `%` (24 h / 7 d) | Jobs `succeeded` / (succeeded + failed) |
| Capacidad lógica | TB | Suma de `size_logical_bytes` de los restore points vigentes |
| Capacidad almacenada | TB | `used_bytes` del repositorio |
| Ratio de deduplicación | `x` | lógico / almacenado; se muestra con la fecha de cálculo |
| Bytes leídos vs subidos | GB | Última ventana de 24 h |
| Próximos backups | Lista | Siguientes 5 `next_run_at` |
| Alertas | Lista | Abiertas por severidad |
| Últimas restauraciones | Lista | Resultado y duración |

### 5.2 Gráficas

- Backups por día (correctos/fallidos).
- Capacidad por repositorio y destino.
- Crecimiento de datos únicos y almacenados.

Cada gráfica incluye un enlace a los datos subyacentes y una nota si hay datos incompletos.

### 5.3 Estados

- **Sin datos:** «Aún no hay equipos. Instala el primer agente» con botón a la guía.
- **Parcial:** si el repositorio no responde, se muestra el resto y un aviso «Datos de almacenamiento no disponibles».
- **Error:** mensaje accionable y botón «Reintentar».

---

## 6. Equipos

### 6.1 Lista

Columnas:

| Columna | Contenido |
|---|---|
| Equipo | hostname, SO, etiqueta de grupo |
| Estado | punto y texto |
| Último backup | fecha relativa y resultado |
| Próximo backup | fecha |
| Plan | plan efectivo |
| Destinos | NAS/S3 |
| Consistencia | última imagen |
| Verificación | último nivel |
| Almacenamiento | tamaño lógico y almacenado |
| Acciones | Backup ahora, Restaurar, Ficha |

Filtros: estado, grupo, plan, SO, «sin backup en 7 días», «con error».

Acciones masivas: asignar plan, mover a grupo, lanzar backup, revocar (con confirmación).

### 6.2 Ficha de equipo

Secciones:

1. **Cabecera:** hostname, SO, estado, último contacto, botones `Backup ahora` y `Restaurar`.
2. **Protección:** plan efectivo, programación, retención, destinos, verificación.
3. **Inventario:** CPU, RAM, discos, particiones, firmware, Secure Boot, TPM, BitLocker/LUKS.
4. **Últimos trabajos:** tabla con resultado y enlace al detalle.
5. **Puntos de recuperación:** últimos 10, con acciones.
6. **Alertas:** del equipo.
7. **Acciones:** editar asignación, mover de grupo, revocar.

Avisos contextuales:

- «BitLocker activo: una restauración a VM puede pedir la clave de recuperación».
- «Disco sin espacio suficiente para staging».

### 6.3 Modal de instalación rápida

Objetivo: instalar el agente sin salir del panel.

Contenido:

1. Nombre y sistema operativo (Windows/Linux).
2. Grupo y plan inicial.
3. Botón «Generar token».
4. Token mostrado **una sola vez**, con caducidad visible (por ejemplo, «Válido 60 min, un uso»).
5. Pestañas de instalación:
   - **Windows:** paquete MSI/PowerShell firmado.
   - **Debian:** paquete `.deb`.
   - **Avanzado:** comando de una línea.
6. Verificación: huella del servidor y hash del instalador.

**Seguridad:** el comando de una línea contiene el token y se trata como secreto. No se registra en logs, no se envía por email y se invalida al usarse. La opción `curl | bash` se muestra como avanzada, con advertencia de que ejecuta código remoto y con la recomendación de usar el paquete firmado y verificar el hash.

Ejemplo de contenido (ilustrativo, no un contrato):

```bash
# Windows (PowerShell, instalador firmado)
bdr-agent-setup.exe --server https://bdr.example:8443 --token <TOKEN>

# Debian (paquete firmado)
sudo apt install ./bdr-agent_1.0.0_amd64.deb
sudo bdr-agent register --server https://bdr.example:8443 --token <TOKEN>
```

Estados:

- Token caducado → «Genera uno nuevo».
- Token usado → «Este token ya se ha utilizado».
- Instalación pendiente → el equipo aparece como `pending` y la UI refresca por SSE.

---

## 7. Grupos

- Lista con nombre, número de equipos, planes asignados y jerarquía.
- Crear/editar: nombre, descripción, grupo padre (sin ciclos), etiquetas.
- Miembros: buscador, alta y baja, importación por etiqueta.
- Planes heredados: se muestran con distinción «heredado» y un enlace al plan.
- Conflictos: si un equipo recibe el mismo plan por varias vías, insignia «Asignación duplicada» con explicación.
- Borrado: solo si está vacío o tras reasignar; nunca borra equipos.

---

## 8. Planes de protección

### 8.1 Lista

Nombre, tipo (`Archivos`, `Imagen`, `PBS`), programación, retención, destinos, equipos asignados, estado.

### 8.2 Editor por pasos

1. **Tipo y nombre.**
2. **Selección:** rutas/volúmenes, discos, exclusiones; para imagen, `image_mode` con explicación de BitLocker y espacio no asignado.
3. **Programación:** cron o asistente (diario/semanal/mensual), zona horaria, ventana.
4. **Retención:** diaria/semanal/mensual/anual, conservar última, fijar.
5. **Destinos:** NAS principal y S3 réplica; prueba de conexión.
6. **Cifrado y compresión:** Zstd, cifrado en cliente (siempre activo, sin opción de desactivarlo en el plan).
7. **Límites:** ancho de banda, CPU, paralelismo.
8. **Verificación:** nivel y frecuencia.
9. **Notificaciones:** email/webhook, umbral.
10. **Resumen y asignación:** equipos y grupos.

Validaciones en línea:

- Cron inválido.
- Ruta inexistente en el equipo (cuando se conoce).
- Retención que no conserva la última.
- Plan de imagen en un equipo sin snapshot soportado (aviso, no bloqueo si se acepta crash-consistent).
- Plan `critical` sin verificación nivel ≥2 (aviso).

---

## 9. Puntos de recuperación

### 9.1 Lista

Filtros por equipo, fecha, tipo, consistencia, verificación y destino. Cada fila muestra:

- Fecha y hora.
- Tipo (`Imagen`, `Archivos`, `PBS`).
- Consistencia.
- Tamaño lógico y almacenado.
- Verificación.
- Acciones: explorar, descargar, restaurar archivos, restaurar disco, restaurar en Proxmox, verificar.

### 9.2 Explorador de archivos

- Árbol de directorios con paneles de migas.
- Búsqueda por nombre o ruta.
- Metadatos: tamaño, fecha, permisos.
- Acciones: descargar, restaurar selección, copiar ruta.
- Vista previa solo para tipos seguros; nunca ejecuta contenido.
- Estado vacío: «Carpeta vacía».
- Error de permiso: «No tienes permiso para restaurar en este equipo».
- La descarga es por streaming y queda auditada.

### 9.3 Detalle de imagen

- Manifiesto renderizado: discos, particiones, firmware, TPM, BitLocker, modo de imagen, segmentos.
- Avisos: «Espacio no asignado no capturado» en `vss_volumes`/`lvm_volumes`.
- Botón «Restaurar en Proxmox».

---

## 10. Wizard de restauración a Proxmox

Cinco pasos, con barra de progreso y posibilidad de volver sin perder datos.

### Paso 1 — Punto de recuperación

- Equipo y snapshot seleccionados.
- Resumen del manifiesto y advertencias.

### Paso 2 — Destino

- Clúster, nodo y storage.
- Espacio disponible y estimado.
- Aviso si el storage no soporta `import`.

### Paso 3 — Recursos

- VMID (sugerido), nombre, CPU, RAM, bridge, firmware (`OVMF`/`SeaBIOS`), vTPM, Secure Boot, bus inicial (`SATA`/`VirtIO`), modelo de NIC.
- Valores sugeridos desde el inventario del origen.
- Validación de VMID libre.

### Paso 4 — Compatibilidad y avisos

- Informe de preflight:
  - Firmware del origen y destino.
  - TPM/Secure Boot (Windows 11).
  - BitLocker: «El disco restaurado puede pedir la clave de recuperación» / «Se restaurará sin BitLocker».
  - Drivers VirtIO: «Windows arrancará con SATA y e1000; después podrás cambiar a VirtIO».
  - Storage Spaces o volúmenes dinámicos: bloqueo con explicación.
  - Espacio en staging y destino.
- Modo prueba: nombre `bdr-test-*`, red aislada, destrucción automática, nunca producción.

### Paso 5 — Revisión y ejecución

- Resumen completo.
- Confirmación tipada (`RESTAURAR`) y step-up.
- Aprobación de dos personas si el destino es producción.
- Botón «Iniciar restauración».

### Progreso

- Pasos visibles: `Preflight`, `Preparando disco`, `Creando VM`, `Arrancando`, `Verificando`.
- Log en vivo por SSE, con opción de copiar.
- Si falla el arranque: estado `failed_boot`, evidencia (captura/consola), botones «Reintentar», «Ver diagnóstico» y «Conservar VM». Nunca «Destruir» por defecto.

---

## 11. Restauración de archivos

- Origen: snapshot y ruta.
- Destino: equipo y ruta; validación de espacio.
- Conflicto: sobrescribir, omitir o renombrar.
- Permisos: el agente valida que el destino está permitido por el plan.
- Progreso por archivos y bytes.
- Resultado con lista de restaurados y omitidos.

---

## 12. Almacenamiento

- Repositorios con estado, capacidad y último maintenance.
- Destinos NAS/S3 con prueba de conexión.
- Réplica: último `sync`, bytes, snapshots, atraso y errores.
- Object Lock: modo, días, avisos de coste.
- PBS: datastores, S3, caché local, verify/prune/gc.
- Acciones destructivas: maintenance, prune y borrado de restore points con confirmación y auditoría.

---

## 13. Proxmox / PBS

- Clústeres y nodos con estado y versión.
- Storages con tipo, contenido, espacio y soporte de `import`.
- VMs con estado y último backup PBS.
- PBS: snapshots, verificación, restore, sync.
- Errores de token o permisos con enlace a la guía de mínimo privilegio.

---

## 14. Trabajos

- Lista con filtros por estado, tipo, equipo y fecha.
- Detalle con línea de tiempo, progreso, bytes, logs y errores.
- Acciones: cancelar (cooperativa), reintentar (si es reintentable), copiar diagnóstico.
- SSE en vivo; si se pierde la conexión, se reconecta y se muestra «Reconectando».

---

## 15. Alertas

- Lista por severidad y estado (`Abierta`, `Reconocida`, `Resuelta`).
- Detalle con regla, objeto, primera/última vez y acciones.
- Reglas: condición, severidad, canales, silencio temporal.
- Agrupación por equipo para evitar ruido.

---

## 16. Informes

- Informe bajo demanda y programado.
- Contenido: cobertura, RPO cumplido, retenciones, capacidad, verificaciones, restauraciones de prueba.
- Exportación PDF/CSV.
- Envío por email.

---

## 17. Configuración y auditoría

- Usuarios, roles y 2FA.
- Tokens de API con scopes y caducidad.
- Tokens de enrolamiento con estado y revocación.
- Secretos: rotación y última verificación; nunca se muestra el valor.
- Auditoría: filtros por actor, acción, objeto y fecha; exportación.
- Ajustes del sistema: zona horaria, notificaciones, retención de metadatos.

---

## 18. Permisos por rol

| Acción | admin | operator | viewer |
|---|---|---|---|
| Ver resumen, equipos y jobs | Sí | Sí | Sí |
| Lanzar backup | Sí | Sí | No |
| Crear/editar planes y grupos | Sí | Sí | No |
| Restaurar archivos | Sí | Sí | No |
| Restaurar a VM | Sí | Sí (con step-up) | No |
| Aprobar restauración en producción | Sí | No | No |
| Revocar agentes | Sí | No | No |
| Gestionar usuarios y 2FA | Sí | No | No |
| Rotar claves y secretos | Sí | No | No |
| Ver auditoría | Sí | Sí (solo lectura) | No |
| Exportar auditoría | Sí | No | No |

Las acciones no permitidas se ocultan o se deshabilitan con explicación; nunca se confía solo en la UI: la API aplica el mismo control.

---

## 19. Mensajes de error

| Código | Mensaje de usuario | Acción sugerida |
|---|---|---|
| `E_VSS_CREATE` | «No se pudo crear la instantánea de Windows» | Revisar servicio VSS y espacio |
| `E_VSS_WRITER` | «Un servicio de aplicación (SQL/AD) no está listo para la copia» | Revisar writers y reintentar |
| `E_VSS_DELTA` | «La instantánea creció demasiado durante la copia» | Ampliar shadow storage o programar en ventana de menor carga |
| `E_BITLOCKER_LOCKED` | «El volumen está cifrado y bloqueado» | Desbloquear o custodiar la clave |
| `E_PVE_SPACE` | «No hay espacio suficiente en el destino» | Liberar espacio o elegir otro storage |
| `E_PVE_IMPORT` | «Proxmox no pudo importar el disco» | Revisar storage y content type `import` |
| `E_BOOT_TIMEOUT` | «La VM se creó pero no arrancó» | Ver diagnóstico; no se ha borrado nada |
| `E_DRIVER_MISSING` | «Faltan controladores de almacenamiento o red» | Instalar VirtIO y cambiar el bus |
| `E_KOPIA_AUTH` | «La credencial del repositorio no es válida» | Rotar credencial del agente |
| `E_KOPIA_CORRUPT` | «Se ha detectado corrupción en el repositorio» | No borrar; abrir incidente |
| `E_MANIFEST_SCHEMA` | «La copia usa un formato no soportado por esta versión» | Actualizar el restaurador |
| `E_PLAN_CONFLICT` | «Hay asignaciones contradictorias para este plan» | Revisar grupos y asignaciones |

Regla: el mensaje de usuario no incluye rutas internas, tokens ni stack traces; el detalle técnico queda en «Ver diagnóstico» con permisos.

---

## 20. Accesibilidad y responsive

- Contraste mínimo AA; foco visible; navegación completa por teclado.
- Formularios con etiquetas asociadas y errores junto al campo.
- Tablas con encabezados y resumen accesible.
- Estados no dependen solo del color: texto e icono.
- Idioma `es` declarado; formatos de fecha y número localizados.
- Diseño desktop-first; en móvil, tablas con scroll y wizard a pantalla completa.
- Diálogos con trampa de foco y cierre con Escape (salvo operaciones en curso).

---

## 21. Criterios de aceptación

- Un operador identifica en <30 s un equipo sin backup y lanza uno.
- El modal de instalación muestra el token una sola vez y lo invalida al usarse.
- El wizard bloquea layouts no soportados y muestra el motivo.
- Una restauración fallida deja evidencia y no destruye la VM.
- Las acciones destructivas exigen step-up y quedan auditadas.
- Los estados vacíos, de error y de carga están diseñados, no improvisados.
- La UI nunca muestra secretos guardados ni tokens usados.
- La navegación por teclado permite completar un backup y una restauración de archivos.

---

## 22. Preguntas abiertas

1. ¿Se usa un asistente de cron visual o solo expresión cron?
2. ¿Se permite restaurar a un equipo distinto del origen? (Sí para archivos; evaluar para VM.)
3. ¿La aprobación de dos personas es obligatoria solo en producción?
4. ¿Se muestra el coste estimado de S3?
5. ¿Se ofrece modo oscuro desde la Fase 1?
6. ¿Se internacionaliza la UI o se mantiene solo español?

---

## 23. Microsoft SQL Server (RF-40–RF-46)

El diseño detallado del módulo está en `24-motor-sql-server.md`. Aquí se recoge la pantalla y sus reglas:

- **Navegación:** sección «Protección de datos → Microsoft SQL Server», junto a Proxmox / PBS.
- **Tarjetas de estado:** instancias registradas, bases protegidas, último backup (correcto/fallo/atrasado), historial de restauraciones.
- **Tabla de instancias:** nombre, versión, edición, bases, modelo de recuperación, estado y último backup.
- **Plan de protección SQL:** por instancia o base de datos; valores por defecto Full dominical 01:00, Diff diaria 02:00, Log cada 15 min, verificación diaria, retención 30 días; configurables.
- **Wizard de restauración:** origen → base → punto de recuperación → destino → nombre y ubicación de archivos → preflight (cadena, versión, espacio) → step-up → ejecución con evidencia.
- **Pantalla de certificados:** certificados de cifrado de backups y de TDE custodiados; alerta visible si un backup cifrado no tiene certificado registrado.
- **Reglas heredadas:** toda restauración exige step-up y auditoría; los errores son accionables; los estados vacío/error/carga están diseñados.
