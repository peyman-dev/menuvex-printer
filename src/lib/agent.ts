import { invoke, isTauri } from '@tauri-apps/api/core';
import type { Printer, PrintJob, AgentStatus } from '../../sdk/src/types';
export const desktop = isTauri();
export type PrinterConfig = Omit<Printer, 'status'>;
export interface RawPrinterSettings {
  raw_target: string | null;
  force_raw: boolean;
  created_generic: boolean;
}
export interface Config {
  port: number;
  maxAttempts: number;
  autostart: boolean;
  /** Local-only raw ESC/POS gate and operator selections, persisted under `raw_passthrough`. */
  raw_passthrough: {
    enabled: boolean;
    /** Decoded-byte limit, 1 KiB–1 MiB and always bounded by the protocol hard cap. */
    max_bytes: number;
    /** Keyed by stable printer ID; websites cannot update this local map. */
    printers: Record<string, RawPrinterSettings>;
  };
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
export interface RawPrinterInfo {
  queueName: string;
  platform: string;
  driverName: string | null;
  portName: string | null;
  isGenericTextOnly: boolean;
  availableUsbPorts: string[];
}
export interface RawTargetCreated {
  queueName: string;
  driverName: string;
  portName: string;
  created: boolean;
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
  discoverNetwork: () => rpc<{ host: string; port: number }[]>('discover.network'),
  installed: () => rpc<{ queueName: string }[]>('printers.installed'),
  clearQueue: () => rpc<{ cancelled: number; removed: number }>('queue.clear'),
  test: (printerId: string) =>
    rpc<PrintJob>('printer.test', { printerId, jobId: `test:${crypto.randomUUID()}` }),
  cancel: (jobId: string) => rpc<PrintJob>('queue.cancel', { jobId }),
  probe: (printerId: string) => call<string>('test_connection', { printerId }),
  rawPlatform: () => call<string>('raw_printer_platform'),
  rawPrinterInfo: (queueName: string) => call<RawPrinterInfo>('raw_printer_info', { queueName }),
  createGenericRawTarget: (printerId: string, sourceQueue: string) =>
    call<RawTargetCreated>('create_generic_raw_target', { printerId, sourceQueue }),
  rawTest: (printerId: string) => call<PrintJob>('test_raw_print', { printerId }),
};
export function errorText(e: unknown): string {
  if (e && typeof e === 'object' && 'message' in e) {
    const value = e as { code?: unknown; message: unknown; actionRequired?: unknown };
    const code = value.code ? `${String(value.code)}: ` : '';
    const action = value.actionRequired ? ` ${String(value.actionRequired)}` : '';
    return `${code}${String(value.message)}${action}`;
  }
  if (e instanceof Error) return e.message;
  return 'عملیات انجام نشد؛ گزارش برنامه را بررسی کنید.';
}
