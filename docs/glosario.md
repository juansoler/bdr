# Glosario

**Documento:** glosario  
**Versión:** 1.0

| Término | Definición |
|---|---|
| **Agente** | Servicio instalado en una máquina protegida que inventaría, captura y restaura datos. |
| **Agente PVE** | Servicio instalado en un nodo Proxmox que orquesta restauraciones y escribe discos. |
| **BDR** | Backup and Disaster Recovery: copia de seguridad y recuperación ante desastres. |
| **Break-glass** | Procedimiento de recuperación de emergencia sin el control plane, usando la clave custodiada. |
| **CBT** | Changed Block Tracking: seguimiento de bloques modificados para evitar releer el disco completo. |
| **Control plane** | Servidor central: API, catálogo, planificación, cola, alertas y orquestación. |
| **Deduplicación** | Almacenar una sola vez los chunks de contenido idénticos. |
| **DEK** | Data Encryption Key: clave que cifra un secreto concreto. |
| **Envelope encryption** | Cifrar datos con una DEK y la DEK con una KEK. |
| **GFS** | Grandfather-Father-Son: retención diaria, semanal, mensual y anual. |
| **Imagen completa** | Copia de tabla de particiones, metadatos y contenido necesario para reconstruir el disco y arrancar. |
| **KEK** | Key Encryption Key: clave que protege las DEK; vive fuera de la BD. |
| **Kopia** | Motor open source de backup incremental, deduplicado y cifrado; se usa como repositorio y para archivos. |
| **Manifiesto** | Documento versionado que describe discos, particiones, segmentos, hashes y pistas de restauración. |
| **mTLS** | TLS mutuo: cliente y servidor presentan certificados. |
| **Object Lock** | Bloqueo de objetos en S3 que impide su borrado durante un periodo. |
| **P2V** | Physical to Virtual: restaurar una máquina física como VM. |
| **PBS** | Proxmox Backup Server: motor de backup de VMs y contenedores de Proxmox. |
| **PVE** | Proxmox Virtual Environment. |
| **Repository Server** | Servidor de Kopia que expone el repositorio y custodia la contraseña maestra. |
| **Restore point** | Punto de recuperación: snapshot y metadatos asociados a una ejecución. |
| **RPO** | Recovery Point Objective: pérdida máxima de datos aceptable. |
| **RTO** | Recovery Time Objective: tiempo máximo de recuperación aceptable. |
| **Segmento** | Rango contiguo de un disco que se envía como snapshot independiente. |
| **Shamir** | Esquema de división de secretos: 3 de 5 shares reconstruyen la clave. |
| **Snapshot** | Vista puntual y consistente de un volumen o conjunto de datos. |
| **Sparse file** | Archivo disperso: ocupa espacio solo donde se escriben datos. |
| **Staging** | Espacio temporal donde se ensambla la imagen antes de importarla. |
| **TOTP** | Time-based One-Time Password: segundo factor. |
| **V2V** | Virtual to Virtual: restaurar una VM como otra VM. |
| **VSS** | Volume Shadow Copy Service de Windows. |
| **Zstd** | Algoritmo de compresión Zstandard. |

---

## Siglas de requisitos

| Sigla | Significado |
|---|---|
| RF | Requisito funcional |
| RNF | Requisito no funcional |
| ADR | Architecture Decision Record |
| PoC | Proof of Concept |
| MVP | Minimum Viable Product |
| GFS | Grandfather-Father-Son |
| PVE/PBS | Proxmox VE / Proxmox Backup Server |
