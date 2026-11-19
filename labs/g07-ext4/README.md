# G0.7 — ext4 sin LVM: crash-consistent, blksnap o dattobd

**Objetivo:** decidir cómo se hace la imagen de un Debian con ext4 sin LVM y validar que la restauración arranca.

**Riesgo:** instalar módulos DKMS puede afectar al kernel y a Secure Boot. Hazlo en una VM de laboratorio, nunca en el host.

---

## 1. Línea base: crash-consistent

1. Crea una VM Debian 12 con ext4 sin LVM y datos de prueba.
2. Con el sistema en marcha, captura el disco con la herramienta de laboratorio (por ejemplo, `dd` del dispositivo o una imagen de la VM).
3. Restaura la imagen en una VM nueva.
4. Arranca y comprueba:
   - journal replay;
   - `systemctl --failed`;
   - integridad de los datos de prueba (`sha256sum`).

**Resultado esperado:** arranca, pero no hay garantía de consistencia de aplicación. Es el modo `crash_direct` documentado.

---

## 2. blksnap (Veeam, GPL-2)

Requisitos: kernels 5.10–6.10, `gcc`, `linux-headers-$(uname -r)`, `dkms`, `dh-dkms` (Debian 12+), `cmake`, `uuid-dev`, `libboost-*`, `libssl-dev`.

```bash
sudo apt install debhelper dkms dh-dkms gcc linux-headers-$(uname -r) \
  g++ cmake uuid-dev libboost-all-dev libssl-dev

# Clona el repositorio en una VM de laboratorio y compila el paquete DKMS
# según el README de veeam/blksnap (pkg/deb/build-blksnap-dkms.sh).
```

Prueba:

1. Carga el módulo: `sudo modprobe blksnap` (o el nombre que indique el paquete).
2. Crea un snapshot con la CLI `blksnap`.
3. Lee el dispositivo del snapshot y calcula un hash.
4. Elimina el snapshot.
5. Reinicia y repite tras una actualización de kernel.

Registra:

| Métrica | Valor |
|---|---|
| Kernel | |
| Carga del módulo | |
| Firma/MOK si Secure Boot | |
| Snapshot creado | |
| Lectura correcta | |
| Rotura tras actualizar kernel | |

---

## 3. dattobd (Datto, GPL-2)

Soporta ext2/3/4 y XFS. El COW se guarda en un archivo `.datto` en el propio volumen.

Prueba:

```bash
sudo dbdctl setup-snapshot /dev/sda1 /.datto 0
# leer /dev/datto0
sudo dbdctl destroy /dev/datto0
```

Registra lo mismo que con blksnap, incluida la rotura por actualización de kernel y el impacto del archivo COW en el espacio libre.

---

## 4. Secure Boot

- Con Secure Boot activo, un módulo sin firmar no carga.
- Opciones: firmar con MOK (`mokutil --import`), usar una clave propia o desactivar Secure Boot en la VM de laboratorio.
- Documenta qué opción es viable para el producto y qué mensaje debe dar el agente si el módulo no está firmado.

---

## 5. Matriz de decisión

| Criterio | crash-consistent | blksnap | dattobd |
|---|---|---|---|
| Consistencia de aplicación | No | Sí | Sí |
| CBT | No | Sí | Sí |
| Dependencia de kernel | No | Alta | Alta |
| Secure Boot | No aplica | Requiere firma | Requiere firma |
| Complejidad operativa | Baja | Alta | Alta |
| Riesgo de rotura | Bajo | Alto | Alto |

**Decisión esperada:** `crash_direct` por defecto en Fase 2; blksnap/dattobd opt-in en Fase 3, solo si la matriz de kernels y la firma son sostenibles.

---

## 6. Criterios de aceptación

- La imagen crash-consistent arranca tras restaurar.
- Se conoce la tasa de éxito y las limitaciones.
- Se ha probado al menos un módulo CBT en una VM.
- Se ha documentado el comportamiento con Secure Boot.
- La decisión está en `docs/decisiones.md` (P-10) y afecta a `docs/08-agente-linux.md`.
