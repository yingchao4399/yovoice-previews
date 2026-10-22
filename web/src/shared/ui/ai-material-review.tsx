import { useEffect, useRef, useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { TextInput } from '@astryxdesign/core/TextInput';
import { TextArea } from '@astryxdesign/core/TextArea';
import { Play, Pause } from 'lucide-react';
import { useTranslator } from '@astryxdesign/core/i18n';
import { formatCallError } from '../i18n/format';
import { call, mediaUrl } from '../lib/client';
import type { ArrangementMaterial, Draft, UiLocale } from '../workbench';
import { generationStateLabel } from '../arrangement-generation';

function Audition({ file, label, heard }: { file: string; label: string; heard: () => void }) {
  const audio = useRef<HTMLAudioElement>(null);
  const [url, setUrl] = useState('');
  const [playing, setPlaying] = useState(false);
  const [time, setTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [error, setError] = useState('');
  useEffect(() => { let cancelled = false; void mediaUrl('outputs', file).then(value => { if (!cancelled) setUrl(value); }).catch(cause => { if (!cancelled) setError(String(cause)); }); return () => { cancelled = true; }; }, [file]);
  return <VStack gap={1}>
    <HStack gap={2} vAlign="center">
      <Button size="sm" variant="ghost" label={label} icon={playing ? <Pause /> : <Play />} isDisabled={!url} onClick={() => { const player = audio.current; if (!player) return; if (playing) player.pause(); else void player.play().catch(cause => setError(String(cause))); }} />
      <input className="preview-progress" aria-label={`${label} · progress`} type="range" min={0} max={duration || 1} step={0.01} value={Math.min(time, duration || 1)} disabled={!duration} onChange={event => { if (audio.current) audio.current.currentTime = Number(event.target.value); }} />
      <small>{time.toFixed(1)} / {duration.toFixed(1)}s</small>
    </HStack>
    <audio ref={audio} src={url || undefined} preload="metadata" onPlay={() => { for (const other of document.querySelectorAll('audio')) { if (other !== audio.current) other.pause(); } setPlaying(true); heard(); }} onPause={() => setPlaying(false)} onEnded={() => setPlaying(false)} onTimeUpdate={() => setTime(audio.current?.currentTime ?? 0)} onLoadedMetadata={() => setDuration(Number.isFinite(audio.current?.duration) ? audio.current!.duration : 0)} onError={() => setError('Audio unavailable')} />
    {error ? <small role="alert">{error}</small> : null}
  </VStack>;
}

// 只记录用户试听结论；本地波形检查不会被描述成乐器/人声识别。
export function AIMaterialReview({ material, draft, locale, queue = false, onResolved, onAdopted, onRetry }: { material: ArrangementMaterial; draft?: Draft; locale: UiLocale; queue?: boolean; onResolved?: (id: string) => void; onAdopted?: (material: ArrangementMaterial) => void; onRetry?: (id: string, feedback: string) => void | Promise<void> }) {
  const t = useTranslator();
  const zh = locale === 'zh-CN';
  const say = (cn: string, en: string) => zh ? cn : en;
  const [offset, setOffset] = useState(String(material.generation?.effectiveOffset ?? 0));
  const [tail, setTail] = useState(String(material.generation?.tailSeconds ?? 0.5));
  const [, setHeard] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [updated, setUpdated] = useState<ArrangementMaterial>();
  const [deleting, setDeleting] = useState(false);
  const [feedback, setFeedback] = useState('');
  const m = updated && updated.id === material.id ? updated : material;
  const c = m.generation;
  const r = m.generationResult;
  const perform = async (action: () => Promise<unknown>) => { setBusy(true); setError(''); try { await action(); } catch (cause) { setError(formatCallError(t, cause)); } finally { setBusy(false); } };
  if (!c || !r) return null;
  const isWordless = c.settings.soundType === 'wordless';
  const processing = ['queued', 'generating', 'checking'].includes(r.state);
  const confirmHint = processing
    ? say('本项仍在生成或检查；如果已有可试听文件，你仍可以先作出采用决定，风险会保留记录。', 'This item is still generating or checking. If an audition file is available, you can still decide to adopt it; risks remain recorded.')
    : r.state !== 'needs_review'
      ? say(`当前自动检查状态为“${generationStateLabel(r.state, zh)}”；确认按钮仍由你决定，风险会保留记录。`, `Automatic checks report “${generationStateLabel(r.state, false)}”; you still decide, and the warning will be retained.`)
      : say('确认采用由你决定，不要求系统检测到播放或自动识别全部内容。', 'Adoption is your decision; playback detection and automatic recognition are not required.');
  return <VStack gap={3}>
    <small>{say('状态', 'State')}: {generationStateLabel(r.state, zh)} · {say('尝试', 'Attempt')} {r.attempt} · {c.pitch === undefined ? say('独立乐句', 'Phrase') : `MIDI ${c.pitch} · ${c.articulation} · ${c.velocityLow}–${c.velocityHigh}`} · {isWordless ? say('无词合唱 / 人声哼唱', 'Wordless choir / vocal hum') : say('器乐候选', 'Instrument candidate')}</small>
    <small>{say('音乐有效长度 / 模型请求长度 / 原片 / 尾音', 'Musical core / requested / raw / tail')}: {c.coreSeconds.toFixed(2)} / {c.requestSeconds.toFixed(2)} / {m.duration.toFixed(2)} / {c.tailSeconds.toFixed(2)}s</small>
    <small>{c.modelId} · {c.engineVersion} · {c.version}{r.semanticFrames ? ` · ${r.semanticFrames} frames` : ''}{r.truncated ? say(' · 达到上限截断', ' · truncated at budget') : ''}</small>
    {m.duration > 0 ? <Audition key={m.fileName} file={m.fileName} label={say('试听原片', 'Audition original')} heard={() => setHeard(true)} /> : null}
    {r.derivedFile ? <Audition key={r.derivedFile} file={r.derivedFile} label={say('试听适配后', 'Audition fitted')} heard={() => setHeard(true)} /> : null}
    {(r.checks ?? []).map((check, i) => <small key={`${check.code}-${i}`} role={check.status === 'fail' ? 'alert' : undefined}>{check.status === 'pass' ? '✓' : '!'} {check.message}</small>)}
    {m.retryRequest?.error ? <small role="alert">{m.retryRequest.error}</small> : null}
    {m.duration > 0 && m.reviewStatus !== 'confirmed' ? <HStack gap={2} wrap="wrap" vAlign="end">
      <TextInput size="sm" label={say('原片起点（秒）', 'Original offset (s)')} value={offset} onChange={setOffset} />
      <TextInput size="sm" label={say('保留尾音（秒）', 'Keep tail (s)')} value={tail} onChange={setTail} />
      <Button size="sm" label={say('检查所选区段', 'Check selected region')} isDisabled={busy} onClick={() => void perform(async () => { const fitted = await call<ArrangementMaterial>('arrangement.material.fit', { id: m.id, offset: Number(offset), seconds: c.coreSeconds, tailSeconds: Number(tail) }); setUpdated(fitted); setHeard(false); })} />
    </HStack> : null}
    <small>{isWordless ? say('请重点听：是否为需要的无词人声，有没有多余的歌词或伴奏，最后一个音是否自然放开，而非突然中断。无需懂乐谱；试听确认不代表已自动核对每个音符和节奏。它作为合唱乐句使用，不登记成器乐音色。', 'Listen for the intended wordless voices, unwanted lyrics or accompaniment, and a final note that releases naturally rather than stopping abruptly. No score reading is needed; audition confirmation does not verify every note or rhythm. This stays a choir phrase, never an instrument sample.') : say('试听确认：乐器身份正确、无不需要的人声或伴奏、奏法及内容完整、同组音色一致。确认后才会入库供编曲使用。', 'Confirm by listening: correct instrument, no unwanted vocals/accompaniment, complete articulation/content, consistent timbre. Only confirmed assets become playable.')}</small>
    {queue && draft && m.projectId === draft.id && m.reviewStatus !== 'confirmed' ? <TextArea label={say('自定义修改意见（可选）', 'Custom revision feedback (optional)')} value={feedback} rows={3} maxLength={1000} placeholder={say('例如：音色柔和一些，保持当前乐器、奏法和乐句长度。', 'For example: soften the timbre while preserving this instrument, articulation and phrase length.')} onChange={setFeedback} /> : null}
    <HStack gap={2} wrap="wrap">
      <Button size="sm" variant="primary" label={m.reviewStatus === 'confirmed' ? say('已确认采用', 'Confirmed') : queue ? say('确认采用，继续下一项', 'Adopt and review next') : say('确认采用', 'Confirm adoption')} isDisabled={busy || m.reviewStatus === 'confirmed'} onClick={() => void perform(async () => { await call('arrangement.material.review', { id: m.id, confirmed: true }); setUpdated(undefined); onAdopted?.(m); onResolved?.(m.id); })} />
      {draft && m.projectId === draft.id && m.reviewStatus !== 'confirmed' ? <Button size="sm" label={queue && feedback.trim() ? say('按意见重新生成', 'Regenerate with feedback') : queue ? say('重新生成这一项', 'Regenerate this item') : say('仅重试本项', 'Retry this item')} isDisabled={busy} onClick={() => void perform(async () => { if (queue && onRetry) await onRetry(m.id, feedback); else { await call('arrangement.material.retry', { id: m.id, draft, feedback: '' }); onResolved?.(m.id); } })} /> : null}
      {m.reviewStatus !== 'confirmed' ? <Button size="sm" variant={deleting ? 'destructive' : 'ghost'} label={deleting ? say('确认删除此候选', 'Confirm deletion') : say('删除候选', 'Delete candidate')} isDisabled={busy || r.state === 'generating' || r.state === 'checking'} onClick={() => { if (!deleting) setDeleting(true); else void perform(() => call('arrangement.material.delete', { id: m.id })); }} /> : null}
      {deleting ? <Button size="sm" variant="ghost" label={say('取消删除', 'Cancel deletion')} onClick={() => setDeleting(false)} /> : null}
    </HStack>
    {confirmHint ? <small role="status">{confirmHint}</small> : null}
    {error ? <small role="alert">{error}</small> : null}
  </VStack>;
}
