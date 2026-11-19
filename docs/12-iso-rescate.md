# ISO de rescate

**Documento:** 12-iso-rescate  
**Versión:** 1.0  
**Requisitos:** RF-18, RF-23  
**Función:** restauración bare-metal, P2V asistido y break-glass sin control plane.

---

## 1. Objetivo

Proporcionar un entorno arrancable, reproducible y verificable que pueda:

1. Restaurar una imagen completa en un disco físico.
2. Restaurar una máquina como VM en Proxmox si el agente PVE no está disponible.
3. Recuperar datos **sin** PostgreSQL, River, panel ni Repository Server, usando la clave custodiada.

La ISO no sustituye al agente PVE; es el fallback universal y el mecanismo de último recurso.

---

## 2. Contenido

| Componente | Uso |
|---|---|
| Debian Live mínimo | Base |
| `bdr-agent-restore` | Cliente de restauración |
| Kopia CLI (versión fijada) | `repository connect`, `snapshot restore`, `verify` |
| `sgdisk`, `sfdisk`, `parted` | Tabla de particiones |
| `grub-install`, `update-grub`, `chroot` | Arranque Linux |
| `bcdboot`, `bootrec` (vía WinPE opcional) | Arranque Windows |
| Cliente S3 (`aws-cli` o `mc`) | Acceso al repositorio réplica |
| Cliente NFS/SMB | Acceso al NAS |
| `qemu-img` | Conversión/verificación |
| Utilidades de red | `ip`, `dhclient`, `wpa_supplicant` si aplica |
| Clave pública del servidor | Verificación TLS |
| Instrucciones y comprobaciones | `README` en la ISO |

No incluye secretos ni claves maestras. La clave se introduce en tiempo de ejecución.

---

## 3. Modos de arranque

### 3.1 Modo asistido

- La ISO obtiene la configuración del control plane (URL, restore point) por kernel cmdline o cloud-init.
- Se autentica con un `RESTORE_TOKEN` de un solo uso.
- Descarga el manifiesto y los segmentos.
- Restaura y notifica.

### 3.2 Modo break-glass

- Sin control plane.
- El operador introduce la URL del repositorio (NAS o S3), el ID del snapshot y la clave maestra.
- La clave se reconstruye con los shares Shamir (3 de 5).
- Se restaura y se repara el arranque.
- Se registra el incidente y se rota la clave.

### 3.3 Modo interactivo

- Shell con menú guiado para operaciones puntuales.
- Útil para diagnóstico y para restaurar archivos.

---

## 4. Inyección de configuración

- **Kernel cmdline:** `bdr.server=...`, `bdr.restore_token=...`, `bdr.snapshot=...`, `bdr.mode=assisted|breakglass`.
- **Cloud-init/NoCloud:** `user-data` y `meta-data` en un volumen etiquetado `CIDATA`.
- **Archivo local:** `/etc/bdr/restore.yaml` en un USB.
- La configuración nunca incluye la clave maestra en claro en el medio; se introduce por consola o se reconstruye con shares.

---

## 5. Flujo de restauración bare-metal

1. Arrancar la ISO en el equipo destino.
2. Detectar discos y confirmar el destino (nunca asumir `/dev/sda`).
3. Cargar configuración y autenticar.
4. Descargar el manifiesto.
5. Verificar que el disco destino tiene tamaño suficiente.
6. Recrear la tabla de particiones (`sgdisk`/`sfdisk`) desde el manifiesto.
7. Restaurar cada segmento en su offset.
8. Reparar el arranque:
   - Linux: `chroot`, `grub-install`, `update-initramfs`.
   - Windows: `bcdboot`, verificar EFI.
9. Verificar hashes y layout.
10. Reiniciar y comprobar el arranque.

---

## 6. Flujo de restauración a VM (fallback)

Si el agente PVE no está disponible:

1. Crear la VM manualmente en Proxmox con un disco vacío del tamaño adecuado.
2. Adjuntar la ISO y arrancar.
3. Dentro de la VM, restaurar la imagen en `/dev/sda` o `/dev/vda`.
4. Reparar el arranque.
5. Expulsar la ISO y reiniciar.
6. Verificar.

Este flujo es más lento que `import-from` y requiere intervención, pero no depende del agente PVE.

---

## 7. Red

- DHCP por defecto; configuración estática por cmdline o archivo.
- Acceso al NAS por NFS/SMB o al S3 por HTTPS.
- Verificación de huella TLS del repositorio.
- En modo de prueba, red aislada.
- Sin exposición a Internet.

---

## 8. Verificación

- Comprobar el hash de la ISO publicada.
- Verificar que el entorno arranca en BIOS y UEFI.
- Probar restauración de un disco pequeño y de uno grande.
- Probar break-glass con el servidor apagado.
- Medir tiempo de restauración y documentarlo.

---

## 9. Seguridad

- ISO firmada y hash publicado.
- Sin secretos embebidos.
- Clave maestra solo en memoria; nunca se escribe en disco.
- Limpiar memoria y buffers al terminar.
- Registrar el uso en un log local y, si es posible, en el servidor al recuperar conexión.
- Tras un break-glass, rotar la clave y re-repartir los shares.

---

## 10. Construcción reproducible

- `iso/` con un script de construcción (Debian live-build o similar).
- Versiones fijadas de Kopia y del agente.
- Hash y firma de la ISO.
- Prueba automática de arranque en VM (BIOS y UEFI) en CI.
- Publicación junto con el release del servidor.

---

## 11. Criterios de aceptación

- La ISO arranca en BIOS y UEFI.
- Restaura una imagen Linux y una Windows en un disco vacío.
- Repara el arranque en los casos documentados.
- Funciona sin el control plane en modo break-glass.
- No escribe secretos en disco.
- El procedimiento está en `17-operacion-runbooks.md`.

---

## 12. Preguntas abiertas

1. ¿Debian Live o Alpine para el tamaño y los drivers?
2. ¿Incluir WinPE para reparar BCD o usar herramientas Linux?
3. ¿Cómo se introduce la clave de forma segura sin dejarla en el historial?
4. ¿La ISO debe poder arrancar por red (PXE) además de USB?
5. ¿Cómo se verifica el arranque Windows desde la ISO?
