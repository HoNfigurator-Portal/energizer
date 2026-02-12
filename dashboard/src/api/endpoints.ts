import { api, fetcher } from './client';
import type {
  InstanceInfo,
  ServerInfo,
  CPUUsage,
  MemoryUsage,
  LogEntry,
  HoNData,
  ApplicationData,
} from '@/types';

// ---- SWR fetchers (for polling) ----

export const fetchInstances = fetcher<{ instances: InstanceInfo[]; total: number }>(
  '/api/monitor/get_instances_status'
);

export const fetchServerInfo = fetcher<ServerInfo>('/api/public/get_server_info');

export const fetchCPU = fetcher<CPUUsage>('/api/monitor/get_cpu_usage');

export const fetchMemory = fetcher<MemoryUsage>('/api/monitor/get_memory_usage');

export const fetchTotalServers = fetcher<{
  total: number;
  running: number;
  occupied: number;
}>('/api/monitor/get_total_servers');

export function fetchLogs(count = 100) {
  return () =>
    api.get<{ entries: LogEntry[] }>(`/api/monitor/get_energizer_log_entries?count=${count}`);
}

export const fetchTasksStatus = fetcher<Record<string, unknown>>(
  '/api/monitor/get_tasks_status'
);

// ---- Actions ----

export const serverActions = {
  start: (port: number) => api.post(`/api/control/start_server/${port}`),
  stop: (port: number) => api.post(`/api/control/stop_server/${port}`),
  restart: (port: number) => api.post(`/api/control/restart_server/${port}`),
  enable: (port: number) => api.post(`/api/control/enable_server/${port}`),
  disable: (port: number) => api.post(`/api/control/disable_server/${port}`),
  message: (port: number, message: string) =>
    api.post(`/api/control/message_server/${port}`, { message }),
};

// ---- Configuration ----

export const configActions = {
  setHoNData: (data: Partial<HoNData>) => api.post('/api/configure/set_hon_data', data),
  setAppData: (data: Partial<ApplicationData>) =>
    api.post('/api/configure/set_app_data', data),
  addServers: (count: number) => api.post('/api/configure/add_servers', { count }),
  removeServers: (ports: number[]) => api.post('/api/configure/remove_servers', { ports }),
};
