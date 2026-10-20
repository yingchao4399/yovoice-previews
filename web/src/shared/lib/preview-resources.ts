import type { ResourceItem, ResourcePage, ResourceQuery } from '../resource';
import type { State } from '../workbench';

// 预览库存只枚举现有记录；文件是否存在由 IndexedDB 在调用边界核对。
export function previewResources(state: State, query: ResourceQuery): ResourcePage {
  const items: ResourceItem[] = [];
  for (const source of state.instrumentSources ?? []) {
    const parent = `source:${source.id}`;
    const meta = { origin: source.origin, license: source.license, licenseUrl: source.licenseUrl, sourceUrl: source.sourceUrl, author: source.author, instrument: source.instrument, sourceId: source.id, status: 'ready' };
    items.push({ ...meta, id: parent, kind: 'instrument', name: source.name, usage: 'notes' });
    for (const sample of source.samples) items.push({ ...meta, ...sample, id: `sample:${source.id}/${sample.id}`, kind: 'sample', parentId: parent, sampleId: sample.id, name: `${source.name} · MIDI ${sample.rootPitch} · ${sample.articulation}`, usage: 'notes', mediaKind: 'instruments' });
  }
  for (const m of state.arrangementMaterials ?? []) items.push({ id: `material:${m.id}`, kind: 'audio', name: `${m.instrument} · ${m.phrase ?? ''}`, origin: 'ai', status: 'ready', usage: 'clip', mediaKind: 'outputs', fileName: m.fileName, duration: m.duration });
  for (const g of state.history) items.push({ id: `generation:${g.id}`, kind: 'audio', name: g.title, origin: g.source !== 'score' && g.settings.kind !== 'score' && /^(yue2|ace-step)/.test(g.settings.modelId) ? 'ai' : 'generated', status: 'ready', usage: 'clip', mediaKind: 'outputs', fileName: g.fileName, duration: g.duration });
  for (const d of state.drafts) for (const a of d.timeline?.assets ?? []) items.push({ ...a, id: `asset:${d.id}/${a.id}`, kind: 'audio', origin: a.origin || 'imported', status: 'ready', usage: 'clip', mediaKind: 'outputs' });
  for (const v of state.voices) items.push({ id: `voice:${v.id}`, kind: 'voice', name: v.name, status: 'ready', usage: 'reference', mediaKind: 'voices', fileName: v.fileName, duration: v.duration });
  for (const b of state.bandPresets ?? []) items.push({ id: `band:${b.id}`, kind: 'band', name: b.name, status: 'ready', usage: 'preset', bandParts: b.parts, aiModelId: b.aiModelId, memberIds: b.parts.filter(p => p.sourceId).map(p => `source:${p.sourceId}`) });
  const text = query.query?.trim().toLocaleLowerCase() ?? '';
  const filtered = items.filter(item => (!query.kind || item.kind === query.kind || query.kind === 'playable' && ['instrument', 'sample', 'audio', 'voice'].includes(item.kind)) && (!query.origin || item.origin === query.origin) && (!query.parentId || item.parentId === query.parentId) && (!text || `${item.name} ${item.instrument ?? ''} ${item.id}`.toLocaleLowerCase().includes(text)));
  const offset = Math.max(0, query.offset ?? 0), limit = Math.max(1, Math.min(query.limit || 40, 100));
  return { items: filtered.slice(offset, offset + limit), total: filtered.length, offset, ...(offset + limit < filtered.length ? { nextOffset: offset + limit } : {}) };
}
