import { useEffect, useRef, useState } from 'react';
import useSWR from 'swr';
import { Header } from '@/components/layout/Header';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Select } from '@/components/ui/select';
import { fetchLogs } from '@/api/endpoints';
import { Terminal, ArrowDown, Pause, Play } from 'lucide-react';
import { cn } from '@/lib/utils';
import type { LogEntry } from '@/types';

const levelColors: Record<string, string> = {
  error: 'text-destructive',
  warn: 'text-warning',
  warning: 'text-warning',
  info: 'text-info',
  debug: 'text-muted-foreground',
  trace: 'text-muted-foreground/50',
};

const levelBg: Record<string, string> = {
  error: 'bg-destructive/5',
  warn: 'bg-warning/5',
  warning: 'bg-warning/5',
};

export function Logs() {
  const [count, setCount] = useState(100);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [autoScroll, setAutoScroll] = useState(true);

  const { data } = useSWR(
    `logs-${count}`,
    fetchLogs(count),
    { refreshInterval: autoRefresh ? 3000 : 0 }
  );

  const entries: LogEntry[] = data?.entries ?? [];

  useEffect(() => {
    if (autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [entries, autoScroll]);

  function handleScroll() {
    if (!scrollRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = scrollRef.current;
    const atBottom = scrollHeight - scrollTop - clientHeight < 50;
    setAutoScroll(atBottom);
  }

  return (
    <div className="flex flex-col">
      <Header title="Logs" subtitle="Application log entries" />

      <div className="p-5">
        <Card className="flex flex-col">
          <CardHeader className="flex-row items-center justify-between space-y-0 pb-3">
            <CardTitle className="flex items-center gap-2">
              <Terminal className="h-4 w-4 text-muted-foreground" />
              Log Viewer
              <span className="ml-1 text-[11px] font-normal text-muted-foreground">
                {entries.length} entries
              </span>
            </CardTitle>
            <div className="flex items-center gap-1.5">
              <Select
                value={count.toString()}
                onChange={(e) => setCount(Number(e.target.value))}
                className="w-20 h-7 text-[11px]"
              >
                <option value="50">50</option>
                <option value="100">100</option>
                <option value="200">200</option>
                <option value="500">500</option>
              </Select>
              <Button
                variant={autoRefresh ? 'default' : 'outline'}
                size="sm"
                onClick={() => setAutoRefresh(!autoRefresh)}
                className="h-7"
              >
                {autoRefresh ? <Pause className="h-2.5 w-2.5" /> : <Play className="h-2.5 w-2.5" />}
                {autoRefresh ? 'Pause' : 'Resume'}
              </Button>
              {!autoScroll && (
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7"
                  onClick={() => {
                    setAutoScroll(true);
                    scrollRef.current?.scrollTo({
                      top: scrollRef.current.scrollHeight,
                      behavior: 'smooth',
                    });
                  }}
                >
                  <ArrowDown className="h-2.5 w-2.5" /> Bottom
                </Button>
              )}
            </div>
          </CardHeader>
          <CardContent>
            <div
              ref={scrollRef}
              onScroll={handleScroll}
              className="h-[calc(100vh-220px)] overflow-auto rounded-lg bg-surface border border-border/40 font-mono text-[11px] leading-relaxed"
            >
              {entries.length === 0 ? (
                <div className="flex flex-col items-center justify-center gap-2 py-16 text-muted-foreground">
                  <Terminal className="h-6 w-6 opacity-30" />
                  <p className="text-xs">No log entries</p>
                </div>
              ) : (
                <div className="p-3">
                  {entries.map((entry, idx) => (
                    <div
                      key={idx}
                      className={cn(
                        'flex gap-3 rounded px-2 py-0.5 hover:bg-accent/30 transition-colors',
                        levelBg[entry.level]
                      )}
                    >
                      <span className="shrink-0 text-muted-foreground/60 tabular-nums">
                        {entry.timestamp
                          ? new Date(entry.timestamp).toLocaleTimeString()
                          : '??:??:??'}
                      </span>
                      <span
                        className={cn(
                          'w-10 shrink-0 text-right font-semibold uppercase',
                          levelColors[entry.level] || 'text-foreground'
                        )}
                      >
                        {entry.level?.slice(0, 4)}
                      </span>
                      <span className="break-all text-foreground/90">{entry.message}</span>
                      {entry.fields && Object.keys(entry.fields).length > 0 && (
                        <span className="shrink-0 text-muted-foreground/50">
                          {Object.entries(entry.fields)
                            .map(([k, v]) => `${k}=${JSON.stringify(v)}`)
                            .join(' ')}
                        </span>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
