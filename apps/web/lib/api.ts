// Tipos alineados con packages/contracts (contracts.go y admin.go).
// Los campos opcionales reflejan los `omitempty` de Go: pueden ausentarse en JSON.
//
// El panel se sirve embebido en el binario del servidor (mismo origen), de
// modo que BASE es cadena vacía en producción. En desarrollo, con el panel
// en :3000 y el control en :8080, se fija NEXT_PUBLIC_API_BASE=http://127.0.0.1:8080.
export const API_BASE = (process.env.NEXT_PUBLIC_API_BASE || "").replace(/\/+$/, "");

export type AgentStatus = "online" | "offline" | "pending" | "revoked";

export type JobState =
  | "pending"
  | "leased"
  | "running"
  | "succeeded"
  | "failed"
  | "canceled"
  | "expired";

export type VerifyState =
  | "unverified"
  | "running"
  | "structural_ok"
  | "content_ok"
  | "failed";

export type AlertSeverity = "critical" | "high" | "medium" | "low";
export type AlertState = "open" | "acknowledged" | "resolved";

export type TargetType = "agent" | "group";

// --- Inventario (contracts.Inventory) ---

export interface Disk {
  name: string;
  mountpoint: string;
  fs_type?: string;
  total_bytes: number;
  free_bytes: number;
}

export interface BlockDevice {
  name: string;
  type: string; // disk | part | lvm | crypt | raid | rom
  size_bytes: number;
  model?: string;
  serial?: string;
  fs_type?: string;
  mountpoint?: string;
  uuid?: string;
  partition_table?: string;
  removable?: boolean;
  children?: BlockDevice[];
}

export interface NetInterface {
  name: string;
  mac?: string;
  addresses?: string[];
  up: boolean;
}

export interface Firmware {
  mode?: string; // uefi | bios
  secure_boot?: boolean | null;
  tpm?: boolean | null;
  tpm_version?: string;
}

export interface EncryptedVolume {
  volume: string;
  kind: string; // bitlocker | luks
  protection?: string;
  locked?: boolean;
}

export interface Inventory {
  collected_at: string;
  hostname: string;
  os_family: string;
  os_version: string;
  arch: string;
  cpu_cores: number;
  ram_mb: number;
  agent_version: string;
  disks?: Disk[];
  block_devices?: BlockDevice[];
  network?: NetInterface[];
  firmware?: Firmware | null;
  encrypted_volumes?: EncryptedVolume[];
  extra?: Record<string, unknown>;
}

// --- Agentes ---

export interface GroupRef {
  id: string;
  name: string;
}

export interface Agent {
  id: string;
  hostname: string;
  os_family: string;
  os_version: string;
  arch: string;
  agent_version: string;
  status: AgentStatus;
  reported_state?: string;
  current_job_id?: string;
  last_seen_at?: string;
  enrolled_at: string;
  revoked_at?: string;
  revoked_reason?: string;
  inventory?: Inventory;
  groups: GroupRef[];
  cert_expires_at?: string;
  repository_username?: string;
  last_backup_at?: string;
  last_backup_state?: string;
  next_backup_at?: string;
  upgrade_requested_at?: string;
}

export interface AgentList {
  items: Agent[];
}

export interface AgentDetail {
  agent: Agent;
  effective_plans: EffectivePlan[];
  recent_jobs: Job[];
  restore_points: RestorePoint[];
  open_alerts: Alert[];
}

// --- Grupos (admin.GroupView) ---

export interface Group {
  id: string;
  name: string;
  description?: string;
  parent_id?: string;
  member_ids: string[];
  plan_ids: string[];
  created_at: string;
  updated_at: string;
}

export interface GroupList {
  items: Group[];
}

// --- Planes (contracts.PlanView) ---

export interface Retention {
  keep_latest: number;
  keep_hourly: number;
  keep_daily: number;
  keep_weekly: number;
  keep_monthly: number;
  keep_annual: number;
}

export interface TimeWindow {
  start: string; // "HH:MM"
  end: string; // "HH:MM"; si end < start cruza la medianoche
}

export interface Assignment {
  id: string;
  plan_id: string;
  target_type: TargetType;
  target_id: string;
  target_name?: string;
  agent_id?: string;
  enabled: boolean;
  created_at: string;
}

