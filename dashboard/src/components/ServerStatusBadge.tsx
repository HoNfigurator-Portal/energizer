import { cn } from '@/lib/utils';
import type { GameStatus } from '@/types';

interface StatusConfig {
  label: string;
  color: string;
  dotClass: string;
  pulse: boolean;
}

const statusConfig: Record<string, StatusConfig> = {
  ready:    { label: 'Ready',    color: 'text-success bg-success/10', dotClass: 'bg-success', pulse: true },
  occupied: { label: 'In Game',  color: 'text-primary bg-primary/10', dotClass: 'bg-primary', pulse: true },
  starting: { label: 'Starting', color: 'text-warning bg-warning/10', dotClass: 'bg-warning', pulse: true },
  queued:   { label: 'Queued',   color: 'text-warning bg-warning/10', dotClass: 'bg-warning', pulse: false },
  sleeping: { label: 'Sleeping', color: 'text-muted-foreground bg-muted', dotClass: 'bg-muted-foreground', pulse: false },
  stopped:  { label: 'Stopped',  color: 'text-muted-foreground bg-muted', dotClass: 'bg-muted-foreground/50', pulse: false },
  unknown:  { label: 'Unknown',  color: 'text-muted-foreground bg-muted', dotClass: 'bg-muted-foreground/50', pulse: false },
};

// Go's GameStatus iota values → string keys
// This handles the case where the API returns numeric status (int) instead of string.
const statusFromNumber: Record<number, string> = {
  0: 'unknown',
  1: 'queued',
  2: 'starting',
  3: 'ready',
  4: 'occupied',
  5: 'sleeping',
  6: 'stopped',
};

// Go's GamePhase iota values → string keys
export const phaseFromNumber: Record<number, string> = {
  0: 'idle',
  1: 'in_lobby',
  2: 'banning',
  3: 'picking',
  4: 'loading',
  5: 'preparation',
  6: 'match_started',
  7: 'game_ending',
  8: 'game_ended',
};

/** Normalize a status value (string or number) to a known status string key. */
export function normalizeStatus(status: unknown): string {
  if (typeof status === 'string' && status in statusConfig) return status;
  if (typeof status === 'number') return statusFromNumber[status] ?? 'unknown';
  return 'unknown';
}

/** Normalize a phase value (string or number) to a known phase string key. */
export function normalizePhase(phase: unknown): string {
  if (typeof phase === 'string' && phase.length > 0) return phase;
  if (typeof phase === 'number') return phaseFromNumber[phase] ?? 'idle';
  return 'idle';
}

export function ServerStatusBadge({ status }: { status: GameStatus | number }) {
  const key = normalizeStatus(status);
  const cfg = statusConfig[key] || statusConfig.unknown;

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-[11px] font-semibold',
        cfg.color
      )}
    >
      <span
        className={cn(
          'status-dot',
          cfg.dotClass,
          cfg.pulse && 'status-dot-pulse'
        )}
      />
      {cfg.label}
    </span>
  );
}
