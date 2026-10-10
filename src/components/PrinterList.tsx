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
  /** USB enumeration is bounded in Rust but can still take tens of seconds; it must not disable
   * the whole window while it runs, and it must surface its own error. */
  const [scanning, setScanning] = useState(false);
  const [scanError, setScanError] = useState('');
  const fresh = (connection: PrinterConfig['connection']): PrinterConfig => ({
    id: `printer:${crypto.randomUUID()}`,
    name: '',
    connection,
    paperMm: 80,
    widthDots: 576,
    copies: 1,
    cut: true,
    fontFamily: 'Noto Sans Arabic',
    fontSize: 24,
    // Zero-touch default: a new printer accepts the website's raw ESC/POS receipts.
    rawPassthrough: true,
    usbFallbackTarget: null,
  });
  const update = <K extends keyof PrinterConfig>(key: K, value: PrinterConfig[K]) =>
    setEdit((e) => (e ? { ...e, [key]: value } : e));
  const setUsbFallback = (target: PrinterConfig['usbFallbackTarget']) =>
    setEdit((e) => (e ? { ...e, usbFallbackTarget: target } : e));
  const fallbackQueueName =
    edit?.usbFallbackTarget?.type === 'spooler' ? edit.usbFallbackTarget.queueName : '';
  return (
    <section>
      <div className="section-heading">
        <div>
          <p className="eyebrow">PRINTERS</p>
          <h2>
            پرینترهای شما <small>{printers.length}</small>
          </h2>
        </div>
        <div className="actions">
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
            جستجوی شبکه
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
          <button
            disabled={disabled}
            onClick={() => setEdit(fresh({ type: 'network', host: '', port: 9100 }))}
          >
            ＋ افزودن دستی
          </button>
        </div>
      </div>
      {!printers.length && (
        <div className="empty">
          <span className="empty-icon">▤</span>
          <h3>چاپ از اینجا شروع می‌شود</h3>
          <p>
            پرینتر USB را وصل کنید یا آدرس پرینتر شبکه را اضافه کنید.
            <br />
            تنظیمات روی همین دستگاه ذخیره می‌شود.
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
              // Use the local stored profile (which keeps `type: spooler`) rather than the
              // compatibility-shaped `printers.list` response sent to older frontends.
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
      {devices.length > 0 && (
        <div className="panel">
          <h3>دستگاه‌های USB شناسایی‌شده</h3>
          {devices.map((d, i) => (
            <div className="discovered" key={i}>
              <span>
                {d.product ?? 'USB Printer'}{' '}
                <small>
                  {d.manufacturer} ·{' '}
                  {d.accessible
                    ? 'باز کردن USB موفق؛ امکان چاپ هنوز تأیید نشده'
                    : 'باز کردن مستقیم USB ناموفق؛ علت را در جزئیات بررسی کنید'}
                </small>
                {d.accessError && (
                  <small dir="ltr" style={{ display: 'block' }}>
                    {d.accessError.code}: {d.accessError.message}
                  </small>
                )}
                <small>
                  این فهرست دستگاه‌های USB است، نه صف‌های چاپ ویندوز. نصب درایور چاپ ویندوز لزوماً
                  دسترسی مستقیم USB را فراهم نمی‌کند.
                </small>
              </span>
              <button
                onClick={() => {
                  setEdit({ ...fresh(d.connection), name: d.product ?? 'USB Printer' });
                  setDevices([]);
                }}
              >
                انتخاب
              </button>
            </div>
          ))}
        </div>
      )}
      {queues && (
        <div className="panel">
          <div className="section-heading">
            <h3>پرینترهای نصب‌شده روی سیستم‌عامل</h3>
            <button type="button" className="subtle" onClick={() => setQueues(null)}>
              بستن ×
            </button>
          </div>
          <p className="muted">
            چاپ از طریق درایور نصب‌شده (صف چاپ ویندوز / CUPS) انجام می‌شود؛ نیازی به تعویض درایور
            USB نیست. ساده‌ترین راه برای پرینترهای USB همین است.
          </p>
          {queues.length === 0 ? (
            <p className="muted">هیچ صف چاپی روی این سیستم پیدا نشد.</p>
          ) : (
            queues.map((q) => (
              <div className="discovered" key={q.queueName}>
                <span>
                  {q.queueName} <small>صف چاپ سیستم‌عامل</small>
                </span>
                <button
                  onClick={() => {
                    setEdit({
                      ...fresh({ type: 'spooler', queueName: q.queueName }),
                      name: q.queueName,
                    });
                    setQueues(null);
                  }}
                >
                  انتخاب
                </button>
              </div>
            ))
          )}
        </div>
      )}
      {network && (
        <div className="panel">
          <div className="section-heading">
            <h3>پرینترهای شبکه پیدا‌شده</h3>
            <button type="button" className="subtle" onClick={() => setNetwork(null)}>
              بستن ×
            </button>
          </div>
          <p className="muted">
            شبکه محلی برای پورت‌های چاپ (مثل 9100) جستجو شد. یکی را انتخاب کنید؛ IP و پورت به‌صورت
            خودکار پر می‌شود.
          </p>
          {network.length === 0 ? (
            <p className="muted">
              پرینتری پیدا نشد. مطمئن شوید پرینتر روشن و به همین شبکه وصل است، سپس دوباره جستجو
              کنید.
            </p>
          ) : (
            network.map((n) => (
              <div className="discovered" key={`${n.host}:${n.port}`}>
                <span>
                  <span dir="ltr">
                    {n.host}:{n.port}
                  </span>{' '}
                  <small>پرینتر شبکه (RAW/JetDirect)</small>
                </span>
                <button
                  onClick={() => {
                    setEdit({
                      ...fresh({ type: 'network', host: n.host, port: n.port }),
                      name: `پرینتر ${n.host}`,
                    });
                    setNetwork(null);
                  }}
                >
                  انتخاب
                </button>
              </div>
            ))
          )}
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
            <label>
              نام پرینتر (نام ایستگاه — مثلاً صندوق، آشپزخانه، بار)
              <input
                required
                maxLength={128}
                placeholder="مثلاً: صندوق"
                value={edit.name}
                onChange={(e) => update('name', e.target.value)}
              />
            </label>
            {edit.connection.type === 'spooler' && (
              <label>
                نام صف چاپ سیستم‌عامل
                <input
                  dir="ltr"
                  required
                  maxLength={256}
                  value={edit.connection.queueName}
                  onChange={(e) => {
                    if (edit.connection.type === 'spooler')
                      update('connection', { ...edit.connection, queueName: e.target.value });
                  }}
                />
              </label>
            )}
            {edit.connection.type === 'network' && (
              <div className="form-grid">
                <label>
                  IP خصوصی شبکه
                  <input
                    dir="ltr"
                    required
                    placeholder="192.168.1.50"
                    value={edit.connection.host}
                    onChange={(e) => {
                      if (edit.connection.type === 'network')
                        update('connection', { ...edit.connection, host: e.target.value });
                    }}
                  />
                </label>
                <label>
                  پورت
                  <input
                    dir="ltr"
                    type="number"
                    min={1}
                    max={65535}
                    required
                    value={edit.connection.port}
                    onChange={(e) => {
                      if (edit.connection.type === 'network')
                        update('connection', { ...edit.connection, port: Number(e.target.value) });
                    }}
                  />
                </label>
              </div>
            )}
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
                      else if (e.target.value === 'network')
                        setUsbFallback({ type: 'network', host: '', port: 9100 });
                    }}
                  >
                    <option value="none">بدون مسیر جایگزین</option>
                    <option value="spooler">صف چاپ نصب‌شده</option>
                    <option value="network">پرینتر شبکه</option>
                  </select>
                </label>
                <p className="muted">
                  فقط وقتی USB قبل از ارسال اولین بایت باز یا آماده نشود استفاده می‌شود؛ در timeout،
                  خطای حین ارسال یا نتیجهٔ نامشخص، مسیر جایگزین هرگز اجرا نمی‌شود.
                </p>
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
                        بارگذاری صف‌های چاپ
                      </button>
                    </div>
                    <label>
                      صف جایگزین
                      <select
                        required
                        value={edit.usbFallbackTarget.queueName}
                        onChange={(e) => {
                          if (edit.usbFallbackTarget?.type === 'spooler')
                            setUsbFallback({
                              ...edit.usbFallbackTarget,
                              queueName: e.target.value,
                            });
                        }}
                      >
                        <option value="">یک صف چاپ را انتخاب کنید</option>
                        {fallbackQueueName &&
                          !fallbackQueues?.some((queue) => queue.queueName === fallbackQueueName) && (
                            <option value={fallbackQueueName}>{fallbackQueueName} (ذخیره‌شده)</option>
                          )}
                        {fallbackQueues?.map((queue) => (
                          <option key={queue.queueName} value={queue.queueName}>
                            {queue.queueName}
                          </option>
                        ))}
                      </select>
                    </label>
                  </>
                )}
                {edit.usbFallbackTarget?.type === 'network' && (
                  <div className="form-grid">
                    <label>
                      IP خصوصی پرینتر جایگزین
                      <input
                        dir="ltr"
                        required
                        placeholder="192.168.1.50"
                        value={edit.usbFallbackTarget.host}
                        onChange={(e) => {
                          if (edit.usbFallbackTarget?.type === 'network')
                            setUsbFallback({ ...edit.usbFallbackTarget, host: e.target.value });
                        }}
                      />
                    </label>
                    <label>
                      پورت
                      <input
                        dir="ltr"
                        type="number"
                        min={1}
                        max={65535}
                        required
                        value={edit.usbFallbackTarget.port}
                        onChange={(e) => {
                          if (edit.usbFallbackTarget?.type === 'network')
                            setUsbFallback({
                              ...edit.usbFallbackTarget,
                              port: Number(e.target.value),
                            });
                        }}
                      />
                    </label>
                  </div>
                )}
              </div>
            )}
            <div className="form-grid">
              <label>
                عرض کاغذ
                <select
                  value={edit.paperMm}
                  onChange={(e) => update('paperMm', Number(e.target.value) as 58 | 80)}
                >
                  <option value={58}>58 mm</option>
                  <option value={80}>80 mm</option>
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
            <p className="muted">
              58mm معمولاً 384 و 80mm معمولاً 576 dots است؛ مشخصات مدل خود را بررسی کنید. کنترل‌های
              ESC/POS خام و انتخاب صف RAW در بخش تنظیمات محلی قرار دارند و به‌طور پیش‌فرض خاموش‌اند.
            </p>
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
