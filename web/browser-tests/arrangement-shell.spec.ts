import { test, expect } from '@playwright/test';
import { seedState } from './seed';
import { emptyState } from '../src/shared/workbench';
import { createScoreDraft } from '../src/features/score/score-draft';
import { bindArrangementSources, neededCatalogDownloads, neededOnlineSearch, realPartReady } from '../src/features/arrangement/arrangement-sources';

test('分句选源会拆分声部并保留缺失音源的可编辑音符', () => {
  const plan = { title: '测试', tempo: 80, timeSignature: [4, 4], seconds: 6, sections: [{ name: 'A', startBar: 1, endBar: 2, phrases: [
    { name: '真采样', startBar: 1, endBar: 1, parts: [{ instrument: '古筝', role: 'melody', source: 'real' as const }] },
    { name: 'AI', startBar: 2, endBar: 2, parts: [{ instrument: '古筝', role: 'melody', source: 'ai' as const }] },
  ] }] };
  const score = { tempo: 80, timeSignature: [4, 4], tracks: [{ id: 'guzheng', name: '古筝', program: 107, notes: [
    { bar: 1, beat: 1, pitch: 60, length: 1, velocity: 90 }, { bar: 2, beat: 1, pitch: 62, length: 1, velocity: 90 },
  ] }] };
  const sources = [{ id: 'real-guzheng', name: '古筝', instrument: '古筝', origin: 'real' as const, defaultArticulation: 'pluck', samples: [
    { id: 'sample', fileName: 'sample.wav', sha256: 'a', rootPitch: 60, articulation: 'pluck', velocityLow: 1, velocityHigh: 127, duration: 1 },
  ] }];
  const result = bindArrangementSources(plan, score, sources);
  expect(neededCatalogDownloads(plan, [])).toEqual(['guzheng']);
  expect(neededCatalogDownloads(plan, sources)).toEqual([]);
  expect(result.score.tracks).toHaveLength(2);
  expect(result.score.tracks.find(track => track.sourceId === 'real-guzheng')?.notes).toHaveLength(1);
  expect(result.score.tracks.find(track => !track.sourceId)?.notes[0].bar).toBe(2);
  expect(result.missing).toEqual(['古筝 · AI']);
});

test('新规划同名乐器声部必须按 partId 匹配，不能误用相邻轨道', () => {
  const a = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';
  const b = 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb';
  const plan = { title: '双二胡', tempo: 120, timeSignature: [4, 4], seconds: 4, sections: [{ name: 'A', startBar: 1, endBar: 2, phrases: [
    { name: '一', startBar: 1, endBar: 1, parts: [{ id: a, instrument: '二胡', role: 'melody', source: 'real' as const }] },
    { name: '二', startBar: 2, endBar: 2, parts: [{ id: b, instrument: '二胡', role: 'melody', source: 'real' as const }] },
  ] }] };
  const score = { tempo: 120, timeSignature: [4, 4], tracks: [
    { id: 'a', partIds: [a], name: '二胡', role: 'melody' as const, program: 40, notes: [{ bar: 1, beat: 1, pitch: 62, length: 1, velocity: 90 }] },
    { id: 'b', partIds: [b], name: '二胡', role: 'melody' as const, program: 40, notes: [{ bar: 2, beat: 1, pitch: 64, length: 1, velocity: 90 }] },
  ] };
  const source = { id: 'erhu', name: '二胡', instrument: '二胡', origin: 'real' as const, defaultArticulation: 'sustain', samples: [
    { id: 'sample', fileName: 'sample.wav', sha256: 'a', rootPitch: 62, articulation: 'sustain', velocityLow: 1, velocityHigh: 127, duration: 1 },
  ] };
  expect(bindArrangementSources(plan, score, [source]).missing).toEqual([]);
  score.tracks[1].partIds = [a];
  expect(bindArrangementSources(plan, score, [source]).missing).toContain('二胡 · 第2小节没有匹配的编曲声部 ID');
});

test('已准备的 AI 乐句可通过拼合预检，缺失时仍明确报错', () => {
  const partID = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';
  const plan = { title: 'AI 箫', tempo: 120, timeSignature: [4, 4], seconds: 2, sections: [{ name: 'A', startBar: 1, endBar: 1,
    phrases: [{ name: '一句', startBar: 1, endBar: 1, parts: [{ id: partID, instrument: '箫', role: 'melody', source: 'ai' as const }] }] }] };
  const score = { tempo: 120, timeSignature: [4, 4], tracks: [{ id: 'xiao', partIds: [partID], name: '箫', role: 'melody' as const,
    program: 73, notes: [{ bar: 1, beat: 1, pitch: 67, length: 1, velocity: 90 }] }] };
  expect(bindArrangementSources(plan, score, []).missing).toEqual(['箫 · AI']);
  const prepared = bindArrangementSources(plan, score, [], new Set(['0:0:0']));
  expect(prepared.missing).toEqual([]);
  expect(prepared.score.tracks[0].sourceId).toBeUndefined();
});

