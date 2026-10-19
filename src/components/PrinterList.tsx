import { useState } from 'react';
import type { Printer } from '../../sdk/src/types';
import { api, errorText, type Config, type PrinterConfig, type UsbDevice } from '../lib/agent';
import { PrinterCard } from './PrinterCard';

interface Props {
  printers: Printer[];
  config: Config;
  disabled: boolean;
  run: (action: () => Promise<void>) => void;
  save: (c: Config) => Promise<void>;
}

export function PrinterList({ printers, config, disabled, run, save }: Props) {
  const [devices, setDevices] = useState<UsbDevice[]>([]);
  const [network, setNetwork] = useState<{ host: string; port: number }[] | null>(null);
  const [queues, setQueues] = useState<{ queueName: string }[] | null>(null);
  const [fallbackQueues, setFallbackQueues] = useState<{ queueName: string }[] | null>(null);
  const [edit, setEdit] = useState<PrinterConfig | null>(null);
  const [scanning, setScanning] = useState(false);
  const [scanError, setScanError] = useState('');

  const fresh = (connection: PrinterConfig['connection'], defaultName = 'صندوق فروش'): PrinterConfig => ({
    id: `printer:${crypto.randomUUID()}`,
    name: defaultName,
    connection,
    paperMm: 80,
    widthDots: 576,
    copies: 1,
    cut: true,
    fontFamily: 'Noto Sans Arabic',
    fontSize: 24,
    rawPassthrough: false,
    usbFallbackTarget: null,
  });

  const update = <K extends keyof PrinterConfig>(key: K, value: PrinterConfig[K]) =>
    setEdit((e) => (e ? { ...e, [key]: value } : e));

  const setUsbFallback = (target: PrinterConfig['usbFallbackTarget']) =>
    setEdit((e) => (e ? { ...e, usbFallbackTarget: target } : e));

  const fallbackQueueName =
    edit?.usbFallbackTarget?.type === 'spooler' ? edit.usbFallbackTarget.queueName : '';

  const discoverAllPrinters = () => {
    setScanning(true);
    setScanError('');
    Promise.allSettled([api.discover(), api.installed(), api.discoverNetwork()])
      .then(([usbRes, queueRes, netRes]) => {
        if (usbRes.status === 'fulfilled') setDevices(usbRes.value);
        if (queueRes.status === 'fulfilled') setQueues(queueRes.value);
        if (netRes.status === 'fulfilled') setNetwork(netRes.value);
        const errs = [usbRes, queueRes, netRes]
          .filter((r): r is PromiseRejectedResult => r.status === 'rejected')
          .map((r) => errorText(r.reason));
        if (errs.length === 3) {
          setScanError(errs[0]);
        }
      })
      .finally(() => setScanning(false));
  };

  return (
    <section>
      <div className="section-heading">
        <div>
          <p className="eyebrow">PRINTERS</p>
          <h2>
            ۱. لیست پرینترهای شما <small>{printers.length}</small>
          </h2>
        </div>
        <div className="actions">
          <button
            disabled={disabled || scanning}
            onClick={discoverAllPrinters}
          >
            {scanning ? 'در حال جستجوی USB و LAN…' : 'جستجوی خودکار همه (USB و LAN)'}
          </button>
          <button
            className="secondary"
            disabled={disabled}
            onClick={() =>
              run(async () => {
                setQueues(await api.installed());
              })
            }
          >
            پرینترهای نصب‌شده
          </button>
          <button
            className="secondary"
            disabled={disabled}
            onClick={() =>
              run(async () => {
                setNetwork(await api.discoverNetwork());
              })
            }
          >
            جستجوی شبکه (LAN)
          </button>
          <button
            className="secondary"
            disabled={disabled || scanning}
            onClick={() => {
              setScanning(true);
              setScanError('');
              setDevices([]);
              api
                .discover()
                .then(setDevices)
                .catch((e) => setScanError(errorText(e)))
                .finally(() => setScanning(false));
            }}
          >
            {scanning ? 'در حال جستجو…' : 'جستجوی USB'}
          </button>
        </div>
      </div>

      {!printers.length && (
        <div className="empty">
          <span className="empty-icon">▤</span>
          <h3>هنوز پرینتری اضافه نشده است</h3>
          <p>
            دکمه «جستجوی خودکار همه (USB و LAN)» را بزنید تا تمام پرینترهای متصل به سیستم و شبکه
            به‌صورت کارت به همراه مشخصات کامل نمایش داده شوند.
          </p>
        </div>
      )}

      <div className="printer-grid">
        {printers.map((p) => (
          <PrinterCard
            key={p.id}
            printer={p}
            disabled={disabled}
            onTest={() =>
              run(async () => {
                await api.test(p.id);
              })
            }
            onProbe={() =>
              run(async () => {
                const s = await api.probe(p.id);
                if (s !== 'online') throw new Error(`وضعیت اتصال: ${s}`);
              })
            }
            onEdit={() => {
              const local = config.printers.find((printer) => printer.id === p.id);
              if (local) {
                setEdit(local);
              } else {
                const { status: _status, ...profile } = p;
                setEdit(profile);
              }
            }}
          />
        ))}
      </div>

      {scanError && (
        <div role="alert" className="notice error" dir="ltr">
          {scanError}
          <button className="subtle" onClick={() => setScanError('')}>
            ×
          </button>
        </div>
      )}

      {(devices.length > 0 || queues !== null || network !== null) && (
        <div className="panel">
          <div className="section-heading">
            <div>
              <h3>۲. پرینترهای شناسایی‌شده (USB، سیستم‌عامل و شبکه LAN)</h3>
              <p className="muted">
                بدون نیاز به وارد کردن دستی IP و پورت • روی «انتخاب» کلیک کنید تا ایستگاه کاری و عرض
                کاغذ (58mm یا 80mm) را تنظیم کنید.
              </p>
            </div>
            <button
              type="button"
              className="subtle"
              onClick={() => {
                setDevices([]);
                setQueues(null);
                setNetwork(null);
              }}
            >
              بستن نتایج ×
            </button>
          </div>

          <div className="printer-grid">
            {devices.map((d, i) => {
              const vidHex = d.connection.vendorId.toString(16).padStart(4, '0').toUpperCase();
              const pidHex = d.connection.productId.toString(16).padStart(4, '0').toUpperCase();
              return (
                <article className="printer-card" key={`usb-${i}`}>
                  <div className="card-heading">
                    <span className="printer-icon">USB</span>
                    <div>
                      <h3>{d.product ?? 'USB Thermal Printer'}</h3>
                      <span className="mono muted">
                        {d.manufacturer ?? 'USB'} · VID:PID {vidHex}:{pidHex}
                      </span>
                    </div>
                    <span className={`tag ${d.accessible ? 'online' : 'error'}`}>
                      <span className="dot" />
                      {d.accessible ? 'آماده اتصال' : 'نیازمند درایور'}
                    </span>
                  </div>
                  <div className="profile">
                    <span>درگاه USB مستقیم</span>
                    <span>کاغذ 80mm / 58mm</span>
                    <span>ESC/POS</span>
                  </div>
                  <div className="card-actions">
                    <button
                      onClick={() => {
                        setEdit(fresh(d.connection, 'صندوق فروش'));
                      }}
                    >
                      انتخاب و تنظیم ایستگاه
                    </button>
                  </div>
                </article>
              );
            })}

            {(queues ?? []).map((q) => {
              const is58 = q.queueName.includes('58');
              return (
                <article className="printer-card" key={`queue-${q.queueName}`}>
                  <div className="card-heading">
                    <span className="printer-icon">OS</span>
                    <div>
                      <h3>{q.queueName}</h3>
                      <span className="mono muted">صف چاپ سیستم‌عامل (USB / LAN Spooler)</span>
                    </div>
                    <span className="tag online">
                      <span className="dot" />
                      نصب‌شده و آماده
                    </span>
                  </div>
                  <div className="profile">
                    <span>مسیر: {q.queueName}</span>
                    <span>کاغذ {is58 ? '58mm' : '80mm'}</span>
                    <span>RAW Spooler</span>
                  </div>
                  <div className="card-actions">
                    <button
                      onClick={() => {
                        const cfg = fresh({ type: 'spooler', queueName: q.queueName }, 'صندوق فروش');
                        if (is58) {
                          cfg.paperMm = 58;
                          cfg.widthDots = 384;
                        }
                        setEdit(cfg);
                      }}
                    >
                      انتخاب و تنظیم ایستگاه
                    </button>
                  </div>
                </article>
              );
            })}

            {(network ?? []).map((n) => (
              <article className="printer-card" key={`net-${n.host}:${n.port}`}>
                <div className="card-heading">
                  <span className="printer-icon">LAN</span>
                  <div>
                    <h3>پرینتر شبکه ({n.host})</h3>
                    <span className="mono muted" dir="ltr">
                      {n.host}:{n.port} · RAW TCP/IP
                    </span>
                  </div>
                  <span className="tag online">
                    <span className="dot" />
                    آنلاین در شبکه
                  </span>
                </div>
                <div className="profile">
                  <span dir="ltr">IP: {n.host}</span>
                  <span dir="ltr">Port: {n.port}</span>
                  <span>کاغذ 80mm / 58mm</span>
                </div>
                <div className="card-actions">
                  <button
                    onClick={() => {
                      setEdit(fresh({ type: 'network', host: n.host, port: n.port }, 'صندوق فروش'));
                    }}
                  >
                    انتخاب و تنظیم ایستگاه
                  </button>
                </div>
              </article>
            ))}
          </div>
        </div>
      )}

      {edit && (
        <div className="modal-backdrop">
          <form
            className="modal"
            onSubmit={(e) => {
              e.preventDefault();
              run(async () => {
                const c = {
                  ...config,
                  printers: [...config.printers.filter((p) => p.id !== edit.id), edit],
                };
                await save(c);
                setEdit(null);
              });
            }}
          >
            <div className="section-heading">
              <h2>تنظیم پرینتر</h2>
              <button type="button" className="subtle" onClick={() => setEdit(null)}>
                بستن ×
              </button>
            </div>

            <div className="notice">
              <strong>
                مشخصات اتصال خودکار:{' '}
                {edit.connection.type === 'network'
                  ? `پرینتر شبکه (LAN) — ${edit.connection.host}:${edit.connection.port}`
                  : edit.connection.type === 'spooler'
                    ? `صف چاپ سیستم‌عامل — ${edit.connection.queueName}`
                    : `پرینتر USB مستقیم — ${edit.connection.vendorId.toString(16).toUpperCase()}:${edit.connection.productId.toString(16).toUpperCase()}`}
              </strong>
            </div>

            <label>
              نام پرینتر (نام ایستگاه — مثلاً صندوق، آشپزخانه، بار)
              <input
                required
                maxLength={128}
                placeholder="مثلاً: صندوق فروش، آشپزخانه، بار گرم"
                value={edit.name}
                onChange={(e) => update('name', e.target.value)}
              />
            </label>
            <div className="actions" style={{ marginBottom: 8 }}>
              {['صندوق فروش', 'آشپزخانه', 'بار گرم و سرد', 'صندوق و آشپزخانه'].map((preset) => (
                <button
                  key={preset}
                  type="button"
                  className="secondary"
                  onClick={() => update('name', preset)}
                >
                  {preset}
                </button>
              ))}
            </div>

            {edit.connection.type === 'usb' && (
              <div className="notice">
                <label>
                  مسیر جایگزین USB (اختیاری)
                  <select
                    value={edit.usbFallbackTarget?.type ?? 'none'}
                    onChange={(e) => {
                      if (e.target.value === 'none') setUsbFallback(null);
                      else if (e.target.value === 'spooler')
                        setUsbFallback({ type: 'spooler', queueName: '' });
                    }}
                  >
                    <option value="none">بدون مسیر جایگزین</option>
                    <option value="spooler">صف چاپ نصب‌شده سیستم‌عامل</option>
                  </select>
                </label>
                {edit.usbFallbackTarget?.type === 'spooler' && (
                  <>
                    <div className="actions">
                      <button
                        type="button"
                        className="secondary"
                        onClick={() =>
                          run(async () => {
                            setFallbackQueues(await api.installed());
                          })
                        }
                      >
                        انتخاب از پرینترهای نصب‌شده
                      </button>
                    </div>
                    {fallbackQueues &&
                      fallbackQueues.map((q) => (
                        <div className="discovered" key={q.queueName}>
                          <span>{q.queueName}</span>
                          <button
                            type="button"
                            onClick={() => {
                              setUsbFallback({ type: 'spooler', queueName: q.queueName });
                              setFallbackQueues(null);
                            }}
                          >
                            انتخاب صف جایگزین
                          </button>
                        </div>
                      ))}
                    {fallbackQueueName && <p className="mono">{fallbackQueueName}</p>}
                  </>
                )}
              </div>
            )}

            <div className="form-grid">
              <label>
                عرض کاغذ (58mm یا 80mm)
                <select
                  value={edit.paperMm}
                  onChange={(e) => {
                    const mm = Number(e.target.value) as 58 | 80;
                    update('paperMm', mm);
                    update('widthDots', mm === 58 ? 384 : 576);
                  }}
                >
                  <option value={80}>80 mm</option>
                  <option value={58}>58 mm</option>
                </select>
              </label>
              <label>
                عرض واقعی — dots
                <input
                  type="number"
                  required
                  min={128}
                  max={832}
                  step={8}
                  value={edit.widthDots}
                  onChange={(e) => update('widthDots', Number(e.target.value))}
                />
              </label>
              <label>
                تعداد نسخه
                <input
                  required
                  type="number"
                  min={1}
                  max={3}
                  value={edit.copies}
                  onChange={(e) => update('copies', Number(e.target.value))}
                />
              </label>
              <label>
                اندازه فونت
                <input
                  required
                  type="number"
                  min={12}
                  max={48}
                  value={edit.fontSize}
                  onChange={(e) => update('fontSize', Number(e.target.value))}
                />
              </label>
            </div>
            <label>
              فونت
              <input
                dir="ltr"
                required
                value={edit.fontFamily}
                onChange={(e) => update('fontFamily', e.target.value)}
              />
            </label>
            <label className="check">
              <input
                type="checkbox"
                checked={edit.cut}
                onChange={(e) => update('cut', e.target.checked)}
              />
              برش خودکار — فقط پرینترهای دارای کاتر
            </label>
            <div className="actions">
              <button disabled={disabled}>ذخیره تنظیمات</button>
              {config.printers.some((p) => p.id === edit.id) && (
                <button
                  type="button"
                  className="danger"
                  disabled={disabled}
                  onClick={() => {
                    if (
                      confirm('پرینتر حذف شود؟ jobهای قبلی با پروفایل ذخیره‌شده خود باقی می‌مانند.')
                    )
                      run(async () => {
                        await save({
                          ...config,
                          printers: config.printers.filter((p) => p.id !== edit.id),
                          raw_passthrough: {
                            ...config.raw_passthrough,
                            printers: Object.fromEntries(
                              Object.entries(config.raw_passthrough.printers).filter(([id]) => id !== edit.id),
                            ),
                          },
                          routes: config.routes.filter((r) => r.printerId !== edit.id),
                        });
                        setEdit(null);
                      });
                  }}
                >
                  حذف پرینتر
                </button>
              )}
            </div>
          </form>
        </div>
      )}
    </section>
  );
}
