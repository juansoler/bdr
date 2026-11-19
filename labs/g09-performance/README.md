# G0.9 — Rendimiento y dimensionamiento

**Objetivo:** medir throughput, deduplicación, RAM del índice y coste de la segunda copia con el hardware real.

**Decide:** tamaño de segmento, paralelismo, host del repositorio y capacidad.

---

## 1. Ejecución

```bash
cd labs/g09-performance
SIZE_MB=1024 ./run.sh
```

El script:

1. Crea un repositorio temporal.
2. Genera `SIZE_MB` de datos aleatorios.
3. Mide un backup completo.
4. Mide un backup sin cambios.
5. Modifica ~5 % y mide otro backup.
6. Registra tiempos, tamaño del repositorio y snapshots.

---

## 2. Medidas adicionales

### Memoria y CPU

```bash
/usr/bin/time -v kopia snapshot create data.bin --json 2> time.txt > snap.json
pidstat -dru -p <pid> 1 > pidstat.txt
```

### Red

- 1 GbE y 10 GbE, si están disponibles.
- NAS local frente a NFS/SMB.
- Latencia y throughput sostenido, no solo pico.

### Índice

- RAM del proceso Kopia con 1, 5 y 10 TB únicos, si el laboratorio lo permite.
- Tiempo de `kopia repository status`.
- Tiempo de `maintenance`.

---

## 3. Tabla de resultados

| Escenario | Datos | Tiempo | Bytes leídos | Bytes subidos | RAM máx | Ratio dedup |
|---|---|---|---|---|---|---|
| Completo | | | | | | |
| Sin cambios | | | | | | |
| 5 % cambios | | | | | | |

---

## 4. Interpretación

- Si el backup sin cambios tarda casi lo mismo que el completo, el coste es la lectura del origen, no la subida.
- Si el repositorio crece en el backup sin cambios, algo falla en la deduplicación.
- Si la RAM crece de forma no lineal, hay que limitar el índice o el paralelismo.
- Si el NAS es el cuello de botella, el repositorio necesita host/red dedicados.

---

## 5. Criterios de aceptación

- El backup sin cambios no aumenta apreciablemente el repositorio.
- Se conoce el throughput por enlace.
- Se conoce la RAM necesaria por TB.
- Se ha elegido un tamaño de segmento y un paralelismo iniciales.
- El dimensionamiento está en `docs/15-almacenamiento-retencion.md`.
