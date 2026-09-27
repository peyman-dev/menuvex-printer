import { AgentError } from './types';
export type JobRoute = 'agent' | 'legacy';
export interface RoutingStore {
  get(jobId: string): Promise<JobRoute | null>;
  claim(jobId: string, route: JobRoute): Promise<{ route: JobRoute; created: boolean }>;
}
/** Durable per-browser routing prevents an already-submitted Agent job moving to legacy after a disconnect. */
export class BrowserRoutingStore implements RoutingStore {
  private open(): Promise<IDBDatabase> {
    return new Promise((resolve, reject) => {
      const r = indexedDB.open('menuvex-print-routes-v1', 1);
      r.onupgradeneeded = () => r.result.createObjectStore('routes');
      r.onsuccess = () => resolve(r.result);
      r.onerror = () => reject(r.error);
      r.onblocked = () => reject(new AgentError('STORAGE_BLOCKED', 'Routing storage blocked'));
    });
  }
  async get(jobId: string): Promise<JobRoute | null> {
    const db = await this.open();
    return new Promise((resolve, reject) => {
      const tx = db.transaction('routes', 'readonly');
      const r = tx.objectStore('routes').get(jobId);
      tx.oncomplete = () => {
        db.close();
        resolve(r.result ?? null);
      };
      tx.onabort = tx.onerror = () => {
        db.close();
        reject(tx.error);
      };
    });
  }
  async claim(jobId: string, route: JobRoute): Promise<{ route: JobRoute; created: boolean }> {
    const db = await this.open();
    return new Promise((resolve, reject) => {
      const tx = db.transaction('routes', 'readwrite');
      const store = tx.objectStore('routes');
      const r = store.get(jobId);
      let selected = route;
      let created = false;
      r.onsuccess = () => {
        if (r.result) selected = r.result;
        else {
          created = true;
          store.put(route, jobId);
        }
      };
      tx.oncomplete = () => {
        db.close();
        resolve({ route: selected, created });
      };
      tx.onabort = tx.onerror = () => {
        db.close();
        reject(tx.error);
      };
    });
  }
}