test('自动来源优先匹配真实采样，轮指缺采样时保留可编辑预览', () => {
  const plan = { title: '测试', tempo: 80, timeSignature: [4, 4], seconds: 6, sections: [{ name: 'A', startBar: 1, endBar: 2, phrases: [
    { name: '拨弦', startBar: 1, endBar: 1, parts: [{ instrument: '琵琶', role: 'melody', source: 'auto' as const }] },
    { name: '轮指', startBar: 2, endBar: 2, parts: [{ instrument: '琵琶', role: 'melody', technique: '轮指', source: 'real' as const }] },
  ] }] };
  const score = { tempo: 80, timeSignature: [4, 4], tracks: [{ id: 'pipa', name: '琵琶', role: 'melody' as const, program: 24, notes: [
    { bar: 1, beat: 1, pitch: 60, length: 1, velocity: 90 }, { bar: 2, beat: 1, pitch: 60, length: 1, velocity: 90 },
  ] }] };
  const samples = [{ id: 'pluck', fileName: 'pluck.wav', sha256: 'a', rootPitch: 60, articulation: 'pluck', velocityLow: 1, velocityHigh: 127, duration: 1 }];
  const sources = [
    { id: 'ai-pipa', name: 'AI 琵琶', instrument: '琵琶', origin: 'ai' as const, defaultArticulation: 'pluck', samples },
    { id: 'real-pipa', name: '真实琵琶', instrument: '琵琶', origin: 'real' as const, defaultArticulation: 'pluck', samples },
  ];
  const result = bindArrangementSources(plan, score, sources);
  expect(realPartReady(plan.sections[0].phrases[1].parts[0], sources)).toBe(false);
  expect(neededOnlineSearch(plan, sources)).toEqual(['琵琶']);
  expect(result.score.tracks.find(track => track.sourceId === 'real-pipa')?.notes).toHaveLength(1);
  expect(result.score.tracks.find(track => !track.sourceId)?.notes[0].bar).toBe(2);
  expect(result.missing).toEqual(['琵琶 · 真实 · 轮指']);
});

test('整曲缺音源时先取真实民乐目录，再去网上寻找其余乐器', () => {
  const plan = { title: '民乐', tempo: 80, timeSignature: [4, 4], seconds: 90, sections: [{ name: 'A', startBar: 1, endBar: 1, phrases: [
    { name: '齐奏', startBar: 1, endBar: 1, parts: [
      { instrument: '二胡', role: 'melody', source: 'real' as const },
      { instrument: '笛子', role: 'answer', source: 'real' as const },
      { instrument: '琵琶', role: 'melody', source: 'real' as const },
    ] },
  ] }] };
  expect(neededCatalogDownloads(plan, [])).toEqual(['erhu', 'dizi']);
  expect(neededOnlineSearch(plan, [])).toEqual(['二胡', '笛子', '琵琶']);
  const installed = [{ id: 'erhu', name: '二胡', instrument: '二胡', origin: 'real' as const,
    samples: [{ id: 'note', fileName: 'note.wav', sha256: 'abc', rootPitch: 62, articulation: 'normal', velocityLow: 1, velocityHigh: 127, duration: 1 }] }];
  expect(neededCatalogDownloads(plan, installed)).toEqual(['dizi']);
  expect(neededOnlineSearch(plan, installed)).toEqual(['笛子', '琵琶']);
});

test('已下载的替代音源按显式选择绑定，保留曲谱原乐器名称', () => {
  const source = { id: 'real-dizi', name: '已下载笛子', instrument: '笛子', origin: 'real' as const,
    defaultArticulation: 'normal', samples: [{ id: 'dizi-note', fileName: 'dizi.wav', sha256: 'abc',
      rootPitch: 67, articulation: 'normal', velocityLow: 1, velocityHigh: 127, duration: 1 }] };
  const part = { instrument: '箫', role: 'melody', source: 'real' as const, sourceId: source.id, allowSubstitution: true };
  const plan = { title: '箫独奏', tempo: 80, timeSignature: [4, 4], seconds: 3,
    sections: [{ name: 'A', startBar: 1, endBar: 1, phrases: [{ name: '箫回应', startBar: 1, endBar: 1, parts: [part] }] }] };
  const score = { tempo: 80, timeSignature: [4, 4], tracks: [{ id: 'xiao', name: '箫', role: 'melody' as const, program: 73,
    notes: [{ bar: 1, beat: 1, pitch: 67, length: 1, velocity: 90 }] }] };
  expect(realPartReady(part, [source])).toBe(true);
  expect(neededOnlineSearch(plan, [source])).toEqual([]);
  const result = bindArrangementSources(plan, score, [source]);
  expect(result.missing).toEqual([]);
  expect(result.score.tracks[0]).toMatchObject({ name: '箫', sourceId: source.id });
});