export interface AssignmentList {
  items: Assignment[];
}

export interface Plan {
  id: string;
  name: string;
  description?: string;
  kind: string;
  paths?: string[]; // null en planes de imagen (equipo completo)
  excludes?: string[];
  disks?: number[]; // planes de imagen: índices a capturar; vacío = todos los discos fijos
  enabled: boolean;
  schedule?: string; // cron de 5 campos; vacío = solo manual
  timezone: string;
  window?: TimeWindow | null;
  retention: Retention;
  rpo_hours?: number;
  storage_target_id?: string; // destino propio; vacío = repo central
  created_at: string;
  updated_at: string;
  assignments?: Assignment[];
  next_runs?: string[];
}

export interface PlanList {
  items: Plan[];
}

export interface PlanValidation {
  valid: boolean;
  errors: string[];
  warnings: string[];
  next_runs: string[];
}

export interface EffectivePlan {
  plan: Plan;
  via: TargetType;
  assignment_id: string;
  group_id?: string;
  group_name?: string;
  shadowed?: string[];
  next_run_at?: string;
  last_run_at?: string;
}

export interface EffectivePlanList {
  items: EffectivePlan[];
}

// --- Jobs (contracts.JobView) ---

export interface SnapshotSummary {
  path: string;
  id: string;
  root_object_id?: string;
  source_host?: string;
  source_user?: string;
  start_time?: string;
  end_time?: string;
  size_bytes: number;
  files: number;
  dirs: number;
  error_count: number;
  excluded_files?: number;
  excluded_dirs?: number;
}

export interface RestoredItem {
  item: string;
  destination: string;
  files: number;
  dirs: number;
  bytes: number;
}

// contracts.Result es json.RawMessage: puede ser BackupJobResult o RestoreJobResult.
export interface JobResult {
  snapshots?: SnapshotSummary[];
  bytes_total?: number;
  duration_ms?: number;
  kopia_version?: string;
  retention_note?: string;
  restored?: RestoredItem[];
}

export interface Job {
  id: string;
  type: string;
  state: JobState;
  agent_id: string;
  plan_id?: string;
  restore_point_id?: string;
  trigger?: string; // manual | schedule | retry | api
  progress: number;
  message?: string;
  bytes_done?: number;
  bytes_total?: number;
  payload?: Record<string, unknown>;
  result?: JobResult;
  error_code?: string;
  error_message?: string;
  attempts: number;
  max_attempts: number;
  retry_of?: string;
  created_by?: string;
  created_at: string;
  updated_at: string;
  started_at?: string;
  finished_at?: string;
  lease_until?: string;
  cancel_requested_at?: string;
}

export interface JobList {
  items: Job[];
  next_cursor?: string;
}

export interface JobEvent {
  seq: number;
  job_id: string;
  at: string;
  kind: "state" | "progress" | "log";
  state?: string;
  percent?: number;
  level?: string;
  message?: string;
}

export interface JobEventList {
  items: JobEvent[];
}

// --- Puntos de recuperación (admin.RestorePointView) ---

export interface RestorePoint {
  id: string;
  agent_id: string;
  hostname?: string;
  plan_id?: string;
  plan_name?: string;
  job_id: string;
  kind: string; // file
  snapshot_id: string;
  root_object_id?: string;
  source_path: string;
  source_host?: string;
  source_user?: string;
  created_at: string;
  start_time: string;
  end_time: string;
  size_bytes: number;
  files: number;
  dirs: number;
  error_count: number;
  consistency: string; // crash | application
  encrypted: boolean;
  destination: string;
  verify_state: VerifyState;
  verified_at?: string;
  verify_detail?: string;
  expired_at?: string;
  browsable: boolean;
}

export interface RestorePointList {
  items: RestorePoint[];
  next_cursor?: string;
}

export interface FileEntry {
  name: string;
  path: string; // relativa a la raíz del snapshot, separador "/"
  type: "file" | "dir" | "symlink" | "other" | string;
  size: number;
  mtime: string;
  mode?: string;
  files?: number;
}

export interface FileListing {
  restore_point_id: string;
  path: string;
  entries: FileEntry[];
}

