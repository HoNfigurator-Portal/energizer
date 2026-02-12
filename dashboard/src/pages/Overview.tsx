import { useState } from 'react';
import useSWR from 'swr';
import { Header } from '@/components/layout/Header';
import { SystemStatCard } from '@/components/SystemStatCard';
import { ServerStatusBadge } from '@/components/ServerStatusBadge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { fetchInstances, fetchCPU, fetchMemory, fetchServerInfo, serverActions } from '@/api/endpoints';
import { Cpu, MemoryStick, Server, Globe, Activity, MonitorX, Play, Square, RotateCcw } from 'lucide-react';
import { formatBytes, num } from '@/lib/utils';
import { normalizePhase } from '@/components/ServerStatusBadge';
import { toast } from '@/lib/toast';
import { useNavigate } from 'react-router-dom';
import type { InstanceInfo } from '@/types';

const POLL_INTERVAL = 5000;

export function Overview() {
  const { data: instancesData, mutate } = useSWR('instances', fetchInstances, { refreshInterval: POLL_INTERVAL });
  const { data: cpuData } = useSWR('cpu', fetchCPU, { refreshInterval: POLL_INTERVAL });
  const { data: memData } = useSWR('memory', fetchMemory, { refreshInterval: POLL_INTERVAL });
  const { data: serverInfo } = useSWR('serverInfo', fetchServerInfo, { refreshInterval: 30000 });
  const navigate = useNavigate();
  const [startAllLoading, setStartAllLoading] = useState(false);
  const [stopAllLoading, setStopAllLoading] = useState(false);
  const [restartAllLoading, setRestartAllLoading] = useState(false);

  const instances = instancesData?.instances ?? [];
  const running = instances.filter((i: InstanceInfo) => i.running).length;
  const occupied = instances.filter((i: InstanceInfo) => {
    const s = i.state?.status;
    return s === 'occupied' || (s as unknown) === 4;
  }).length;
  const total = instances.length;

  const cpuPct = num(cpuData?.cpu_percent);
  const memPct = num(memData?.used_percent);

  async function handleStartAll() {
    if (startAllLoading || total === 0) return;
    setStartAllLoading(true);
    try {
      await serverActions.startAll();
      toast.success('Start All — servers are starting');
      setTimeout(() => mutate(), 2000);
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Start All failed';
      toast.error(msg);
    } finally {
      setStartAllLoading(false);
    }
  }

  async function handleStopAll() {
    if (stopAllLoading || total === 0) return;
    setStopAllLoading(true);
    try {
      await serverActions.stopAll();
      toast.success('Stop All — servers are stopping');
      setTimeout(() => mutate(), 1000);
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Stop All failed';
      toast.error(msg);
    } finally {
      setStopAllLoading(false);
    }
  }

  async function handleRestartAll() {
    if (restartAllLoading || total === 0) return;
    setRestartAllLoading(true);
    try {
      await serverActions.restartAll();
      toast.success('Restart All — servers are restarting');
      setTimeout(() => mutate(), 3000);
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Restart All failed';
      toast.error(msg);
    } finally {
      setRestartAllLoading(false);
    }
  }

  return (
    <div className="flex flex-col">
      <Header
        title="Overview"
        subtitle={serverInfo?.server_name || 'Energizer Server Manager'}
        actions={
          <div className="flex items-center gap-1.5">
            <Button
              size="sm"
              variant="default"
              onClick={handleStartAll}
              disabled={startAllLoading || total === 0 || running === total}
              className="gap-1.5"
            >
              <Play className="h-3.5 w-3.5" />
              {startAllLoading ? 'Starting…' : 'Start All'}
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={handleStopAll}
              disabled={stopAllLoading || total === 0 || running === 0}
              className="gap-1.5"
            >
              <Square className="h-3.5 w-3.5" />
              {stopAllLoading ? 'Stopping…' : 'Stop All'}
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={handleRestartAll}
              disabled={restartAllLoading || total === 0 || running === 0}
              className="gap-1.5"
            >
              <RotateCcw className="h-3.5 w-3.5" />
              {restartAllLoading ? 'Restarting…' : 'Restart All'}
            </Button>
          </div>
        }
      />

      <div className="space-y-5 p-5">
        {/* Stats Row */}
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4 stagger-children">
          <SystemStatCard
            title="CPU"
            value={cpuData ? `${cpuPct.toFixed(1)}%` : '--'}
            icon={<Cpu className="h-4 w-4" />}
            color={cpuPct > 80 ? 'destructive' : cpuPct > 60 ? 'warning' : 'primary'}
            progress={cpuData ? cpuPct : undefined}
          />
          <SystemStatCard
            title="Memory"
            value={memData ? `${memPct.toFixed(1)}%` : '--'}
            subtitle={
              memData
                ? `${formatBytes(num(memData.used_mb))} / ${formatBytes(num(memData.total_mb))}`
                : undefined
            }
            icon={<MemoryStick className="h-4 w-4" />}
            color={memPct > 85 ? 'destructive' : memPct > 70 ? 'warning' : 'primary'}
            progress={memData ? memPct : undefined}
          />
          <SystemStatCard
            title="Servers"
            value={`${running} / ${total}`}
            subtitle={
              serverInfo?.max_instances
                ? `Max ${serverInfo.max_instances} (${serverInfo.cpu_cores} cores × ${serverInfo.servers_per_core}/core)${occupied > 0 ? ` · ${occupied} in game` : ''}`
                : occupied > 0 ? `${occupied} in game` : 'All idle'
            }
            icon={<Server className="h-4 w-4" />}
            color={running === total && total > 0 ? 'success' : running === 0 && total > 0 ? 'destructive' : 'default'}
          />
          <SystemStatCard
            title="Network"
            value={serverInfo?.public_ip || '--'}
            subtitle={
              serverInfo
                ? `${serverInfo.server_location ?? ''} ${serverInfo.server_region ? `(${serverInfo.server_region})` : ''}`
                : undefined
            }
            icon={<Globe className="h-4 w-4" />}
            color="primary"
          />
        </div>

        {/* Server Instance Grid */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Activity className="h-4 w-4 text-muted-foreground" />
              Server Instances
            </CardTitle>
          </CardHeader>
          <CardContent>
            {instances.length === 0 ? (
              <div className="flex flex-col items-center justify-center gap-3 py-16 text-muted-foreground">
                <MonitorX className="h-8 w-8 opacity-40" />
                <div className="text-center">
                  <p className="text-sm font-medium">No Instances</p>
                  <p className="text-xs">No server instances configured or API is unreachable</p>
                </div>
              </div>
            ) : (
              <div className="grid gap-2.5 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 stagger-children">
                {instances.map((instance: InstanceInfo) => {
                  const phase = normalizePhase(instance.state?.phase);
                  const playerCount = num(instance.state?.player_count);
                  const cpuUsage = num(instance.state?.cpu_usage);
                  const isRunning = instance.running;

                  return (
                    <button
                      key={instance.port}
                      onClick={() => navigate(`/servers/${instance.port}`)}
                      className="group flex flex-col gap-2 rounded-xl border border-border/50 bg-surface/50 p-3.5 text-left transition-all duration-200 hover:border-primary/20 hover:bg-accent/50 hover:shadow-sm cursor-pointer animate-fade-in"
                    >
                      {/* Top row: Name + Status */}
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-xs font-semibold text-foreground truncate">
                          {instance.server_name || `Server ${instance.id || instance.port}`}
                        </span>
                        <ServerStatusBadge status={instance.state?.status} />
                      </div>

                      {/* Port + Phase */}
                      <div className="flex items-center justify-between">
                        <span className="font-mono text-[10px] text-muted-foreground">
                          :{instance.port}
                        </span>
                        {phase !== 'idle' && phase !== '0' && phase !== '' ? (
                          <span className="text-[10px] text-muted-foreground capitalize">
                            {phase.replace(/_/g, ' ')}
                          </span>
                        ) : (
                          <span className="text-[10px] text-muted-foreground/50">idle</span>
                        )}
                      </div>

                      {/* Bottom: PID/Players + CPU */}
                      <div className="flex items-center justify-between text-[10px] text-muted-foreground/70">
                        <span>
                          {isRunning ? `PID ${instance.pid}` : 'Not running'}
                          {playerCount > 0 && (
                            <span className="ml-1.5 font-medium text-primary">{playerCount} players</span>
                          )}
                        </span>
                        {cpuUsage > 0 && (
                          <span>{cpuUsage.toFixed(1)}% CPU</span>
                        )}
                      </div>
                    </button>
                  );
                })}
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
