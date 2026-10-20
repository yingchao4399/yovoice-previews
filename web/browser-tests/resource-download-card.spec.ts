import { test, expect } from '@playwright/test';
import { emptyState, type State } from '../src/shared/workbench';
import { seedState } from './seed';

function downloadState(running = false): State {
  const state = emptyState();
  state.aiProviders = [{ id: 'provider', preset: 'ollama', name: '测试服务', format: 'openai', baseURL: 'http://localhost:11434/v1', modelsPath: '/models', model: 'qwen3' }]; state.aiProviderID = 'provider';
  state.assistantSessions = { currentId: 'download-session', sessions: [{ id: 'download-session', title: '准备音源', updatedAt: new Date().toISOString(), messages: [{ id: 'download-message', role: 'assistant', status: 'completed', createdAt: new Date().toISOString(), blocks: [{ kind: 'tool', card: { tool: 'propose_resource_download', state: 'done', summary: '准备琵琶和箫', proposal: {
    id: 'download-job', action: 'download_resources', state: running ? 'confirmed' : 'pending', reason: '补齐已规划声部', downloads: { sessionId: 'download-session', state: running ? 'running' : 'awaiting_approval', items: [
      { catalogId: 'pipa', name: '琵琶录音', license: 'CC0-1.0', bytes: 100, manifest: 'fixed', state: running ? 'completed' : 'queued' },
      { catalogId: 'xiao', name: '箫录音', license: 'CC0-1.0', bytes: 200, manifest: 'fixed', state: running ? 'running' : 'queued' },
    ] },
  } } }] }] }] };
  return state;
}

test('下载卡展示精确清单与许可，预览不假装下载，拒绝后保持本地库不变', async ({ page }) => {
  await seedState(page, downloadState()); await page.goto('/'); await page.getByTestId('assistant-toggle').click();
  const card = page.getByTestId('resource-download-card');
  await expect(card).toContainText('琵琶录音'); await expect(card).toContainText('箫录音'); await expect(card).toContainText('CC0-1.0');
  await card.getByRole('button', { name: '确认下载', exact: true }).click();
  await expect(card.getByRole('alert')).toContainText(/桌面|预览/);
  await card.getByRole('button', { name: '不用了', exact: true }).click();
  await expect(card).toContainText('已取消');
  await expect(card.getByRole('button', { name: '确认下载', exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => JSON.parse(localStorage.getItem('voice-workbench-v1')!).instrumentSources ?? [])).toHaveLength(0);
});

test('批量进度合并已完成项目与当前实际字节数', async ({ page }) => {
  await seedState(page, downloadState(true)); await page.goto('/'); await page.getByTestId('assistant-toggle').click();
  await page.evaluate(() => (window as unknown as { __previewActivity: (slot: string, activity: unknown) => void }).__previewActivity('activity', {
    kind: 'instrumentDownload', requestId: 'download-job', status: 'running', code: '@yovoice.activity.downloading', params: { name: '箫录音' }, received: 80, total: 200,
  }));
  const progress = page.getByTestId('resource-download-card').getByRole('progressbar', { name: '音源下载进度' });
  await expect(progress).toHaveAttribute('max', '300'); await expect(progress).toHaveAttribute('value', '180');
  await expect(page.getByTestId('resource-download-card').getByRole('button', { name: '取消', exact: true })).toBeVisible();
});
