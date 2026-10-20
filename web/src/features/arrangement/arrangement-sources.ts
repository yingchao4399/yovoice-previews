import type { ArrangementPart, ArrangementPlan, InstrumentSource, Score, ScoreNote, ScoreTrack } from '../../shared/workbench';

const aliases: Record<string, string> = { pipa: '琵琶', xiao: '箫', guzheng: '古筝', suona: '唢呐', erhu: '二胡', dizi: '笛子', guqin: '古琴', violin: '小提琴', violins: '小提琴', fiddle: '小提琴', piano: '钢琴', guitar: '吉他', acousticguitar: '原声吉他', 竹笛: '笛子', 横笛: '笛子', 洞箫: '箫' };
const instrumentKey = (value: string) => {
  const plain = value.toLowerCase().replace(/[\s()（）·_-]/g, '');
  return aliases[plain] ?? plain;
};
export const sameInstrument = (left: string, right: string) => {
  const a = instrumentKey(left), b = instrumentKey(right);
  return a === b || (a.length >= 2 && b.includes(a)) || (b.length >= 2 && a.includes(b));
};

function partFor(plan: ArrangementPlan, track: ScoreTrack, note: ScoreNote): { part: ArrangementPart; ref: string } | undefined {
  for (const [si, section] of plan.sections.entries()) for (const [pi, phrase] of section.phrases.entries()) {
    if (note.bar < phrase.startBar || note.bar > phrase.endBar) continue;
    const candidates = phrase.parts.map((part, ai) => ({ part, ref: si + ':' + pi + ':' + ai }));
    if (phrase.parts.some(part => part.id)) return candidates.find(({ part }) => part.id && track.partIds?.includes(part.id));
    const named = candidates.filter(({ part }) => sameInstrument(part.instrument, track.name));
    return named.find(({ part }) => part.role === track.role) ?? named[0];
  }
  return undefined;
}

function noteArticulation(source: InstrumentSource, note: ScoreNote, needsTremolo: boolean): string {
  if (needsTremolo) return 'tremolo';
  const requested = note.articulation || source.defaultArticulation || 'normal';
  if (requested === 'normal' && !source.samples.some(sample => sample.articulation === 'normal')) return source.defaultArticulation || requested;
  return requested;
}

function usableSource(part: ArrangementPart, note: ScoreNote, sources: InstrumentSource[], needsTremolo: boolean, preferredSourceID?: string): { source: InstrumentSource; articulation: string } | undefined {
  const origins = part.source === 'auto' ? ['real', 'ai'] : [part.source];
  for (const origin of origins) {
    const fixed = part.sourceId ? sources.find(item => item.id === part.sourceId) : undefined;
    const preferred = preferredSourceID ? sources.find(item => item.id === preferredSourceID) : undefined;
    const ordered = part.sourceId ? (fixed ? [fixed] : []) : preferred ? [preferred, ...sources.filter(item => item.id !== preferred.id)] : sources;
    const source = ordered.find(item => item.origin === origin && (item.health === undefined || item.health === "ready") && (origin !== 'real' || part.allowSubstitution || !/模拟/.test(item.instrument) && item.provenance !== 'synthesized') && (part.allowSubstitution || sameInstrument(item.instrument, part.instrument)) && (item.plugin ? item.plugin.articulations.includes(noteArticulation(item,note,needsTremolo)) : item.samples.some(sample =>
      sample.articulation === noteArticulation(item, note, needsTremolo) &&
      Math.abs(sample.rootPitch - note.pitch) <= 7 && note.velocity >= sample.velocityLow && note.velocity <= sample.velocityHigh)));
    if (source) return { source, articulation: noteArticulation(source, note, needsTremolo) };
  }
  return undefined;
}

