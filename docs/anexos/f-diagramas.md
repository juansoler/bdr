# Anexo F — Diagramas

**Documento:** anexos/f-diagramas  
**Versión:** 1.0  
**Formato:** Mermaid; se puede renderizar en GitHub o en la documentación.

---

## 1. Contexto

```mermaid
flowchart LR
    Admin[Administrador] --> Panel[Panel web]
    Panel --> CP[Control Plane]
    CP --> PVE[Proxmox VE]
    CP --> PBS[Proxmox Backup Server]
    CP --> RS[Kopia Repository Server]
    RS --> NAS[(NAS)]
    RS --> S3[(S3)]
    Agent[Agentes Windows/Linux] --> CP
    Agent --> RS
    PVEAgent[Agente PVE] --> CP
    PVEAgent --> PVE
    PVEAgent --> RS
    ISO[ISO de rescate] --> NAS
    ISO --> S3
```

---

## 2. Contenedores

```mermaid
flowchart TB
    subgraph Host[Host de control]
        Panel[bdr-panel Next.js]
        Proxy[Proxy TLS]
        CP[bdr-control]
        Worker[bdr-worker]
        DB[(PostgreSQL)]
        River[River]
        Panel --> Proxy --> CP
        CP --> DB
        Worker --> DB
        CP --> River
        Worker --> River
    end
    subgraph Repo[Host/VM dedicado]
        RS[Kopia Repository Server]
        NAS[(NAS)]
        RS --> NAS
    end
    CP --> RS
    RS --> S3[(S3)]
    Agent[Agentes] -->|mTLS :8443| CP
    Agent -->|HTTPS :51515| RS
    PVEAgent[Agente PVE] -->|mTLS| CP
    PVEAgent -->|API| PVE[Proxmox VE]
    PVEAgent -->|API| PBS[PBS]
```

---

## 3. Backup de archivos

```mermaid
sequenceDiagram
    participant U as Usuario
    participant P as Panel
    participant C as Control Plane
    participant A as Agente
    participant R as Repository Server
    participant N as NAS
    U->>P: Backup ahora
    P->>C: POST /admin/agents/{id}/backup
    C->>C: crea job
    A->>C: lease
    C-->>A: job BACKUP_FILE
    A->>R: kopia snapshot create
    R->>N: chunks
    A->>C: progress
    A->>C: complete(snapshot_id)
    C->>C: restore_point
    C-->>P: SSE done
```

---

## 4. Backup de imagen

```mermaid
sequenceDiagram
    participant A as Agente
    participant V as VSS/Snapshot
    participant L as Lector
    participant R as Repository Server
    participant C as Control Plane
    A->>V: crear snapshot
    A->>A: layout + preflight
    loop segmentos
        A->>L: leer rango
        L->>L: sha256
        L->>R: kopia --stdin-file
        A->>C: segments/{n}
    end
    A->>R: snapshot manifiesto
    A->>C: complete
    A->>V: eliminar snapshot
```

---

## 5. Restauración P2V

```mermaid
sequenceDiagram
    participant U as Usuario
    participant C as Control Plane
    participant PV as Agente PVE
    participant R as Repository Server
    participant PVE as Proxmox VE
    U->>C: POST /admin/restores/proxmox
    C->>C: preflight manifiesto
    C->>PV: job RESTORE_VM
    PV->>R: restore segmentos
    PV->>PV: staging sparse
    PV->>PVE: POST /nodes/{node}/qemu import-from
    PV->>PVE: start
    PV->>PVE: status/console
    PV->>C: steps + resultado
    C-->>U: SSE done
```

---

## 6. PBS

```mermaid
sequenceDiagram
    participant C as Control Plane
    participant PVE as Proxmox VE
    participant PBS as PBS
    C->>PVE: POST /nodes/{node}/vzdump
    PVE->>PBS: backup
    C->>PBS: GET snapshots
    C->>PBS: verify
    C->>PVE: restore archive
```

---

## 7. Réplica

```mermaid
flowchart LR
    NAS[(NAS repo)] -->|kopia repository sync-to| S3[(S3 repo)]
    W[Worker singleton] -->|advisory lock| NAS
    W -->|verifica| S3
```

---

## 8. Modelo de datos (resumen)

```mermaid
erDiagram
    USERS ||--o{ SESSIONS : has
    AGENTS ||--o{ JOBS : runs
    AGENTS ||--o{ RESTORE_POINTS : produces
    RESTORE_POINTS ||--o{ MANIFESTS : has
    RESTORE_POINTS ||--o{ SNAPSHOT_SEGMENTS : has
    PLANS ||--o{ PLAN_ASSIGNMENTS : assigned
    AGENTS ||--o{ PLAN_ASSIGNMENTS : receives
    REPOSITORIES ||--o{ STORAGE_TARGETS : has
    RESTORE_POINTS ||--o{ RESTORES : restored
    RESTORES ||--o{ RESTORE_STEPS : steps
    AGENTS ||--o{ ALERTS : triggers
```

---

## 9. Fases

```mermaid
flowchart LR
    F0[Fase 0 PoC] --> F1[Fase 1 MVP archivos]
    F1 --> F2[Fase 2 Imagen + P2V]
    F2 --> F3[Fase 3 Robustez 3-2-1]
    F3 --> F4[Fase 4 Avanzado]
```

---

## 10. Break-glass

```mermaid
flowchart TB
    ISO[ISO de rescate] --> Net[Red aislada]
    Net --> Repo[ NAS o S3]
    Shares[3 de 5 shares] --> Key[Clave maestra en memoria]
    Key --> Repo
    Repo --> Restore[Restaurar imagen]
    Restore --> Boot[Reparar arranque]
    Boot --> Verify[Verificar]
    Verify --> Rotate[Rotar clave]
```
