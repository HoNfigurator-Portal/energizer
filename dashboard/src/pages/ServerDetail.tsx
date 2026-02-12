import { useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import useSWR from 'swr';
import { Header } from '@/components/layout/Header';
import { ServerStatusBadge } from '@/components/ServerStatusBadge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { fetchInstances, serverActions } from '@/api/endpoints';
import {
  Play, Square, RotateCcw, ArrowLeft, Send,
  Users, Gamepad2, Map, Hash, Cpu, AlertTriangle,
  Activity, MessageSquare,
} from 'lucide-react';
import { relativeTime, num } from '@/lib/utils';
import { normalizePhase } from '@/components/ServerStatusBadge';
import { toast } from '@/lib/toast';
import type { InstanceInfo, PlayerInfo } from '@/types';

const POLL_INTERVAL = 3000;

export function ServerDetail() {
  const { port: portParam } = useParams<{ port: string }>();
  const port = Number(portParam);
  const navigate = useNavigate();
  const { data, mutate } = useSWR('instances', fetchInstances, { refreshInterval: POLL_INTERVAL });
  const [message, setMessage] = useState('');
  const [sending, setSending] = useState(false);
  const [actionLoading, setActionLoading] = useState<string | null>(null);

  const instance: InstanceInfo | undefined = data?.instances?.find(
    (i: InstanceInfo) => i.port === port
  );

  async function handleAction(action: 'start' | 'stop' | 'restart') {
    setActionLoading(action);
    try {
      await serverActions[action](port);
      toast.success(`Server :${port} - ${action} successful`);
      setTimeout(() => mutate(), 1000);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Server :${port} - ${action} failed: ${msg}`);
    } finally {
      setActionLoading(null);
    }
  }

  async function handleSendMessage() {
    if (!message.trim()) return;
    setSending(true);
    try {
      await serverActions.message(port, message);
      toast.success('Message sent');
      setMessage('');
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Failed to send: ${msg}`);
    } finally {
      setSending(false);
    }
  }

  if (!instance) {
    return (
      <div className="flex flex-col">
        <Header title={`Server :${port}`} />
        <div className="flex flex-col items-center justify-center gap-4 p-16 text-muted-foreground">
          <AlertTriangle className="h-8 w-8 opacity-40" />
          <div className="text-center">
            <p className="text-sm font-medium">Instance Not Found</p>
            <p className="mt-1 text-xs">Server instance not found or API is unreachable</p>
          </div>
          <Button variant="outline" size="sm" onClick={() => navigate('/servers')}>
            <ArrowLeft className="h-3 w-3" /> Back to Servers
          </Button>
        </div>
      </div>
    );
  }

  const state = instance.state ?? {};
  const players: PlayerInfo[] = state.players ? Object.values(state.players) : [];
  const cpuUsage = num(state.cpu_usage);
  const phase = normalizePhase(state.phase);
  const lagEvents = num(state.total_lag_events);
  const matchId = num(state.match_id);

  return (
    <div className="flex flex-col">
      <Header
        title={instance.server_name || `Server :${port}`}
        subtitle={instance.running ? `Port ${port} / PID ${instance.pid}` : `Port ${port} / Not running`}
        actions={
          <div className="flex items-center gap-1.5">
            <Button variant="ghost" size="sm" onClick={() => navigate('/servers')}>
              <ArrowLeft className="h-3 w-3" /> Back
            </Button>
            <div className="mx-1 h-4 w-px bg-border" />
            {!instance.running ? (
              <Button
                size="sm"
                onClick={() => handleAction('start')}
                disabled={actionLoading === 'start'}
                className="bg-success text-white hover:bg-success/90"
              >
                <Play className="h-3 w-3" /> Start
              </Button>
            ) : (
              <>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => handleAction('stop')}
                  disabled={actionLoading === 'stop'}
                >
                  <Square className="h-3 w-3" /> Stop
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => handleAction('restart')}
                  disabled={actionLoading === 'restart'}
                >
                  <RotateCcw className="h-3 w-3" /> Restart
                </Button>
              </>
            )}
          </div>
        }
      />

      <div className="space-y-4 p-5">
        {/* Status Grid */}
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4 stagger-children">
          {[
            {
              icon: Hash, label: 'Status',
              content: <ServerStatusBadge status={state.status} />,
            },
            {
              icon: Gamepad2, label: 'Phase',
              content: <span className="text-sm font-medium capitalize">{phase.replace(/_/g, ' ')}</span>,
            },
            {
              icon: Map, label: 'Map',
              content: <span className="text-sm font-medium">{state.map_name || '-'}</span>,
            },
            {
              icon: Cpu, label: 'CPU',
              content: <span className="text-sm font-medium">{cpuUsage > 0 ? `${cpuUsage.toFixed(1)}%` : '-'}</span>,
            },
          ].map(({ icon: Icon, label, content }) => (
            <Card key={label} className="animate-fade-in">
              <CardContent className="flex items-center gap-3 p-3.5">
                <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-muted">
                  <Icon className="h-3.5 w-3.5 text-muted-foreground" />
                </div>
                <div>
                  <p className="text-[10px] uppercase tracking-wider text-muted-foreground">{label}</p>
                  {content}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>

        {/* Match Info */}
        <Card className="animate-fade-in">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Activity className="h-3.5 w-3.5 text-muted-foreground" />
              Match Details
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid gap-x-6 gap-y-3 sm:grid-cols-2 lg:grid-cols-3">
              {[
                { label: 'Match ID', value: matchId || '-', mono: true },
                { label: 'Game Mode', value: state.game_mode || '-' },
                { label: 'Uptime', value: instance.uptime || '-' },
                {
                  label: 'Lag Events',
                  value: lagEvents > 0
                    ? <Badge variant="warning">{lagEvents}</Badge>
                    : <span className="text-muted-foreground">0</span>,
                },
                { label: 'Status Changed', value: relativeTime(state.status_changed_at) },
                {
                  label: 'Next Restart',
                  value: instance.next_restart
                    ? new Date(instance.next_restart).toLocaleTimeString()
                    : '-',
                },
              ].map(({ label, value, mono }) => (
                <div key={label} className="flex items-center justify-between border-b border-border/30 py-2 last:border-0 sm:flex-col sm:items-start sm:border-0 sm:py-0">
                  <p className="text-[10px] uppercase tracking-wider text-muted-foreground">{label}</p>
                  <p className={`text-sm ${mono ? 'font-mono' : ''}`}>{value}</p>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>

        {/* Players */}
        <Card className="animate-fade-in">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Users className="h-3.5 w-3.5 text-muted-foreground" />
              Players
              <span className="ml-auto text-[11px] font-normal text-muted-foreground">
                {players.length} connected
              </span>
            </CardTitle>
          </CardHeader>
          <CardContent>
            {players.length === 0 ? (
              <div className="flex flex-col items-center gap-2 py-10 text-muted-foreground">
                <Users className="h-6 w-6 opacity-30" />
                <p className="text-xs">No players connected</p>
              </div>
            ) : (
              <div className="overflow-x-auto -mx-5">
                <table className="w-full text-xs">
                  <thead>
                    <tr className="border-b border-border/60">
                      <th className="px-5 pb-2 text-left font-medium text-[10px] uppercase tracking-wider text-muted-foreground">Name</th>
                      <th className="px-3 pb-2 text-left font-medium text-[10px] uppercase tracking-wider text-muted-foreground">PSR</th>
                      <th className="px-3 pb-2 text-left font-medium text-[10px] uppercase tracking-wider text-muted-foreground">Ping</th>
                      <th className="px-5 pb-2 text-left font-medium text-[10px] uppercase tracking-wider text-muted-foreground">Joined</th>
                    </tr>
                  </thead>
                  <tbody>
                    {players.map((p: PlayerInfo) => (
                      <tr key={p.id} className="border-b border-border/30 transition-colors hover:bg-accent/30">
                        <td className="px-5 py-2 font-medium">{p.name}</td>
                        <td className="px-3 py-2 text-muted-foreground">{num(p.psr).toFixed(0)}</td>
                        <td className="px-3 py-2">
                          <span
                            className={
                              num(p.ping) > 150
                                ? 'text-destructive'
                                : num(p.ping) > 80
                                ? 'text-warning'
                                : 'text-success'
                            }
                          >
                            {num(p.ping)}ms
                          </span>
                        </td>
                        <td className="px-5 py-2 text-muted-foreground">{relativeTime(p.joined_at)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </CardContent>
        </Card>

        {/* Send Message */}
        <Card className="animate-fade-in">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <MessageSquare className="h-3.5 w-3.5 text-muted-foreground" />
              In-Game Message
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex gap-2">
              <Input
                placeholder="Type a message to broadcast..."
                value={message}
                onChange={(e) => setMessage(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && handleSendMessage()}
                disabled={!instance.running}
                className="text-xs"
              />
              <Button
                size="sm"
                onClick={handleSendMessage}
                disabled={!instance.running || sending || !message.trim()}
              >
                <Send className="h-3 w-3" /> Send
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
