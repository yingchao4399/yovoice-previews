import { useEffect, useEffectEvent, useState, type ReactNode } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { TextArea } from '@astryxdesign/core/TextArea';
import { useTranslator } from '@astryxdesign/core/i18n';
import { ArrowUpRight, Check, CircleAlert, LoaderCircle, PenLine, Settings2 } from 'lucide-react';
import { call, isDesktop } from '../lib/client';
import { formatCallError } from '../i18n/format';
import { Selector } from '../selector';
import { formatTime, type AITask, type AITaskKind, type State } from '../workbench';

// 任务已用时间：运行中每秒刷新，结束后停在完成时刻。
function useElapsed(task?: AITask) {
  const [now, setNow] = useState(() => Date.now());
  const running = task?.status === 'running';
  useEffect(() => {
    if (!running) return;
    setNow(Date.now());
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [running, task?.id]);
  if (!task) return 0;
  const end = task.finishedAt ? Date.parse(task.finishedAt) : now;
  return Math.max(0, Math.floor((end - Date.parse(task.startedAt)) / 1000));
}

const dismiss = (task: AITask) => call('ai.dismiss', { id: task.id });

// AI 写作栏：一句话描述 → 后台写作 → 写好后应用。任务在后台运行，可离开页面继续编辑；
// 内容为空时写好直接填入，已有内容时先问是否替换；失败时显示原因、用时，可重试或打开日志。
export function AIComposer({ kind, projectId, state, placeholder, submitLabel, extra, overwrite, start, apply, configure, initialBrief, externalBrief, maxBriefLength = 2000 }: {
  kind: AITaskKind; projectId: string; state: State; placeholder: string; submitLabel: string; extra?: ReactNode;
  overwrite: boolean; start: (brief: string) => Promise<unknown>; apply: (result: unknown) => void; configure: () => void; initialBrief?: string; externalBrief?: string; maxBriefLength?: number;
}) {
  const t = useTranslator();
  const current = state.aiTask;
  const task = current && current.kind === kind && current.projectId === projectId ? current : undefined;
  const elsewhere = current?.status === 'running' && !task;
  const [brief, setBrief] = useState(task?.brief ?? initialBrief ?? '');
  const submittedBrief = externalBrief ?? brief;
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState('');
  const elapsed = useElapsed(task);
  const provider = state.aiProviders?.find(p => p.id === state.aiProviderID);
  // 与常见 AI 应用一致：写作栏直接在已配置的服务和模型之间切换，按服务分组；选项值为“服务 ID + 模型”。
  const ready = (state.aiProviders ?? []).filter(p => p.model || p.models?.length);
  const choice = (id: string, model: string) => `${id}\n${model}`;
  const modelOptions = ready.map(p => ({ type: 'section' as const, title: p.name, options: [...new Set([p.model, ...(p.models ?? [])].filter(Boolean))].map(model => ({ value: choice(p.id, model), label: model })) }));
  const pick = (value: string) => { const [id, model] = value.split('\n'); void call('ai.provider.use', { id, model }).catch(e => setError(formatCallError(t, e))); };
  // 结束后的收尾：取消直接清掉；内容为空时写好直接应用。
  const settle = useEffectEvent(() => {
    if (task?.status === 'cancelled') void dismiss(task);
    else if (task?.status === 'completed' && !overwrite) { apply(task.result); void dismiss(task); }
  });
  useEffect(() => settle(), [task?.status, task?.id]);
  useEffect(() => { if (task?.brief) setBrief(task.brief); }, [task?.brief]);
  useEffect(() => { if (initialBrief && !task) setBrief(initialBrief); }, [initialBrief, task]);
  async function run() {
    setStarting(true); setError('');
    try { if (task && task.status !== 'running') await dismiss(task); await start(submittedBrief.trim()); }
    catch (e) { setError(formatCallError(t, e)); }
    finally { setStarting(false); }
  }
  const status = task?.status === 'cancelled' ? undefined : task?.status;
  if (!provider?.model && !task) return <HStack className="ai-composer" data-status="setup" gap={3} vAlign="center" wrap="wrap">
    <PenLine className="ai-composer-mark" size={16} aria-hidden />
    <small className="grow">{t(`@yovoice.ai.setup.${kind}`)}</small>
    <Button size="sm" variant="secondary" icon={<Settings2 />} label={t('@yovoice.ai.configure')} onClick={configure} />
  </HStack>;
  return <VStack className="ai-composer" data-status={status ?? 'idle'} gap={0} aria-busy={status === 'running'}>
    {status === 'running' && task ? <VStack className="ai-composer-body" gap={2} role="status">
      <HStack gap={2} vAlign="center">
        <LoaderCircle className="generation-spinner" size={16} aria-hidden />
        <b className="grow">{t(`@yovoice.ai.stage.${task.stage === 'repairing' ? 'repairing' : 'writing'}`, { model: task.model })}</b>
        <small className="ai-composer-time">{formatTime(elapsed)} / {formatTime(task.timeout)}</small>
      </HStack>
      <p className="ai-composer-brief">{task.brief}</p>
      <progress className="ai-composer-progress" value={Math.min(elapsed, task.timeout)} max={task.timeout} aria-label={t('@yovoice.create.elapsed')} />
    </VStack> : status === 'completed' && task ? <HStack className="ai-composer-body" gap={3} vAlign="center" role="status">
      <Check className="ai-composer-mark" size={16} aria-hidden />
      <VStack className="grow" gap={0}><b>{t(`@yovoice.ai.task.done.${kind}`)}</b><small>{t(`@yovoice.ai.task.replace.${kind}`)}</small></VStack>
    </HStack> : status === 'failed' && task ? <HStack className="ai-composer-body" gap={3} vAlign="start" role="alert">
      <CircleAlert className="ai-composer-mark" size={16} aria-hidden />
      <VStack className="grow" gap={1}>
        <b>{t(`@yovoice.ai.task.failed.${kind}`)}</b>
        <p>{formatCallError(t, task)}</p>
        <small>{t('@yovoice.ai.failedMeta', { time: formatTime(elapsed), model: task.model })}</small>
      </VStack>
    </HStack> : externalBrief === undefined ? <TextArea className="ai-composer-input" label={t(`@yovoice.ai.brief.${kind}`)} isLabelHidden value={brief} rows={2} maxLength={maxBriefLength} placeholder={placeholder} hasSpellCheck={false} onChange={value => setBrief(value.slice(0, maxBriefLength))} /> : null}
    <HStack className="ai-composer-footer" gap={2} vAlign="center" wrap="wrap">
      {status === 'running' ? <>
        <small className="grow">{t('@yovoice.ai.backgroundHint')}</small>
        <Button size="sm" label={t('@yovoice.action.cancel')} onClick={() => void call('ai.cancel')} />
      </> : status === 'completed' && task ? <>
        <HStack className="grow" gap={0} />
        <Button size="sm" variant="ghost" label={t('@yovoice.ai.discard')} onClick={() => void dismiss(task)} />
        <Button size="sm" variant="primary" icon={<Check />} label={t('@yovoice.ai.apply')} onClick={() => { apply(task.result); void dismiss(task); }} />
      </> : status === 'failed' && task ? <>
        <HStack className="grow" gap={0} />
        {isDesktop ? <Button size="sm" variant="ghost" icon={<ArrowUpRight />} label={t('@yovoice.ai.openLogs')} onClick={() => void call('logs.open').catch(e => setError(formatCallError(t, e)))} /> : null}
        <Button size="sm" variant="ghost" label={t('@yovoice.action.closeNotice')} onClick={() => void dismiss(task)} />
        <Button size="sm" variant="primary" label={t('@yovoice.ai.retry')} isLoading={starting} onClick={() => void run()} />
      </> : <>
        {elsewhere ? <small className="grow ai-composer-via">{t('@yovoice.ai.busyElsewhere')}</small>
          : <HStack className="grow ai-composer-via" gap={0}><Selector label={t('@yovoice.ai.model')} isLabelHidden size="sm" variant="ghost" className="ai-composer-model" value={provider?.model ? choice(provider.id, provider.model) : undefined} options={modelOptions} hasSearch={modelOptions.reduce((n, s) => n + s.options.length, 0) > 12} searchPlaceholder={t('@yovoice.ai.searchModels')}
            renderValue={option => <HStack gap={2} vAlign="center"><PenLine size={14} aria-hidden />{provider?.name} · {option.label}</HStack>} onChange={pick} /></HStack>}
        {extra}
        <Button size="sm" variant="primary" label={submitLabel} isLoading={starting} isDisabled={!submittedBrief.trim() || elsewhere} onClick={() => void run()} />
      </>}
    </HStack>
    {error ? <small className="ai-composer-error" role="alert">{error}</small> : null}
  </VStack>;
}
