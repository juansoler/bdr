# Esquemas

**Documento:** esquemas/README  
**Versión:** 1.0  
**Uso:** artefactos machine-readable que acompañan a la especificación.

---

## 1. Contenido

| Archivo | Qué define | Documento normativo |
|---|---|---|
| `manifest-v1.schema.json` | Manifiesto de imagen completa, versión 1 (JSON Schema Draft 2020-12) | `../10-motor-imagen-manifiestos.md` |

---

## 2. Reglas

- El esquema es **normativo** para `schema_version: 1`. Si el documento y el esquema discrepan, se corrige el documento y se versiona el esquema.
- Un cambio incompatible incrementa `schema_version` y crea `manifest-v2.schema.json`; no se modifica v1 en su sitio.
- Un cambio aditivo opcional puede hacerse en v1 solo si no rompe validadores existentes; aun así, se registra en `../23-registro-de-cambios.md`.
- El manifiesto es inmutable una vez cerrado. No se reescribe un manifiesto antiguo para “actualizarlo”.
- Los ejemplos del documento 10 son ilustrativos: contienen marcadores como `"hex..."` y `"..."` que **no** validan contra el esquema. Los manifiestos reales deben cumplir el patrón de SHA-256 y los formatos declarados.

---

## 3. Validación

Con Python y `jsonschema`:

```bash
python -I -m jsonschema \
  -i manifiesto.json \
  docs/esquemas/manifest-v1.schema.json
```

Con Node.js y `ajv-cli`:

```bash
npx ajv-cli validate \
  -s docs/esquemas/manifest-v1.schema.json \
  -d manifiesto.json \
  --spec=draft2020
```

En el producto, el agente valida el manifiesto **antes** de subirlo y el restaurador lo valida **antes** de escribir cualquier byte. Una validación fallida es `E_MANIFEST_SCHEMA` y no se reintenta automáticamente.

---

## 4. Garantías que expresa el esquema

- Todos los discos del origen están en `disks`, ordenados por `index`.
- Cada segmento referencia un disco válido y tiene un SHA-256 con formato correcto.
- Un manifiesto final solo contiene segmentos `complete`.
- `image_mode` y `consistency` pertenecen a conjuntos cerrados.
- `restore_hints` obliga a declarar firmware esperado y necesidad de VirtIO.
- `tooling` registra las versiones de agente y Kopia para reproducibilidad.
- No hay campos para secretos ni claves.

---

## 5. Lo que el esquema no puede garantizar

- Que los offsets no se solapen o no excedan `total_bytes` (lo valida el agente).
- Que `partitions` cubra exactamente el disco (lo valida el preflight).
- Que `boot_partition_index` exista en `disks[system_disk].partitions` (lo valida el restaurador).
- Que el `sha256` coincida con el contenido (se verifica al restaurar).

Estas comprobaciones semánticas son parte de la implementación y deben cubrirse con tests.
