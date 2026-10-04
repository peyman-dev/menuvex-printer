import { useEffect, useState } from 'react';
import { call, type Config } from '../lib/agent';
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
  useEffect(() => setDraft(config), [config]);
  useEffect(() => {
    if (!secret) return;
    const timer = setTimeout(() => setSecret(''), 60000);
    return () => clearTimeout(timer);
  }, [secret]);
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
                          ? [
                              {
                                role,
                                printerId: e.target.value,
                                autoPrint: route?.autoPrint ?? false,
                              },
                            ]
                          : []),
                      ],
                    })
                  }
                >
                  <option value="">انتخاب نشده</option>
                  {draft.printers.map((p) => (
                    <option value={p.id} key={p.id}>
                      {p.name}
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
                      routes: draft.routes.map((r) =>
                        r.role === role ? { ...r, autoPrint: e.target.checked } : r,
                      ),
                    })
                  }
                />
                چاپ خودکار در PWA
              </label>
            </div>
          );
        })}
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
