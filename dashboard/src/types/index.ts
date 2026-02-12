// Types matching the Go API response structures

export type GameStatus = 'unknown' | 'queued' | 'starting' | 'ready' | 'occupied' | 'sleeping' | 'stopped';
export type GamePhase = 'idle' | 'in_lobby' | 'banning' | 'picking' | 'loading' | 'preparation' | 'match_started' | 'game_ending' | 'game_ended';

export interface PlayerInfo {
  name: string;
  id: number;
  psr: number;
  joined_at: string;
  ping: number;
}

export interface GameStateSnapshot {
  status: GameStatus;
  phase: GamePhase;
  match_id: number;
  map_name: string;
  game_mode: string;
  player_count: number;
  players: Record<string, PlayerInfo>;
  uptime: number;
  cpu_usage: number;
  total_lag_events: number;
  status_changed_at: string;
  phase_changed_at: string;
}

export interface InstanceInfo {
  id: number;
  server_name: string;
  port: number;
  enabled: boolean;
  running: boolean;
  pid: number;
  uptime: string;
  state: GameStateSnapshot;
  next_restart: string;
}

export interface ServerInfo {
  server_name: string;
  server_location: string;
  server_region: string;
  total_servers: number;
  running_servers: number;
  occupied_servers: number;
  platform: string;
  cpu_model: string;
  cpu_cores: number;
  total_memory_mb: number;
  public_ip: string;
  servers_per_core: number;
  max_instances: number;
}

export interface CPUUsage {
  cpu_percent: number;
}

export interface MemoryUsage {
  total_mb: number;
  used_mb: number;
  available_mb: number;
  used_percent: number;
}

export interface LogEntry {
  timestamp: string;
  level: string;
  message: string;
  fields?: Record<string, unknown>;
}

export interface HoNData {
  hon_install_directory: string;
  hon_home_directory: string;
  hon_artefacts_directory: string;
  hon_executable_name: string;
  svr_login: string;
  svr_password: string;
  svr_name: string;
  svr_location: string;
  svr_region: string;
  svr_ip: string;
  svr_total: number;
  svr_total_per_core: number;
  svr_starting_gamePort: number;
  svr_starting_voicePort: number;
  svr_managerPort: number;
  svr_api_port: number;
  svr_starting_proxyPort: number;
  svr_starting_voiceProxyLocalPort: number;
  svr_starting_voiceProxyRemotePort: number;
  svr_masterServer: string;
  svr_chatAddress: string;
  svr_chatPort: number;
  man_enableProxy: boolean;
  man_use_cowmaster: boolean;
  svr_beta_mode: boolean;
  svr_noConsole: boolean;
  svr_override_affinity: boolean;
  svr_allow_bot_matches: boolean;
  svr_max_idle_time: number;
  svr_version: string;
}

export interface ApplicationData {
  timers: Record<string, number>;
  replay_cleaner: {
    enabled: boolean;
    cleanup_time: string;
    retention_days: number;
    tmp_retention_days: number;
  };
  longterm_storage: {
    enabled: boolean;
    path: string;
  };
  filebeat: {
    enabled: boolean;
  };
  discord: {
    owner_id: string;
    webhook_url: string;
    notify_on_lag: boolean;
    notify_on_crash: boolean;
    notify_on_disk: boolean;
  };
  mqtt: {
    enabled: boolean;
    broker_url: string;
    port: number;
    use_tls: boolean;
    cert_file: string;
    key_file: string;
    ca_file: string;
    client_id: string;
  };
  security: {
    tls_enabled: boolean;
    tls_cert_file: string;
    tls_key_file: string;
    allowed_origins: string[] | null;
    rate_limit_rps: number;
    ip_whitelist: string[] | null;
    auth_disabled: boolean;
  };
  logging: {
    level: string;
    directory: string;
    max_size_mb: number;
    max_backups: number;
  };
}
