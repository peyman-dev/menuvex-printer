import { describe, expect, it } from 'vitest';
import { connectionSchema, printerSchema } from '../src/types';
describe('Legacy Windows printer descriptors', () => {
  it('accepts real Windows spooler queues without invented USB descriptors', () => {
    const connection = { type: 'spooler', queueName: 'POS-80' };
    expect(connectionSchema.parse(connection)).toEqual(connection);
    expect(
      printerSchema.parse({
        id: 'printer:legacy',
        name: 'Invoice',
        connection,
        paperMm: 80,
        widthDots: 576,
        copies: 1,
        cut: true,
        fontFamily: 'Noto Sans Arabic',
        fontSize: 24,
        status: 'unknown',
      }).connection.type,
    ).toBe('spooler');
  });
  it('keeps USB and network profiles compatible', () => {
    expect(connectionSchema.parse({ type: 'network', host: '192.168.1.50', port: 9100 }).type).toBe(
      'network',
    );
    expect(
      connectionSchema.parse({
        type: 'usb',
        vendorId: 1,
        productId: 2,
        serial: null,
        bus: 1,
        ports: [1],
        interface: 0,
        endpoint: 1,
        alternate: 0,
      }).type,
    ).toBe('usb');
  });
  it('rejects incomplete or field-injected spooler descriptors', () => {
    expect(() => connectionSchema.parse({ type: 'spooler', queueName: '' })).toThrow();
    expect(() =>
      connectionSchema.parse({ type: 'spooler', queueName: 'POS', host: 'external' }),
    ).toThrow();
  });
});