test('竹笛复用笛子目录，显式选用模拟箫时不再自动下载', () => {
  const dizi = { id: 'dizi-local', catalogId: 'dizi', name: '笛子', instrument: '笛子', origin: 'real' as const,
    defaultArticulation: 'sustain', samples: [{ id: 'note', fileName: 'note.wav', sha256: 'abc', rootPitch: 67,
      articulation: 'sustain', velocityLow: 1, velocityHigh: 127, duration: 1 }] };
  const surrogate = { ...dizi, id: 'xiao-local', catalogId: 'xiao', name: '箫替代', instrument: '箫（模拟）' };
  const bamboo = { instrument: '竹笛', role: 'melody', source: 'real' as const };
  const xiao = { instrument: '箫', role: 'melody', source: 'real' as const, sourceId: surrogate.id, allowSubstitution: true };
  const plan = { title: '山间', tempo: 80, timeSignature: [4, 4], seconds: 3,
    sections: [{ name: 'A', startBar: 1, endBar: 1, phrases: [{ name: '吹奏', startBar: 1, endBar: 1, parts: [bamboo, xiao] }] }] };
  expect(neededCatalogDownloads(plan, [])).toEqual(['dizi']);
  expect(neededCatalogDownloads(plan, [dizi, surrogate])).toEqual([]);
  expect(neededOnlineSearch(plan, [dizi, surrogate])).toEqual([]);
  expect(realPartReady(xiao, [surrogate])).toBe(true);
  expect(realPartReady({ ...xiao, sourceId: undefined, allowSubstitution: false }, [surrogate])).toBe(false);
  const score = { tempo: 80, timeSignature: [4, 4], tracks: [
    { id: 'bamboo', name: '竹笛', role: 'melody' as const, program: 73, notes: [{ bar: 1, beat: 1, pitch: 67, length: 1, velocity: 90, articulation: 'normal' }] },
    { id: 'xiao', name: '箫', role: 'melody' as const, program: 73, notes: [{ bar: 1, beat: 1, pitch: 67, length: 1, velocity: 90 }] },
  ] };
  const result = bindArrangementSources(plan, score, [dizi, surrogate]);
  expect(result.missing).toEqual([]);
  expect(result.score.tracks.map(track => [track.name, track.sourceId, track.notes[0].articulation])).toEqual([
    ['竹笛', 'dizi-local', 'sustain'], ['箫', 'xiao-local', 'sustain'],
  ]);
});

test('旧工程的 violin 自动复用已下载小提琴组，不再在线寻找', () => {
  const part = { instrument: 'violin', role: 'melody', source: 'real' as const };
  const plan = { title: '小提琴独奏', tempo: 80, timeSignature: [4, 4], seconds: 3,
    sections: [{ name: 'A', startBar: 1, endBar: 1, phrases: [{ name: '独奏', startBar: 1, endBar: 1, parts: [part] }] }] };
  const source = { id: 'violin-group', name: '弦乐 · 小提琴组', instrument: '弦乐 · 小提琴组', origin: 'real' as const,
    defaultArticulation: 'sustain', samples: [{ id: 'note', fileName: 'note.wav', sha256: 'abc', rootPitch: 67,
      articulation: 'sustain', velocityLow: 1, velocityHigh: 127, duration: 1 }] };
  expect(realPartReady(part, [source])).toBe(true);
  expect(neededOnlineSearch(plan, [source])).toEqual([]);
  const score = { tempo: 80, timeSignature: [4, 4], tracks: [{ id: 'violin', name: 'violin', role: 'melody' as const, program: 40,
    notes: [{ bar: 1, beat: 1, pitch: 67, length: 1, velocity: 90 }] }] };
  expect(bindArrangementSources(plan, score, [source]).score.tracks[0].sourceId).toBe(source.id);
  score.tracks[0].notes[0].pitch = 98;
  expect(bindArrangementSources(plan, score, [source]).missing).toEqual(['violin · 真实 · 第1小节 MIDI 98 超出可用音域']);
});

