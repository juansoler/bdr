# Anexo C — Comandos de Kopia verificados

**Documento:** anexos/c-comandos-kopia-verificados  
**Versión:** 1.1  
**Cautela:** la CLI de Kopia no es una API estable. Fijar versión y ejecutar contract tests. Confirmar cada flag en la versión instalada antes de producción.

---

## 1. Repositorio

### Crear repositorio filesystem

```bash
kopia repository create filesystem \
  --path=/mnt/nas/bdr-repo \
  --password=<clave>
```

### Crear repositorio S3

```bash
kopia repository create s3 \
  --bucket=<bucket> \
  --endpoint=<endpoint> \
  --region=<region> \
  --access-key=<...> \
  --secret-access-key=<...>
```

### Conectar

```bash
kopia repository connect filesystem --path=/mnt/nas/bdr-repo
kopia repository connect s3 --bucket=<bucket> --endpoint=<endpoint>
```

### Estado y mantenimiento

```bash
kopia repository status
kopia repository status --json
kopia maintenance info
kopia maintenance run --full
kopia maintenance set --extend-object-locks=true
```

**Verificado:** `repository create/connect/status`, `maintenance`. Confirmar `--json` y flags exactos en la versión fijada.

---

## 2. Repository Server

### Crear usuario

```bash
kopia server user add agent-<uuid>@<host>
kopia server user list
kopia server user set agent-<uuid>@<host>
kopia server user delete agent-<uuid>@<host>
```

### Arrancar

```bash
KOPIA_PASSWORD="<clave>" \
KOPIA_SERVER_CONTROL_PASSWORD="<control>" \
kopia server start \
  --tls-generate-cert \
  --tls-cert-file=/etc/bdr/kopia.crt \
  --tls-key-file=/etc/bdr/kopia.key \
  --address=127.0.0.1:51515 \
  --server-control-username=control
```

### ACL

```bash
kopia server acl enable
kopia server acl add \
  --user=agent-<uuid>@<host> \
  --access=APPEND \
  --target=type=snapshot,hostname=<host>
```

Niveles: `READ`, `APPEND`, `FULL`.

### Refrescar usuarios

```bash
kopia server refresh \
  --address=https://127.0.0.1:51515 \
  --server-username=control \
  --server-password=<control>
```

**Verificado:** `server start`, `server user add`, ACL y refresh. Los cambios de usuario pueden tardar 5–10 min; forzar refresh o SIGHUP.

**Límite:** las ACL protegen manifiestos y políticas, no contenidos.

**Cautela:** la sintaxis de etiquetas de ACL (`hostname`, `path`, `username`, `policyType`) y su combinación exacta deben validarse en la versión fijada (P-22). El ejemplo anterior es ilustrativo: si la etiqueta no existe, la ACL puede quedar más amplia o más estrecha de lo previsto. Una ACL mal escrita no debe considerarse un control de seguridad hasta probarla con dos usuarios reales.

---

## 3. Snapshot de archivos

```bash
kopia snapshot create /ruta \
  --config-file=/var/lib/bdr/kopia.config \
  --json \
  --description="plan=<id> run=<run_id>" \
  --tag=bdr:plan=<id> --tag=bdr:kind=file \
  --parallel=4 \
  --checkpoint-interval=10m
```

Flags relevantes:

| Flag | Uso |
|---|---|
| `--json` | Salida parseable |
| `--description` | Trazabilidad |
| `--tag` | Filtros y reanudación |
| `--parallel` | Archivos en paralelo |
| `--checkpoint-interval` | Checkpoint de archivos |
| `--upload-limit-mb` | Límite de subida |
| `--stdin-file` | Snapshot desde stdin |

**Verificado:** `--stdin-file` existe y marca el snapshot como manual. `--stdin` no existe.

---

## 4. Snapshot de imagen por segmento

```bash
kopia snapshot create \
  --config-file=/var/lib/bdr/kopia.config \
  --json \
  --description="rp=<run_id> disk=<n> seg=<k>" \
  --tag=bdr:kind=image --tag=bdr:run=<run_id> \
  --tag=bdr:disk=<n> --tag=bdr:seg=<k> \
  --stdin-file=disk<n>.seg<k>
```

El flujo se pasa por stdin. El nombre lógico es el valor de `--stdin-file`.

**Cautela:** no se ha verificado checkpoint/reanudación para stdin; la segmentación es la estrategia de reanudación. Tampoco está confirmado si el comando exige un argumento de origen además de `--stdin-file` (P-23). Probar ambas formas en G0.2 y documentar la que funcione en la versión fijada.

---

## 5. Listar y verificar

```bash
kopia snapshot list --json
kopia snapshot list --json --tags=bdr:run=<run_id>
kopia snapshot verify --verify-files-percent=1 --file-parallelism=4
kopia snapshot verify --verify-files-percent=100 --file-parallelism=10
```

**Verificado:** `snapshot verify` valida estructura; `--verify-files-percent` descarga/descifra/descomprime. No detecta bit rot con la verificación estructural.

---

## 6. Restaurar

```bash
kopia snapshot restore <snapshot_id> /destino
kopia restore <snapshot_id> /destino
```

Para archivos selectivos, usar la ruta dentro del snapshot o montar:

```bash
kopia mount <snapshot_id> /mnt/restore
```

**Verificado:** restauración y montaje. La sintaxis exacta de restauración selectiva debe confirmarse en la versión fijada.

---

## 7. Réplica

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

Variantes: `filesystem --path=`, `from-config --token-file=`, `rclone`, `azure`, `gcs`.

**Verificado:** comando y subcomandos en docs/comunidad. Confirmar flags y comportamiento con la versión fijada.

**Cautela:** concurrencia con escritura y NFS; Object Lock + `--delete`.

---

## 8. Object Lock

```bash
kopia repository create s3 \
  --bucket=<bucket> \
  --retention-mode=GOVERNANCE \
  --retention-period=30d
```

Requisitos:

- Bucket creado con Object Lock y versioning.
- `GOVERNANCE` o `COMPLIANCE`; ambos requieren periodo.
- `maintenance set --extend-object-locks=true`.
- Lifecycle para versiones no actuales.

**Verificado:** flags y semántica en foro/docs. Confirmar en la versión fijada.

---

## 9. Políticas de retención

```bash
kopia policy set /ruta --keep-daily=7 --keep-weekly=4 --keep-monthly=6 --keep-annual=1
kopia policy show /ruta
```

**Verificado:** flags de retención. La política global puede aplicarse a todos los agentes.

---

## 10. Errores y diagnóstico

```bash
kopia repository status -t -s
kopia logs ...
kopia snapshot verify --max-errors=10
```

- `-t -s` genera un token de reconexión con la contraseña; **no usar en logs**.
- Parsear solo `--json`; no la barra de progreso humana.
- Códigos de salida → errores tipados.

---

## 11. Contract tests mínimos

| Comando | Qué validar |
|---|---|
| `snapshot create --json` | Campos: id, rootEntry, stats |
| `snapshot list --json` | IDs, tags, timestamps |
| `repository status --json` | Modo, storage, parámetros |
| `snapshot verify` | Código de salida y errores |
| `sync-to` | Código de salida y progreso |
| `server user/ACL` | Comportamiento y tiempos |

Si cambia el esquema, el test falla y se revisa la versión.

---

## 12. Versión fijada

- Registrar la versión exacta en `tooling.kopia_version` del manifiesto.
- Distribuir el binario con el producto y su NOTICE.
- No actualizar sin pasar contract tests.
- Mantener una matriz de compatibilidad con las versiones de PVE/PBS/SO.
