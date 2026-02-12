import { useState } from 'react';
import useSWR from 'swr';
import { useNavigate } from 'react-router-dom';
import { Header } from '@/components/layout/Header';
import { ServerStatusBadge } from '@/components/ServerStatusBadge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { fetchInstances, serverActions, configActions } from '@/api/endpoints';
import {
  Play, Square, RotateCcw, Power, PowerOff, Server, MonitorX, Plus, Trash2,
} from 'lucide-react';
import { num } from '@/lib/utils';
import { normalizePhase } from '@/components/ServerStatusBadge';
import { toast } from '@/lib/toast';
import type { InstanceInfo } from '@/types';

const POLL_INTERVAL = 5000;
const MAX_ADD = 20;

export function Servers() {
  const { data, mutate } = useSWR('instances', fetchInstances, { refreshInterval: POLL_INTERVAL });
  const [loading, setLoading] = useState<Record<string, boolean>>({});
  const [addCount, setAddCount] = useState(1);
  const [addLoading, setAddLoading] = useState(false);
  const [removeLoading, setRemoveLoading] = useState<number | null>(null);
  const [removeAllLoading, setRemoveAllLoading] = useState(false);
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

  async function handleAddServers() {
    const count = Math.min(MAX_ADD, Math.max(1, addCount));
    setAddLoading(true);
    try {
      await configActions.addServers(count);
      toast.success(`Added ${count} server(s)`);
      setAddCount(1);
      setTimeout(() => mutate(), 2000);
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Add servers failed';
      toast.error(msg);
    } finally {
      setAddLoading(false);
    }
  }

  async function handleRemoveServer(port: number, serverName: string) {
    if (!confirm(`Remove server "${serverName}" (port ${port})? It will be stopped and removed from the pool.`)) return;
    setRemoveLoading(port);
    try {
      await configActions.removeServers([port]);
      toast.success(`Server :${port} removed`);
      setTimeout(() => mutate(), 500);
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Remove failed';
      toast.error(msg);
    } finally {
      setRemoveLoading(null);
    }
  }

  async function handleRemoveAll() {
    if (instances.length === 0) return;
    if (!confirm(`Remove ALL ${instances.length} server instances? They will be stopped and removed from the pool.`)) return;
    setRemoveAllLoading(true);
    try {
      const allPorts = instances.map((i: InstanceInfo) => i.port);
      await configActions.removeServers(allPorts);
      toast.success(`All ${allPorts.length} instances removed`);
      setTimeout(() => mutate(), 500);
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Remove All failed';
      toast.error(msg);
    } finally {
      setRemoveAllLoading(false);
    }
  }

  return (
    <div className="flex flex-col">
      <Header
        title="Servers"
        subtitle={`${instances.length} instances configured`}
        actions={
          <Button
            size="sm"
            variant="outline"
            onClick={handleRemoveAll}
            disabled={removeAllLoading || instances.length === 0}
            className="gap-1.5 text-destructive hover:text-destructive"
          >
            <Trash2 className="h-3.5 w-3.5" />
            {removeAllLoading ? 'Removing…' : 'Remove All'}
          </Button>
        }
      />

      <div className="p-5 space-y-4">
        {/* Add servers */}
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center justify-between text-sm font-medium text-muted-foreground">
              <span className="flex items-center gap-2">
                <Plus className="h-3.5 w-3.5" />
                Add Game Server Instances
              </span>
              <div className="flex items-center gap-2">
                <Input
                  type="number"
                  min={1}
                  max={MAX_ADD}
                  value={addCount}
                  onChange={(e) => setAddCount(Math.min(MAX_ADD, Math.max(1, Number(e.target.value) || 1)))}
                  className="w-16 h-8 text-xs text-center"
                />
                <Button
                  size="sm"
                  onClick={handleAddServers}
                  disabled={addLoading}
                  className="gap-1.5"
                >
                  <Plus className="h-3 w-3" />
                  {addLoading ? 'Adding…' : 'Add'}
                </Button>
              </div>
            </CardTitle>
          </CardHeader>
        </Card>

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
                      <th className="px-3 pb-2.5 text-right font-medium text-muted-foreground uppercase tracking-wider text-[10px]">Actions</th>
                      <th className="px-5 pb-2.5 text-right font-medium text-muted-foreground uppercase tracking-wider text-[10px] w-12">Remove</th>
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
                          <td className="px-5 py-2.5 text-right" onClick={(e) => e.stopPropagation()}>
                            <Button
                              variant="ghost"
                              size="icon"
                              title="Remove instance"
                              disabled={removeLoading === inst.port}
                              onClick={() => handleRemoveServer(inst.port, inst.server_name || `Server ${inst.id}`)}
                              className="h-7 w-7 text-destructive hover:text-destructive"
                            >
                              <Trash2 className="h-3 w-3" />
                            </Button>
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
