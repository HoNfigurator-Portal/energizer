import { NavLink } from 'react-router-dom';
import {
  LayoutDashboard,
  Server,
  ScrollText,
  Settings,
} from 'lucide-react';
import { cn } from '@/lib/utils';

const navItems = [
  { to: '/', icon: LayoutDashboard, label: 'Overview' },
  { to: '/servers', icon: Server, label: 'Servers' },
  { to: '/logs', icon: ScrollText, label: 'Logs' },
  { to: '/config', icon: Settings, label: 'Settings' },
];

export function Sidebar() {
  return (
    <aside className="fixed inset-y-0 left-0 z-50 flex w-[220px] flex-col border-r border-border/50 bg-card">
      {/* Brand */}
      <div className="flex h-14 items-center gap-2.5 px-5">
        <img
          src="/logo/logo.png"
          alt="Energizer"
          className="h-8 w-8 shrink-0 rounded-lg object-contain"
        />
        <div className="flex flex-col">
          <span className="text-sm font-bold tracking-tight text-foreground">Energizer</span>
          <span className="text-[10px] leading-none text-muted-foreground">Server Manager</span>
        </div>
      </div>

      {/* Divider */}
      <div className="mx-4 border-t border-border/50" />

      {/* Navigation */}
      <nav className="flex-1 space-y-0.5 p-3">
        {navItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.to === '/'}
            className={({ isActive }) =>
              cn(
                'group flex items-center gap-2.5 rounded-lg px-3 py-2 text-[13px] font-medium transition-all duration-150',
                isActive
                  ? 'bg-primary/10 text-primary shadow-sm'
                  : 'text-muted-foreground hover:bg-accent hover:text-foreground'
              )
            }
          >
            {({ isActive }) => (
              <>
                <item.icon
                  className={cn(
                    'h-4 w-4 transition-colors',
                    isActive ? 'text-primary' : 'text-muted-foreground group-hover:text-foreground'
                  )}
                />
                {item.label}
                {isActive && (
                  <div className="ml-auto h-1.5 w-1.5 rounded-full bg-primary" />
                )}
              </>
            )}
          </NavLink>
        ))}
      </nav>

      {/* Footer */}
      <div className="border-t border-border/50 px-5 py-3">
        <div className="flex items-center gap-2">
          <div className="h-1.5 w-1.5 rounded-full bg-success animate-pulse-glow" />
          <span className="text-[11px] text-muted-foreground">System Online</span>
        </div>
      </div>
    </aside>
  );
}