export interface FileSearchResult {
  restore_point_id: string;
  path: string;
  query: string;
  truncated: boolean;
  items: FileEntry[];
}

export interface RestoreResponse {
  restore_id: string;
  job_id: string;
  state: string;
}

// --- Alertas (admin.AlertView) ---

export interface Alert {
  id: string;
  key: string;
  kind: string;
  severity: AlertSeverity | string;
  state: AlertState | string;
  title: string;
  detail?: string;
  agent_id?: string;
  plan_id?: string;
  job_id?: string;
  count: number;
  first_at: string;
  last_at: string;
  ack_by?: string;
  ack_at?: string;
  resolved_by?: string;
  resolved_at?: string;
  notified_at?: string;
}

export interface AlertList {
  items: Alert[];
}

// --- Resumen (admin.Summary) ---

export interface Ratio {
  succeeded: number;
  failed: number;
  percent: number; // 0–100; -1 si no hay datos
}

export interface UpcomingRun {
  at: string;
  plan_id: string;
  plan_name: string;
  agent_id: string;
  hostname: string;
}

export interface DayCount {
  day: string; // YYYY-MM-DD
  succeeded: number;
  failed: number;
}

export interface Summary {
  generated_at: string;
  agents_total: number;
  agents_online: number;
  agents_protected: number;
  agents_revoked: number;
  plans_total: number;
  jobs_active: number;
  success_24h: Ratio;
  success_7d: Ratio;
  restore_points: number;
  logical_bytes: number;
  stored_bytes?: number;
  bytes_read_24h: number;
  upcoming: UpcomingRun[];
  alerts_open: Record<string, number> | null;
  recent_restores: Job[];
  backups_per_day: DayCount[];
  repository_status: string; // ok | limited | error | unknown | unconfigured
  last_scheduler_tick?: string;
}

// --- Repositorios (admin.RepositoryView) ---

export interface Repository {
  id: string;
  name: string;
  kind: string; // server | filesystem
  url?: string;
  path?: string;
  fingerprint?: string;
  server_side: boolean;
  destination: string;
  status: string; // ok | error | unknown | unconfigured
  status_detail?: string;
  checked_at?: string;
  snapshot_count: number;
  logical_bytes: number;
  stored_bytes?: number;
  agents_with_creds: number;
}

export interface RepositoryList {
  items: Repository[];
}

// --- Enrolamiento ---

export interface EnrollmentToken {
  id: string;
  token: string;
  expires_at: string;
  server_url?: string;
  agent_url?: string;
  server_fingerprint?: string;
}

export interface EnrollmentTokenView {
  id: string;
  created_at: string;
  expires_at: string;
  used_at?: string;
  used_by_agent_id?: string;
  expected_hostname?: string;
  expired: boolean;
  revoked_at?: string;
  group_id?: string;
  plan_id?: string;
  note?: string;
  created_by?: string;
}

export interface EnrollmentTokenList {
  items: EnrollmentTokenView[];
}

// --- Tokens de API (admin.APITokenView) ---

export interface APIToken {
  id: string;
  name: string;
  role: string;
  prefix: string;
  created_at: string;
  created_by?: string;
  expires_at?: string;
  last_used_at?: string;
  revoked_at?: string;
}

export interface APITokenCreated extends APIToken {
  token: string;
}

export interface APITokenList {
  items: APIToken[];
}

// --- Inventario histórico ---

export interface InventoryRecord {
  hash: string;
  collected_at: string;
  inventory: Inventory;
}

export interface InventoryHistory {
  items: InventoryRecord[];
}

export interface Health {
  status: string;
  version: string;
}

// --- Actualizaciones (modo notificación) ---

export interface UpdateStatus {
  check_enabled: boolean;
  current: string;
  latest?: string;
  channel?: string;
  notes_url?: string;
  update_available: boolean;
  checked_at?: string;
  last_error?: string;
  platform?: string;
}

export interface UpdateInstallResponse {
  restarting: boolean;
  version: string;
  message?: string;
}

export class ApiError extends Error {
  status: number;
  code: string;
  requestId?: string;

  constructor(status: number, code: string, detail: string, requestId?: string) {
    super(detail);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.requestId = requestId;
  }
}

