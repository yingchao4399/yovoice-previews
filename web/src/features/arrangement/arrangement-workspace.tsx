import { useEffect, useRef, useState } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { TextInput } from '@astryxdesign/core/TextInput';
import { TextArea } from '@astryxdesign/core/TextArea';
import { useTranslator } from '@astryxdesign/core/i18n';
import { Check, Download, FileMusic, Layers3, LockKeyhole, Plus, Settings2, Sparkles, UnlockKeyhole } from 'lucide-react';
import { call, isDesktop } from '../../shared/lib/client';
import modelCatalog from '../../shared/lib/catalog.json';
import { formatCallError } from '../../shared/i18n/format';
import { Selector } from '../../shared/selector';
import { AIComposer } from '../../shared/ui/ai-composer';
import { SettingsGroup, SettingsRow } from '../../shared/ui/settings-group';
import { InstrumentDownloadProgress } from '../../shared/ui/instrument-download-progress';
import { formatTime, isUntitled, type ArrangementInput, type ArrangementPlan, type ArrangementPart, type ArrangementSource, type Draft, type InstrumentSource, type State, type UiLocale } from '../../shared/workbench';
import { composedScorePatch, noteCount } from '../score/score-draft';
import { ArrangementReference } from './arrangement-reference';
import { bindArrangementSources, matchedRealSource, realPartReady, sameInstrument } from './arrangement-sources';

const durations = [30, 60, 90, 120, 180, 300, 480, 600];
const sourceModes = ['auto', 'real', 'ai', 'hybrid'] as const;
const audioModels = modelCatalog.filter(model => model.family === 'ace_step' || model.family === 'yue2');

interface ArrangementInspection {
  revision: string; ready: boolean; score?: Draft['score'];
  issues: { code: string; message: string }[];
  parts: { partId?: string; materialId?: string; materialCondition?: string; reviewStatus?: string; ref: { sectionIndex: number; phraseIndex: number; partIndex: number }; ready: boolean; issues: { code: string; message: string }[];
    candidates: { id: string; name: string; ensemble: boolean }[] }[];
}

function scoreBrief(plan: ArrangementPlan, sources: InstrumentSource[]) {
  const phrases = plan.sections.flatMap(section => section.phrases.map(phrase => `${section.name} ${phrase.startBar}-${phrase.endBar}: ${phrase.name}; ${phrase.parts.map(part => `${part.instrument}/${part.role}/${part.technique ?? ''}/${part.source}`).join(', ')}`));
  const ranges = new Map<string, string>();
  for (const part of plan.sections.flatMap(section => section.phrases.flatMap(phrase => phrase.parts))) {
    if (part.source !== 'real') continue;
    const source = matchedRealSource(part, sources);
    if (!source?.samples.length) continue;
    const roots = source.samples.map(sample => sample.rootPitch);
    ranges.set(part.id ?? `${part.instrument}/${part.sourceId}`, `${part.id ?? part.instrument}: use downloaded source ${source.name}; playable MIDI pitches ${Math.max(0, Math.min(...roots) - 7)}–${Math.min(127, Math.max(...roots) + 7)}`);
  }
  return `${plan.brief ?? plan.title}\nTempo ${plan.tempo}, meter ${plan.timeSignature.join('/')}, key ${plan.key ?? ''}. Keep all phrase boundaries, entrances, solos and techniques. GM programs are previews; named instruments and real/AI source decisions remain in the project plan. Do not write notes outside the listed installed source pitch ranges.\n${[...ranges.values()].join('\n')}\n${phrases.join('\n')}`;
}

