# G0.2 — Streaming con `--stdin-file`

**Objetivo:** determinar si `kopia snapshot create --stdin-file` permite checkpoint o reanudación, cuánto re-sube tras un corte y qué rendimiento y memoria consume.

**Decide:** tamaño de segmento, número de segmentos en paralelo y si la reanudación por segmento es suficiente.

---

## 1. Requisitos

- Linux o WSL con `kopia`, `jq`, `dd` y `timeout`.
- Repositorio temporal local (el script lo crea).
- Al menos 2 × `SIZE_MB` de espacio libre.

---

## 2. Ejecución

```bash
cd labs/g02-streaming
SIZE_MB=256 FORM=a ./run.sh
```

`FORM=a` usa la forma documentada:

```bash
cat data.bin | kopia snapshot create --stdin-file=disk0.seg0 --json
```

`FORM=b` prueba si el comando exige un argumento de origen (pendiente P-23):

```bash
kopia snapshot create data.bin --stdin-file=disk0.seg0 --json
```

Repite con 64, 256 y 1024 MiB para ver la relación entre tamaño y re-subida.

---

## 3. Qué hace el script

1. Crea un repositorio temporal con contraseña efímera.
2. Genera datos aleatorios.
3. Fase A: inicia el snapshot y lo mata con `SIGKILL` a los 10 s.
4. Fase B: repite el mismo snapshot sin límite de tiempo.
5. Mide snapshots, tamaño del repositorio y códigos de salida.
6. Escribe `g02-summary.md` y los JSON de cada fase en `labs/evidence/<fecha>/`.

**No modifica** ningún repositorio real ni ningún disco.

---

## 4. Medidas que debes añadir a mano

| Medida | Cómo |
|---|---|
| Bytes leídos del origen | `du` del origen o contador del lector |
| Bytes subidos | salida JSON de Kopia (`uploadedBytes` si existe) |
| Memoria máxima | `/usr/bin/time -v` envolviendo el comando |
| CPU | `pidstat` o `time` |
| Tiempo de A y B | `time` |
| Integridad | `kopia snapshot verify` |

Ejemplo con memoria:

```bash
/usr/bin/time -v bash -c 'cat data.bin | kopia snapshot create --stdin-file=disk0.seg0 --json' \
  2> time-a.txt > snap-a.json
```

---

## 5. Interpretación

| Resultado | Consecuencia |
|---|---|
| B vuelve a subir todo | La reanudación por segmento es obligatoria; el segmento debe ser pequeño |
| B solo sube lo que falta | Kopia checkpointa el flujo; aun así, segmentar ayuda al paralelismo |
| A deja un snapshot parcial corrupto | Hay que limpiar snapshots parciales por tag antes de reanudar |
| Kopia rechaza `--stdin-file` sin origen | Usar `FORM=b` y documentar el comando exacto |
| Memoria crece con el tamaño | Reducir segmento o paralelismo |

---

## 6. Criterios de aceptación

- Se conoce el comando exacto que funciona en la versión fijada.
- Se sabe si hay checkpoint nativo.
- Se ha medido la re-subida tras un corte.
- Se ha elegido un tamaño de segmento inicial con datos.
- El repositorio queda consistente tras el corte (`snapshot verify`).

---

## 7. Evidencia

Copia en `labs/evidence/<fecha>/G02.md`:

- `g02-phase-a.json` y `.exit`.
- `g02-phase-b.json` y `.exit`.
- `g02-snapshots.json`.
- `g02-repo-status.json`.
- `g02-summary.md`.
- Salida de `kopia snapshot verify`.
- Medidas de memoria y CPU.
