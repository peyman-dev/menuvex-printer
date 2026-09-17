import type { Printer } from '../../sdk/src/types';
const labels = {
  online: 'در دسترس',
  offline: 'قطع شده',
  unknown: 'بررسی نشده',
  busy: 'در حال ارسال',
  error: 'خطا',
};
export function PrinterCard({
  printer: p,
  onTest,
  onProbe,
  onEdit,
  disabled,
}: {
  printer: Printer;
  onTest: () => void;
  onProbe: () => void;
  onEdit: () => void;
  disabled: boolean;
}) {
  return (
    <article className="printer-card">
      <div className="card-heading">
        <span className="printer-icon">▤</span>
        <div>
          <h3>{p.name}</h3>
          <span className="mono muted">
            {p.connection.type === 'network'
              ? `${p.connection.host}:${p.connection.port}`
              : p.connection.type === 'spooler'
                ? `Windows · ${p.connection.queueName}`
                : `USB · ${p.connection.vendorId.toString(16)}:${p.connection.productId.toString(16)}`}
          </span>
        </div>
        <span className={`tag ${p.status}`}>
          <span className="dot" />
          {labels[p.status]}
        </span>
      </div>
      <div className="profile">
        <span>{p.paperMm} میلی‌متر</span>
        <span>{p.widthDots} dots</span>
        <span>{p.copies} نسخه</span>
      </div>
      <div className="card-actions">
        <button disabled={disabled} onClick={onTest}>
          چاپ آزمایشی ↗
        </button>
        <button className="subtle" disabled={disabled} onClick={onProbe}>
          بررسی اتصال
        </button>
        <button className="subtle" disabled={disabled} onClick={onEdit}>
          تنظیمات
        </button>
      </div>
    </article>
  );
}
