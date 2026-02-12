import { BrowserRouter, Routes, Route, Outlet } from 'react-router-dom';
import { Sidebar } from '@/components/layout/Sidebar';
import { ToastContainer } from '@/components/ToastContainer';
import { Overview } from '@/pages/Overview';
import { Servers } from '@/pages/Servers';
import { ServerDetail } from '@/pages/ServerDetail';
import { Logs } from '@/pages/Logs';
import { Config } from '@/pages/Config';
import { useTheme } from '@/lib/theme';

function Layout() {
  useTheme();

  return (
    <div className="flex min-h-screen bg-background">
      <Sidebar />
      <main className="relative flex-1 pl-[220px]">
        {/* Background image with dark overlay */}
        <div
          className="pointer-events-none fixed inset-0 left-[220px] z-0 bg-cover bg-center bg-no-repeat"
          style={{ backgroundImage: 'url(/bg/bg.png)' }}
        >
          <div className="absolute inset-0 bg-background/85 backdrop-blur-sm dark:bg-background/90" />
        </div>
        {/* Page content above the background */}
        <div className="relative z-10">
          <Outlet />
        </div>
      </main>
      <ToastContainer />
    </div>
  );
}

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/" element={<Overview />} />
          <Route path="/servers" element={<Servers />} />
          <Route path="/servers/:port" element={<ServerDetail />} />
          <Route path="/logs" element={<Logs />} />
          <Route path="/config" element={<Config />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