test('规划页展示实际匹配音源，并指出曲谱超出采样音域', async ({ page }) => {
  const state = emptyState();
  const draft = createScoreDraft('zh-CN', false);
  draft.arrangement = { title: '小提琴独奏', tempo: 80, timeSignature: [4, 4], seconds: 3, approved: true,
    sections: [{ name: 'A', startBar: 1, endBar: 1, phrases: [{ name: '独奏', startBar: 1, endBar: 1,
      parts: [{ instrument: 'violin', role: 'melody', source: 'real' }] }] }] };
  draft.score = { tempo: 80, timeSignature: [4, 4], tracks: [{ id: 'violin', name: '小提琴', role: 'melody', program: 40,
    notes: [{ bar: 1, beat: 1, pitch: 98, length: 1, velocity: 90 }] }] };
  state.drafts = [draft];
  state.instrumentSources = [{ id: 'violin-group', name: '弦乐 · 小提琴组', instrument: '弦乐 · 小提琴组', origin: 'real',
    defaultArticulation: 'sustain', samples: [{ id: 'note', fileName: 'note.wav', sha256: 'abc', rootPitch: 67,
      articulation: 'sustain', velocityLow: 1, velocityHigh: 127, duration: 1 }] }];
  await seedState(page, state);
  await page.goto('/');
  await page.getByTestId('nav-arrangement').click();
  await expect(page.getByRole('combobox', { name: '替换为已下载音源' })).toContainText('弦乐 · 小提琴组');
  await expect(page.getByText('violin · 真实 · 第1小节 MIDI 98 超出可用音域')).toBeVisible();
});

test('审核后替换已下载音源仍保留音源准备步骤', async ({ page }, testInfo) => {
  const state = emptyState();
  const draft = createScoreDraft('zh-CN', false);
  draft.arrangement = { title: '替换测试', tempo: 80, timeSignature: [4, 4], seconds: 3, approved: true,
    sections: [{ name: 'A', startBar: 1, endBar: 1, phrases: [{ name: '箫', startBar: 1, endBar: 1,
      parts: [{ instrument: '箫', role: 'melody', source: 'real' }] }] }] };
  state.drafts = [draft];
  state.instrumentSources = [{ id: 'dizi-local', name: '已下载竹笛', instrument: '笛子', origin: 'real',
    defaultArticulation: 'sustain', samples: [{ id: 'note', fileName: 'note.wav', sha256: 'abc', rootPitch: 67,
      articulation: 'sustain', velocityLow: 1, velocityHigh: 127, duration: 1 }] }];
  await seedState(page, state);
  await page.goto('/');
  await page.getByTestId('nav-arrangement').click();
  await page.getByRole('button', { name: '选择替代音色' }).click();
  await page.getByRole('combobox', { name: '替换为已下载音源' }).click();
  await page.getByRole('option', { name: /已下载竹笛/ }).click();
  await expect(page.getByRole('heading', { name: '音源准备' })).toBeVisible();
  await expect(page.getByText('已下载，可直接使用')).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath('downloaded-source-replacement.png'), fullPage: true });
});

test('智能编曲与音源库有独立入口，原编曲音色库仍在设置中', async ({ page }, testInfo) => {
  const state = emptyState();
  const draft = createScoreDraft('zh-CN', false);
  draft.title = '山间听弦';
  draft.arrangement = { title: draft.title, tempo: 80, timeSignature: [4, 4], key: 'D 宫调式', seconds: 90, sourceMode: 'auto', sections: [
    { name: '前奏', startBar: 1, endBar: 2, phrases: [{ name: '起句', startBar: 1, endBar: 2, parts: [{ instrument: '琵琶', role: 'melody', technique: '挑弹', source: 'real', reason: '保留真实拨弦' }] }] },
  ] };
  state.drafts = [draft];
  await seedState(page, state);
  await page.goto('/');

  const destinations = ['nav-text', 'nav-story', 'nav-music', 'nav-arrangement', 'nav-characters', 'nav-instruments'];
  for (const destination of destinations) await expect(page.getByTestId(destination)).toBeVisible();
  expect(await page.getByTestId('sidebar').locator('[data-testid^="nav-"]').evaluateAll(rows => rows.map(row => row.getAttribute('data-testid')).filter(id => id !== 'nav-settings' && id !== 'nav-new'))).toEqual(destinations);
  await page.getByTestId('nav-arrangement').click();
  await expect(page.getByRole('heading', { name: '智能编曲' })).toBeVisible();
  const requirements = page.getByRole('textbox', { name: '描述想要的整曲编排' });
  await expect(requirements).toBeVisible();
  await requirements.fill('一首轻快的乡村舞曲，二胡主奏，尾声用唢呐呼应。');
  await expect(page.getByRole('heading', { name: /前奏 · 1–2/ })).toBeVisible();
  await page.getByRole('textbox', { name: '乐器' }).fill('阮');
  await expect(page.getByRole('textbox', { name: '乐器' })).toHaveValue('阮');
  await page.screenshot({ path: testInfo.outputPath('arrangement.png') });

  await page.getByTestId('nav-instruments').click();
  await page.getByTestId('nav-arrangement').click();
  await expect(page.getByRole('textbox', { name: '乐器' })).toHaveValue('阮');
  await page.reload();
  await page.getByTestId('nav-arrangement').click();
  await expect(page.getByRole('textbox', { name: '描述想要的整曲编排' })).toHaveValue('一首轻快的乡村舞曲，二胡主奏，尾声用唢呐呼应。');
  await expect(page.getByRole('textbox', { name: '乐器' })).toHaveValue('阮');
  await page.getByTestId('nav-instruments').click();
  await expect(page.getByRole('heading', { name: '音源库与乐器' })).toBeVisible();
  await page.getByRole('button', { name: '导入音源' }).click();
  await page.getByRole('radio', { name: 'AI 生成' }).click();
  await expect(page.getByRole('radio', { name: 'AI 生成' })).toBeChecked();
  await page.getByRole('button', { name: '返回音源库' }).click();
  await page.getByRole('tab', { name: 'AI 音源与模型' }).click();
  await expect(page.getByText('模型下载仍在设置中')).toBeVisible();
  await page.getByTestId('nav-settings').click();
  await expect(page.getByRole('tab', { name: '编曲音色库' })).toBeVisible();
  await expect(page.getByRole('tab', { name: '插件管理' })).toBeVisible();
});

