# Proxmox: backup y restauración de VMs

Backup y restauración de VMs de Proxmox (qemu) desde el panel, con
restauración en sitio o como **instancia nueva con la misma configuración**.

## Arquitectura

```
nodo PVE (agente bdr-agent) ──► qemu-nbd (volúmenes en caliente)
        │  segmentos 64 MiB + SHA-256
        ▼
bucket S3 del destino (segmentos + manifiesto con qm config)
        │  restore: descarga segmentos → escribe vía qemu-nbd
        ▼
misma VM (in_place) o VM nueva (qm create con la config del manifiesto)
```

El agente se instala **en el propio nodo Proxmox** (es un Linux normal) y
trabaja con las herramientas del nodo: `qm`, `pvesm`, `qemu-nbd`.

## 1. Instalación en el nodo

1. Panel → **Proxmox → Añadir instancia** (token de API, como ya tienes).
2. En el nodo PVE, como root, ejecuta el instalador Linux de «Añadir equipo»
   (descarga el binario y crea el servicio systemd) **o** manualmente:

```sh
curl -fsSL http://203.0.113.132:8080/api/v1/download/agent/linux-amd64 \
  -o /usr/local/bin/bdr-agent && chmod 0755 /usr/local/bin/bdr-agent
/usr/local/bin/bdr-agent register --server http://203.0.113.132:8080 \
  --token <TOKEN> --data-dir /var/lib/bdr-agent
```

3. Carga el módulo NBD (necesario para leer/escribir los volúmenes):

```sh
modprobe nbd max_part=8
echo nbd >> /etc/modules-load.d/bdr.conf
```

4. Panel → **Proxmox → Editar instancia** → selecciona el agente del nodo
   en «Agente del nodo PVE» → Guardar.
5. Kopia debe estar disponible para el agente si el plan lo usa: el nodo
   recibe las credenciales del repositorio igual que cualquier agente.
   Instala también `kopia` en el nodo si harás backups de archivos allí.

## 2. Backup de una VM

1. **Proxmox → Ver VMs** → elige el **destino de backups de VM** (bucket S3
   con Object Lock recomendado) en la tarjeta de la instancia.
2. Pulsa **Backup** en la VM. El job queda en *Jobs* con progreso por
   segmento (`disco 0: segmento N`).

Qué hace el agente:

- Lee la configuración de la VM (`qm config`) y guarda la cruda en el
  manifiesto: cores, memoria, BIOS, máquina, red, discos y sus tamaños.
- Para cada disco: `pvesm path` → conecta el volumen a `/dev/nbdN`
  **read-only** y captura en segmentos de 64 MiB con SHA-256, subiéndolos
  directamente al bucket (streaming, sin espacio en disco local; segmentos
  enteramente a cero no se suben; los ya existentes no se re-suben).
- La VM sigue encendida: la lectura NBD es snapshot-consistente respecto al
  volumen en ese instante (crash-consistent).
- Al terminar sube `manifest-<id>.json` con todo y registra el **punto de
  restauración** (botón «Puntos» en la VM).

## 3. Restauración

**En sitio** (sobrescribe los discos de la VM): detiene la VM, reconecta
cada volumen read-write vía NBD y reescribe los segmentos **verificando el
SHA-256** de cada uno; luego arranca la VM.

**Instancia nueva**: crea la VM con `qm create` usando la configuración del
manifiesto (cores, memoria, BIOS/machine, red, scsihw, discos del mismo
tamaño en el storage elegido —incluye efidisk y TPM—) y escribe los datos
en sus volúmenes. Al terminar queda lista para arrancar. VMID nuevo
automático (`qm nextid`) o el que indiques.

En el panel: **Proxmox → Puntos** (en la fila de la VM) → elige el punto →
modo e instancia destino → Restaurar.

## 4. Requisitos y límites

- El agente en el nodo debe ser **≥ 0.7.0** y correr como root (systemd).
- Módulo kernel `nbd` cargado (paso 3). Los volúmenes deben ser de tipo
  archivo/bloque gestionados por PVE (`local-lvm`, `local`, NFS…). ZFS:
  los volúmenes ZVOL también se exponen por NBD.
- Coherencia crash-consistent; para app-consistent, el `qm guest agent` con
  `fsfreeze` llegará como mejora (Fase 2b).
- No se capturan VMs LXC (contenedores) en este slice.
- La primera captura sube todo lo no-cero; las siguientes solo los
  segmentos cuyo SHA cambió (por VM y bucket).

## 5. Despliegue P2V ↔ PVE

El manifiesto de imagen de un equipo físico (`Kind: image`) y el de una VM
(`Kind: vm`) comparten formato: `image-fetch` + `image-write` permiten
desplegar un equipo físico dentro de un volumen de VM (P2V) y viceversa.
El hallazgo G0.5 se mantiene: efidisk y tpmstate se mueven como copias
completas junto a los discos de datos.
