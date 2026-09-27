import { invoke, isTauri } from '@tauri-apps/api/core';
import type { Printer, PrintJob, AgentStatus } from '../../sdk/src/types';
export const desktop = isTauri();
export type PrinterConfig = Omit<Printer, 'status'>;
export interface Config {
  port: number;
  maxAttempts: number;
  autostart: boolean;
  printers: PrinterConfig[];
  routes: { role: string; printerId: string; autoPrint: boolean }[];
}
export interface UsbDevice {
  connection: Extract<Printer['connection'], { type: 'usb' }>;
  manufacturer: string | null;
  product: string | null;
  accessible: boolean;
  accessError?: { code: string; message: string } | null;
}
export function call<T>(command: string, args?: Record<string, unknown>): Promise<T> {
  if (!desktop)
    return Promise.reject(
      new Error(
        'این صفحه پیش‌نمایش رابط است. برای دسترسی به سخت‌افزار، برنامه دسکتاپ را نصب و اجرا کنید.',
      ),
    );
  return invoke<T>(command, args);
}
export function rpc<T>(type: string, fields: Record<string, unknown> = {}): Promise<T> {
  return call('local_command', {
    request: JSON.stringify({ version: 1, requestId: crypto.randomUUID(), type, ...fields }),
  });
}
export const api = {
  config: () => call<Config>('get_config'),
  save: (config: Config) => call<void>('save_config', { config }),
  printers: () => rpc<Printer[]>('printers.list'),
  queue: () => rpc<PrintJob[]>('queue.list'),
  status: () => rpc<AgentStatus>('agent.status'),
  discover: () => call<UsbDevice[]>('discover_usb'),
  test: (printerId: string) =>
    rpc<PrintJob>('printer.test', { printerId, jobId: `test:${crypto.randomUUID()}` }),
  cancel: (jobId: string) => rpc<PrintJob>('queue.cancel', { jobId }),
  probe: (printerId: string) => call<string>('test_connection', { printerId }),
};
export function errorText(e: unknown): string {
  if (e instanceof Error) return e.message;
  if (e && typeof e === 'object' && 'message' in e)
    return `${'code' in e ? String(e.code) + ': ' : ''}${String(e.message)}`;
  return 'عملیات انجام نشد؛ گزارش برنامه را بررسی کنید.';
}
