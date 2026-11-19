# Recuperación bare-metal: desplegar una imagen en otro equipo

Guía operativa para el ciclo completo: capturar el equipo íntegro (particiones
de arranque incluidas) → custodiar la imagen → desplegarla en un equipo
(distinto) y que arranque con la misma información y configuración.

## 1. Captura (equipo origen)

1. Instala el agente 0.6.0+ en el equipo («Añadir equipo» en el panel).
2. **Planes → Crear plan → Tipo: Imagen completa**. Sin rutas: captura todos
   los discos fijos (tabla de particiones, ESP, sistema y datos). Opcionalmente
   indica **Discos a capturar** (índices separados por comas, p. ej. `0` o
   `0, 2`) para limitar la imagen a esos discos — un disco de datos separado
   puede quedar fuera (vacío = todos). Los discos excluidos no se restaurarán
   en el despliegue bare-metal.
3. Asigna el plan al equipo y lanza el backup (manual o programado).

Qué hace el agente:

- Enumera los discos fijos (`lsblk` en Linux, `Get-Disk/Get-Partition` en
  Windows) y construye el **manifiesto v1**: tabla de particiones, roles
  (ESP, MSR, recuperación), modo de arranque y hints de restauración.
- Lee cada disco **entero en bloques de 64 MiB** (coherencia ante caídas,
  tipo crash-consistent: sin desmontar ni suspender servicios) y **sube cada
  segmento directamente al bucket del destino** (minio-go): el equipo origen
  solo necesita el journal, no espacio para la imagen. Los objetos se nombran
  con el SHA-256 del segmento → reanudación incremental entre ejecuciones
  (los segmentos cuyo objeto ya existe no se vuelven a subir) y salto de
  segmentos enteramente a cero. Si el destino tiene Object Lock, los
  segmentos quedan inmutables (GOVERNANCE).
- El manifiesto v1 se sube como objeto (`manifest-<id>.json`) con la tabla
  de particiones, roles (ESP, recuperación), firmware y el SHA-256 de cada
  segmento. Su clave aparece en el resultado del job (`manifest_key`).

Nota Windows: la lectura de `\\.\PhysicalDriveN` con el sistema en marcha
produce imágenes crash-consistent (equivalente a arrancar tras un corte de
luz). Para coherencia de aplicación (VSS) se planifica como mejora del
motor; ESP y particiones de recuperación se capturan igualmente.

## 2. Custodiar la imagen

El snapshot vive en el repositorio Kopia (y se replica a los destinos S3 de
la sección Almacenamiento). Localiza el snapshot ID en el resultado del job
(`kopia_snapshot_id`) o con:

```sh
kopia snapshot list --all
```

## 3. Despliegue en otro equipo (equipo destino)

Requisitos: equipo destino arrancado con un **entorno live** (Linux live USB
sirve; para Windows destino, cualquier live con `bdr-agent` copiable), disco
destino de tamaño ≥ al original y credenciales del bucket del destino (las
mismas del target; `mc` de MinIO también vale).

```sh
# 3.1 Descargar la imagen (manifiesto + segmentos) al disco local/USB
bdr-agent image-fetch \
  --endpoint 203.0.113.5:9000 --bucket bdr-repo \
  --access-key ... --secret-key ... --no-tls \
  --manifest image/manifest-<id>.json --dir /mnt/staging

# 3.2 Identificar el disco destino (¡verifica dos veces!)
lsblk

# 3.3 Escribir la imagen completa (DESTRUCTIVO, requiere --yes)
bdr-agent image-write --segments-dir /mnt/staging --target /dev/sda --yes
```

`image-write` repone byte a byte el disco: tabla de particiones, ESP, sistema
y datos, **verificando el SHA-256 de cada segmento** contra el manifiesto.
Los segmentos marcados como cero en el manifiesto se escriben como ceros sin
descargar.

## 4. Primer arranque

- **UEFI + firmware similar**: el equipo arranca directamente — el ESP
  restaurado contiene el cargador original. En UEFI nuevos, si no aparece la
  entrada, regístrala desde el setup (o `efibootmgr` desde live).
- **Windows**: si el hardware cambia sustancialmente, puede pedir
  reparación de arranque automática en el primer boot (normal; no toca los
  datos). Si requieres migración a hardware muy distinto, considera
  `sysprep` antes de la captura.
- **Linux**: si el initramfs no tiene drivers del nuevo hardware, chroot +
  `update-initramfs -u` / `dracut -f`; en migraciones a otros controladores
  SATA/NVMe añade el driver al initramfs.
- **Proxmox (P2V)**: crea la VM con UEFI (OVMF) y TPM si el origen lo tenía;
  importa el disco restaurado como disco RAW y marca `efidisk`/`tpmstate`
  como copias completas (hallazgo G0.5: recrearlos hace fallar el arranque).

## Limitaciones actuales (0.6.0)

- Coherencia crash-consistent (no VSS/LVM snapshots todavía).
- Los planes de imagen requieren destino S3 propio (los segmentos se suben
  en streaming al bucket; el repo central queda para backups de archivos).
- La primera captura sube todo lo no-cero; las siguientes solo los segmentos
  cuyo SHA cambió (reanudación incremental por objeto).
- `image-write` exige disco destino de tamaño ≥ origen.
- Sin arrastre de BitLocker/LUKS: captura los bytes cifrados; para arrancar
  en el destino hace falta la clave original (fuera de alcance de 0.6).
