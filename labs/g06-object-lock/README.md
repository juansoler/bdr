# G0.6 — Réplica `sync-to` y Object Lock

**Objetivo:** comprobar la réplica NAS→S3, el comportamiento de `--delete` con Object Lock, el coste en versiones bloqueadas y la restauración desde la réplica.

**Riesgo:** crea objetos y consume almacenamiento. Usa un bucket de laboratorio, nunca producción.

---

## 1. Crear el bucket con Object Lock

### AWS CLI

```bash
BUCKET=bdr-lab-lock-$RANDOM
REGION=eu-west-1

aws s3api create-bucket \
  --bucket "$BUCKET" \
  --region "$REGION" \
  --create-bucket-configuration LocationConstraint="$REGION" \
  --object-lock-enabled-for-bucket

aws s3api put-object-lock-configuration \
  --bucket "$BUCKET" \
  --object-lock-configuration \
  'ObjectLockEnabled=Enabled,Rule={DefaultRetention={Mode=GOVERNANCE,Days=7}}'
```

### MinIO

```bash
mc mb --with-lock myminio/bdr-lab-lock
mc retention set --default GOVERNANCE 7d myminio/bdr-lab-lock
```

El bucket debe crearse con lock; no se puede habilitar después.

---

## 2. Crear el repositorio con retención

```bash
export KOPIA_PASSWORD="$(openssl rand -base64 24)"
kopia repository create s3 \
  --bucket="$BUCKET" \
  --region="$REGION" \
  --retention-mode=GOVERNANCE \
  --retention-period=7d
```

**P-05 / D-05:** confirma los nombres exactos de los flags en la versión fijada. Si `repository create` no los acepta, usa `repository set-parameters` y anótalo.

---

## 3. Snapshot y réplica

```bash
mkdir -p /tmp/g06/data
dd if=/dev/urandom of=/tmp/g06/data/blob bs=1M count=64 status=none
kopia snapshot create /tmp/g06/data --json --tag=bdr:lab=g06

kopia repository sync-to s3 \
  --bucket="$BUCKET" \
  --region="$REGION" \
  --delete \
  --parallel=8
```

Verifica el destino:

```bash
aws s3 ls "s3://$BUCKET" --recursive --summarize | tail -20
```

---

## 4. Probar el borrado

1. Elige un objeto del bucket.
2. Intenta borrarlo dentro de la retención:

```bash
aws s3api delete-object --bucket "$BUCKET" --key "<clave>"
```

3. Comprueba si se crea un delete marker y si la versión bloqueada sigue existiendo:

```bash
aws s3api list-object-versions --bucket "$BUCKET" --prefix "<prefijo>" | jq .
```

4. Repite con un principal que tenga permisos de `GOVERNANCE` (si aplica) y anota la diferencia frente a `COMPLIANCE`.

> **No uses COMPLIANCE en el laboratorio salvo que aceptes no poder borrar los objetos hasta que expire la retención.**

---

## 5. Medir el coste

Tras varios ciclos de backup + `sync-to --delete`:

| Métrica | Valor |
|---|---|
| Objetos actuales | |
| Versiones no actuales | |
| Bytes totales | |
| Bytes de versiones bloqueadas | |
| Objetos con delete marker | |

Usa `aws s3api list-object-versions` y `aws s3 ls --summarize`, o las métricas del proveedor.

---

## 6. Restaurar desde la réplica

1. En un host limpio, conecta un cliente al bucket:

```bash
kopia repository connect s3 --bucket="$BUCKET" --region="$REGION"
```

2. `kopia snapshot list`.
3. Restaura un archivo y verifica su hash.
4. Anota el tiempo y los errores.

---

## 7. Criterios de aceptación

- La réplica completa los snapshots.
- Un borrado dentro de la retención es denegado o deja la versión bloqueada.
- Se conoce el crecimiento de versiones y su coste.
- La restauración desde S3 funciona.
- El modo (GOVERNANCE/COMPLIANCE) y la retención están justificados.

---

## 8. Decisión que alimenta

- `docs/15-almacenamiento-retencion.md` (modo, retención, lifecycle).
- `docs/18-riesgos.md` (R-12, R-15).
- `docs/decisiones.md` (P-05, P-15, O-01, O-02).
