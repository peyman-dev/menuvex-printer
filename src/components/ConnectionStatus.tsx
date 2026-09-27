import type { AgentStatus } from '../../sdk/src/types';
export function ConnectionStatus({
  status,
  desktop,
}: {
  status: AgentStatus | null;
  desktop: boolean;
}) {
  return (
    <div className={`status-pill ${status?.ready ? 'online' : ''}`}>
      <span className="dot" />
      {!desktop ? 'پیش‌نمایش مرورگر' : status?.ready ? 'Agent آماده است' : 'Agent آماده نیست'}
    </div>
  );
}
