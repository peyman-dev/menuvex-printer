import { PrinterAgentClient } from './client';
/** One instance for the application. Construct your own at composition root only for a non-default port. */
export const printerAgent = new PrinterAgentClient();