// 独立的智能编曲页面：先审核整曲分句配器，再调用已有 AI 写谱生成可编辑 MIDI 曲谱。
export function ArrangementWorkspace({ active, draft, state, change, locale, openSettings, openScore, openDraft, create, onAssemblyReady }: {
  active: boolean; draft: Draft; state: State; change: (patch: Partial<Draft>) => void; locale: UiLocale;
  openSettings: () => void; openScore: () => void; openDraft: (draft: Draft) => void; create: (input: ArrangementInput) => void;
  onAssemblyReady?: (draft: Draft) => void;
}) {
  const t = useTranslator();
  const [pendingInput, setPendingInput] = useState<ArrangementInput>({ brief: '', seconds: 90, source: 'auto' });
  const [error, setError] = useState('');
  const [preparing, setPreparing] = useState(false);
  const [materialBusy, setMaterialBusy] = useState(false);
  const [materialPhase, setMaterialPhase] = useState('');
  const [aiModelID, setAIModelID] = useState(() => draft.arrangement?.aiModelId ?? state.models.find(model => audioModels.some(item => item.id === model.id))?.id ?? audioModels[0]?.id ?? '');
    const [bandID, setBandID] = useState('');
  const [bandNotice, setBandNotice] = useState('');
  const [inspection, setInspection] = useState<ArrangementInspection>();
  const [substitutionParts, setSubstitutionParts] = useState<ReadonlySet<string>>(new Set());
  const downloadedSources = useRef<InstrumentSource[] | null>(null);
  const plan = draft.kind === 'score' && Array.isArray(draft.arrangement?.sections) && Array.isArray(draft.arrangement.timeSignature) ? draft.arrangement : undefined;
  const readyAI = new Set<string>();
  if (plan) for (const [si, section] of plan.sections.entries()) for (const [pi, phrase] of section.phrases.entries()) for (const [ai, part] of phrase.parts.entries()) {
    if (part.source !== 'ai') continue;
    const ref = si + ':' + pi + ':' + ai;
    if (inspection?.parts.some(item => item.ref.sectionIndex === si && item.ref.phraseIndex === pi && item.ref.partIndex === ai && item.materialId)) readyAI.add(ref);
  }
  const sourceIssues = isDesktop ? (inspection?.issues.map(issue => issue.message) ?? []) : plan && draft.score ? bindArrangementSources(plan, draft.score, state.instrumentSources ?? [], readyAI).missing : [];
  const inspectInput = JSON.stringify({ draft: { ...draft, timeline: undefined }, stage: 'score', aiModelId: aiModelID });
  const hasPlan = !!plan;
  useEffect(() => {
    if (!active || !hasPlan || !isDesktop) return;
    let cancelled = false;
    const timer = setTimeout(() => { void call<ArrangementInspection>('arrangement.inspect', JSON.parse(inspectInput)).then(result => { if (!cancelled) setInspection(result); }).catch(cause => { if (!cancelled) setError(formatCallError(t, cause)); }); }, 350);
    return () => { cancelled = true; clearTimeout(timer); };
  }, [active, hasPlan, inspectInput, state.instrumentSources, state.arrangementMaterials, t]);
  const currentJob = state.arrangementJobs?.filter(job => job.projectId === draft.id).at(-1);
  const jobRunning = currentJob?.status === 'queued' || currentJob?.status === 'running';
  const input = draft.kind === 'score' ? draft.arrangementInput ?? { brief: plan?.brief ?? '', seconds: plan?.seconds ?? 90, source: plan?.sourceMode ?? 'auto' } : pendingInput;
  const updateInput = (patch: Partial<ArrangementInput>) => {
    const next = { ...input, ...patch };
    if (draft.kind === 'score') change({ arrangementInput: next }); else setPendingInput(next);
  };
  const planning = state.aiTask?.kind === 'arrangement' && state.aiTask.status === 'running' && state.aiTask.projectId === draft.id;
  const projects = state.drafts.filter(item => item.kind === 'score');
  useEffect(() => { setError(''); downloadedSources.current = null; setBandNotice(''); }, [draft.id]);
  useEffect(() => { downloadedSources.current = state.instrumentSources ?? []; }, [state.instrumentSources]);
  useEffect(() => { setAIModelID(draft.arrangement?.aiModelId ?? state.models.find(model => audioModels.some(item => item.id === model.id))?.id ?? audioModels[0]?.id ?? ''); }, [draft.id, draft.arrangement?.aiModelId, state.models]);
  if (!active) return null;
  const sourceOptions = sourceModes.map(value => ({ value, label: t(`@yovoice.arrangement.mode.${value}`) }));
  const replacePart = (si: number, pi: number, ai: number, patch: Partial<ArrangementPart>) => {
    if (!plan) return;
    change({ arrangement: { ...plan, approved: plan.approved && !('instrument' in patch || 'technique' in patch), sections: plan.sections.map((section, s) => s !== si ? section : {
      ...section, phrases: section.phrases.map((phrase, p) => p !== pi ? phrase : {
        ...phrase, parts: phrase.parts.map((part, a) => a !== ai ? part : { ...part, ...patch }),
      }),
    }) } });
  };
  async function bindSource(part: ArrangementPart, sourceId: string, allowSubstitution = false, ref?: { sectionIndex: number; phraseIndex: number; partIndex: number }) {
    if (!part.id && !ref) { setError(t('@yovoice.arrangement.replanForBinding')); return; }
    setError('');
    try {
      const result = await call<Draft>('arrangement.bindSource', { draft, partId: part.id, ref, sourceId, allowSubstitution });
      change({ arrangement: result.arrangement, score: result.score, arrangementBindings: result.arrangementBindings });
    } catch (cause) { setError(formatCallError(t, cause)); }
  }
  async function approveAndCompose() {
    if (!plan || preparing) return;
    if (plan.sections.some(section => section.phrases.some(phrase => phrase.parts.some(part => part.source === 'auto')))) { setError(t('@yovoice.arrangement.sourceUndecided')); return; }
    setError('');
    setPreparing(true);
    change({ arrangement: { ...plan, approved: true, aiModelId: aiModelID } });
    try { await call('ai.score', { projectId: draft.id, brief: scoreBrief(plan, state.instrumentSources ?? []), seconds: plan.seconds, locale, arrangement: { ...plan, approved: true, aiModelId: aiModelID } }); }
    catch (cause) { setError(formatCallError(t, cause)); }
    finally { setPreparing(false); }
  }
  async function snapshot() {
    const result = await call<{ state: State }>('state.get', {});
    downloadedSources.current = result.state.instrumentSources ?? [];
    return result.state;
  }
  async function waitForActivity(kind: 'generate' | 'instrumentDownload', limitMs: number) {
    const deadline = Date.now() + limitMs;
    while (Date.now() < deadline) {
      const current = await snapshot();
      const activity = kind === 'generate' ? current.activity : current.modelActivity;
      if (activity?.kind === kind && (kind !== 'generate' || activity.projectId === draft.id)) {
        if (activity.status === 'completed') return current;
        if (activity.status === 'failed' || activity.status === 'cancelled') throw new Error(String(activity.errorParams?.detail ?? activity.errorCode ?? t('@yovoice.arrangement.downloadFailed')));
      }
      await new Promise(resolve => setTimeout(resolve, 700));
    }
    throw new Error(t('@yovoice.arrangement.prepareTimeout'));
  }
  async function prepare(mode: 'real' | 'ai' | 'both', target?: { section: number; phrase: number; part: number }) {
    if (!plan || materialBusy || !isDesktop) return;
    setMaterialBusy(true); setError('');
    try {
      await call('arrangement.prepare', { draft, mode, aiModelId: aiModelID,
        target: target ? { sectionIndex: target.section, phraseIndex: target.phrase, partIndex: target.part } : undefined });
      await snapshot();
    } catch (cause) { setError(formatCallError(t, cause)); }
    finally { setMaterialBusy(false); setMaterialPhase(''); }
  }
  async function assemble() {
    if (!plan || !draft.score || materialBusy || !isDesktop) return;
    setMaterialBusy(true); setMaterialPhase(t('@yovoice.arrangement.assembling')); setError('');
    try {
      const currentSources = (await snapshot()).instrumentSources ?? [];
      const checked = await call<ArrangementInspection>('arrangement.inspect', { draft, stage: 'render', aiModelId: aiModelID });
      setInspection(checked);
      if (!checked.ready) { setError(checked.issues.map(issue => issue.message).join('；')); return; }
      const boundScore = checked.score ?? bindArrangementSources(plan, draft.score, currentSources, readyAI).score;
      const assembledDraft = { ...draft, score: boundScore };
      await call('arrangement.render', { draft: assembledDraft, aiModelId: aiModelID });
      const current = await waitForActivity('generate', 20 * 60 * 1000);
      const saved = current.drafts.find(item => item.id === draft.id);
      if (!saved?.timeline?.tracks.length) throw new Error(t('@yovoice.arrangement.assemblyFailed'));
      onAssemblyReady?.(saved);
    } catch (cause) { setError(formatCallError(t, cause)); }
    finally { setMaterialBusy(false); setMaterialPhase(''); }
  }
  async function saveBand() {
    if (!plan || !isDesktop) return;
    setBandNotice(''); setError('');
    try {
      await call('arrangement.band.save', { draft, aiModelId: aiModelID });
      setBandNotice(t('@yovoice.arrangement.bandSaved'));
    } catch (cause) { setError(formatCallError(t, cause)); }
  }
  function applyBand() {
    const band = state.bandPresets?.find(item => item.id === bandID);
    if (!plan || !band) return;
    change({ arrangement: { ...plan, approved: false, sections: plan.sections.map(section => ({ ...section, phrases: section.phrases.map(phrase => ({ ...phrase,
      parts: phrase.parts.map(part => { if (part.locked) return part; const saved = band.parts.find(item => sameInstrument(item.instrument, part.instrument) && item.role === part.role); return saved ? { ...part, source: saved.source, sourceId: saved.sourceId, allowSubstitution: saved.allowSubstitution, technique: saved.technique || part.technique } : part; }),
    })) })) } });
    if (band.aiModelId) setAIModelID(band.aiModelId);
    setBandNotice(t('@yovoice.arrangement.bandApplied'));
  }
  return <VStack className="settings-page arrangement-page" gap={6}>
    <HStack as="header" hAlign="between" vAlign="center" wrap="wrap" gap={3}><VStack gap={1}><h1>{t('@yovoice.arrangement.title')}</h1><small>{t('@yovoice.arrangement.intro')}</small></VStack><Button size="sm" icon={<Plus />} label={t('@yovoice.arrangement.new')} onClick={() => create(input)} /></HStack>
    <SettingsGroup title={t('@yovoice.arrangement.project')}>
      <SettingsRow label={t('@yovoice.arrangement.current')} hint={t('@yovoice.arrangement.projectHint')}>
        <Selector size="sm" label={t('@yovoice.arrangement.current')} isLabelHidden value={draft.kind === 'score' ? draft.id : ''} options={[{ value: '', label: t('@yovoice.arrangement.chooseProject') }, ...projects.map(item => ({ value: item.id, label: item.title }))]} onChange={id => { const item = projects.find(project => project.id === id); if (item) openDraft(item); }} width="calc(var(--spacing-10) * 7)" />
      </SettingsRow>
    </SettingsGroup>
    {draft.kind === 'score' ? <ArrangementReference draft={draft} state={state} change={change} openSettings={openSettings} openScore={openScore} /> : null}
    <SettingsGroup title={t('@yovoice.arrangement.planStage')} description={t('@yovoice.arrangement.planHint')}>
        <TextArea label={t('@yovoice.ai.brief.arrangement')} value={input.brief} rows={5} maxLength={2000} placeholder={t('@yovoice.arrangement.briefPlaceholder')} hasSpellCheck={false} onChange={brief => updateInput({ brief })} />
        <SettingsRow label={t('@yovoice.arrangement.duration')}><Selector size="sm" label={t('@yovoice.arrangement.duration')} isLabelHidden value={String(input.seconds)} options={durations.map(value => ({ value: String(value), label: formatTime(value) }))} isDisabled={planning} onChange={value => updateInput({ seconds: Number(value) })} /></SettingsRow>
        <SettingsRow label={t('@yovoice.arrangement.sourceMode')} hint={t('@yovoice.arrangement.sourceHint')}><Selector size="sm" label={t('@yovoice.arrangement.sourceMode')} isLabelHidden value={input.source} options={sourceOptions} isDisabled={planning} onChange={value => updateInput({ source: value as (typeof sourceModes)[number] })} /></SettingsRow>
        {draft.kind === 'score' ? <AIComposer kind="arrangement" projectId={draft.id} state={state} overwrite={!!plan} configure={openSettings} apply={result => {
          const next = result as ArrangementPlan;
          change({ arrangement: { ...next, brief: state.aiTask?.brief ?? input.brief, sourceMode: input.source, approved: false }, ...(next.title && isUntitled(draft.title) ? { title: next.title.slice(0, 120) } : {}) });
        }} externalBrief={input.brief} placeholder={t('@yovoice.arrangement.briefPlaceholder')} submitLabel={t('@yovoice.arrangement.planAction')} start={brief => call('ai.arrangement.plan', { projectId: draft.id, brief, seconds: input.seconds, source: input.source, referenceAbc: input.referenceAbc, locale })} /> : <Button size="sm" variant="primary" icon={<Plus />} label={t('@yovoice.arrangement.new')} onClick={() => create(input)} />}
      </SettingsGroup>
    {draft.kind === 'score' ? <>
      {plan ? <VStack gap={5}>
        <HStack hAlign="between" vAlign="center" wrap="wrap" gap={2}><VStack gap={1}><h2>{plan.title}</h2><small>{plan.tempo} BPM · {plan.timeSignature.join('/')} · {plan.seconds}s · {plan.key}</small></VStack><Button size="sm" variant="secondary" icon={<Settings2 />} label={t('@yovoice.arrangement.aiSettings')} onClick={openSettings} /></HStack>
        {plan.sections.map((section, si) => <SettingsGroup key={`${si}-${section.startBar}`} title={`${section.name} · ${section.startBar}–${section.endBar}`}>
          {section.phrases.map((phrase, pi) => <VStack key={`${pi}-${phrase.startBar}`} className="settings-row" gap={3}>
            <HStack hAlign="between" wrap="wrap" gap={2}><b>{phrase.name}</b><small>{t('@yovoice.arrangement.bars', { start: phrase.startBar, end: phrase.endBar })}</small></HStack>
            {phrase.parts.map((part, ai) => <HStack key={`${ai}-${part.role}`} gap={2} vAlign="center" wrap="wrap">
              <TextInput size="sm" label={t('@yovoice.arrangement.instrument')} value={part.instrument} onChange={instrument => replacePart(si, pi, ai, { instrument, sourceId: undefined })} width="calc(var(--spacing-10) * 4)" />
              <TextInput size="sm" label={t('@yovoice.arrangement.technique')} value={part.technique ?? ''} onChange={technique => replacePart(si, pi, ai, { technique })} width="calc(var(--spacing-10) * 4)" />
              <Selector size="sm" label={t('@yovoice.arrangement.source')} value={part.source} options={sourceOptions.filter(item => item.value === 'real' || item.value === 'ai' || item.value === 'auto')} onChange={value => replacePart(si, pi, ai, { source: value as ArrangementSource, sourceId: undefined })} />
              <Selector size="sm" label={t('@yovoice.arrangement.specificSource')} value={part.sourceId ?? ''} hasSearch={(state.instrumentSources ?? []).length > 8}
                isDisabled={part.locked}
                options={[{ value: '', label: `${t('@yovoice.arrangement.autoMatch')}${part.source === 'real' && matchedRealSource(part, state.instrumentSources ?? []) ? ` · ${matchedRealSource(part, state.instrumentSources ?? [])?.name}` : ''}` }, ...(state.instrumentSources ?? []).filter(source =>
                  (source.samples.length > 0 || !!source.plugin) && (part.source === 'auto' || source.origin === part.source) && (source.id === part.sourceId || substitutionParts.has(part.id ?? '') || (!/模拟/.test(source.instrument) && source.provenance !== 'synthesized' && sameInstrument(source.instrument, part.instrument)))).map(source => ({ value: source.id, label: `${source.name} · ${source.instrument}${source.provenance === 'synthesized' ? ' · ' + t('@yovoice.plugins.synthesized') : ''}` }))]}
                onChange={sourceId => {
                  if (isDesktop) void bindSource(part, sourceId, substitutionParts.has(part.id ?? ''), { sectionIndex: si, phraseIndex: pi, partIndex: ai });
                  else replacePart(si, pi, ai, { sourceId: sourceId || undefined, allowSubstitution: substitutionParts.has(part.id ?? '') });
                }} />
              <Button size="sm" variant="ghost" label={t(substitutionParts.has(part.id ?? '') ? '@yovoice.arrangement.matchingOnly' : '@yovoice.arrangement.chooseSubstitute')} isDisabled={part.locked} onClick={() => setSubstitutionParts(previous => { const next = new Set(previous); const id = part.id ?? ''; if (next.has(id)) next.delete(id); else next.add(id); return next; })} />
              {inspection?.parts.find(item => item.ref.sectionIndex === si && item.ref.phraseIndex === pi && item.ref.partIndex === ai)?.issues.map((issue, index) => <small key={`${issue.code}-${index}`} role="status">{issue.message}</small>)}
              {!part.locked && inspection?.parts.find(item => item.ref.sectionIndex === si && item.ref.phraseIndex === pi && item.ref.partIndex === ai)?.issues.length ? inspection.parts.find(item => item.ref.sectionIndex === si && item.ref.phraseIndex === pi && item.ref.partIndex === ai)?.candidates.filter(candidate => candidate.id !== part.sourceId).slice(0, 3).map(candidate => <Button key={candidate.id} size="sm" variant="secondary" label={`${t('@yovoice.arrangement.useLocal')} ${candidate.name}${candidate.ensemble ? ` · ${t('@yovoice.arrangement.ensembleSample')}` : ''}`} onClick={() => void bindSource(part, candidate.id, false, { sectionIndex: si, phraseIndex: pi, partIndex: ai })} />) : null}
              <Button size="sm" variant="ghost" icon={part.locked ? <LockKeyhole /> : <UnlockKeyhole />} label={part.locked ? t('@yovoice.arrangement.unlock') : t('@yovoice.arrangement.lock')} isIconOnly onClick={() => replacePart(si, pi, ai, { locked: !part.locked })} />
              {plan.approved && (part.source === 'ai' || (part.source === 'real' && !realPartReady(part, downloadedSources.current ?? state.instrumentSources ?? []))) ? <Button size="sm" variant="secondary" icon={part.source === 'ai' ? <Sparkles size={15} /> : <Download size={15} />}
                label={t(part.source === 'ai' ? '@yovoice.arrangement.prepareOneAI' : '@yovoice.arrangement.prepareOneReal')}
                isDisabled={!isDesktop || materialBusy || jobRunning} onClick={() => void prepare(part.source === 'ai' ? 'ai' : 'real', { section: si, phrase: pi, part: ai })} /> : null}
              {plan.approved && part.source === 'ai' && readyAI.has(`${si}:${pi}:${ai}`) ? <small>{t('@yovoice.arrangement.materialReady')}</small> : null}
              {part.source === 'real' && realPartReady(part, state.instrumentSources ?? []) ?
                <small role="status">{sourceIssues.find(issue => issue.startsWith(`${part.instrument} · 真实`)) ?? t('@yovoice.arrangement.realReady')}</small> : null}
              {currentJob?.steps.filter(step => step.partId === part.id).map(step => <small key={step.partId} role="status">{t(`@yovoice.arrangement.job.${step.status}`)} {step.error}</small>)}
              {part.reason ? <small>{part.reason}</small> : null}
            </HStack>)}
          </VStack>)}
        </SettingsGroup>)}
        {draft.arrangementBindings?.length ? <Button size="sm" variant="ghost" label={t('@yovoice.arrangement.undoBinding')} onClick={() => void call<Draft>('arrangement.undoBinding', draft).then(result => change({ arrangement: result.arrangement, score: result.score, arrangementBindings: result.arrangementBindings })).catch(cause => setError(formatCallError(t, cause)))} /> : null}
        <small>{t('@yovoice.arrangement.previewLimit')}</small>
        <HStack gap={2} wrap="wrap"><Button size="sm" variant="primary" icon={<Check />} label={preparing ? t('@yovoice.arrangement.preparing') : t('@yovoice.arrangement.approve')} isDisabled={preparing} onClick={() => void approveAndCompose()} /><Button size="sm" variant="secondary" icon={<FileMusic />} label={t('@yovoice.arrangement.openScore')} onClick={openScore} />{plan.approved ? <Button size="sm" variant="secondary" label={t('@yovoice.arrangement.saveBand')} onClick={() => void saveBand()} /> : null}</HStack>
        {state.bandPresets?.length ? <HStack gap={2} wrap="wrap" vAlign="center"><Selector size="sm" label={t('@yovoice.arrangement.reuseBand')} value={bandID} options={[{ value: '', label: t('@yovoice.arrangement.chooseBand') }, ...state.bandPresets.map(band => ({ value: band.id, label: band.name }))]} onChange={setBandID} /><Button size="sm" variant="secondary" label={t('@yovoice.arrangement.applyBand')} isDisabled={!bandID} onClick={applyBand} /></HStack> : null}
        {bandNotice ? <small role="status">{bandNotice}</small> : null}
        {error ? <p role="alert">{error}</p> : null}
        {plan.approved && state.arrangementScores?.some(item => item.projectId === draft.id) ? <HStack gap={2} wrap="wrap"><small>{t('@yovoice.arrangement.scoreProgress', { count: state.arrangementScores.filter(item => item.projectId === draft.id).length })}</small><Button size="sm" variant="ghost" label={t('@yovoice.arrangement.resetScoreProgress')} isDisabled={preparing || state.aiTask?.status === 'running'} onClick={() => void call('arrangement.score.reset', { id: draft.id })} /></HStack> : null}
        {plan.approved ? <SettingsGroup title={t('@yovoice.arrangement.materialStage')} description={t('@yovoice.arrangement.materialHint')}>
          <SettingsRow label={t('@yovoice.arrangement.aiModel')} hint={t('@yovoice.arrangement.aiModelHint')}>
            <Selector size="sm" label={t('@yovoice.arrangement.aiModel')} isLabelHidden value={aiModelID}
              options={audioModels.map(model => ({ value: model.id, label: `${model.name} · ${model.precision} · ${t(state.models.some(item => item.id === model.id) ? '@yovoice.instruments.installed' : '@yovoice.instruments.available')}` }))}
              onChange={value => { setAIModelID(value); change({ arrangement: { ...plan, aiModelId: value } }); }} />
          </SettingsRow>
          {!state.models.some(model => model.id === aiModelID) ? <Button size="sm" variant="ghost" label={t('@yovoice.arrangement.aiModelInstall')} onClick={openSettings} /> : null}
          <HStack gap={2} wrap="wrap">
            <Button size="sm" variant="secondary" icon={<Download size={15} />} label={t('@yovoice.arrangement.prepareReal')} isDisabled={!isDesktop || materialBusy || jobRunning} onClick={() => void prepare('real')} />
            <Button size="sm" variant="secondary" icon={<Sparkles size={15} />} label={t('@yovoice.arrangement.prepareAI')} isDisabled={!isDesktop || materialBusy || jobRunning} onClick={() => void prepare('ai')} />
            <Button size="sm" variant="primary" icon={<Layers3 size={15} />} label={t('@yovoice.arrangement.prepareBoth')} isDisabled={!isDesktop || materialBusy || jobRunning} onClick={() => void prepare('both')} />
          </HStack>
          {materialBusy ? <small role="status">{materialPhase}</small> : null}
          {currentJob ? <VStack gap={2}>
            <small role="status">{t('@yovoice.arrangement.jobProgress', { done: currentJob.steps.filter(step => step.status === 'ready').length, total: currentJob.steps.length })}</small>
            {currentJob.steps.filter(step => step.error).map((step, i) => <small key={i} role="alert">{step.instrument}: {step.error}</small>)}
            {jobRunning ? <Button size="sm" variant="ghost" label={t('@yovoice.action.cancel')} onClick={() => void call('arrangement.cancel')} /> : currentJob.status !== 'ready' ? <Button size="sm" variant="secondary" label={t('@yovoice.arrangement.resumeJob')} onClick={() => void call('arrangement.prepare', { draft, mode: currentJob.mode, aiModelId: aiModelID, resumeId: currentJob.id }).catch(cause => setError(formatCallError(t, cause)))} /> : null}
          </VStack> : null}
          {(materialBusy || jobRunning) && state.modelActivity?.kind === 'instrumentDownload' && state.modelActivity.status === 'running' ?
            <InstrumentDownloadProgress received={state.modelActivity.received} total={state.modelActivity.total} unit={state.modelActivity.params?.unit} /> : null}
        </SettingsGroup> : null}
        {plan.approved ? <SettingsGroup title={t('@yovoice.arrangement.scoreStage')} description={t('@yovoice.arrangement.scoreHint')}>
          <AIComposer kind="score" projectId={draft.id} state={state} overwrite configure={openSettings} apply={result => {
            const patch = composedScorePatch(draft.title, result);
            if (!patch.score) return;
            const bound = bindArrangementSources(plan, patch.score, downloadedSources.current ?? state.instrumentSources ?? []);
            change({ ...patch, score: bound.score });
            setError(bound.missing.length ? t('@yovoice.arrangement.sourcesMissing', { names: bound.missing.join('、') }) : '');
          }} initialBrief={scoreBrief(plan, state.instrumentSources ?? [])} maxBriefLength={30000} placeholder={t('@yovoice.ai.scorePlaceholder')} submitLabel={t('@yovoice.ai.scoreSubmit')} start={brief => call('ai.score', { projectId: draft.id, brief, seconds: plan.seconds, locale, arrangement: plan })} />
          {draft.score && noteCount(draft.score) > 0 ? <Button size="sm" variant="secondary" label={t('@yovoice.arrangement.openScore')} onClick={openScore} /> : null}
        </SettingsGroup> : null}
        {plan.approved && draft.score && noteCount(draft.score) > 0 ? <SettingsGroup title={t('@yovoice.arrangement.assemblyStage')} description={t('@yovoice.arrangement.assemblyHint')}>
          <Button size="sm" variant="primary" icon={<Layers3 size={15} />} label={t('@yovoice.arrangement.assemble')} isDisabled={!isDesktop || materialBusy || jobRunning} onClick={() => void assemble()} />
        </SettingsGroup> : null}
      </VStack> : null}
    </> : null}
  </VStack>;
}