interface Problem {
  title?: string;
  detail?: string;
  code?: string;
  request_id?: string;
}

async function parseProblem(res: Response): Promise<ApiError> {
  let problem: Problem = {};
  try {
    problem = (await res.json()) as Problem;
  } catch {
    // sin cuerpo JSON
  }
  return new ApiError(
    res.status,
    problem.code ?? "E_HTTP",
    problem.detail ?? problem.title ?? `Error HTTP ${res.status}`,
    problem.request_id,
  );
}

export async function api<T>(
  path: string,
  token: string,
  init: RequestInit = {},
): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    cache: "no-store",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
      ...(init.headers ?? {}),
    },
  });

  if (!res.ok) {
    throw await parseProblem(res);
  }
  if (res.status === 204) {
    return undefined as T;
  }

  // Algunos endpoints responden 202 con cuerpo vacío; no fallar por eso.
  const text = await res.text();
  if (!text) return undefined as T;
  try {
    return JSON.parse(text) as T;
  } catch {
    return undefined as T;
  }
}

// Descarga binaria con autenticación (p. ej. ficheros de un snapshot).
export async function apiDownload(path: string, token: string): Promise<Blob> {
  const res = await fetch(`${API_BASE}${path}`, {
    cache: "no-store",
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) {
    throw await parseProblem(res);
  }
  return await res.blob();
}

export async function publicHealth(): Promise<Health | null> {
  try {
    const res = await fetch(`${API_BASE}/healthz`, { cache: "no-store" });
    if (!res.ok) return null;
    return (await res.json()) as Health;
  } catch {
    return null;
  }
}

// --- Instaladores («Añadir equipo») y Proxmox ---

export interface InstallerOut {
  os: string;
  arch: string;
  token: string;
  expires_at: string;
  installer_path: string;
  one_liner: string;
}

export interface PVEInstanceSummary {
  instance_id: string;
  name: string;
  url: string;
  last_check?: string;
  agent_id?: string;
}

export interface PVEVM {
  vmid: number;
  name: string;
  node: string;
  status: string;
  type: string;
  mem_mib: number;
  disk_gib: number;
  cores?: number;
}

export interface PVEVMList {
  instance_id: string;
  name: string;
  url: string;
  agent_id?: string;
  items: PVEVM[];
  last_check?: string;
}

// Lista de puntos de restauración (genérica; usada por Proxmox para VMs).
export interface RestorePointViewList {
  items: RestorePoint[];
}

export interface StorageTargetView {
  id: string;
  name: string;
  kind: "s3" | "filesystem";
  endpoint?: string;
  bucket?: string;
  prefix?: string;
  region?: string;
  path?: string;
  smb?: boolean;
  smb_user?: string;
  smb_domain?: string;
  access_key?: string;
  no_tls: boolean;
  object_lock: boolean;
  retention_days: number;
  sync_interval_minutes: number;
  enabled: boolean;
  repo_password_set?: boolean;
  created_at: string;
  last_sync_at?: string;
  last_sync_status?: string;
  last_verify_at?: string;
  last_verify_status?: string;
}

export interface StorageTargetList {
  items: StorageTargetView[];
}

// --- Terminal remota (Remote Access) ---

export interface TerminalCapabilities {
  agent_id: string;
  online: boolean;
  os_family: string;
  shells: string[];
  privileges: string[];
}

export interface TerminalSessionView {
  session_id: string;
  agent_id: string;
  hostname?: string;
  os_family?: string;
  shell: string;
  privilege: string;
  state: string;
  cols: number;
  rows: number;
  created_at: string;
  opened_at?: string;
  closed_at?: string;
  expires_at: string;
  reason?: string;
  operator_id: string;
  operator_name?: string;
  stream_url?: string;
  /** Solo en la creación: credencial del WebSocket del stream (query param ?ticket=…). */
  stream_ticket?: string;
}

export interface TerminalSessionList {
  items: TerminalSessionView[];
}

export interface CreateTerminalSessionRequest {
  shell?: string;
  cols?: number;
  rows?: number;
  privilege?: string;
}

export interface CreateTerminalSessionResponse {
  session_id: string;
  agent_id: string;
  state: string;
  expires_at: string;
  stream_url?: string;
}