test('音源库沿用声音库布局，完整 CC0 目录可按名称搜索', async ({ page }, testInfo) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await seedState(page, emptyState());
  await page.goto('/');
  await page.getByTestId('nav-instruments').click();
  await page.getByRole('tab', { name: 'CC0 音源目录' }).click();
  await expect(page.getByText('71 项 CC0 音源')).toBeVisible();
  await expect(page.locator('.instrument-entry')).toHaveCount(71);
  const search = page.getByRole('textbox', { name: '搜索乐器、奏法或来源' });
  await search.fill('二胡');
  await expect(page.locator('.instrument-entry')).toHaveCount(1);
  await expect(page.getByRole('heading', { name: '二胡' })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath('instrument-catalog-search.png') });
  await search.fill('');
  await expect(page.locator('.instrument-entry')).toHaveCount(71);
  const scroller = page.locator('.library-content:visible');
  expect(await scroller.evaluate(element => element.scrollHeight > element.clientHeight)).toBe(true);
  await scroller.evaluate(element => element.scrollTo(0, element.scrollHeight));
  await expect(page.locator('.instrument-entry').last()).toBeInViewport();
});

test('其他音源站可搜索，并区分官网登录、插件与单音导入', async ({ page }, testInfo) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await seedState(page, emptyState());
  await page.goto('/');
  await page.getByTestId('nav-instruments').click();
  await page.getByRole('tab', { name: '更多音源站' }).click();
  await expect(page.locator('.instrument-entry')).toHaveCount(8);
  await expect(page.getByRole('heading', { name: 'Pianobook' })).toBeVisible();
  await expect(page.getByText('账号登录由官网或插件自己完成', { exact: false })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Splice INSTRUMENT / LABS' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'DSK Asian DreamZ' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Kong Audio Mini DiZi / ErHu' })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath('instrument-sites.png') });
  await page.getByRole('textbox', { name: '搜索乐器、奏法或来源' }).fill('SFZ');
  await expect(page.getByRole('heading', { name: 'SFZ Instruments' })).toBeVisible();
});

test('真实音源下载在目录和详情显示实际字节进度', async ({ page }) => {
  const state = emptyState();
  state.modelActivity = { kind: 'instrumentDownload', code: '@yovoice.activity.downloading', params: { unit: 'bytes', name: '立式钢琴' },
    status: 'running', received: 524288, total: 1048576, errorCode: null, errorParams: null };
  await seedState(page, state);
  await page.goto('/');
  await page.getByTestId('nav-instruments').click();
  await page.getByRole('tab', { name: 'CC0 音源目录' }).click();
  await expect(page.getByText('音源下载 0.5/1.0 MB')).toBeVisible();
  await page.getByRole('textbox', { name: '搜索乐器、奏法或来源' }).fill('立式钢琴');
  await page.getByRole('button', { name: '查看采样' }).click();
  await expect(page.getByText('音源下载 0.5/1.0 MB')).toBeVisible();
});

