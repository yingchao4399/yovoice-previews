import { useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { TextInput } from '@astryxdesign/core/TextInput';
import { Settings2 } from 'lucide-react';
import { call, isDesktop } from '../../shared/lib/client';
import { Selector } from '../../shared/selector';
import { AppDialog } from '../../shared/ui/app-dialog';
import { SettingsGroup, SettingsRow } from '../../shared/ui/settings-group';
import { AIMaterialReview } from '../../shared/ui/ai-material-review';
import type { ArrangementGenerationSettings, ArrangementGenerationPreflight } from '../../shared/arrangement-generation';
import { generationStateLabel } from '../../shared/arrangement-generation';
import type { ArrangementPart, Draft, State, UiLocale } from '../../shared/workbench';

export function defaultGeneration(part: ArrangementPart): ArrangementGenerationSettings {
  if (!part.generation && part.role === 'drums') return { assetKind: 'phrase', mode: 'free', method: 'direct', lengthMode: 'phrase', soundType: 'percussion', tailSeconds: 0.5 };
  if (part.generation) return part.generation;
  const wordless = /合唱|人声|choir|vocal/i.test(part.instrument);
  return { assetKind: wordless ? 'phrase' : 'instrument', mode: 'score', method: wordless ? 'direct' : 'sampler', lengthMode: 'phrase', soundType: wordless ? 'wordless' : 'instrument', tailSeconds: 0.5 };
}

export function ArrangementGenerationSettingsButton({ draft, state, part, refIndex, modelId, locale, change }: {
  draft: Draft; state: State; part: ArrangementPart; refIndex: { sectionIndex: number; phraseIndex: number; partIndex: number }; modelId: string; locale: UiLocale; change: (patch: Partial<Draft>) => void;
}) {
  const [open, setOpen] = useState(false);
  const [settings, setSettings] = useState(() => defaultGeneration(part));
  const [preflight, setPreflight] = useState<ArrangementGenerationPreflight>();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [expanded, setExpanded] = useState('');
  const zh = locale === 'zh-CN';
  const say = (cn: string, en: string) => zh ? cn : en;
  const patch = (value: Partial<ArrangementGenerationSettings>) => { setSettings(previous => ({ ...previous, ...value })); setPreflight(undefined); };
  const updatedDraft = (): Draft => ({ ...draft, arrangement: draft.arrangement ? { ...draft.arrangement, sections: draft.arrangement.sections.map((s, si) => si !== refIndex.sectionIndex ? s : { ...s, phrases: s.phrases.map((p, pi) => pi !== refIndex.phraseIndex ? p : { ...p, parts: p.parts.map((a, ai) => ai !== refIndex.partIndex ? a : { ...a, generation: settings }) }) }) } : undefined });
  const inspect = async () => { setBusy(true); setError(''); try { setPreflight(await call<ArrangementGenerationPreflight>('arrangement.generation.preflight', { draft: updatedDraft(), target: refIndex, modelId })); } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); } finally { setBusy(false); } };
  const materials = (state.arrangementMaterials ?? []).filter(m => m.projectId === draft.id && (part.id ? m.partId === part.id : m.sectionIndex === refIndex.sectionIndex && m.phraseIndex === refIndex.phraseIndex && m.partIndex === refIndex.partIndex));
  const options = (entries: [string, string, string][]) => entries.map(([value, cn, en]) => ({ value, label: say(cn, en) }));
  return <>
    <Button size="sm" variant="ghost" icon={<Settings2 />} label={say('生成设置', 'Generation settings')} onClick={() => { setSettings(defaultGeneration(part)); setOpen(true); setError(''); setPreflight(undefined); }} />
    {open ? <AppDialog title={`${part.instrument} · ${say('生成设置', 'Generation settings')}`} width={760} busy={busy} closeLabel={say('关闭', 'Close')} onClose={() => setOpen(false)} error={error} actions={<>
      <Button label={say('检查任务', 'Inspect task')} isDisabled={!isDesktop} isLoading={busy} onClick={() => void inspect()} />
      <Button variant="primary" label={say('保存设置', 'Save settings')} isDisabled={busy} onClick={() => { change({ arrangement: updatedDraft().arrangement }); setOpen(false); }} />
    </>}>
      <SettingsGroup description={say('按谱默认由已确认的 AI 音色交给采样器演奏；模型直接生成仍是待试听候选。', 'Score mode uses confirmed AI samples played by the sampler. Direct model output is a candidate requiring audition.')}>
        <SettingsRow label={say('生成内容', 'Asset')}><Selector label={say('生成内容', 'Asset')} size="sm" value={settings.assetKind} options={options([['instrument', '可演奏音色', 'Playable instrument'], ['phrase', '独立乐句', 'Phrase']])} onChange={value => patch(value === 'instrument' ? { assetKind: 'instrument', mode: 'score', method: 'sampler', soundType: 'instrument' } : { assetKind: 'phrase' })} /></SettingsRow>
        <SettingsRow label={say('创作模式', 'Creative mode')}><Selector label={say('创作模式', 'Creative mode')} size="sm" value={settings.mode} options={options([['score', '按谱', 'Score'], ['free', '自由', 'Free']])} onChange={value => patch(value === 'free' ? { mode: 'free', assetKind: 'phrase', method: 'direct' } : { mode: 'score' })} /></SettingsRow>
        <SettingsRow label={say('实现方式', 'Method')}><Selector label={say('实现方式', 'Method')} size="sm" value={settings.method} options={options([['sampler', '音色按谱演奏', 'Sampler plays score'], ['direct', '模型直接生成乐句', 'Direct model phrase']])} onChange={value => patch(value === 'direct' ? { method: 'direct', assetKind: 'phrase' } : { method: 'sampler', mode: 'score' })} /></SettingsRow>
        <SettingsRow label={say('声音类别', 'Sound type')}><Selector label={say('声音类别', 'Sound type')} size="sm" value={settings.soundType} options={options([['instrument', '器乐', 'Instrument'], ['wordless', '无词合唱 / 人声哼唱', 'Wordless choir / vocal hum'], ['percussion', '打击', 'Percussion'], ['effect', '环境或效果', 'Ambience / effect']])} onChange={value => patch(value === 'percussion' || value === 'effect' ? { soundType: value, assetKind: 'phrase', method: 'direct', mode: 'free' } : value === 'wordless' ? { soundType: 'wordless', assetKind: 'phrase', method: 'direct' } : { soundType: 'instrument', assetKind: 'instrument', mode: 'score', method: 'sampler' })} /></SettingsRow>
        <SettingsRow label={say('长度', 'Length')} hint={say('不会修改已审核曲谱；长度不足会报告冲突。', 'Approved notes remain intact. Insufficient length is reported.')}><Selector label={say('长度方式', 'Length mode')} size="sm" value={settings.lengthMode} options={options([['phrase', '跟随本句', 'Follow phrase'], ['bars', '指定小节', 'Bars'], ['seconds', '指定秒数', 'Seconds']])} onChange={value => patch({ lengthMode: value as ArrangementGenerationSettings['lengthMode'], bars: settings.bars ?? 1, seconds: settings.seconds ?? 9 })} />
          {settings.lengthMode !== 'phrase' ? <TextInput size="sm" label={say(settings.lengthMode === 'bars' ? '小节数' : '秒数', settings.lengthMode === 'bars' ? 'Bars' : 'Seconds')} value={String(settings.lengthMode === 'bars' ? settings.bars ?? 1 : settings.seconds ?? 9)} onChange={value => patch(settings.lengthMode === 'bars' ? { bars: Number(value) } : { seconds: Number(value) })} /> : null}
        </SettingsRow>
        <SettingsRow label={say('保留尾音（秒）', 'Keep tail (s)')}><TextInput size="sm" label={say('保留尾音（秒）', 'Keep tail (s)')} value={String(settings.tailSeconds)} onChange={value => patch({ tailSeconds: Number(value) })} /></SettingsRow>
        <SettingsRow label={say('模型', 'Model')}><Selector label={say('生成模型', 'Generation model')} size="sm" value={settings.modelId ?? ''} options={[{ value: '', label: say('跟随编曲模型', 'Follow arrangement model') }, ...state.models.filter(m => /yue2|ace-step/.test(m.id)).map(m => ({ value: m.id, label: m.id }))]} onChange={value => patch({ modelId: value || undefined })} /></SettingsRow>
      </SettingsGroup>
      {preflight ? <SettingsGroup title={say('任务预检', 'Preflight')}>
        <VStack className="settings-row" gap={2}>
          <small>{say('音乐有效 / 模型请求 / 尾音', 'Core / requested / tail')}: {preflight.contract.coreSeconds.toFixed(2)} / {preflight.contract.requestSeconds.toFixed(2)} / {preflight.contract.tailSeconds.toFixed(2)}s</small>
          <small>{say('采样项 / 已复用 / 最多候选次数 / 预估耗时', 'Cells / reused / maximum candidates / time')}: {preflight.cells.length} / {preflight.reused} / {preflight.candidates} / {say('未知', 'Unknown')}</small>
          <small>{say('接收谱条件', 'Score conditioning')}: {String(preflight.capability.scoreConditioning)} · {preflight.capability.minDuration}–{preflight.capability.maxDuration}s · {say('续写', 'Continuation')}: {String(preflight.capability.continuation)}</small>
          <small>{preflight.capability.evidence}</small>
          <small>{say('原调性 / 模型记谱上下文', 'Original key / model notation context')}: {draft.arrangement?.key ?? ''} / {preflight.contract.normalizedKey ?? ''}</small>
          {preflight.warnings?.map(warning => <small key={warning} role="status">{warning}</small>)}
          {preflight.issues.map(issue => <small key={issue} role="alert">{issue}</small>)}
        </VStack>
        {preflight.cells.map(cell => <SettingsRow key={cell.key} label={`MIDI ${cell.pitch} · ${cell.articulation} · ${cell.velocityLow}–${cell.velocityHigh}`} hint={`${cell.sustainSeconds.toFixed(2)}s · ${generationStateLabel(cell.state, zh)}${cell.pilot ? say(' · 代表采样', ' · pilot') : ''}`} />)}
      </SettingsGroup> : null}
      {materials.length ? <SettingsGroup title={say('原片与候选', 'Originals and candidates')}>
        {materials.map(m => <VStack className="settings-row" gap={2} key={m.id}>
          <HStack hAlign="between" gap={2} wrap="wrap"><small>{m.generation?.pitch === undefined ? m.phrase : `MIDI ${m.generation.pitch} · ${m.generation.articulation}`} · {generationStateLabel(m.generationResult?.state ?? m.reviewStatus ?? '', zh)} · {m.duration.toFixed(2)}s</small><Button size="sm" label={expanded === m.id ? say('收起', 'Collapse') : say('检查与试听', 'Inspect and audition')} onClick={() => setExpanded(expanded === m.id ? '' : m.id)} /></HStack>
          {expanded === m.id ? <AIMaterialReview key={m.id} material={m} draft={draft} locale={locale} /> : null}
        </VStack>)}
      </SettingsGroup> : null}
    </AppDialog> : null}
  </>;
}
