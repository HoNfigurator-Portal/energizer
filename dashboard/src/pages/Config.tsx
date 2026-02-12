import { useState, useEffect } from 'react';
import useSWR from 'swr';
import { Header } from '@/components/layout/Header';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { configActions } from '@/api/endpoints';
import { api } from '@/api/client';
import {
  Save, Check, AlertTriangle,
  Server, Network, Shield, FolderOpen,
  BookOpen, Trash2, MessageCircle, Lock,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { toast } from '@/lib/toast';
import type { HoNData, ApplicationData } from '@/types';

type Tab = 'hon' | 'app';

/* ---------- Section definitions for HoN fields ---------- */
interface FieldDef {
  key: keyof HoNData;
  label: string;
  type: 'text' | 'number' | 'boolean';
}
interface SectionDef {
  title: string;
  icon: React.ReactNode;
  fields: FieldDef[];
}

const honSections: SectionDef[] = [
  {
    title: 'Server Identity',
    icon: <Server className="h-3.5 w-3.5" />,
    fields: [
      { key: 'svr_name', label: 'Server Name', type: 'text' },
      { key: 'svr_ip', label: 'Server IP', type: 'text' },
      { key: 'svr_location', label: 'Location', type: 'text' },
      { key: 'svr_region', label: 'Region', type: 'text' },
      { key: 'svr_login', label: 'Login', type: 'text' },
      { key: 'svr_password', label: 'Password', type: 'text' },
    ],
  },
  {
    title: 'Capacity',
    icon: <Network className="h-3.5 w-3.5" />,
    fields: [
      { key: 'svr_total', label: 'Total Servers', type: 'number' },
      { key: 'svr_total_per_core', label: 'Per Core', type: 'number' },
      { key: 'svr_max_idle_time', label: 'Max Idle (min)', type: 'number' },
    ],
  },
  {
    title: 'Ports',
    icon: <Network className="h-3.5 w-3.5" />,
    fields: [
      { key: 'svr_starting_gamePort', label: 'Game Port', type: 'number' },
      { key: 'svr_starting_voicePort', label: 'Voice Port', type: 'number' },
      { key: 'svr_managerPort', label: 'Manager Port', type: 'number' },
      { key: 'svr_api_port', label: 'API Port', type: 'number' },
      { key: 'svr_chatPort', label: 'Chat Port', type: 'number' },
    ],
  },
  {
    title: 'Connections',
    icon: <Network className="h-3.5 w-3.5" />,
    fields: [
      { key: 'svr_masterServer', label: 'Master Server', type: 'text' },
      { key: 'svr_chatAddress', label: 'Chat Address', type: 'text' },
    ],
  },
  {
    title: 'Paths',
    icon: <FolderOpen className="h-3.5 w-3.5" />,
    fields: [
      { key: 'hon_install_directory', label: 'Install Dir', type: 'text' },
      { key: 'hon_home_directory', label: 'Home Dir', type: 'text' },
      { key: 'hon_executable_name', label: 'Executable', type: 'text' },
    ],
  },
  {
    title: 'Features',
    icon: <Shield className="h-3.5 w-3.5" />,
    fields: [
      { key: 'man_enableProxy', label: 'Proxy (DDoS)', type: 'boolean' },
      { key: 'svr_beta_mode', label: 'Beta Mode', type: 'boolean' },
      { key: 'svr_noConsole', label: 'No Console', type: 'boolean' },
      { key: 'svr_override_affinity', label: 'Override Affinity', type: 'boolean' },
      { key: 'svr_allow_bot_matches', label: 'Bot Matches', type: 'boolean' },
    ],
  },
];

/* ---------- Toggle component ---------- */
function Toggle({
  checked,
  onChange,
  label,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label?: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      className={cn(
        'group relative inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full transition-colors duration-200',
        checked ? 'bg-primary' : 'bg-muted'
      )}
      onClick={() => onChange(!checked)}
    >
      <span
        className={cn(
          'pointer-events-none block h-3.5 w-3.5 rounded-full bg-white shadow-sm transition-transform duration-200',
          checked ? 'translate-x-[18px]' : 'translate-x-[3px]'
        )}
      />
      {label && (
        <span className="ml-11 whitespace-nowrap text-xs text-muted-foreground">
          {checked ? 'On' : 'Off'}
        </span>
      )}
    </button>
  );
}

/* ---------- Config fetcher ---------- */
function fetchConfig() {
  return async () => {
    return api.get<{
      hon_data: HoNData;
      application_data: ApplicationData;
    }>('/api/configure/get_config');
  };
}

/* ---------- Main Component ---------- */
export function Config() {
  const [tab, setTab] = useState<Tab>('hon');
  const [honData, setHonData] = useState<Partial<HoNData>>({});
  const [appData, setAppData] = useState<Partial<ApplicationData>>({});
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { data: configData } = useSWR('config', fetchConfig(), {
    revalidateOnFocus: false,
  });

  useEffect(() => {
    if (configData) {
      if (configData.hon_data) setHonData(configData.hon_data);
      if (configData.application_data) setAppData(configData.application_data);
    }
  }, [configData]);

  async function handleSave() {
    setSaving(true);
    setError(null);
    setSaved(false);
    try {
      if (tab === 'hon') {
        await configActions.setHoNData(honData);
      } else {
        await configActions.setAppData(appData);
      }
      setSaved(true);
      toast.success('Configuration saved');
      setTimeout(() => setSaved(false), 3000);
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to save';
      setError(msg);
      toast.error(`Save failed: ${msg}`);
    } finally {
      setSaving(false);
    }
  }

  function updateHonField(key: keyof HoNData, value: string | number | boolean) {
    setHonData((prev) => ({ ...prev, [key]: value }));
  }

  return (
    <div className="flex flex-col">
      <Header
        title="Settings"
        subtitle="Manage Energizer configuration"
        actions={
          <div className="flex items-center gap-2">
            {saved && (
              <span className="flex items-center gap-1 text-[11px] text-success">
                <Check className="h-3 w-3" /> Saved
              </span>
            )}
            {error && (
              <span className="flex items-center gap-1 text-[11px] text-destructive">
                <AlertTriangle className="h-3 w-3" />
              </span>
            )}
            <Button size="sm" onClick={handleSave} disabled={saving}>
              <Save className="h-3 w-3" /> {saving ? 'Saving...' : 'Save'}
            </Button>
          </div>
        }
      />

      <div className="p-5 space-y-4">
        {/* Tabs */}
        <div className="flex gap-0.5 rounded-lg bg-muted/50 p-0.5 w-fit">
          {[
            { id: 'hon' as Tab, label: 'HoN Server' },
            { id: 'app' as Tab, label: 'Application' },
          ].map((t) => (
            <button
              key={t.id}
              className={cn(
                'rounded-md px-4 py-1.5 text-xs font-medium transition-all duration-150 cursor-pointer',
                tab === t.id
                  ? 'bg-card text-foreground shadow-sm'
                  : 'text-muted-foreground hover:text-foreground'
              )}
              onClick={() => setTab(t.id)}
            >
              {t.label}
            </button>
          ))}
        </div>

        {/* HoN Tab */}
        {tab === 'hon' && (
          <div className="space-y-4 stagger-children">
            {honSections.map((section) => (
              <Card key={section.title} className="animate-fade-in">
                <CardHeader className="pb-2">
                  <CardTitle className="flex items-center gap-2 text-muted-foreground">
                    {section.icon}
                    {section.title}
                  </CardTitle>
                </CardHeader>
                <CardContent>
                  <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                    {section.fields.map((field) => (
                      <div key={field.key} className="space-y-1">
                        <label className="text-[11px] font-medium text-muted-foreground">
                          {field.label}
                        </label>
                        {field.type === 'boolean' ? (
                          <div className="flex items-center gap-2 h-9">
                            <Toggle
                              checked={Boolean(honData[field.key])}
                              onChange={(v) => updateHonField(field.key, v)}
                              label={field.label}
                            />
                          </div>
                        ) : (
                          <Input
                            type={field.type}
                            value={(honData[field.key] as string | number) ?? ''}
                            onChange={(e) =>
                              updateHonField(
                                field.key,
                                field.type === 'number'
                                  ? Number(e.target.value)
                                  : e.target.value
                              )
                            }
                          />
                        )}
                      </div>
                    ))}
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        )}

        {/* App Tab */}
        {tab === 'app' && (
          <div className="space-y-4 stagger-children">
            {/* Logging */}
            <Card className="animate-fade-in">
              <CardHeader className="pb-2">
                <CardTitle className="flex items-center gap-2 text-muted-foreground">
                  <BookOpen className="h-3.5 w-3.5" />
                  Logging
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                  {[
                    { label: 'Level', key: 'level' as const },
                    { label: 'Directory', key: 'directory' as const },
                    { label: 'Max Size (MB)', key: 'max_size_mb' as const, type: 'number' },
                    { label: 'Max Backups', key: 'max_backups' as const, type: 'number' },
                  ].map((f) => (
                    <div key={f.key} className="space-y-1">
                      <label className="text-[11px] font-medium text-muted-foreground">{f.label}</label>
                      <Input
                        type={f.type || 'text'}
                        value={appData.logging?.[f.key] ?? ''}
                        onChange={(e) =>
                          setAppData((prev) => ({
                            ...prev,
                            logging: {
                              ...prev.logging!,
                              [f.key]: f.type === 'number' ? Number(e.target.value) : e.target.value,
                            },
                          }))
                        }
                      />
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>

            {/* Replay Cleaner */}
            <Card className="animate-fade-in">
              <CardHeader className="pb-2">
                <CardTitle className="flex items-center gap-2 text-muted-foreground">
                  <Trash2 className="h-3.5 w-3.5" />
                  Replay Cleaner
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                  <div className="space-y-1">
                    <label className="text-[11px] font-medium text-muted-foreground">Enabled</label>
                    <div className="flex items-center h-9">
                      <Toggle
                        checked={appData.replay_cleaner?.enabled ?? false}
                        onChange={(v) =>
                          setAppData((prev) => ({
                            ...prev,
                            replay_cleaner: { ...prev.replay_cleaner!, enabled: v },
                          }))
                        }
                      />
                    </div>
                  </div>
                  <div className="space-y-1">
                    <label className="text-[11px] font-medium text-muted-foreground">Cleanup Time</label>
                    <Input
                      value={appData.replay_cleaner?.cleanup_time ?? ''}
                      onChange={(e) =>
                        setAppData((prev) => ({
                          ...prev,
                          replay_cleaner: { ...prev.replay_cleaner!, cleanup_time: e.target.value },
                        }))
                      }
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-[11px] font-medium text-muted-foreground">Retention Days</label>
                    <Input
                      type="number"
                      value={appData.replay_cleaner?.retention_days ?? ''}
                      onChange={(e) =>
                        setAppData((prev) => ({
                          ...prev,
                          replay_cleaner: { ...prev.replay_cleaner!, retention_days: Number(e.target.value) },
                        }))
                      }
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-[11px] font-medium text-muted-foreground">Temp Retention</label>
                    <Input
                      type="number"
                      value={appData.replay_cleaner?.tmp_retention_days ?? ''}
                      onChange={(e) =>
                        setAppData((prev) => ({
                          ...prev,
                          replay_cleaner: { ...prev.replay_cleaner!, tmp_retention_days: Number(e.target.value) },
                        }))
                      }
                    />
                  </div>
                </div>
              </CardContent>
            </Card>

            {/* Discord */}
            <Card className="animate-fade-in">
              <CardHeader className="pb-2">
                <CardTitle className="flex items-center gap-2 text-muted-foreground">
                  <MessageCircle className="h-3.5 w-3.5" />
                  Discord
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1">
                    <label className="text-[11px] font-medium text-muted-foreground">Owner ID</label>
                    <Input
                      value={appData.discord?.owner_id ?? ''}
                      onChange={(e) =>
                        setAppData((prev) => ({
                          ...prev,
                          discord: { ...prev.discord!, owner_id: e.target.value },
                        }))
                      }
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-[11px] font-medium text-muted-foreground">Webhook URL</label>
                    <Input
                      value={appData.discord?.webhook_url ?? ''}
                      onChange={(e) =>
                        setAppData((prev) => ({
                          ...prev,
                          discord: { ...prev.discord!, webhook_url: e.target.value },
                        }))
                      }
                    />
                  </div>
                </div>
              </CardContent>
            </Card>

            {/* Security */}
            <Card className="animate-fade-in">
              <CardHeader className="pb-2">
                <CardTitle className="flex items-center gap-2 text-muted-foreground">
                  <Lock className="h-3.5 w-3.5" />
                  Security
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                  <div className="space-y-1">
                    <label className="text-[11px] font-medium text-muted-foreground">Rate Limit (RPS)</label>
                    <Input
                      type="number"
                      value={appData.security?.rate_limit_rps ?? ''}
                      onChange={(e) =>
                        setAppData((prev) => ({
                          ...prev,
                          security: { ...prev.security!, rate_limit_rps: Number(e.target.value) },
                        }))
                      }
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-[11px] font-medium text-muted-foreground">Auth Disabled</label>
                    <div className="flex items-center gap-2 h-9">
                      <Toggle
                        checked={appData.security?.auth_disabled ?? false}
                        onChange={(v) =>
                          setAppData((prev) => ({
                            ...prev,
                            security: { ...prev.security!, auth_disabled: v },
                          }))
                        }
                      />
                      <span className="text-[11px] text-muted-foreground">
                        {appData.security?.auth_disabled ? 'Local mode' : 'OAuth2'}
                      </span>
                    </div>
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>
        )}
      </div>
    </div>
  );
}