test('自动找到的真实录音在我的音源中明确待试听，并可确认', async ({ page }) => {
  const state = emptyState();
  state.instrumentSources = [{ id: 'found-pipa', name: 'Pipa C4 pluck', instrument: '琵琶', origin: 'real', verification: 'metadata',
    license: 'CC0-1.0', sourceUrl: 'https://freesound.org/people/example/sounds/123/',
    samples: [{ id: 'c4', fileName: 'sample.wav', sha256: 'abc', rootPitch: 60, articulation: 'normal', velocityLow: 1, velocityHigh: 127, duration: 1 }] }];
  await seedState(page, state);
  await page.goto('/');
  await page.getByTestId('nav-instruments').click();
  await expect(page.getByText('自动检索 · 请试听').first()).toBeVisible();
  await page.getByRole('button', { name: '已试听，确认乐器' }).click();
  await expect(page.getByRole('button', { name: '已试听，确认乐器' })).toHaveCount(0);
  await expect(page.getByRole('img', { name: '已安装' })).toBeVisible();
});

test('音源详情列出每个已安装采样，目录条目提示先下载再试听', async ({ page }, testInfo) => {
  const state = emptyState();
  state.instrumentSources = [{ id: 'erhu', catalogId: 'erhu', name: '二胡', instrument: '二胡', origin: 'real', verification: 'catalog', license: 'CC0-1.0',
    samples: [
      { id: 'd4', fileName: 'sample-d4.wav', sha256: 'abc', rootPitch: 62, articulation: 'sustain', velocityLow: 1, velocityHigh: 127, duration: 2.2 },
      { id: 'e4', fileName: 'sample-e4.wav', sha256: 'def', rootPitch: 64, articulation: 'sustain', velocityLow: 1, velocityHigh: 127, duration: 2.7 },
    ] }];
  await seedState(page, state);
  await page.goto('/');
  await page.getByTestId('nav-instruments').click();
  await page.getByRole('button', { name: '查看采样' }).click();
  await expect(page.getByRole('heading', { name: '逐个试听采样' })).toBeVisible();
  await expect(page.getByText('D4 · MIDI 62')).toBeVisible();
  await expect(page.getByText('E4 · MIDI 64')).toBeVisible();
  await expect(page.getByRole('button', { name: '试听' })).toHaveCount(2);
  await page.screenshot({ path: testInfo.outputPath('instrument-sample-detail.png') });
  await page.getByRole('button', { name: '返回音源库' }).click();
  await page.getByRole('tab', { name: 'CC0 音源目录' }).click();
  await page.getByRole('textbox', { name: '搜索乐器、奏法或来源' }).fill('立式钢琴');
  await page.getByRole('button', { name: '查看采样' }).click();
  await expect(page.getByText('下载并校验音源后')).toBeVisible();
  await expect(page.getByRole('button', { name: '试听' })).toHaveCount(0);
});

test('目标时长以分秒显示，分钟选项选择后随工程保存', async ({ page }) => {
  await seedState(page, emptyState());
  await page.goto('/');
  await page.getByTestId('nav-arrangement').click();
  const duration = page.getByRole('combobox', { name: '目标时长', exact: true });
  await expect(duration).toHaveText('01:30');
  await duration.click();
  for (const time of ['00:30', '01:00', '01:30', '02:00', '03:00', '05:00', '08:00', '10:00']) {
    await expect(page.getByRole('option', { name: time, exact: true })).toBeVisible();
  }
  await page.getByRole('option', { name: '03:00', exact: true }).click();
  await page.getByRole('textbox', { name: '描述想要的整曲编排' }).fill('三分钟器乐测试。');
  await page.getByRole('button', { name: '新建智能编曲' }).first().click();
  await expect(duration).toHaveText('03:00');
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem('voice-workbench-v1') ?? '{}').drafts?.find((d: { kind?: string }) => d.kind === 'score')?.arrangementInput?.seconds)).toBe(180);
  await page.reload();
  await page.getByTestId('nav-arrangement').click();
  await expect(duration).toHaveText('03:00');
});

test('没有线上模型和编曲工程时也能先写自己的需求', async ({ page }) => {
  await seedState(page, emptyState());
  await page.goto('/');
  await page.getByTestId('nav-arrangement').click();
  const requirements = page.getByRole('textbox', { name: '描述想要的整曲编排' });
  await expect(requirements).toBeVisible();
  await requirements.fill('四分钟现代国风器乐，古琴与笛子交替主奏，末段逐渐减弱。');
  await page.getByRole('button', { name: '新建智能编曲' }).first().click();
  await expect(requirements).toHaveValue('四分钟现代国风器乐，古琴与笛子交替主奏，末段逐渐减弱。');
  await page.reload();
  await page.getByTestId('nav-arrangement').click();
  await expect(page.getByRole('textbox', { name: '描述想要的整曲编排' })).toHaveValue('四分钟现代国风器乐，古琴与笛子交替主奏，末段逐渐减弱。');
});

