import type { Connection } from './types';

/**
 * Return an installed OS queue name from either the explicit spooler descriptor or the reserved
 * USB-compatible wire form. A returned name means this is not a physical USB device.
 */
export function spoolerQueueName(connection: Connection): string | undefined {
  if (connection.type === 'spooler') return connection.queueName || undefined;
  if (
    connection.type === 'usb' &&
    connection.vendorId === 0 &&
    typeof connection.serial === 'string' &&
    connection.serial.startsWith('queue:')
  ) {
    return connection.serial.slice('queue:'.length) || undefined;
  }
  return undefined;
}
