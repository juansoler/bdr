# Anexo E — Contrato de la CLI de Kopia

**Documento:** anexos/e-contrato-cli-kopia  
**Versión:** 1.0  
**Objetivo:** encapsular Kopia como caja negra, fijar versión y detectar cambios de contrato.

---

## 1. Principios

1. No se importan paquetes internos de Kopia.
2. El binario se distribuye con el producto y se fija por versión.
3. Un único módulo (`internal/kopia`) construye argumentos, ejecuta, parsea y traduce errores.
4. Solo se parsea `--json`; nunca la salida humana.
5. Los contract tests fallan si cambia el esquema o el código de salida.

---

## 2. Operaciones envueltas

| Operación | Comando | Uso |
|---|---|---|
| Conectar | `repository connect` | Agente/servidor |
| Estado | `repository status --json` | Health |
| Snapshot archivo | `snapshot create <ruta> --json` | Backup archivos |
| Snapshot stream | `snapshot create --stdin-file=<n> --json` | Imagen |
| Listar | `snapshot list --json` | Catálogo/reanudación |
| Verificar | `snapshot verify` | Nivel 1/2 |
| Restaurar | `snapshot restore` | Restore |
| Sync | `repository sync-to` | Réplica |
| Maintenance | `maintenance run/info/set` | Operación |
| Policy | `policy set/show` | Retención |
| Server | `server start`, `server user`, `server acl` | Repository Server |

---

## 3. Contratos de salida

### `snapshot create --json`

Campos mínimos esperados:

```json
{
  "id": "k...",
  "source": {"host": "...", "userName": "...", "path": "..."},
  "description": "...",
  "startTime": "...",
  "endTime": "...",
  "rootEntry": {"name": "...", "type": "f", "size": 123},
  "stats": {"totalSize": 123, "hashedFiles": 1, "uploadedBytes": 123}
}
```

Validar: `id`, `rootEntry`, `stats`, tiempos.

### `snapshot list --json`

Array de snapshots con `id`, `source`, `description`, `startTime`, `endTime`, `tags`.

### `repository status --json`

Debe incluir modo, storage, parámetros y estado. Validar que el repositorio abre.

### `snapshot verify`

No hay un JSON rico garantizado; validar código de salida y errores. Si se usa `--json`, confirmar en la versión fijada.

---

## 4. Códigos de salida y errores

| Situación | Código esperado | Error interno |
|---|---|---|
| Éxito | 0 | — |
| Autenticación | ≠0 | `E_KOPIA_AUTH` |
| Lock | ≠0 | `E_KOPIA_LOCK` |
| Cuota/espacio | ≠0 | `E_KOPIA_QUOTA` |
| Stream | ≠0 | `E_KOPIA_STREAM` |
| Corrupción | ≠0 | `E_KOPIA_CORRUPT` |
| Cancelación | ≠0 | `E_CANCELED` |

El wrapper captura stdout/stderr, aplica timeout y mata el proceso si excede. No reintentar errores de autenticación o cuota.

---

## 5. Variables de entorno

| Variable | Uso |
|---|---|
| `KOPIA_PASSWORD` | Contraseña del repositorio (solo servidor) |
| `KOPIA_CONFIG_PATH`/`--config-file` | Configuración aislada por agente |
| `KOPIA_SERVER_CONTROL_USER/PASSWORD` | Control del servidor |
| `KOPIA_SERVER_USERNAME/PASSWORD` | Autenticación de cliente/servidor según comando |
| `KOPIA_LOG_LEVEL` | Nivel de log |

**Cautela:** los nombres de variables de servidor no son consistentes entre comandos; confirmar en la versión fijada y no asumir.

---

## 6. Configuración por agente

- Un archivo de configuración por agente (`kopia.config`), permisos 0600.
- Usuario `agent-<uuid>@<host>`.
- Contraseña emitida por el control plane con TTL corto y rotación.
- No se usa la contraseña maestra en el agente.
- La huella TLS se fija en el primer contacto.

---

## 7. Contract tests

### 7.1 Estructura

```
tests/contract/kopia/
  snapshot_create_test.go
  snapshot_list_test.go
  repository_status_test.go
  verify_test.go
  sync_test.go
  server_acl_test.go
  fixtures/
```

### 7.2 Casos

1. Crear repositorio temporal.
2. Crear un archivo y snapshot.
3. Validar JSON de `snapshot create/list`.
4. Verificar.
5. Restaurar y comparar hash.
6. Ejecutar `sync-to` a otro repositorio temporal.
7. Validar códigos de error con credenciales incorrectas.

### 7.3 Regla

Si un campo esperado desaparece o cambia de tipo, el test falla y se bloquea la actualización de Kopia hasta revisar el wrapper.

---

## 8. Versionado

- `kopia_version` en el manifiesto.
- Matriz de versiones soportadas.
- No se actualiza en producción sin contract tests.
- Se mantiene una versión anterior del binario para rollback.

---

## 9. Riesgos

| Riesgo | Mitigación |
|---|---|
| Cambio de JSON | Contract tests |
| Cambio de flags | Tests de argumentos y smoke tests |
| Cambio de códigos de salida | Mapeo explícito y tests |
| Cambio de ACL/servidor | Pruebas de aislamiento |
| Cambio de `--stdin-file` | Prueba de imagen y reanudación |

---

## 10. Decisión

Kopia se trata como **dependencia externa versionada**, no como librería. El wrapper es el único punto de integración. Cualquier cambio de contrato se trata como un cambio de dependencia mayor.