test('长编曲方案在较矮窗口中能滚动到底部操作', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1060, height: 650 });
  const state = emptyState();
  const draft = createScoreDraft('zh-CN', false);
  draft.title = '长编曲方案';
  draft.arrangement = { title: draft.title, tempo: 80, timeSignature: [4, 4], key: 'D 宫调式', seconds: 90, sourceMode: 'auto', sections: Array.from({ length: 8 }, (_, index) => ({
    name: `段落 ${index + 1}`, startBar: index * 2 + 1, endBar: index * 2 + 2,
    phrases: [{ name: `乐句 ${index + 1}`, startBar: index * 2 + 1, endBar: index * 2 + 2,
      parts: [{ instrument: '琵琶', role: 'melody' as const, technique: '挑弹', source: 'real' as const }] }],
  })) };
  state.drafts = [draft];
  await seedState(page, state);
  await page.goto('/');
  await page.getByTestId('nav-arrangement').click();
  const scroller = page.locator('.arrangement-page');
  await expect(scroller).toBeVisible();
  expect(await scroller.evaluate(element => element.scrollHeight > element.clientHeight)).toBe(true);
  await scroller.evaluate(element => element.scrollTo(0, element.scrollHeight));
  await expect(page.getByRole('button', { name: '确认方案并 AI 写谱' })).toBeInViewport();
  await expect(page.getByRole('heading', { name: /段落 8/ })).toBeInViewport();
  await page.screenshot({ path: testInfo.outputPath('arrangement-scroll-bottom.png') });
});

test('智能编曲沿用当前 DeepSeek 服务和自定义需求', async ({ page }) => {
  const state = emptyState();
  state.aiProviders = [{ id: 'deepseek-test', preset: 'deepseek', name: 'DeepSeek', format: 'chat_completions', baseURL: 'https://api.deepseek.com', modelsPath: '/models', model: 'deepseek-flash', keyMask: '••••••••' }];
  state.aiProviderID = 'deepseek-test';
  await seedState(page, state);
  await page.goto('/');
  await page.getByTestId('nav-arrangement').click();
  await page.getByRole('textbox', { name: '描述想要的整曲编排' }).fill('为皮影戏配乐，板胡主奏，快板后以锣鼓收尾。');
  await page.getByRole('button', { name: '新建智能编曲' }).first().click();
  const composer = page.locator('.ai-composer:visible');
  await expect(composer).toContainText('DeepSeek · deepseek-flash');
  await composer.getByRole('button', { name: '规划整首曲子' }).click();
  await expect(composer.getByRole('alert')).toContainText('请在桌面应用中使用此功能');
  await expect(page.getByRole('textbox', { name: '描述想要的整曲编排' })).toHaveValue('为皮影戏配乐，板胡主奏，快板后以锣鼓收尾。');
});

test('已审核方案展示单项与批量备料、拼合和可复用乐队入口', async ({ page }, testInfo) => {
  const state = emptyState();
  const draft = createScoreDraft('zh-CN', false);
  draft.title = '山间听弦';
  draft.arrangement = { title: draft.title, brief: '琵琶与箫', tempo: 120, timeSignature: [4, 4], seconds: 4, approved: true, sections: [{ name: 'A', startBar: 1, endBar: 2, phrases: [
    { name: '琵琶独奏', startBar: 1, endBar: 1, parts: [{ instrument: '琵琶', role: 'melody', technique: '挑弹', source: 'real' }] },
    { name: '箫回应', startBar: 2, endBar: 2, parts: [{ instrument: '箫', role: 'melody', technique: '长音', source: 'ai' }] },
  ] }] };
  draft.score = { tempo: 120, timeSignature: [4, 4], tracks: [
    { id: 'pipa', name: '琵琶', role: 'melody', program: 24, notes: [{ bar: 1, beat: 1, pitch: 60, length: 1, velocity: 90 }] },
    { id: 'xiao', name: '箫', role: 'melody', program: 73, notes: [{ bar: 2, beat: 1, pitch: 67, length: 1, velocity: 80 }] },
  ] };
  state.drafts = [draft];
  state.arrangementMaterials = [{ id: 'ai-phrase', projectId: draft.id, planHash: 'fixture', sectionIndex: 0, phraseIndex: 1, partIndex: 0,
    instrument: '箫', role: 'melody', technique: '长音', phrase: '箫回应', prompt: 'Dry isolated xiao', tempo: 120, key: 'D 宫调式', modelId: 'ace-step-1.5-turbo-bf16', fileName: 'import-ai-phrase.wav', duration: 10, createdAt: new Date().toISOString() }];
  state.bandPresets = [{ id: 'band', name: '山间听弦 · 乐队组合', parts: [{ instrument: '琵琶', role: 'melody', source: 'real' }], createdAt: new Date().toISOString() }];
  await seedState(page, state);
  await page.goto('/');
  await page.getByTestId('nav-arrangement').click();
  await expect(page.getByRole('button', { name: '下载此音源' })).toBeVisible();
  await expect(page.getByRole('button', { name: '生成此乐句' })).toBeVisible();
  await expect(page.getByRole('button', { name: '一键准备真实音源' })).toBeVisible();
  await expect(page.getByRole('button', { name: '一键生成 AI 音源' })).toBeVisible();
  await expect(page.getByRole('button', { name: '一键下载并生成' })).toBeVisible();
  await page.getByRole('combobox', { name: 'AI 音频模型' }).click();
  await expect(page.getByText(/YuE2 3B/).first()).toBeVisible();
  await expect(page.getByRole('button', { name: '生成可编辑拼合时间线' })).toBeVisible();
  await expect(page.getByRole('button', { name: '保存为乐队组合' })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath('arrangement-stages.png'), fullPage: true });
  await page.locator('.arrangement-page').evaluate(element => element.scrollTo(0, element.scrollHeight));
  await expect(page.getByRole('button', { name: '生成可编辑拼合时间线' })).toBeInViewport();
  await page.screenshot({ path: testInfo.outputPath('arrangement-assembly.png') });
  await page.getByTestId('nav-instruments').click();
  await page.getByRole('tab', { name: 'AI 音源与模型' }).click();
  await expect(page.getByRole('heading', { name: /箫 · 长音/ })).toBeVisible();
  await page.getByRole('button', { name: '查看详情' }).click();
  await expect(page.getByText('Dry isolated xiao')).toBeVisible();
  await page.getByRole('button', { name: '返回音源库' }).click();
  await page.getByRole('tab', { name: '乐队组合' }).click();
  await expect(page.getByRole('heading', { name: /山间听弦 · 乐队组合/ })).toBeVisible();
});

