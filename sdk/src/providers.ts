import { PrinterAgentClient } from './client';
import { AgentError, type PrintRequest, type PrintJob, printRequestSchema } from './types';
import { BrowserRoutingStore, type RoutingStore } from './routing';
export interface PrinterProvider {
  print(request: PrintRequest): Promise<PrintJob | void>;
}
export class MenuVexAgentPrinter implements PrinterProvider {
  constructor(readonly client: PrinterAgentClient) {}
  print(request: PrintRequest) {
    return this.client.print(request);
  }
}
/** Adapter only: inject MenuVex's existing legacy print function. This SDK never requests USB devices. */
export class BrowserWebUSBPrinter implements PrinterProvider {
  constructor(private legacyPrint: (request: PrintRequest) => Promise<void>) {}
  print(request: PrintRequest) {
    return this.legacyPrint(request);
  }
}
export class MigratingPrinterProvider implements PrinterProvider {
  constructor(
    private agent: PrinterAgentClient,
    private legacy: PrinterProvider,
    private allowLegacy: () => boolean,
    private routes: RoutingStore = new BrowserRoutingStore(),
  ) {}
  async print(request: PrintRequest) {
    const parsed = printRequestSchema.parse(request);
    const old = await this.routes.get(parsed.jobId);
    if (old === 'legacy')
      throw new AgentError(
        'LEGACY_OUTCOME_UNKNOWN',
        'This ID was already handed to legacy printing. Inspect output; automatic replay is unsafe.',
      );
    if (!this.agent.isConnected()) {
      try {
        await this.agent.connect();
      } catch (error) {
        // Never move an Agent-owned ID to legacy. Persist assignment BEFORE invoking either provider.
        if (
          !old &&
          error instanceof AgentError &&
          error.code === 'AGENT_UNAVAILABLE' &&
          this.allowLegacy()
        ) {
          const route = await this.routes.claim(parsed.jobId, 'legacy');
          if (route.route === 'legacy' && route.created) return this.legacy.print(parsed);
        }
        throw error;
      }
    }
    const selected = await this.routes.claim(parsed.jobId, 'agent');
    if (selected.route !== 'agent')
      throw new AgentError('LEGACY_OUTCOME_UNKNOWN', 'Job belongs to legacy; do not duplicate it');
    return this.agent.print(parsed);
  }
}
