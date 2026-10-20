import { useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { TextArea } from '@astryxdesign/core/TextArea';
import { useTranslator } from '@astryxdesign/core/i18n';
import { call } from '../../shared/lib/client';
import { formatCallError } from '../../shared/i18n/format';
import { AppDialog } from '../../shared/ui/app-dialog';
import type { AITask, Draft, State } from '../../shared/workbench';

export interface LyricsSelection { projectId: string; mode: 'generate' | 'optimize'; base: string; start: number; end: number }

export function LyricsTool({ draft, state, selection, apply, close }: { draft: Draft; state: State; selection: LyricsSelection; apply: (lyrics: string) => void; close: () => void }) {
  const t = useTranslator();
  const [brief, setBrief] = useState(draft.text);
  const [id, setID] = useState('');
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState('');
  const task = state.aiTask?.id === id ? state.aiTask : undefined;
  const running = starting || task?.status === 'running';
  const result = task?.status === 'completed' ? task.result as { lyrics?: string } : undefined;
  const hasResult = typeof result?.lyrics === 'string';
  const label = t(selection.mode === 'generate' ? '@yovoice.music.generateLyrics' : '@yovoice.music.optimizeLyrics');
  const run = async () => {
    setStarting(true); setError('');
    try {
      const next = await call<AITask>('ai.lyrics', { projectId: draft.id, brief, mode: selection.mode,
        currentLyrics: selection.base.slice(selection.start, selection.end), language: draft.synthesisLanguage ?? 'zh',
        seconds: draft.music ? draft.music.duration.mode === 'fixed' ? draft.music.duration.seconds : 0 : Number(draft.modelOptions?.ace_step?.duration_seconds ?? 0), locale: state.preferences.uiLocale });
      setID(next.id);
    } catch (e) { setError(formatCallError(t, e)); }
    finally { setStarting(false); }
  };
  const accept = () => {
    if ((draft.lyrics ?? '') !== selection.base) { setError(t('@yovoice.music.lyricsChanged')); return; }
    const next = selection.mode === 'generate' ? result!.lyrics! : selection.base.slice(0, selection.start) + result!.lyrics! + selection.base.slice(selection.end);
    if (next.length > 4000) { setError(t('@yovoice.music.lyricsTooLong')); return; }
    apply(next); close();
  };
  return <AppDialog title={label} width={680} error={error || (task?.status === 'failed' ? formatCallError(t, task) : undefined)} closeLabel={t('@yovoice.action.close')} onClose={() => {
    if (task?.status === 'running') void call('ai.cancel');
    close();
  }} actions={<>
    <Button label={t('@yovoice.action.cancel')} onClick={() => { if (task?.status === 'running') void call('ai.cancel'); close(); }} />
    {hasResult ? <Button label={t('@yovoice.ai.apply')} variant="primary" onClick={accept} /> : <Button label={label} variant="primary" isLoading={running} isDisabled={running || !brief.trim()} onClick={() => void run()} />}
  </>}>
    <TextArea label={t('@yovoice.music.lyricsRequirement')} value={brief} onChange={setBrief} rows={3} maxLength={2000} isDisabled={running} />
    <small>{t('@yovoice.music.lyricsPreviewHint')}</small>
    {selection.mode === 'optimize' ? <VStack gap={1}><small>{t('@yovoice.music.lyricsSource')}</small><p className="history-full-text">{selection.base.slice(selection.start, selection.end)}</p></VStack> : null}
    {running ? <HStack role="status" gap={2}><small>{t('@yovoice.ai.stage.writing', { model: task?.model ?? '' })}</small></HStack> : null}
    {hasResult ? <TextArea label={t('@yovoice.music.lyricsPreview')} value={result!.lyrics!} onChange={() => undefined} isReadOnly rows={10} /> : null}
  </AppDialog>;
}
