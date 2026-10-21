import { useSyncExternalStore } from 'react';
import type { AssistantLive, State } from '../workbench';

// Ocho 的实时状态（流式回复、排队、跳转请求）单独存放：流式输出每秒推送十几次，
// 只让助手面板与入口跟着重绘，不再带动整个应用。读取一律用 useAssistantLive，不读 state.assistantLive。
let current: AssistantLive | undefined;
const listeners = new Set<() => void>();

export function setAssistantLive(value: AssistantLive | undefined) {
  if (value === current) return;
  current = value;
  listeners.forEach(fn => fn());
}

const subscribe = (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; };
export const useAssistantLive = () => useSyncExternalStore(subscribe, () => current);

// 不需要重绘、只需响应变化的地方（如消费跳转请求）用它：立即以当前值调用一次，之后每次变化调用。
export function watchAssistantLive(listener: (live: AssistantLive | undefined) => void) {
  const notify = () => listener(current);
  notify();
  return subscribe(notify);
}

// 新状态与当前状态相比只有助手实时状态变了（其余字段引用相同）时，不需要更新应用状态。
export function onlyAssistantLiveChanged(previous: State, next: State): boolean {
  const ignored = new Set(['assistantLive', 'assistantMissing']);
  const keys = new Set([...Object.keys(previous), ...Object.keys(next)]);
  for (const key of keys) if (!ignored.has(key) && previous[key as keyof State] !== next[key as keyof State]) return false;
  return true;
}
