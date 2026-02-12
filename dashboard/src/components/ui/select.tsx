import { cn } from '@/lib/utils';
import type { SelectHTMLAttributes } from 'react';

export function Select({ className, children, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={cn(
        'flex h-9 w-full rounded-lg border border-input bg-surface/50 px-3 py-1.5 text-sm',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:border-primary/40',
        'disabled:cursor-not-allowed disabled:opacity-50',
        'transition-all duration-150',
        className
      )}
      {...props}
    >
      {children}
    </select>
  );
}
