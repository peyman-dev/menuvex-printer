import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import { printerAgent, PrinterAgentClient, type ConnectionState } from '../src';
const Context = createContext<{ client: PrinterAgentClient; status: ConnectionState } | null>(null);
export function PrinterAgentProvider({
  children,
  client = printerAgent,
}: {
  children: ReactNode;
  client?: PrinterAgentClient;
}) {
  const [status, setStatus] = useState<ConnectionState>(client.getConnectionState());
  useEffect(() => {
    const off = client.onAgentStatus(setStatus);
    void client.connect().catch(() => {
      /* State subscription exposes unavailable/pairing-required without unhandled rejection. */
    });
    return () => {
      off();
      client.disconnect();
    };
  }, [client]);
  return <Context.Provider value={{ client, status }}>{children}</Context.Provider>;
}
export function usePrinterAgent() {
  const value = useContext(Context);
  if (!value) throw new Error('Wrap the app in PrinterAgentProvider');
  return value;
}