test('智能编曲作品从曲谱渲染入口返回备料拼合，不要求普通音色库', async ({ page }) => {
  const state = emptyState(); const draft = createScoreDraft('zh-CN', false);
  draft.arrangement = { title: '真实独奏', tempo: 80, timeSignature: [4, 4], seconds: 3, approved: true, sections: [{ name: 'A', startBar: 1, endBar: 1, phrases: [{ name: '一句', startBar: 1, endBar: 1, parts: [{ instrument: '箫', role: 'melody', source: 'real' }] }] }] };
  state.drafts = [draft]; await seedState(page, state); await page.goto('/');
  await page.getByTestId('nav-arrangement').click(); await page.getByRole('button', { name: '打开编曲编辑器' }).first().click();
  await page.getByRole('button', { name: '前往智能编曲拼合' }).first().click();
  await expect(page.getByRole('heading', { name: '智能编曲', exact: true })).toBeVisible();
});

test('参考音频明确未分析，用户演奏素材独立加入手工轨道', async ({ page }) => {
  const state = emptyState(); const draft = createScoreDraft('zh-CN', false); state.drafts = [draft];
  await seedState(page, state); await page.goto('/'); await page.getByTestId('nav-arrangement').click();
  const wav = Buffer.alloc(44 + 32000); wav.write('RIFF'); wav.writeUInt32LE(wav.length - 8, 4); wav.write('WAVEfmt ', 8); wav.writeUInt32LE(16, 16); wav.writeUInt16LE(1, 20); wav.writeUInt16LE(1, 22); wav.writeUInt32LE(16000, 24); wav.writeUInt32LE(32000, 28); wav.writeUInt16LE(2, 32); wav.writeUInt16LE(16, 34); wav.write('data', 36); wav.writeUInt32LE(32000, 40);
  for (const [label, name] of [['添加参考音频', 'reference.wav'], ['添加用户演奏素材', 'performance.wav']]) {
    const chooser = page.waitForEvent('filechooser'); await page.getByRole('button', { name: label, exact: true }).click(); await (await chooser).setFiles({ name, mimeType: 'audio/wav', buffer: wav });
    await expect.poll(async () => page.evaluate(id => JSON.parse(localStorage.getItem('voice-workbench-v1') ?? '{}').drafts?.find((d: { id: string }) => d.id === id)?.timeline?.assets?.length ?? 0, draft.id)).toBe(name === 'reference.wav' ? 1 : 2);
  }
  await expect(page.getByText('仅已导入，尚未分析；AI 不能直接听见这个文件。')).toBeVisible();
  const saved = await page.evaluate(id => JSON.parse(localStorage.getItem('voice-workbench-v1') ?? '{}').drafts.find((d: { id: string }) => d.id === id), draft.id);
  expect(saved.arrangementInput.referenceAssetId).toBe(saved.timeline.assets[0].id); expect(saved.timeline.tracks).toHaveLength(1); expect(saved.timeline.tracks[0].origin).not.toBe('arrangement');
});
