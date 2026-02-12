// Simple global toast notification state
type ToastType = 'success' | 'error' | 'info';

interface Toast {
  id: number;
  type: ToastType;
  message: string;
}

type Listener = (toasts: Toast[]) => void;

let toasts: Toast[] = [];
let nextId = 0;
const listeners = new Set<Listener>();

function notify() {
  listeners.forEach((fn) => fn([...toasts]));
}

export function addToast(type: ToastType, message: string, durationMs = 4000) {
  const id = nextId++;
  toasts = [...toasts, { id, type, message }];
  notify();
  setTimeout(() => {
    toasts = toasts.filter((t) => t.id !== id);
    notify();
  }, durationMs);
}

export function toast(message: string) { addToast('info', message); }
toast.success = (message: string) => addToast('success', message);
toast.error = (message: string) => addToast('error', message, 6000);

export function subscribe(fn: Listener): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

export function getToasts(): Toast[] {
  return toasts;
}

export type { Toast, ToastType };
