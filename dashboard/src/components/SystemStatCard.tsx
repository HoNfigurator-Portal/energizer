import { Card } from '@/components/ui/card';
import type { ReactNode } from 'react';
import { cn } from '@/lib/utils';

interface SystemStatCardProps {
  title: string;
  value: string | number;
  subtitle?: string;
  icon: ReactNode;
  color?: 'default' | 'success' | 'warning' | 'destructive' | 'primary';
  progress?: number; // 0-100
}

const colorMap = {
  default: {
    icon: 'bg-muted text-muted-foreground',
    ring: 'stroke-muted-foreground/30',
    ringActive: 'stroke-muted-foreground',
  },
  primary: {
    icon: 'bg-primary/10 text-primary',
    ring: 'stroke-primary/20',
    ringActive: 'stroke-primary',
  },
  success: {
    icon: 'bg-success/10 text-success',
    ring: 'stroke-success/20',
    ringActive: 'stroke-success',
  },
  warning: {
    icon: 'bg-warning/10 text-warning',
    ring: 'stroke-warning/20',
    ringActive: 'stroke-warning',
  },
  destructive: {
    icon: 'bg-destructive/10 text-destructive',
    ring: 'stroke-destructive/20',
    ringActive: 'stroke-destructive',
  },
};

export function SystemStatCard({
  title,
  value,
  subtitle,
  icon,
  color = 'default',
  progress,
}: SystemStatCardProps) {
  const c = colorMap[color];
  const circumference = 2 * Math.PI * 18;
  const offset = progress != null ? circumference * (1 - progress / 100) : circumference;

  return (
    <Card className="animate-fade-in">
      <div className="flex items-center gap-4 p-4">
        {/* Icon or Progress Ring */}
        {progress != null ? (
          <div className="relative flex h-11 w-11 shrink-0 items-center justify-center">
            <svg className="progress-ring h-11 w-11" viewBox="0 0 40 40">
              <circle
                className={c.ring}
                cx="20" cy="20" r="18"
                fill="none" strokeWidth="3"
              />
              <circle
                className={cn('progress-ring-circle', c.ringActive)}
                cx="20" cy="20" r="18"
                fill="none" strokeWidth="3"
                strokeLinecap="round"
                strokeDasharray={circumference}
                strokeDashoffset={offset}
              />
            </svg>
            <div className="absolute inset-0 flex items-center justify-center">
              <span className="text-[10px] font-bold">{Math.round(progress)}%</span>
            </div>
          </div>
        ) : (
          <div className={cn('flex h-10 w-10 shrink-0 items-center justify-center rounded-lg', c.icon)}>
            {icon}
          </div>
        )}

        {/* Text */}
        <div className="min-w-0 flex-1">
          <p className="text-[11px] font-medium text-muted-foreground uppercase tracking-wider">
            {title}
          </p>
          <p className="text-lg font-bold leading-tight tracking-tight">{value}</p>
          {subtitle && (
            <p className="text-[11px] text-muted-foreground truncate">{subtitle}</p>
          )}
        </div>
      </div>
    </Card>
  );
}
