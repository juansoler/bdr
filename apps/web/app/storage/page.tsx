"use client";

import { useCallback, useEffect, useState } from "react";
import { Button, ErrorBox, Field, Input, cx } from "@/components/ui";
import { ApiError, api, type StorageTargetView } from "@/lib/api";
import { useAuth } from "@/lib/auth";

type TargetKind = "s3" | "filesystem";

export default function StoragePage() {
  const { token } = useAuth();
  const [targets, setTargets] = useState<StorageTargetView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [showAdd, setShowAdd] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);

  const [kind, setKind] = useState<TargetKind>("s3");
  const [name, setName] = useState("");
  const [path, setPath] = useState("");
  const [smbUser, setSmbUser] = useState("");
  const [smbPassword, setSmbPassword] = useState("");
  const [smbDomain, setSmbDomain] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [bucket, setBucket] = useState("");
  const [prefix, setPrefix] = useState("");
  const [region, setRegion] = useState("");
  const [accessKey, setAccessKey] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [noTLS, setNoTLS] = useState(true);
  const [objectLock, setObjectLock] = useState(true);
  const [retentionDays, setRetentionDays] = useState(7);
  const [syncInterval, setSyncInterval] = useState(60);

  const isFS = kind === "filesystem";
  const isSMBPath = (p: string) => p.trim().startsWith("//") || p.trim().toLowerCase().startsWith("smb://");
  const isSMB = isFS && isSMBPath(path);

  const load = useCallback(async () => {
    if (!token) return;
    setLoading(true);
    setError(null);
    try {
      const out = await api<{ items: StorageTargetView[] }>(
        "/api/v1/admin/storage/targets", token);
      setTargets(out.items);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se pudo cargar.");
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => { load(); }, [load]);

  async function create(e: React.FormEvent) {
    e.preventDefault();
    setBusy("create");
    setError(null);
    if (isFS && !path.startsWith("/")) {
      if (!isSMB) {
        setError("La ruta debe ser absoluta (p. ej. /mnt/backups) o una compartida SMB (//servidor/recurso).");
        setBusy(null);
        return;
      }
      if (!/^\/\/[^/]+\/.+/.test(path.trim().replace(/^smb:\/\//, ""))) {
        setError("La ruta SMB debe incluir el recurso compartido (p. ej. //servidor/recurso).");
        setBusy(null);
        return;
      }
    }
    const body = JSON.stringify(
      isFS
        ? {
            name, kind, path, prefix: prefix || undefined,
            smb_user: isSMB ? smbUser || undefined : undefined,
            smb_domain: isSMB && smbDomain ? smbDomain : undefined,
            smb_password: isSMB && smbPassword ? smbPassword : undefined,
            sync_interval_minutes: syncInterval, enabled: true,
          }
        : {
            name, kind, endpoint, bucket, prefix: prefix || undefined,
            region: region || undefined,
            access_key: accessKey, secret_key: secretKey, no_tls: noTLS,
            object_lock: objectLock, retention_days: retentionDays,
            sync_interval_minutes: syncInterval, enabled: true,
          },
    );
    try {
      await api(
        editingId
          ? `/api/v1/admin/storage/targets/${editingId}`
          : "/api/v1/admin/storage/targets",
        token,
        { method: editingId ? "PUT" : "POST", body },
      );
      setShowAdd(false);
      setEditingId(null);
      reset();
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se pudo guardar el destino.");
    } finally {
      setBusy(null);
    }
  }

  function startEdit(t: StorageTargetView) {
    setEditingId(t.id);
    setShowAdd(true);
    setError(null);
    setKind(t.kind ?? "s3");
    setName(t.name);
    setPath(t.path ?? "");
    setSmbUser(t.smb_user ?? "");
    setSmbDomain(t.smb_domain ?? "");
    setSmbPassword(""); // vacío = conserva la contraseña SMB cifrada actual
    setEndpoint(t.endpoint ?? "");
    setBucket(t.bucket ?? "");
    setPrefix(t.prefix ?? "");
    setRegion(t.region ?? "");
    setAccessKey(t.access_key ?? "");
    setSecretKey(""); // vacío = conserva el secreto cifrado actual
    setNoTLS(t.no_tls);
    setObjectLock(t.object_lock);
    setRetentionDays(t.retention_days);
    setSyncInterval(t.sync_interval_minutes);
  }

  async function verify(id: string) {
    setBusy(id);
    setError(null);
    try {
      await api(`/api/v1/admin/storage/targets/${id}/verify`, token, { method: "POST" });
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Verificación fallida.");
    } finally {
      setBusy(null);
    }
  }

  async function sync(id: string) {
    setBusy(id);
    setError(null);
    try {
      await api(`/api/v1/admin/storage/targets/${id}/sync`, token, { method: "POST" });
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Réplica fallida.");
    } finally {
      setBusy(null);
    }
  }

  async function remove(id: string) {
    setBusy(id);
    try {
      await api(`/api/v1/admin/storage/targets/${id}`, token, { method: "DELETE" });
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se pudo eliminar.");
    } finally {
      setBusy(null);
    }
  }

  function reset() {
    setKind("s3");
    setName(""); setPath(""); setSmbUser(""); setSmbPassword(""); setSmbDomain("");
    setEndpoint(""); setBucket(""); setPrefix(""); setAccessKey(""); setSecretKey("");
  }

  return (
    <div className="mx-auto max-w-6xl px-6 py-8">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="font-mono text-[10px] uppercase tracking-widest2 text-signal/80">
            Réplica 3-2-1
          </p>
          <h1 className="mt-1 font-display text-4xl uppercase tracking-wide text-slate-100">
            Almacenamiento
          </h1>
          <p className="mt-2 max-w-2xl text-sm text-slate-400">
            Destinos para la réplica y los backups: buckets S3-compatible con
            Object Lock opcional o rutas físicas (disco local, NAS montado).
            La ruta debe ser accesible desde el servidor de control y desde los
            agentes que la usen.
          </p>
        </div>
        <Button onClick={() => { setShowAdd((v) => !v); setEditingId(null); reset(); }}>
          {showAdd ? "Cerrar" : "Añadir destino"}
        </Button>
      </div>

      {showAdd ? (
        <form onSubmit={create} className="panel mt-6 space-y-4 p-5">
          <p className="font-mono text-[10px] uppercase tracking-widest2 text-signal/80">
            {editingId ? "Editar destino" : "Nuevo destino"}
          </p>
          <Field label="Tipo de destino">
            <div className="flex gap-2">
              {([
                { id: "s3", label: "S3-compatible", desc: "MinIO, AWS, Wasabi…" },
                { id: "filesystem", label: "Ruta física", desc: "Disco local o NAS montado" },
              ] as const).map((opt) => (
                <button
                  key={opt.id}
                  type="button"
                  onClick={() => setKind(opt.id)}
                  className={cx(
                    "border px-3 py-2 text-left text-sm transition",
                    kind === opt.id
                      ? "border-signal/60 bg-signal/10 text-slate-100"
                      : "border-ink-600 text-slate-400 hover:border-ink-500",
                  )}
                >
                  <span className="block font-mono text-[10px] uppercase tracking-widest2">{opt.label}</span>
                  <span className="text-xs text-slate-500">{opt.desc}</span>
                </button>
              ))}
            </div>
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Nombre" hint="Etiqueta en el panel">
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="minio-lab" required />
            </Field>
            {isFS ? (
              <Field label={isSMB ? "Ruta SMB" : "Ruta física"} hint={isSMB ? "//servidor/recurso[/subdir] — cliente SMB nativo, sin montar" : "Absoluta, p. ej. /mnt/backups (debe existir)"}>
                <Input value={path} onChange={(e) => setPath(e.target.value)} placeholder={isSMB ? "//nas-casa/backups" : "/mnt/backups"} required />
              </Field>
            ) : (
              <>
                <Field label="Endpoint" hint="host:puerto (p. ej. 203.0.113.5:9000)">
                  <Input value={endpoint} onChange={(e) => setEndpoint(e.target.value)} placeholder="203.0.113.5:9000" required />
                </Field>
                <Field label="Bucket" hint="Debe existir en el proveedor">
                  <Input value={bucket} onChange={(e) => setBucket(e.target.value)} placeholder="bdr-backups" required />
                </Field>
                <Field label="Access Key">
                  <Input value={accessKey} onChange={(e) => setAccessKey(e.target.value)} required />
                </Field>
                <Field label="Secret Key" hint={editingId ? "Vacío = conserva el secreto actual" : "Se guarda cifrado (AES-256-GCM)"}>
                  <Input type="password" value={secretKey} onChange={(e) => setSecretKey(e.target.value)} required={!editingId} />
                </Field>
              </>
            )}
            {isSMB ? (
              <>
                <Field label="Usuario SMB" hint="Usuario del NAS o de AD (p. ej. backup@DOMINIO → usuario aquí, dominio abajo)">
                  <Input value={smbUser} onChange={(e) => setSmbUser(e.target.value)} placeholder="bdr-backup" />
                </Field>
                <Field label="Contraseña SMB" hint={editingId ? "Vacío = conserva la actual" : "Se guarda cifrada (AES-256-GCM)"}>
                  <Input type="password" value={smbPassword} onChange={(e) => setSmbPassword(e.target.value)} required={!editingId} />
                </Field>
                <Field label="Dominio (opcional)" hint="Dominio AD o workgroup del NAS (p. ej. EMPRESA.LOCAL)">
                  <Input value={smbDomain} onChange={(e) => setSmbDomain(e.target.value)} placeholder="EMPRESA.LOCAL" />
                </Field>
              </>
            ) : null}
            <Field label="Prefijo (opcional)" hint={isSMB ? "Subdirectorio dentro del recurso" : isFS ? "Subdirectorio dentro de la ruta" : "Para separar usos del mismo bucket (p. ej. imagen)"}>
              <Input value={prefix} onChange={(e) => setPrefix(e.target.value)} placeholder={isFS ? "imagen" : "imagen"} />
            </Field>
            {!isFS && !isSMB ? (
              <Field label="Región (opcional)" hint="Código de ubicación del proveedor S3 (p. ej. hel1 en Hetzner, us-east-1 en AWS)">
                <Input value={region} onChange={(e) => setRegion(e.target.value)} placeholder="hel1" />
              </Field>
            ) : null}
            {!isFS ? (
              <Field label="Retención (días)" hint="Object Lock GOVERNANCE: 0 = sin lock">
                <Input type="number" min={0} value={retentionDays}
                  onChange={(e) => setRetentionDays(Number(e.target.value) || 0)} />
              </Field>
            ) : null}
            <Field label="Réplica cada (minutos)" hint="0 = solo manual">
              <Input type="number" min={0} value={syncInterval}
                onChange={(e) => setSyncInterval(Number(e.target.value) || 0)} />
            </Field>
          </div>
          {!isFS ? (
            <div className="flex flex-wrap gap-4">
              <label className="flex items-center gap-2 text-sm text-slate-300">
                <input type="checkbox" checked={noTLS} onChange={(e) => setNoTLS(e.target.checked)} />
                Sin TLS (MinIO local)
              </label>
              <label className="flex items-center gap-2 text-sm text-slate-300">
                <input type="checkbox" checked={objectLock} onChange={(e) => setObjectLock(e.target.checked)} />
                Object Lock activo (inmutabilidad)
              </label>
            </div>
          ) : (
            <div className="border border-ink-600 bg-ink-900/40 p-3 text-xs text-slate-400">
              {isSMB ? (
                <>
                  Las compartidas SMB se acceden con un cliente SMB nativo (sin
                  montar): válidas para <strong className="text-slate-200">backups de imagen, VM y LXC</strong>,
                  con autenticación NTLM contra dominio AD o workgroup. La
                  réplica del repositorio a SMB usa <code className="text-signal">rclone</code>.
                  Sin Object Lock: la inmutabilidad depende de la retención del
                  destino.
                </>
              ) : (
                <>
                  La ruta física no aplica Object Lock: la inmutabilidad de los
                  backups de archivo la garantiza la política de retención del
                  repositorio Kopia del destino.
                </>
              )}
            </div>
          )}
          {error ? <ErrorBox message={error} /> : null}
          <div className="flex justify-end gap-2">
            {editingId ? (
              <Button type="button" variant="ghost" onClick={() => { setShowAdd(false); setEditingId(null); reset(); }}>
                Cancelar
              </Button>
            ) : null}
            <Button type="submit" loading={busy === "create"}>
              {editingId ? "Guardar cambios" : "Registrar destino"}
            </Button>
          </div>
        </form>
      ) : null}

      {error && !showAdd ? <div className="mt-4"><ErrorBox message={error} /></div> : null}

      <div className="mt-8 space-y-6">
        {loading ? (
          <p className="text-sm text-slate-500">Cargando…</p>
        ) : targets.length === 0 ? (
          <p className="text-sm text-slate-500">
            Sin destinos. Añade un destino S3-compatible o una ruta física para la réplica.
          </p>
        ) : (
          targets.map((t) => (
            <div key={t.id} className="panel p-5">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <h2 className="font-display text-xl uppercase tracking-wide text-slate-100">{t.name}</h2>
                  <p className="mt-1 font-mono text-[10px] text-slate-500">
                    {t.kind === "filesystem"
                      ? <>
                          {t.path}{t.prefix ? "/" + t.prefix : ""} · {t.smb ? "SMB" : "ruta física"}
                          {t.smb && t.smb_user ? ` · ${t.smb_domain ? t.smb_domain + "\\" : ""}${t.smb_user}` : ""}
                        </>
                      : <>
                          {t.endpoint} · {t.bucket}{t.prefix ? "/" + t.prefix : ""} · {t.access_key}
                          {t.no_tls ? " · sin TLS" : " · TLS"}
                        </>}
                  </p>
                </div>
                <div className="flex gap-2">
                  <Button size="sm" variant="ghost" onClick={() => startEdit(t)}>Editar</Button>
                  <Button size="sm" variant="ghost" loading={busy === t.id}
                    onClick={() => verify(t.id)}>Verificar</Button>
                  <Button size="sm" variant="ghost" loading={busy === t.id}
                    onClick={() => sync(t.id)}>Replicar ahora</Button>
                  <Button size="sm" variant="ghost" loading={busy === t.id}
                    onClick={() => remove(t.id)}>Eliminar</Button>
                </div>
              </div>
              <div className="mt-3 flex flex-wrap gap-3 font-mono text-[10px] text-slate-500">
                <span className="border border-ink-600 px-2 py-0.5 text-slate-400">
                  {t.kind === "filesystem" ? (t.smb ? "SMB" : "ruta física") : "S3"}
                </span>
                {t.kind === "filesystem" ? (
                  <span className="border border-ink-600 px-2 py-0.5">sin Object Lock</span>
                ) : (
                  <span className={cx(
                    "border px-2 py-0.5",
                    t.object_lock ? "border-signal/30 text-signal" : "border-ink-600 text-slate-500",
                  )}>
                    {t.object_lock ? `Object Lock ${t.retention_days}d` : "Sin lock"}
                  </span>
                )}
                <span className="border border-ink-600 px-2 py-0.5">
                  réplica cada {t.sync_interval_minutes} min
                </span>
                {t.enabled
                  ? <span className="border border-emerald-600/30 text-emerald-400 px-2 py-0.5">activo</span>
                  : <span className="border border-ink-600 px-2 py-0.5 text-slate-500">inactivo</span>}
                {t.last_verify_status
                  ? <span>verificado: {t.last_verify_status}</span>
                  : null}
                {t.last_sync_at
                  ? <span>última réplica: {t.last_sync_at.slice(0, 16).replace("T", " ")}</span>
                  : null}
                {t.last_sync_status
                  ? <span className={t.last_sync_status === "ok" ? "text-emerald-400" : "text-alert"}>
                      {t.last_sync_status}
                    </span>
                  : null}
              </div>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
