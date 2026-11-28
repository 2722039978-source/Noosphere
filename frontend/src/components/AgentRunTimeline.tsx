import type { RunEvent } from '@/lib/api';

const policyStyles = {
  allow: { label: '允许', color: 'text-ok' },
  ask: { label: '需审批', color: 'text-warn' },
  deny: { label: '拒绝', color: 'text-err' },
};

function policyFor(data: unknown) {
  if (!data || typeof data !== 'object' || !('policy' in data)) return;
  const policy = data.policy;
  if (!policy || typeof policy !== 'object' || !('decision' in policy)) return;
  const decision = policy.decision;
  if (decision !== 'allow' && decision !== 'ask' && decision !== 'deny') return;
  const reason = 'reason' in policy && typeof policy.reason === 'string' ? policy.reason : '';
  return { ...policyStyles[decision], reason };
}

export function AgentRunTimeline({ events }: { events: RunEvent[] }) {
  return <ol aria-label="执行时间线" className="grid gap-1 py-2 text-xs text-text-2">
    {events.filter(event => event.message).map(event => {
      const policy = policyFor(event.data);
      return <li key={event.seq}>
        {event.created_at && <time className="mr-2 text-text-3" dateTime={event.created_at}>{new Date(event.created_at).toLocaleTimeString()}</time>}
        <span>{event.message}</span>
        {policy && <span className="ml-2 inline-flex flex-wrap gap-x-2">
          <span className={`font-semibold ${policy.color}`}>{policy.label}</span>
          {policy.reason && <span>{policy.reason}</span>}
        </span>}
      </li>;
    })}
  </ol>;
}