// 将分句选源落到实际声部。乐谱仍保留可编辑音符；不同来源的音符合并成不同声部。
export function bindArrangementSources(plan: ArrangementPlan, score: Score, sources: InstrumentSource[], preparedAI: ReadonlySet<string> = new Set()): { score: Score; missing: string[] } {
  const missing = new Set<string>();
  const tracks: ScoreTrack[] = [];
  const usedIds = new Set(score.tracks.map(track => track.id));
  let nextId = 1;
  const uniqueId = () => { while (usedIds.has(`arr-source-${nextId}`)) nextId++; const id = `arr-source-${nextId++}`; usedIds.add(id); return id; };
  for (const track of score.tracks) {
    if (track.drums) { tracks.push(track); continue; }
    const groups = new Map<string, ScoreNote[]>();
    for (const originalNote of track.notes) {
      let note = originalNote;
      const match = partFor(plan, track, note);
      const part = match?.part;
      const phrase = plan.sections.flatMap(section => section.phrases).find(item => note.bar >= item.startBar && note.bar <= item.endBar);
      if (!part && phrase?.parts.some(item => item.id)) {
        missing.add(track.name + ' · 第' + note.bar + '小节没有匹配的编曲声部 ID');
        groups.set('', [...(groups.get('') ?? []), note]);
        continue;
      }
      let sourceId = track.sourceId ?? '';
      const needsTremolo = !!part && (/轮指|tremolo/i.test(part.technique ?? '') || note.articulation === 'tremolo');
      if (part) {
        const choice = usableSource(part, note, sources, needsTremolo, track.sourceId);
        if (choice) sourceId = choice.source.id;
        else if (part.source === 'ai' && match && preparedAI.has(match.ref)) sourceId = '';
        else {
          sourceId = '';
          const label = `${part.instrument} · ${part.source === 'ai' ? 'AI' : part.source === 'auto' ? '自动' : '真实'}${needsTremolo ? ' · 轮指' : ''}`;
          const source = part.source === 'real' ? matchedRealSource(part, sources) : undefined;
          const withinPitch = source?.samples.some(sample => Math.abs(sample.rootPitch - note.pitch) <= 7 && note.velocity >= sample.velocityLow && note.velocity <= sample.velocityHigh);
          const fixed = part.sourceId ? sources.find(item => item.id === part.sourceId) : undefined;
          const mismatch = fixed && !part.allowSubstitution && (!sameInstrument(fixed.instrument, part.instrument) || /模拟/.test(fixed.instrument));
          const detail = mismatch ? ` · 绑定了${fixed.name}（${fixed.instrument}），乐器不符` : source && !withinPitch ? ` · 第${note.bar}小节 MIDI ${note.pitch} 超出可用音域` : '';
          if (![...missing].some(item => item.startsWith(label))) missing.add(label + detail);
        }
        if (choice && choice.articulation !== note.articulation) note = { ...note, articulation: choice.articulation };
      }
      groups.set(sourceId, [...(groups.get(sourceId) ?? []), note]);
    }
    if (groups.size === 0) { tracks.push(track); continue; }
    for (const [sourceId, notes] of groups) {
      tracks.push({ ...track, id: groups.size === 1 ? track.id : uniqueId(), name: track.name,
        sourceId: sourceId || undefined, notes });
    }
  }
  if (tracks.length > 32) return { score, missing: [...missing, '声部数量超过 32，无法按音源拆分'] };
  return { score: { ...score, tracks }, missing: [...missing] };
}

const realCatalog: Record<string, string> = { 古筝: 'guzheng', 二胡: 'erhu', 笛子: 'dizi' };
const needsReal = (part: ArrangementPart) => part.source === 'real' || part.source === 'auto';
export const matchedRealSource = (part: ArrangementPart, sources: InstrumentSource[]) => sources.find(source =>
  source.origin === 'real' && (source.health === undefined || source.health === 'ready') && (!part.sourceId || source.id === part.sourceId) && (part.allowSubstitution || !/模拟/.test(source.instrument) && source.provenance !== 'synthesized') &&
  (part.allowSubstitution || sameInstrument(source.instrument, part.instrument)) && (source.plugin ? (!/轮指|tremolo/i.test(part.technique ?? '') || source.plugin.articulations.includes('tremolo')) : source.samples.some(sample =>
    !/轮指|tremolo/i.test(part.technique ?? '') || sample.articulation === 'tremolo')));
export const realPartReady = (part: ArrangementPart, sources: InstrumentSource[]) => !!matchedRealSource(part, sources);

// 先从已审核目录安装真实录音；模拟箫和唢呐不能满足真实音源要求。
export function neededCatalogDownloads(plan: ArrangementPlan, sources: InstrumentSource[]): string[] {
  const ids = new Set<string>();
  for (const part of plan.sections.flatMap(section => section.phrases.flatMap(phrase => phrase.parts))) {
    const id = realCatalog[instrumentKey(part.instrument)];
    if (needsReal(part) && !part.sourceId && id && !realPartReady(part, sources) && !sources.some(source => source.catalogId === id)) ids.add(id);
  }
  return [...ids];
}

// 目录没有覆盖的真实声部继续在线寻找；同一种乐器在整曲里只发起一次任务。
export function neededOnlineSearch(plan: ArrangementPlan, sources: InstrumentSource[]): string[] {
  const names = new Map<string, string>();
  for (const part of plan.sections.flatMap(section => section.phrases.flatMap(phrase => phrase.parts))) {
    if (needsReal(part) && !part.sourceId && !realPartReady(part, sources)) names.set(instrumentKey(part.instrument), part.instrument.trim());
  }
  return [...names.values()].slice(0, 8);
}
