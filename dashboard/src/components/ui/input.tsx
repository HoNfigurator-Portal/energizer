import { cn } from '@/lib/utils';
import type { InputHTMLAttributes } from 'react';

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={cn(
        'flex h-9 w-full rounded-lg border border-input bg-surface/50 px-3 py-1.5 text-sm',
        'placeholder:text-muted-foreground/60',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:border-primary/40',
        'disabled:cursor-not-allowed disabled:opacity-50',
        'transition-all duration-150',
        className
      )}
      {...props}
    />
  );
}
