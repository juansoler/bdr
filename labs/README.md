# Laboratorio de Fase 0

**Versión:** 1.0  
**Objetivo:** ejecutar los nueve experimentos de `docs/anexos/b-poc-0-plan-evidencias.md`, registrar evidencia reproducible y cerrar la puerta de Fase 0 antes de escribir código de producto.

---

## 1. Reglas de seguridad

1. **Nunca en producción.** Usa máquinas virtuales, discos de prueba y un bucket S3 de laboratorio.
2. **Los scripts destructivos son dry-run por defecto.** Los que crean VMs, escriben discos o cambian módulos exigen `--apply` o `-confirm`.
3. **No ejecutes el probe VSS sin entender qué hace.** Crea una instantánea de volumen (operación reversible) y la elimina al terminar; requiere administrador.
4. **No guardes secretos en este directorio.** Usa variables de entorno o un gestor de secretos; `labs/evidence/` está ignorado por Git.
5. **Registra la versión exacta** de Kopia, agente, PVE, PBS y del sistema operativo en cada evidencia.
6. **Un experimento sin evidencia no cuenta.** La puerta se cierra con archivos, no con recuerdos.

---

## 2. Estructura

```
labs/
├── README.md
├── common/
│   ├── lib.sh                  utilidades comunes
│   ├── capture-env.sh          captura de entorno Linux
│   ├── capture-env.ps1         captura de entorno Windows (solo lectura)
│   ├── evidence-template.md    plantilla de evidencia
│   └── pve-create-vm.sh        creación de VM con import-from (dry-run)
├── g01-vss/                    VSS: shadow set, lectura raw, delta
├── g02-streaming/              --stdin-file: corte y reanudación
├── g03-acl/                    ACL del Repository Server
├── g04-p2v-windows/            P2V Windows
├── g05-p2v-debian/             P2V Debian
├── g06-object-lock/            sync-to + Object Lock
├── g07-ext4/                   ext4 sin LVM: blksnap/dattobd/crash
├── g08-custody/                Shamir y break-glass
├── g09-performance/            rendimiento y dimensionamiento
├── g10-sql-server/             SQL Server: descubrimiento, backups nativos, restauración (Fase 3)
├── gate-check.sh               comprobación de evidencias
└── evidence/                   resultados (ignorado por Git)
```

---

## 3. Requisitos

| Herramienta | Uso | Nota |
|---|---|---|
| Go | Compilar el probe VSS | **≥ 1.25** para `SmithOperatingSolutions/go-vss`; ≥ 1.21 para `mxk/go-vss` |
| Kopia | Repositorio, snapshots, verify, sync | Versión fijada; anotarla |
| `jq` | Parseo JSON | Linux/WSL |
| `curl` | API de Proxmox/PBS | Linux/WSL |
| `aws` o `mc` | S3/Object Lock | Según proveedor |
| `ssss` | Shamir 3-de-5 | Paquete Debian `ssss` |
| PowerShell 5.1+ | Windows | Administrador para VSS |
| SQL Server 2022 Developer | G0.10: motor nativo | VM de laboratorio; snapshot antes de restaurar `master` |
| Proxmox VE | P2V/PBS | Laboratorio |
| PBS | VMs | Laboratorio |
| NAS y S3 | Réplica | Laboratorio |

La máquina de desarrollo actual tiene Go 1.20, Node 26, Python 3.11 y Git; **no** tiene Kopia ni un daemon Docker activo. Los experimentos que dependen de ellos se ejecutan en el laboratorio, no aquí.

---

## 4. Flujo de trabajo

1. Crear el directorio de evidencia:

   ```bash
   export LAB_DATE=$(date +%Y-%m-%d)
   mkdir -p labs/evidence/$LAB_DATE
   cp labs/common/evidence-template.md labs/evidence/$LAB_DATE/G0x.md
   ```

2. Capturar el entorno antes de tocar nada:

   ```bash
   ./labs/common/capture-env.sh labs/evidence/$LAB_DATE
   ```

   En Windows, en una consola elevada:

   ```powershell
   powershell -NoProfile -ExecutionPolicy Bypass `
     -File labs/common/capture-env.ps1 `
     -OutDir labs/evidence/$LAB_DATE
   ```

3. Ejecutar el experimento siguiendo su README.
4. Adjuntar salidas, capturas y hashes en el mismo directorio.
5. Completar la plantilla y ejecutar `./labs/gate-check.sh`.
6. Actualizar `docs/decisiones.md` y `docs/poc-restauracion.md`.

---

## 5. Matriz de experimentos

| ID | Experimento | Directorio | Decide |
|---|---|---|---|
| G0.1 | VSS con ESP/recovery/C: y lectura raw | `g01-vss/` | go-vss vs mxk; límites |
| G0.2 | `--stdin-file`, corte y reanudación | `g02-streaming/` | segmentación y tamaño |
| G0.3 | ACL del Repository Server | `g03-acl/` | aislamiento o dominio de confianza |
| G0.4 | P2V Windows | `g04-p2v-windows/` | SATA+e1000, VirtIO, BitLocker |
| G0.5 | P2V Debian | `g05-p2v-debian/` | chroot/initramfs/red |
| G0.6 | `sync-to` + Object Lock | `g06-object-lock/` | modo y coste |
| G0.7 | ext4 sin LVM | `g07-ext4/` | blksnap/dattobd o crash-consistent |
| G0.8 | Custodia Shamir | `g08-custody/` | procedimiento break-glass |
| G0.9 | Rendimiento | `g09-performance/` | dimensionamiento |
| G0.10 | SQL Server (P-24–P-29) | `g10-sql-server/` | driver TDS, permisos, cadenas, `master` — Fase 3, fuera de la puerta de Fase 0 |

---

## 6. Qué NO se ejecuta en esta máquina

- Creación de instantáneas VSS: requiere decisión explícita; el probe exige `-confirm`.
- Escritura de discos, `import-from`, creación de VMs: requieren Proxmox de laboratorio y `--apply`.
- Instalación de módulos DKMS: puede afectar al kernel; solo en una VM de prueba.
- Operaciones sobre S3/Object Lock: solo con bucket de laboratorio.
- Pruebas de corte de red en equipos reales: usar VM o interfaz de laboratorio.

---

## 7. Puerta de Fase 0

La puerta se considera superada cuando:

- Los nueve directorios de Fase 0 contienen una evidencia completa.
- `gate-check.sh` no reporta faltantes.
- G0.10 (SQL Server) pertenece a la Fase 3: se ejecuta con su propio directorio de evidencia, pero no bloquea la puerta de Fase 0.
- `docs/decisiones.md` no tiene pendientes críticos sin dueño.
- `docs/poc-restauracion.md` está firmado.
- El roadmap se ha ajustado con lo aprendido.

Si un experimento demuestra que una decisión no es viable (por ejemplo, `--stdin-file` no reanuda y la segmentación no compensa), se documenta y se revisa el ADR correspondiente **antes** de pasar a Fase 1.
