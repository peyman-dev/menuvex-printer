import { useState } from 'react';
import type { Printer } from '../../sdk/src/types';
import { api, type Config, type PrinterConfig, type UsbDevice } from '../lib/agent';
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
  const [edit, setEdit] = useState<PrinterConfig | null>(null);
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
  });
  const update = <K extends keyof PrinterConfig>(key: K, value: PrinterConfig[K]) =>
    setEdit((e) => (e ? { ...e, [key]: value } : e));
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
                setDevices(await api.discover());
              })
            }
          >
            جستجوی USB
          </button>
          <button
            disabled={disabled}
            onClick={() => setEdit(fresh({ type: 'network', host: '', port: 9100 }))}
          >
            ＋ افزودن پرینتر شبکه
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
              const { status: _, ...profile } = p;
              setEdit(profile);
            }}
          />
        ))}
      </div>
      {devices.length > 0 && (
        <div className="panel">
          <h3>دستگاه‌های USB شناسایی‌شده</h3>
          {devices.map((d, i) => (
            <div className="discovered" key={i}>
              <span>
                {d.product ?? 'USB Printer'}{' '}
                <small>
                  {d.manufacturer} ·{' '}
                  {d.accessible ? 'دسترسی برقرار' : 'نیازمند driver / permission'}
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
              نام پرینتر
              <input
                required
                maxLength={128}
                value={edit.name}
                onChange={(e) => update('name', e.target.value)}
              />
            </label>
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
              58mm معمولاً 384 و 80mm معمولاً 576 dots است؛ مشخصات مدل خود را بررسی کنید.
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
