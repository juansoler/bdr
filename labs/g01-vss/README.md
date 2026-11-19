# G0.1 — VSS: shadow set, lectura raw y delta

**Objetivo:** decidir entre `SmithOperatingSolutions/go-vss` (COM real) y `mxk/go-vss` (WMI), y medir si el shadow set incluye ESP, MSR, recovery y C: con lectura raw estable.

**Riesgo:** crear instantáneas de volumen es una operación de sistema. Es reversible y de solo lectura, pero requiere Administrador y consume shadow storage mientras exista. **Solo en laboratorio.**

---

## 1. Preparación

- Windows 11 o Server 2025, con UEFI y GPT.
- Consola **elevada**.
- Go ≥ 1.25 para el probe COM; ≥ 1.21 para el probe WMI.
- `vssadmin` disponible (viene con Windows).
- Registrar el espacio de shadow storage antes y después:

```powershell
vssadmin list shadowstorage
vssadmin list writers | Out-File -Encoding utf8 vss-writers-before.txt
```

- Identificar los volúmenes, incluidas las particiones sin letra:

```powershell
Get-Partition | Select-Object DiskNumber, PartitionNumber, DriveLetter, Offset, Size, Type, GptType
Get-Volume | Select-Object DriveLetter, FileSystemLabel, FileSystem, Size, UniqueId
mountvol
```

Las particiones ESP/MSR/recovery suelen no tener letra. Usa su ruta `\\?\Volume{GUID}\` con el probe.

---

## 2. Probe COM (preferido)

```powershell
cd labs\g01-vss\probe
go mod tidy
go build -o vssprobe.exe .
.\vssprobe.exe -list
```

Lectura del volumen del sistema:

```powershell
.\vssprobe.exe -confirm -volumes 'C:\' -bytes 1048576 |
  Tee-Object -FilePath ..\..\evidence\g01-com-c.json
```

Shadow set con varios volúmenes (sustituye los GUID por los reales):

```powershell
.\vssprobe.exe -confirm `
  -volumes '\\?\Volume{1111...}\,\\?\Volume{2222...}\,C:\' `
  -bytes 1048576 |
  Tee-Object -FilePath ..\..\evidence\g01-com-set.json
```

El probe imprime JSON por línea:

- `event=shadow_set_created`: número de snapshots, `verify_ok`, writers y degradados.
- Un objeto por volumen con `bytes_read`, `sha256`, `device_object` y `duration_ms`.
- `event=shadow_set_closed`: duración total.

**No uses `-confirm` en un equipo de producción.**

---

## 3. Probe WMI (comparativa)

```powershell
cd labs\g01-vss\probe-mxk
go mod tidy
go build -o vssprobe-mxk.exe .
.\vssprobe-mxk.exe -list
.\vssprobe-mxk.exe -confirm -volume 'C:\' -bytes 1048576 |
  Tee-Object -FilePath ..\..\evidence\g01-mxk.json
```

Diferencias esperadas:

| Aspecto | COM (Smith) | WMI (mxk) |
|---|---|---|
| Atomicidad multi-volumen | `CreateSet` hasta 64 | Una por volumen |
| Lectura raw | `OpenRaw` | Ruta del dispositivo |
| Writers | `WriterStatuses`, `Degraded` | No expone |
| USN/CBT | `USNChangesSince` | No |
| Madurez | Nueva (v1.0.0) | Estable (v1.2.x) |
| Go mínimo | 1.25 | 1.21 |

---

## 4. Medición del delta

Con carga de escritura ligera (por ejemplo, copiar un archivo grande o ejecutar una compilación):

1. Anotar `vssadmin list shadowstorage` antes.
2. Crear el shadow set y mantenerlo mientras se lee (el probe cierra al terminar; para una prueba larga, aumenta `-bytes`).
3. Anotar el espacio usado después.
4. Repetir con carga y sin carga.

Registra:

| Escenario | Delta usado | Tiempo de lectura | Errores |
|---|---|---|---|
| Sin carga | | | |
| Con escritura | | | |

---

## 5. BitLocker

```powershell
manage-bde -status
```

Si hay volúmenes cifrados:

- Documenta el estado (protección activa, porcentaje, protectores **sin** exportar la clave).
- Comprueba si la vista del volumen del shadow copy es legible (el probe lo demuestra si lee bytes y calcula SHA-256).
- No extraigas ni guardes claves de recuperación en la evidencia.

---

## 6. Criterios de aceptación

- El shadow set contiene todos los volúmenes solicitados (incluidos ESP/recovery cuando se pasan).
- `verify_ok` es verdadero o el motivo es comprensible.
- Se leen bytes de cada volumen y se obtiene un SHA-256 estable entre dos ejecuciones del mismo volumen.
- El delta medido es aceptable para la ventana de backup.
- La decisión COM vs WMI está justificada con datos (atomicidad, writers, madurez, versión de Go).

---

## 7. Evidencia

Copia en `labs/evidence/<fecha>/G01.md`:

- `vss-writers-before.txt` y `vss-writers-after.txt`.
- `vssadmin list shadowstorage` antes/después.
- Salida JSON de ambos probes.
- `manage-bde -status` (sin claves).
- Tu decisión y el ADR/documento afectado.

---

## 8. Qué puede salir mal

| Síntoma | Causa probable | Acción |
|---|---|---|
| `os.ErrPermission` | Consola no elevada | Ejecutar como Administrador |
| `ErrUnsupported` | 32 bits/WOW64 o no Windows | Usar binario amd64 nativo |
| El volumen no aparece | Ruta sin barra final o GUID incorrecto | Normalizar `C:\` o usar `\\?\Volume{...}\` |
| Delta alto | Carga de escritura alta | Programar en ventana de menor actividad |
| Fallo de `verify` | Writer en error | Revisar `vssadmin list writers` |
