import { useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { VStack } from '@astryxdesign/core/Layout';
import { useTranslator } from '@astryxdesign/core/i18n';
import { formatCallError } from '../../shared/i18n/format';
import { AppDialog } from '../../shared/ui/app-dialog';
import { AIMaterialReview } from '../../shared/ui/ai-material-review';
import { call } from '../../shared/lib/client';
import type { ArrangementMaterial, Draft, State, UiLocale } from '../../shared/workbench';

/** 候选随状态推送进入当前编曲页的试听队列。 */
export function MaterialReviewQueue({ draft, state, locale }: { draft: Draft; state: State; locale: UiLocale }) {
  const t = useTranslator();
  const [deferred, setDeferred] = useState<Set<string>>(new Set());
  const [continueQueue, setContinueQueue] = useState<ArrangementMaterial[]>([]);
  const [retryError, setRetryError] = useState('');
  const continuing = useRef(false);
  useEffect(() => { setDeferred(new Set()); setContinueQueue([]); setRetryError(''); }, [draft.id]);
  const materials = (state.arrangementMaterials ?? []).filter(item => item.projectId === draft.id);
  const pending = useMemo(() => {
    const latest = new Map<string, ArrangementMaterial>();
    for (const item of [...(state.arrangementMaterials ?? [])].sort((a, b) => a.createdAt.localeCompare(b.createdAt))) {
      if (item.projectId !== draft.id || !item.generation) continue;
      const part = draft.arrangement?.sections[item.sectionIndex]?.phrases[item.phraseIndex]?.parts[item.partIndex];
      if (!part || (part.id && item.partId && part.id !== item.partId)) continue;
      const c = item.generation;
      const key = [item.partId || `${item.sectionIndex}:${item.phraseIndex}:${item.partIndex}`, item.scoreHash, item.planHash, c.pitch ?? 'phrase', c.articulation, c.velocityLow, c.velocityHigh].join(':');
      latest.set(key, item);
    }
    return [...latest.values()].filter(item => item.reviewStatus !== 'confirmed' &&
      ['needs_review', 'needs_fit', 'failed'].includes(item.generationResult?.state ?? '') &&
      !['queued', 'running', 'completed'].includes(item.retryRequest?.status ?? '') && !deferred.has(item.id));
  }, [state.arrangementMaterials, draft.id, draft.arrangement, deferred]);
  const item = pending[0];
  const activity = state.activity;
  const generating = activity?.kind === 'generate' && activity.projectId === draft.id && (activity.status === 'running' || activity.status === 'queued');
  const dismiss = (id: string) => setDeferred(previous => new Set(previous).add(id));
  const queueRetry = async (id: string, feedback: string) => {
    await call('arrangement.material.retry', { id, draft, feedback });
    dismiss(id); setRetryError('');
  };
  const continueAfterAdoption = (material: ArrangementMaterial) => {
    // 代表采样确认后，自动展开当前曲谱还缺少的音高、力度和奏法。
    if (material.generation?.pitch === undefined) return;
    setContinueQueue(previous => previous.some(item => item.sectionIndex === material.sectionIndex && item.phraseIndex === material.phraseIndex && item.partIndex === material.partIndex) ? previous : [...previous, material]);
  };
  useEffect(() => {
    if (generating || state.arrangementJobs?.some(job => job.status === 'running' || job.status === 'queued') || continuing.current) return;
    const next = continueQueue[0];
    if (!next) return;
    continuing.current = true;
    void call('arrangement.prepare', {
      draft, mode: 'ai',
      aiModelId: next.modelId,
      target: { sectionIndex: next.sectionIndex, phraseIndex: next.phraseIndex, partIndex: next.partIndex },
    }).catch(cause => {
        setRetryError(formatCallError(t, cause));
      })
      .finally(() => { continuing.current = false; setContinueQueue(previous => previous.slice(1)); });
  }, [continueQueue, generating, state.arrangementJobs, draft, t]);
  const zh = locale === 'zh-CN';
  const queued = materials.filter(material => ['queued', 'running'].includes(material.retryRequest?.status ?? ''));
  const title = !item ? '' : item.generation?.pitch === undefined ? `${item.instrument} · ${item.phrase || (zh ? 'AI 乐句' : 'AI phrase')}` : `${item.instrument} · MIDI ${item.generation.pitch} · ${item.generation.articulation}`;
  return <>
    {queued.length ? <small role="status">{zh ? `已排队/执行 ${queued.length} 项重新生成；正在生成的下一项完成后优先处理，切换页面不会丢失。` : `${queued.length} regenerations queued/running; processed after the current item, even if you leave this page.`}</small> : null}
    {!generating && queued.some(material => material.retryRequest?.status === 'queued') ? <Button size="sm" variant="secondary" label={zh ? '继续已排队的重新生成' : 'Resume queued regeneration'} onClick={() => { const next = queued.find(material => material.retryRequest?.status === 'queued'); if (next) void queueRetry(next.id, next.retryRequest?.feedback ?? '').catch(cause => setRetryError(formatCallError(t, cause))); }} /> : null}
    {retryError ? <small role="alert">{retryError}</small> : null}
    {deferred.size ? <Button size="sm" variant="ghost" label={zh ? '重新打开待试听候选' : 'Reopen pending reviews'} onClick={() => setDeferred(new Set())} /> : null}
    {item ? <AppDialog title={`${zh ? 'AI 音源试听确认' : 'Review AI audio'} · ${title}`} width={620}
    subtitle={zh ? `候选 ${item.generationResult?.attempt ?? 1} · ${item.modelId} · ${item.condition === 'score_conditioned' ? '谱条件乐句' : item.condition === 'sample_candidate' ? '可演奏音色采样' : '自由音频候选'}${generating ? ' · 下一项正在生成' : ''}` : `Candidate ${item.generationResult?.attempt ?? 1} · ${item.modelId} · ${generating ? 'Next item is generating' : 'Review the current result'}`}
    closeLabel={zh ? '稍后试听' : 'Review later'} onClose={() => dismiss(item.id)}>
    <VStack gap={3}>
      <small>{zh ? `目标乐句：${item.phrase || '当前曲谱声部'} · 有效长度 ${item.generation?.coreSeconds.toFixed(2)} 秒 · 原片 ${item.duration.toFixed(2)} 秒 · 尾音 ${item.generation?.tailSeconds.toFixed(2)} 秒` : `Phrase: ${item.phrase || 'current score part'} · effective ${item.generation?.coreSeconds.toFixed(2)}s · original ${item.duration.toFixed(2)}s · tail ${item.generation?.tailSeconds.toFixed(2)}s`}</small>
      <small>{zh ? '请直接听候选是否像目标乐器/人声、有没有多余内容、乐句是否自然结束。确认后立即用于当前编曲；重新生成或提交自定义意见会只重做这一项，其他已完成素材保留。' : 'Listen for the intended sound, unwanted content and a natural ending. Confirmed audio is used in this arrangement. Regeneration only replaces this item; completed assets remain.'}</small>
      <AIMaterialReview key={item.id} material={item} draft={draft} locale={locale} queue onResolved={dismiss} onAdopted={continueAfterAdoption} onRetry={queueRetry} />
      {pending.length > 1 ? <small role="status">{zh ? `还有 ${pending.length - 1} 项等待试听；正在生成的后续候选会完成后自动出现。` : `${pending.length - 1} more candidates await review. New results appear automatically when ready.`}</small> : null}
      <Button size="sm" variant="ghost" label={zh ? '暂不处理' : 'Decide later'} onClick={() => dismiss(item.id)} />
    </VStack>
  </AppDialog> : null}
  </>;
}
