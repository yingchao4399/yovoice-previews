import { useEffect, useState } from 'react';

// 正在进行的任务已用的秒数，每秒刷新；active 为假时归零。startedAt 为任务开始时间（ISO），缺失时从现在起算。
export function useElapsed(active: boolean, startedAt?: string | null): number {
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    if (!active) { setElapsed(0); return; }
    const started = startedAt ? Date.parse(startedAt) : Date.now();
    const update = () => setElapsed(Math.max(0, Math.floor((Date.now() - started) / 1000)));
    update();
    const timer = setInterval(update, 1000);
    return () => clearInterval(timer);
  }, [active, startedAt]);
  return elapsed;
}
