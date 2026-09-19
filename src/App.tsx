import { useCallback, useEffect, useState } from 'react';
import { listen } from '@tauri-apps/api/event';
import type { AgentStatus, Printer, PrintJob } from '../sdk/src/types';
import { api, desktop, errorText, type Config } from './lib/agent';
import { ConnectionStatus } from './components/ConnectionStatus';
import { PrinterList } from './components/PrinterList';
import { Settings } from './components/Settings';
const emptyConfig: Config = {
  port: 8765,
  maxAttempts: 3,
  autostart: true,
  printers: [],
  routes: [],
};
const statusLabel = {
  queued: 'در صف',
  printing: 'در حال ارسال',
  completed: 'ارسال شد',
  failed: 'ناموفق',
  cancelled: 'لغوشده',
};
export default function App() {
  const [tab, setTab] = useState('printers');
  const [config, setConfig] = useState<Config>(emptyConfig);
  const [printers, setPrinters] = useState<Printer[]>([]);
  const [jobs, setJobs] = useState<PrintJob[]>([]);
  const [status, setStatus] = useState<AgentStatus | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const refresh = useCallback(async () => {
    if (!desktop) return;
    const [c, p, j, s] = await Promise.all([
      api.config(),
      api.printers(),
      api.queue(),
      api.status(),
    ]);
    setConfig(c);
    setPrinters(p);
    setJobs(j);
    setStatus(s);
  }, []);
  useEffect(() => {
    void refresh().catch((e) => setError(errorText(e)));
    if (!desktop) return;
    let disposed = false;
    let cleanup: (() => void)[] = [];
    let timer: ReturnType<typeof setTimeout> | undefined;
    void Promise.all([
      listen<string>('navigate', (e) => setTab(e.payload === 'open' ? 'printers' : e.payload)),
      listen('agent-event', () => {
        clearTimeout(timer);
        timer = setTimeout(() => void refresh().catch((e) => setError(errorText(e))), 150);
      }),
    ]).then((f) => {
      if (disposed) f.forEach((fn) => fn());
      else cleanup = f;
    });
    const initial = setTimeout(() => void refresh().catch((e) => setError(errorText(e))), 1200);
    return () => {
      disposed = true;
      cleanup.forEach((f) => f());
      clearTimeout(timer);
      clearTimeout(initial);
    };
  }, [refresh]);
  function run(f: () => Promise<void>) {
    if (busy) return;
    setBusy(true);
    setError('');
    void f()
      .then(refresh)
      .catch((e) => setError(errorText(e)))
      .finally(() => setBusy(false));
  }
  const save = async (c: Config) => {
    await api.save(c);
    setConfig(c);
  };
  const pending = jobs.filter((j) => j.status === 'queued' || j.status === 'printing').length;
  return (
    <div className="app">
      <aside>
        <div className="brand">
          <span className="brand-mark">M</span>
          <div>
            MenuVex<small>PRINTER AGENT</small>
          </div>
        </div>
        <div className="workspace-label">فضای چاپ شما</div>
        <nav>
          {[
            ['printers', '▤', 'پرینترها'],
            ['queue', '☷', 'صف چاپ'],
            ['settings', '⚙', 'تنظیمات'],
          ].map(([key, icon, label]) => (
            <button className={tab === key ? 'active' : ''} key={key} onClick={() => setTab(key)}>
              <span>{icon}</span>
              {label}
              {key === 'queue' && pending > 0 && <b>{pending}</b>}
            </button>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <div className="local-icon">⌁</div>
          <strong>محلی. امن. همیشه آماده.</strong>
          <p>
            چاپ مستقیم از MenuVex
            <br />
            بدون انتخاب مجدد دستگاه
          </p>
          <span className="mono">v1.0.0</span>
        </div>
      </aside>
      <main>
        <header>
          <span className="breadcrumb">
            MenuVex <span>/</span> مدیریت چاپ
          </span>
          <ConnectionStatus status={status} desktop={desktop} />
        </header>
        <div className="content">
          <div className="hero">
            <div>
              <p className="eyebrow">YOUR LOCAL PRINTING COMPANION</p>
              <h1>
                سفارش ثبت شود.
                <br />
                <span>فاکتور چاپ شود.</span>
              </h1>
              <p>ارتباط پایدار بین صندوق شما و پرینترهای کافه.</p>
            </div>
            <div className="connection-diagram">
              <span>MenuVex PWA</span>
              <i>↓</i>
              <strong>
                Print Agent <span className="dot" />
              </strong>
              <i>↓</i>
              <span>USB / LAN Printer</span>
            </div>
          </div>
          {!desktop && (
            <div className="notice">
              <strong>این صفحه فقط رابط Agent است.</strong> اتصال به سخت‌افزار و keychain در مرورگر
              در دسترس نیست. برای استفاده واقعی برنامه Tauri را اجرا کنید؛ هیچ پرینتر یا job ساختگی
              نمایش داده نمی‌شود.
            </div>
          )}
          {error && (
            <div role="alert" className="notice error">
              {error}
              <button className="subtle" onClick={() => setError('')}>
                ×
              </button>
            </div>
          )}
          {status?.serverError && (
            <div role="alert" className="notice error" dir="ltr">
              {status.serverError}
            </div>
          )}
          <div className="summary">
            <div>
              <span>پرینترهای تنظیم‌شده</span>
              <strong>{printers.length.toLocaleString('fa-IR')}</strong>
            </div>
            <div>
              <span>در انتظار ارسال</span>
              <strong>{pending.toLocaleString('fa-IR')}</strong>
            </div>
            <div>
              <span>اتصال محلی</span>
              <strong className="mono small">127.0.0.1:{status?.port ?? config.port}</strong>
            </div>
          </div>
          {tab === 'printers' && (
            <PrinterList
              printers={printers}
              config={config}
              save={save}
              disabled={!desktop || busy}
              run={run}
            />
          )}
          {tab === 'settings' && (
            <Settings config={config} save={save} run={run} disabled={!desktop || busy} />
          )}
          {tab === 'queue' && (
            <section>
              <div className="section-heading">
                <div>
                  <p className="eyebrow">PRINT QUEUE</p>
                  <h2>صف چاپ</h2>
                </div>
                <button
                  className="secondary"
                  disabled={!desktop || busy}
                  onClick={() => run(refresh)}
                >
                  به‌روزرسانی
                </button>
              </div>
              <p className="muted">
                ۵۰۰ job اخیر · «ارسال شد» تأیید چاپ فیزیکی نیست. نتیجه نامشخص را قبل از چاپ مجدد
                بررسی کنید.
              </p>
              {jobs.length === 0 ? (
                <div className="empty">
                  <span className="empty-icon">☷</span>
                  <h3>هنوز job چاپی وجود ندارد</h3>
                  <p>درخواست‌های چاپ و نتیجه ارسال آن‌ها اینجا نمایش داده می‌شود.</p>
                </div>
              ) : (
                <div className="table-wrap">
                  <table>
                    <thead>
                      <tr>
                        <th>شناسه</th>
                        <th>پرینتر</th>
                        <th>وضعیت</th>
                        <th>تلاش</th>
                        <th />
                      </tr>
                    </thead>
                    <tbody>
                      {jobs.map((j) => (
                        <tr key={j.jobId}>
                          <td>
                            <span className="mono">{j.jobId}</span>
                            <small>{new Date(j.createdAt * 1000).toLocaleString('fa-IR')}</small>
                            {j.error && (
                              <small className="error-text">
                                {j.error.code} — {j.error.message}
                              </small>
                            )}
                          </td>
                          <td>{printers.find((p) => p.id === j.printerId)?.name ?? j.printerId}</td>
                          <td>
                            <span
                              className={`tag ${j.status === 'completed' ? 'online' : j.status === 'failed' ? 'offline' : ''}`}
                            >
                              {statusLabel[j.status]}
                            </span>
                          </td>
                          <td>{j.attempts}</td>
                          <td>
                            {j.status === 'queued' && (
                              <button
                                disabled={busy}
                                className="subtle"
                                onClick={() =>
                                  run(async () => {
                                    await api.cancel(j.jobId);
                                  })
                                }
                              >
                                لغو
                              </button>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </section>
          )}
          <footer>
            <span>تمام ارتباط سخت‌افزاری روی همین دستگاه انجام می‌شود.</span>
            <span className="mono">ESC/POS · USB · TCP/9100</span>
          </footer>
        </div>
      </main>
    </div>
  );
}
