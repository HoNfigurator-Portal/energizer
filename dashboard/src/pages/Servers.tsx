import { useState } from 'react';
import useSWR from 'swr';
import { useNavigate } from 'react-router-dom';
import { Header } from '@/components/layout/Header';
import { ServerStatusBadge } from '@/components/ServerStatusBadge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { fetchInstances, serverActions } from '@/api/endpoints';
import {
  Play, Square, RotateCcw, Power, PowerOff, Server, MonitorX,
} from 'lucide-react';
import { num } from '@/lib/utils';
import { normalizePhase } from '@/components/ServerStatusBadge';
import { toast } from '@/lib/toast';
import type { InstanceInfo } from '@/types';

const POLL_INTERVAL = 5000;

export function Servers() {
  const { data, mutate } = useSWR('instances', fetchInstances, { refreshInterval: POLL_INTERVAL });
  const [loading, setLoading] = useState<Record<string, boolean>>({});
  const navigate = useNavigate();

  const instances = data?.instances ?? [];

  async function handleAction(
    port: number,
    action: 'start' | 'stop' | 'restart' | 'enable' | 'disable'
  ) {
    const key = `${port}-${action}`;
    setLoading((prev) => ({ ...prev, [key]: true }));
    try {
      await serverActions[action](port);
      toast.success(`Server :${port} - ${action} successful`);
      setTimeout(() => mutate(), 1000);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Server :${port} - ${action} failed: ${msg}`);
    } finally {
      setLoading((prev) => ({ ...prev, [key]: false }));
    }
  }

  return (
    <div className="flex flex-col">
      <Header title="Servers" subtitle={`${instances.length} instances configured`} />

      <div className="p-5">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Server className="h-4 w-4 text-muted-foreground" />
              Game Server Instances
            </CardTitle>
          </CardHeader>
          <CardContent>
            {instances.length === 0 ? (
              <div className="flex flex-col items-center justify-center gap-3 py-16 text-muted-foreground">
                <MonitorX className="h-8 w-8 opacity-40" />
                <p className="text-xs">No server instances found</p>
              </div>
            ) : (
              <div className="overflow-x-auto -mx-5">
                <table className="w-full text-xs">
                  <thead>
                    <tr className="border-b border-border/60">
                      <th className="px-5 pb-2.5 text-left font-medium text-muted-foreground uppercase tracking-wider text-[10px]">Name</th>
                      <th className="px-3 pb-2.5 text-left font-medium text-muted-foreground uppercase tracking-wider text-[10px]">Port</th>
                      <th className="px-3 pb-2.5 text-left font-medium text-muted-foreground uppercase tracking-wider text-[10px]">Status</th>
                      <th className="px-3 pb-2.5 text-left font-medium text-muted-foreground uppercase tracking-wider text-[10px]">Phase</th>
                      <th className="px-3 pb-2.5 text-left font-medium text-muted-foreground uppercase tracking-wider text-[10px]">Players</th>
                      <th className="px-3 pb-2.5 text-left font-medium text-muted-foreground uppercase tracking-wider text-[10px]">Uptime</th>
                      <th className="px-3 pb-2.5 text-left font-medium text-muted-foreground uppercase tracking-wider text-[10px]">CPU</th>
                      <th className="px-3 pb-2.5 text-left font-medium text-muted-foreground uppercase tracking-wider text-[10px]">PID</th>
                      <th className="px-5 pb-2.5 text-right font-medium text-muted-foreground uppercase tracking-wider text-[10px]">Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {instances.map((inst: InstanceInfo) => {
                      const cpuUsage = num(inst.state?.cpu_usage);
                      const playerCount = num(inst.state?.player_count);
                      const phase = normalizePhase(inst.state?.phase);
                      return (
                        <tr
                          key={inst.port}
                          className="group border-b border-border/30 transition-colors duration-100 hover:bg-accent/40 cursor-pointer"
                          onClick={() => navigate(`/servers/${inst.port}`)}
                        >
                          <td className="px-5 py-2.5">
                            <span className="font-medium text-foreground truncate max-w-[180px] block">
                              {inst.server_name || `Server ${inst.id || inst.port}`}
                            </span>
                          </td>
                          <td className="px-3 py-2.5">
                            <span className="font-mono font-bold text-foreground">:{inst.port}</span>
                          </td>
                          <td className="px-3 py-2.5">
                            <ServerStatusBadge status={inst.state?.status} />
                          </td>
                          <td className="px-3 py-2.5 capitalize text-muted-foreground">
                            {phase.replace(/_/g, ' ')}
                          </td>
                          <td className="px-3 py-2.5">
                            {playerCount > 0 ? (
                              <span className="font-medium text-primary">{playerCount}</span>
                            ) : (
                              <span className="text-muted-foreground/50">-</span>
                            )}
                          </td>
                          <td className="px-3 py-2.5 text-muted-foreground">
                            {inst.uptime || '-'}
                          </td>
                          <td className="px-3 py-2.5 text-muted-foreground">
                            {cpuUsage > 0 ? `${cpuUsage.toFixed(1)}%` : '-'}
                          </td>
                          <td className="px-3 py-2.5 font-mono text-muted-foreground/70">
                            {inst.pid > 0 ? inst.pid : '-'}
                          </td>
                          <td className="px-5 py-2.5" onClick={(e) => e.stopPropagation()}>
                            <div className="flex items-center justify-end gap-0.5">
                              {!inst.running ? (
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  title="Start"
                                  disabled={loading[`${inst.port}-start`]}
                                  onClick={() => handleAction(inst.port, 'start')}
                                  className="h-7 w-7"
                                >
                                  <Play className="h-3 w-3 text-success" />
                                </Button>
                              ) : (
                                <>
                                  <Button
                                    variant="ghost"
                                    size="icon"
                                    title="Stop"
                                    disabled={loading[`${inst.port}-stop`]}
                                    onClick={() => handleAction(inst.port, 'stop')}
                                    className="h-7 w-7"
                                  >
                                    <Square className="h-3 w-3 text-destructive" />
                                  </Button>
                                  <Button
                                    variant="ghost"
                                    size="icon"
                                    title="Restart"
                                    disabled={loading[`${inst.port}-restart`]}
                                    onClick={() => handleAction(inst.port, 'restart')}
                                    className="h-7 w-7"
                                  >
                                    <RotateCcw className="h-3 w-3 text-muted-foreground" />
                                  </Button>
                                </>
                              )}
                              {inst.enabled ? (
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  title="Disable"
                                  disabled={loading[`${inst.port}-disable`]}
                                  onClick={() => handleAction(inst.port, 'disable')}
                                  className="h-7 w-7"
                                >
                                  <PowerOff className="h-3 w-3 text-warning" />
                                </Button>
                              ) : (
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  title="Enable"
                                  disabled={loading[`${inst.port}-enable`]}
                                  onClick={() => handleAction(inst.port, 'enable')}
                                  className="h-7 w-7"
                                >
                                  <Power className="h-3 w-3 text-success" />
                                </Button>
                              )}
                            </div>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
