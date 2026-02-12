import { useEffect, useState } from 'react';
import { subscribe, type Toast } from '@/lib/toast';
import { cn } from '@/lib/utils';
import { CheckCircle2, XCircle, Info } from 'lucide-react';

const icons = {
  success: CheckCircle2,
  error: XCircle,
  info: Info,
};

const styles = {
  success: 'border-success/20 bg-success/5 text-success',
  error: 'border-destructive/20 bg-destructive/5 text-destructive',
  info: 'border-primary/20 bg-primary/5 text-primary',
};

export function ToastContainer() {
  const [toasts, setToasts] = useState<Toast[]>([]);

  useEffect(() => {
    return subscribe(setToasts);
  }, []);

  if (toasts.length === 0) return null;

  return (
    <div className="fixed bottom-5 right-5 z-[100] flex flex-col gap-2 max-w-sm">
      {toasts.map((t) => {
        const Icon = icons[t.type];
        return (
          <div
            key={t.id}
            className={cn(
              'flex items-center gap-2.5 rounded-lg border px-4 py-2.5 text-xs font-medium shadow-lg animate-slide-right',
              'backdrop-blur-md',
              styles[t.type]
            )}
          >
            <Icon className="h-3.5 w-3.5 shrink-0" />
            <span className="leading-snug">{t.message}</span>
          </div>
        );
      })}
    </div>
  );
}
