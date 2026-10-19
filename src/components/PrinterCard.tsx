import { spoolerQueueName } from '../../sdk/src/compat';
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
  const queueName = spoolerQueueName(p.connection);
  const connectionType =
    p.connection.type === 'network' && queueName === undefined
      ? 'network'
      : p.connection.type === 'usb'
        ? 'usb'
        : 'spooler';

  const connectionLabel =
    connectionType === 'network' && p.connection.type === 'network'
      ? `${p.connection.host}:${p.connection.port}`
      : queueName !== undefined
        ? queueName
        : p.connection.type === 'usb'
          ? `${p.connection.vendorId.toString(16).padStart(4, '0').toUpperCase()}:${p.connection.productId.toString(16).padStart(4, '0').toUpperCase()}`
          : p.connection.type === 'spooler'
            ? p.connection.queueName
            : '';

  const typeBadge =
    connectionType === 'network'
      ? 'پرینتر شبکه (LAN)'
      : connectionType === 'usb'
        ? 'پرینتر USB'
        : 'صف چاپ سیستم‌عامل';

  return (
    <article className="printer-card">
      <div className="card-heading">
        <span className="printer-icon" aria-hidden="true">
          {connectionType === 'usb' ? (
            <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.9">
              <path d="M12 2v15" />
              <path d="M9 5l3-3 3 3" />
              <circle cx="12" cy="19" r="2" />
              <path d="M12 14l-5-3v-3" />
              <path d="M12 12l5-2.5V7" />
            </svg>
          ) : connectionType === 'network' ? (
            <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.9">
              <path d="M5 12.55a11 11 0 0 1 14.08 0" />
              <path d="M1.42 9a16 16 0 0 1 21.16 0" />
              <path d="M8.53 16.11a6 6 0 0 1 6.95 0" />
              <circle cx="12" cy="20" r="1.5" />
            </svg>
          ) : (
            <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.9">
              <path d="M6 9V3h12v6" />
              <rect x="3" y="9" width="18" height="8" rx="2" />
              <path d="M6 17h12v4H6z" />
            </svg>
          )}
        </span>
        <div>
          <h3>{p.name}</h3>
          <span className="mono muted">
            {typeBadge} · {connectionLabel}
          </span>
        </div>
        <span className={`tag ${p.status}`}>
          <span className="dot" />
          {labels[p.status]}
        </span>
      </div>
      <div className="profile">
        <span>کاغذ {p.paperMm} میلی‌متر</span>
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
          تنظیمات پرینتر
        </button>
      </div>
    </article>
  );
}
