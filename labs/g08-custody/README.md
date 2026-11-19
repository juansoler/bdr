# G0.8 — Custodia de la clave y break-glass

**Objetivo:** demostrar que 3 de 5 shares reconstruyen la clave maestra y que se puede restaurar un snapshot en un host limpio sin el control plane.

**Riesgo:** maneja la clave maestra. Usa un repositorio de laboratorio y destruye las claves al terminar.

---

## 1. Preparación

En un host de laboratorio con Kopia:

```bash
export KOPIA_PASSWORD="$(openssl rand -base64 32)"
mkdir -p ~/g08/repo ~/g08/data
kopia repository create filesystem --path=~/g08/repo
dd if=/dev/urandom of=~/g08/data/blob bs=1M count=16 status=none
kopia snapshot create ~/g08/data --json
```

Anota el ID del snapshot y el SHA-256 del archivo.

---

## 2. Ceremonia de división

`ssss` es interactivo; la ceremonia debe ser presencial y con testigos. Instala el paquete `ssss` y ejecuta:

```bash
ssss-split -t 3 -n 5 -w bdr
```

Introduce la contraseña maestra cuando se solicite. Obtendrás cinco shares etiquetadas. Guárdalas en sobres separados y registra:

| Share | Custodio | Ubicación | Fecha | Hash del sobre |
|---|---|---|---|---|
| 1 | | | | |
| 2 | | | | |
| 3 | | | | |
| 4 | | | | |
| 5 | | | | |

**No** guardes las shares en el repositorio, en Git, en email ni en la nube.

> Alternativa automatizable para pruebas: `sss-cli` (Rust) lee el secreto por stdin. Para la ceremonia real se prefiere una herramienta interactiva y auditada.

---

## 3. Break-glass

1. Apaga el servidor de control y el Repository Server (o desconecta la red).
2. Arranca un host limpio con Kopia instalado.
3. Reconstruye la clave:

```bash
ssss-combine -t 3
```

Introduce tres shares en cualquier orden. La salida es la contraseña maestra.

4. Conecta al repositorio sin el servidor central:

```bash
export KOPIA_PASSWORD="<clave-reconstruida>"
kopia repository connect filesystem --path=/ruta/al/repo
kopia snapshot list
kopia snapshot restore <snapshot_id> /tmp/restore
sha256sum /tmp/restore/blob
```

5. Compara con el hash original.
6. Destruye la clave de memoria y los temporales:

```bash
unset KOPIA_PASSWORD
shred -u /tmp/clave.txt 2>/dev/null || true
```

---

## 4. Métricas

| Métrica | Valor |
|---|---|
| Tiempo hasta reconstruir la clave | |
| Tiempo hasta restaurar | |
| Integridad verificada | |
| Incidencias | |

---

## 5. Criterios de aceptación

- 3 shares reconstruyen la clave; 2 no.
- Un share corrupto o incorrecto se detecta.
- La restauración funciona sin el control plane.
- El procedimiento está documentado y firmado.
- La clave se rota después del ejercicio.

---

## 6. Evidencia

- Hash del archivo original y restaurado.
- Tiempos.
- Fotos o registro de los sobres (sin mostrar las shares).
- Procedimiento final en `docs/14-custodia-clave-maestra.md`.
