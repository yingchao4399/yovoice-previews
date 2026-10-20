import { test, expect } from '@playwright/test';
import { abcToScore } from '../src/features/score/score-abc';

test('ABC 解析遵守调号、临时升号、休止符和时值', () => {
  const score = abcToScore('X:1\nM:4/4\nL:1/8\nQ:1/4=120\nK:G\nF2 z2 G2 A2 | ^C D/2 E3/2 F |', 'zh-CN');
  expect(score).toMatchObject({ tempo: 120, timeSignature: [4, 4], key: 'G' });
  expect(score.tracks[0].notes.map(note => [note.bar, note.beat, note.pitch, note.length])).toEqual([
    [1, 1, 66, 1], [1, 3, 67, 1], [1, 4, 69, 1],
    [2, 1, 61, .5], [2, 1.5, 62, .25], [2, 1.75, 64, .75], [2, 2.5, 66, .5],
  ]);
  expect(() => abcToScore('K:C\n[CEG] |', 'zh-CN')).toThrow('@yovoice.score.abcUnsupported');
});

test('粘贴 AI Music Composer 的 ABC 旋律并保留作品', async ({ page }, testInfo) => {
  await page.goto('/');
  await page.getByTestId('nav-new').click();
  await page.getByRole('menuitem', { name: '编曲生成', exact: true }).click();
  await page.getByRole('button', { name: '粘贴 ABC 曲谱' }).first().click();
  const abc = 'X:1\nT:民乐旋律\nM:4/4\nL:1/4\nQ:1/4=100\nK:G\nG A B d | ^C D E F |';
  await page.getByRole('textbox', { name: 'ABC 记谱文本' }).fill(abc);
  await page.screenshot({ path: testInfo.outputPath('abc-import.png') });
  await page.getByRole('button', { name: '导入曲谱' }).click();
  await expect(page.getByRole('textbox', { name: '作品名称' })).toHaveValue('民乐旋律');
  await expect(page.locator('.score-summary')).toContainText('G · 100 BPM');
  await expect(page.locator('.score-track')).toHaveCount(1);
  await expect(page.locator('.saved')).toContainText('已保存');
  const saved = await page.evaluate(() => JSON.parse(localStorage.getItem('voice-workbench-v1')!).drafts.find((draft: { kind?: string }) => draft.kind === 'score'));
  expect(saved.score.tracks[0].notes.map((note: { pitch: number }) => note.pitch)).toEqual([67, 69, 71, 74, 61, 62, 64, 66]);
});
