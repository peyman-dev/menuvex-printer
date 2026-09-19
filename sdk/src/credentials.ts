import { AgentError } from './types';
export interface CredentialStore {
  get(): Promise<CryptoKey | null>;
  save(key: CryptoKey): Promise<void>;
  clear(): Promise<void>;
}
/** IndexedDB structured-clones a non-extractable signing key. No token in localStorage/cookies/URLs. */
export class BrowserCredentialStore implements CredentialStore {
  constructor(private name = 'menuvex-printer-pairing-v1') {}
  private open(): Promise<IDBDatabase> {
    return new Promise((resolve, reject) => {
      const r = indexedDB.open(this.name, 1);
      r.onupgradeneeded = () => r.result.createObjectStore('keys');
      r.onsuccess = () => resolve(r.result);
      r.onerror = () => reject(r.error);
      r.onblocked = () => reject(new AgentError('STORAGE_BLOCKED', 'Close other pairing tabs'));
    });
  }
  private async run<T>(
    mode: IDBTransactionMode,
    action: (store: IDBObjectStore) => IDBRequest<T>,
  ): Promise<T> {
    const db = await this.open();
    return new Promise<T>((resolve, reject) => {
      const tx = db.transaction('keys', mode);
      const r = action(tx.objectStore('keys'));
      tx.oncomplete = () => {
        db.close();
        resolve(r.result);
      };
      tx.onabort = tx.onerror = () => {
        db.close();
        reject(tx.error);
      };
    });
  }
  async get() {
    return ((await this.run('readonly', (s) => s.get('hmac'))) as CryptoKey | undefined) ?? null;
  }
  async save(key: CryptoKey) {
    await this.run('readwrite', (s) => s.put(key, 'hmac'));
  }
  async clear() {
    await this.run('readwrite', (s) => s.delete('hmac'));
  }
}
export async function importPairingSecret(secret: string): Promise<CryptoKey> {
  let bytes: Uint8Array;
  try {
    bytes = Uint8Array.from(atob(secret.trim()), (c) => c.charCodeAt(0));
  } catch {
    throw new AgentError('INVALID_PAIRING_SECRET', 'Invalid base64 pairing secret');
  }
  if (bytes.length !== 32)
    throw new AgentError('INVALID_PAIRING_SECRET', 'Pairing requires a 256-bit secret');
  try {
    return await crypto.subtle.importKey(
      'raw',
      bytes as BufferSource,
      { name: 'HMAC', hash: 'SHA-256' },
      false,
      ['sign'],
    );
  } finally {
    bytes.fill(0);
  }
}
export async function signChallenge(
  key: CryptoKey,
  nonce: string,
  origin: string,
  server = false,
): Promise<string> {
  const signature = await crypto.subtle.sign(
    'HMAC',
    key,
    new TextEncoder().encode(
      `menuvex-print-agent:${server ? 'server:' : ''}v1\n${nonce}\n${origin}`,
    ),
  );
  return btoa(String.fromCharCode(...new Uint8Array(signature)));
}
