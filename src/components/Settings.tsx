import { useEffect, useState } from 'react';
import { api, call, desktop, errorText, type Config, type PrinterConfig, type RawPrinterInfo, type RawPrinterSettings } from '../lib/agent';

type QueueInspection = { info: RawPrinterInfo } | { error: string };
const RAW_HARD_LIMIT = 1024 * 1024;
const DEFAULT_RAW_PRINTER_SETTINGS: RawPrinterSettings = {
  raw_target: null,
  force_raw: false,
  created_generic: false,
};

export function Settings({
  config,
  save,
  run,
  disabled,
}: {
  config: Config;
  save: (c: Config) => Promise<void>;
  run: (f: () => Promise<void>) => void;
  disabled: boolean;
}) {
  const [draft, setDraft] = useState(config);
  const [secret, setSecret] = useState('');
  const [platform, setPlatform] = useState('unknown');
  const [installedQueues, setInstalledQueues] = useState<{ queueName: string }[]>([]);
  const [sources, setSources] = useState<Record<string, string>>({});
  const [inspections, setInspections] = useState<Record<string, QueueInspection>>({});
  const [loadingQueues, setLoadingQueues] = useState(false);
  const [queueError, setQueueError] = useState('');

  useEffect(() => setDraft(config), [config]);
  useEffect(() => {
    if (!desktop) {
      setPlatform('browser');
      return;
    }
    let active = true;
    void api.rawPlatform().then((value) => active && setPlatform(value));
    void api.installed().then((queues) => active && setInstalledQueues(queues)).catch(() => {});
    return () => {
      active = false;
    };
  }, []);
  useEffect(() => {
    if (!secret) return;
    const timer = setTimeout(() => setSecret(''), 60000);
    return () => clearTimeout(timer);
  }, [secret]);
  useEffect(() => {
    setSources((previous) => {
      const next: Record<string, string> = {};
      for (const printer of draft.printers) {
        const prior = previous[printer.id];
        next[printer.id] = prior ?? (printer.connection.type === 'spooler' ? printer.connection.queueName : '');
      }
      return Object.keys(next).length === Object.keys(previous).length &&
        Object.keys(next).every((key) => next[key] === previous[key])
        ? previous
        : next;
    });
  }, [draft.printers]);

  const inspectTargets = Array.from(new Set(
    draft.printers
      .map((printer) => draft.raw_passthrough.printers[printer.id]?.raw_target?.trim() || (printer.connection.type === 'spooler'
        ? printer.connection.queueName
        : ''))
      .filter(Boolean),
  ));
  const inspectKey = inspectTargets.join('\u0000');
  useEffect(() => {
    if (!desktop || !inspectKey) {
      setInspections({});
      return;
    }
    let active = true;
    setInspections({});
    const targets = inspectKey.split('\u0000');
    void Promise.all(targets.map(async (queueName) => {
      try {
        return [queueName, { info: await api.rawPrinterInfo(queueName) }] as const;
      } catch (error) {
        return [queueName, { error: errorText(error) }] as const;
      }
    })).then((entries) => {
      if (active) setInspections(Object.fromEntries(entries));
    });
    return () => {
      active = false;
    };
  }, [desktop, inspectKey]);

  const updatePrinter = (id: string, patch: Partial<PrinterConfig>) =>
    setDraft((current) => ({
      ...current,
      printers: current.printers.map((printer) => printer.id === id ? { ...printer, ...patch } : printer),
    }));

  const updateRawPrinter = (id: string, patch: Partial<RawPrinterSettings>) =>
    setDraft((current) => {
      const existing = current.raw_passthrough.printers[id] ?? DEFAULT_RAW_PRINTER_SETTINGS;
      return {
        ...current,
        raw_passthrough: {
          ...current.raw_passthrough,
          printers: {
            ...current.raw_passthrough.printers,
            [id]: { ...existing, ...patch },
          },
        },
      };
    });

  const loadInstalledQueues = async () => {
    setLoadingQueues(true);
    setQueueError('');
    try {
      setInstalledQueues(await api.installed());
    } catch (error) {
      setQueueError(errorText(error));
    } finally {
      setLoadingQueues(false);
    }
  };

  const rawStatus = (printer: PrinterConfig) => {
    const enabled = draft.raw_passthrough.enabled && (printer.rawPassthrough ?? false);
    const queueName = draft.raw_passthrough.printers[printer.id]?.raw_target?.trim() || (printer.connection.type === 'spooler'
      ? printer.connection.queueName
      : '');
    const inspection = queueName ? inspections[queueName] : undefined;
    if (!enabled) return { className: 'offline', label: 'خاموش', detail: 'برای این چاپگر فعال نیست.' };
    if (!queueName && printer.connection.type === 'network') {
      return { className: 'online', label: 'فعال · TCP', detail: 'بایت‌ها مستقیماً به پورت شبکه ارسال می‌شوند؛ درایور صفی در مسیر نیست.' };
    }
    if (!queueName && printer.connection.type === 'usb') {
      return { className: 'warning', label: 'نیازمند صف RAW', detail: 'برای این پروفایل USB صف چاپ RAW انتخاب نشده است.' };
    }
    if (!inspection) return { className: 'unknown', label: 'در حال بررسی', detail: 'درایور و پورت صف محلی در حال خواندن است…' };
    if ('error' in inspection) return { className: 'offline', label: 'صف ناموجود', detail: inspection.error };
    if (inspection.info.platform !== 'windows') {
      return { className: 'online', label: 'CUPS RAW', detail: inspection.info.driverName ?? 'صف RAW سیستم‌عامل' };
    }
    if (inspection.info.isGenericTextOnly) {
      return { className: 'online', label: 'Generic / Text Only', detail: 'درایور مناسب برای صف RAW.' };
    }
    return {
      className: 'warning',
      label: 'درایور غیر Generic',
      detail: `درایور انتخاب‌شده: ${inspection.info.driverName ?? 'نامشخص'}؛ ممکن است داده را تفسیر یا تغییر دهد.`,
    };
  };

  return (
    <section>
      <p className="eyebrow">SETTINGS</p>
      <h2>یک‌بار تنظیم، همیشه آماده</h2>
      <form
        className="panel"
        onSubmit={(e) => {
          e.preventDefault();
          run(() => save(draft));
        }}
      >
        <div className="form-grid">
          <label>
            پورت اتصال محلی
            <input
              type="number"
              required
              min={1024}
              max={65535}
              value={draft.port}
              onChange={(e) => setDraft({ ...draft, port: Number(e.target.value) })}
            />
          </label>
          <label>
            حداکثر تلاش قبل از ارسال
            <input
              type="number"
              required
              min={1}
              max={5}
              value={draft.maxAttempts}
              onChange={(e) => setDraft({ ...draft, maxAttempts: Number(e.target.value) })}
            />
          </label>
        </div>
        <label className="check">
          <input
            type="checkbox"
            checked={draft.autostart}
            onChange={(e) => setDraft({ ...draft, autostart: e.target.checked })}
          />
          اجرا پس از ورود به سیستم
        </label>
        <p className="muted">
          پس از تغییر پورت از منوی tray، Restart Agent را انتخاب و پورت SDK را هم تغییر دهید.
        </p>
        <h3>مسیرهای چاپ</h3>
        {['invoice', 'kitchen', 'bar'].map((role) => {
          const route = draft.routes.find((r) => r.role === role);
          return (
            <div className="route" key={role}>
              <label>
                {{ invoice: 'صندوق', kitchen: 'آشپزخانه', bar: 'بار' }[role]}
                <select
                  value={route?.printerId ?? ''}
                  onChange={(e) =>
                    setDraft({
                      ...draft,
                      routes: [
                        ...draft.routes.filter((r) => r.role !== role),
                        ...(e.target.value
                          ? [{ role, printerId: e.target.value, autoPrint: route?.autoPrint ?? false }]
                          : []),
                      ],
                    })
                  }
                >
                  <option value="">انتخاب نشده</option>
                  {draft.printers.map((printer) => (
                    <option value={printer.id} key={printer.id}>
                      {printer.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="check">
                <input
                  type="checkbox"
                  disabled={!route}
                  checked={route?.autoPrint ?? false}
                  onChange={(e) =>
                    setDraft({
                      ...draft,
                      routes: draft.routes.map((item) =>
                        item.role === role ? { ...item, autoPrint: e.target.checked } : item,
                      ),
                    })
                  }
                />
                چاپ خودکار در PWA
              </label>
            </div>
          );
        })}

        <div className="raw-settings">
          <div className="section-heading">
            <div>
              <p className="eyebrow">LOCAL OPERATOR CONTROL</p>
              <h3>چاپ مستقیم ESC/POS</h3>
            </div>
            <span className={`tag ${draft.raw_passthrough.enabled ? 'warning' : 'offline'}`}>
              {draft.raw_passthrough.enabled ? 'دروازه کلی روشن' : 'پیش‌فرض خاموش'}
            </span>
          </div>
          <div className="notice">
            ESC/POS خام از رندر فاکتور عبور نمی‌کند. در صف ویندوز، RAW از GDI rendering جلوگیری
            می‌کند اما تضمین نمی‌کند درایور فروشنده یا افزونه‌های spooler بایت‌ها را تغییر ندهند.
            درایور <strong>Generic / Text Only</strong> توصیه می‌شود. درایور فعلی هرگز تعویض یا
            ویرایش نمی‌شود؛ ساخت صف دوم ممکن است فقط برای همان عملیات به دسترسی مدیر نیاز داشته باشد.
          </div>
          <label className="check raw-global-toggle">
            <input
              type="checkbox"
              disabled={!desktop}
              checked={draft.raw_passthrough.enabled}
              onChange={(e) => setDraft({
                ...draft,
                raw_passthrough: { ...draft.raw_passthrough, enabled: e.target.checked },
              })}
            />
            اجازه کلی برای اسناد ESC/POS خام (فقط تنظیم محلی؛ وب‌سایت نمی‌تواند روشنش کند)
          </label>
          <label className="raw-limit">
            بیشترین اندازه سند خام (بایت؛ حداکثر ۱ MiB)
            <input
              dir="ltr"
              type="number"
              disabled={!desktop}
              min={1024}
              max={RAW_HARD_LIMIT}
              step={1024}
              required
              value={draft.raw_passthrough.max_bytes}
              onChange={(e) => setDraft({
                ...draft,
                raw_passthrough: { ...draft.raw_passthrough, max_bytes: Number(e.target.value) },
              })}
            />
          </label>
          <div className="section-heading raw-queue-heading">
            <div>
              <h4>تنظیم جداگانه هر چاپگر</h4>
              <p className="muted">
                تغییرات تا زدن «ذخیره تنظیمات» اعمال نمی‌شوند؛ هر دو کلید کلی و چاپگر باید روشن باشند.
              </p>
            </div>
            <button type="button" className="secondary" disabled={!desktop || loadingQueues} onClick={() => void loadInstalledQueues()}>
              {loadingQueues ? 'در حال خواندن صف‌ها…' : 'بارگذاری صف‌های نصب‌شده'}
            </button>
          </div>
          {queueError && <p className="error-text" role="alert">{queueError}</p>}
          <div className="table-wrap raw-table-wrap">
            <table>
              <thead>
                <tr>
                  <th>چاپگر</th>
                  <th>فعال‌سازی</th>
                  <th>صف RAW مقصد</th>
                  <th>وضعیت / درایور</th>
                  <th>عملیات محلی</th>
                </tr>
              </thead>
              <tbody>
                {draft.printers.length === 0 ? (
                  <tr><td colSpan={5}>ابتدا یک پروفایل چاپگر اضافه کنید.</td></tr>
                ) : draft.printers.map((printer) => {
                  const status = rawStatus(printer);
                  const rawSettings = draft.raw_passthrough.printers[printer.id] ?? DEFAULT_RAW_PRINTER_SETTINGS;
                  const selectedSource = sources[printer.id] ?? '';
                  const inspectionQueue = rawSettings.raw_target?.trim() || (printer.connection.type === 'spooler'
                    ? printer.connection.queueName
                    : '');
                  const inspection = inspectionQueue ? inspections[inspectionQueue] : undefined;
                  const driverDetail = inspection && 'info' in inspection
                    ? `${inspection.info.driverName ?? 'درایور نامشخص'}${inspection.info.portName ? ` · پورت ${inspection.info.portName}` : ''}`
                    : inspection && 'error' in inspection
                      ? inspection.error
                      : printer.connection.type === 'network'
                        ? 'ارسال مستقیم TCP'
                        : 'صف RAW انتخاب نشده';
                  const queueChoices = Array.from(new Set([
                    ...installedQueues.map((queue) => queue.queueName),
                    ...(rawSettings.raw_target ? [rawSettings.raw_target] : []),
                    ...(printer.connection.type === 'spooler' ? [printer.connection.queueName] : []),
                  ]));
                  const sourceChoices = Array.from(new Set([
                    ...installedQueues.map((queue) => queue.queueName),
                    ...(printer.connection.type === 'spooler' ? [printer.connection.queueName] : []),
                  ]));
                  const canCreate = platform === 'windows' && Boolean(selectedSource);
                  const isSpoolerRaw = platform === 'windows' && Boolean(inspectionQueue);
                  return (
                    <tr key={printer.id}>
                      <td>
                        <strong>{printer.name || 'بدون نام'}</strong>
                        {rawSettings.created_generic && <small>صف Generic توسط MenuVex ساخته شده است.</small>}
                      </td>
                      <td>
                        <label className="check raw-printer-toggle">
                          <input
                            type="checkbox"
                            disabled={!desktop}
                            checked={printer.rawPassthrough ?? false}
                            onChange={(e) => updatePrinter(printer.id, { rawPassthrough: e.target.checked })}
                          />
                          ESC/POS خام
                        </label>
                        <span className={`tag ${status.className}`}>{status.label}</span>
                      </td>
                      <td className="raw-target-cell">
                        <label>
                          صفی که بایت‌ها به آن ارسال شوند
                          <select
                            dir="ltr"
                            disabled={!desktop}
                            value={rawSettings.raw_target ?? ''}
                            onChange={(e) => updateRawPrinter(printer.id, {
                              raw_target: e.target.value || null,
                              ...(e.target.value !== rawSettings.raw_target ? { created_generic: false } : {}),
                            })}
                          >
                            <option value="">استفاده از مسیر اصلی پروفایل</option>
                            {queueChoices.map((queue) => <option dir="ltr" value={queue} key={queue}>{queue}</option>)}
                          </select>
                        </label>
                        {platform === 'windows' && (
                          <label className="raw-source-select">
                            صف فروشنده برای شناسایی پورت USB
                            <select
                              dir="ltr"
                              disabled={!desktop}
                              value={selectedSource}
                              onChange={(e) => setSources((current) => ({ ...current, [printer.id]: e.target.value }))}
                            >
                              <option value="">انتخاب دستی صف فروشنده</option>
                              {sourceChoices.map((queue) => <option dir="ltr" value={queue} key={queue}>{queue}</option>)}
                            </select>
                          </label>
                        )}
                      </td>
                      <td>
                        <span className={`tag ${status.className}`}>{status.label}</span>
                        <small>{driverDetail}</small>
                        <small>{status.detail}</small>
                        {isSpoolerRaw && (
                          <label className="check raw-force-toggle">
                            <input
                              type="checkbox"
                              disabled={!desktop}
                              checked={rawSettings.force_raw}
                              onChange={(e) => updateRawPrinter(printer.id, { force_raw: e.target.checked })}
                            />
                            Force RAW هنگام OpenPrinter
                          </label>
                        )}
                      </td>
                      <td>
                        {platform === 'windows' && (
                          <button
                            type="button"
                            className="secondary raw-action"
                            disabled={disabled || !canCreate}
                            title={!selectedSource ? 'ابتدا صف چاپ فروشنده را انتخاب کنید.' : 'یک صف جداگانه می‌سازد و درایور موجود را تغییر نمی‌دهد.'}
                            onClick={() => run(async () => {
                              const target = await api.createGenericRawTarget(printer.id, selectedSource);
                              const next: Config = {
                                ...draft,
                                raw_passthrough: {
                                  ...draft.raw_passthrough,
                                  printers: {
                                    ...draft.raw_passthrough.printers,
                                    [printer.id]: {
                                      ...rawSettings,
                                      raw_target: target.queueName,
                                      created_generic: rawSettings.created_generic || target.created,
                                    },
                                  },
                                },
                              };
                              await save(next);
                            })}
                          >
                            ساخت صف Generic / Text Only
                          </button>
                        )}
                        <button
                          type="button"
                          className="secondary raw-action"
                          disabled={!desktop || disabled || !draft.raw_passthrough.enabled || !(printer.rawPassthrough ?? false)}
                          title="تنظیمات را ابتدا ذخیره کنید؛ چاپ آزمایشی یک متن کوتاه ESC/POS می‌فرستد."
                          onClick={() => run(async () => { await api.rawTest(printer.id); })}
                        >
                          چاپ آزمایشی RAW
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          {!desktop && <p className="muted">تنظیمات RAW فقط از پنجره محلی برنامه دسکتاپ قابل تغییر است.</p>}
          <p className="muted">
            درگاه شبکه به‌صورت پیش‌فرض خاموش است. وب‌سایت نمی‌تواند آن یا کلید هر چاپگر را فعال کند.
            اندازه‌یابی امضا و محدودیت بایت در Agent دوباره بررسی می‌شود؛ سند نامعتبر هرگز به رندر فاکتور برنمی‌گردد.
          </p>
        </div>

        <button disabled={disabled}>ذخیره تنظیمات</button>
      </form>
      <div className="panel">
        <h3>اتصال امن به MenuVex</h3>
        <p>
          کلید را فقط در صفحه pairing سایت رسمی MenuVex وارد کنید. این کلید دسترسی چاپ می‌دهد؛ آن را
          در پیام‌رسان یا گزارش خطا ارسال نکنید.
        </p>
        <div className="actions">
          <button
            className="secondary"
            disabled={disabled}
            onClick={() => run(async () => setSecret(await call<string>('pairing_secret')))}
          >
            نمایش کلید اتصال برای ۶۰ ثانیه
          </button>
          <button
            className="danger"
            disabled={disabled}
            onClick={() => {
              if (confirm('دسترسی همه مرورگرهای جفت‌شده لغو شود؟'))
                run(async () => {
                  await call('rotate_secret');
                  setSecret('');
                });
            }}
          >
            لغو دسترسی مرورگرها
          </button>
        </div>
        {secret && (
          <pre className="secret" dir="ltr">
            {secret}
          </pre>
        )}
        <p className="muted">
          Secret در keychain سیستم است. مرورگر کلید امضای non-extractable را در IndexedDB ذخیره
          می‌کند. هر پروفایل مرورگر یک‌بار pairing می‌شود.
        </p>
      </div>
    </section>
  );
}
