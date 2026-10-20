import { useCallback, useEffect, useState } from 'react';
import { TextInput } from '@astryxdesign/core/TextInput';
import { Selector } from '@astryxdesign/core/Selector';
import { Button } from '@astryxdesign/core/Button';
import { HStack, VStack } from '@astryxdesign/core/Layout';
import { useTranslator } from '@astryxdesign/core/i18n';
import { FolderPlus, Play, RefreshCw, SlidersHorizontal } from 'lucide-react';
import { call, isDesktop, mediaUrl } from '../../shared/lib/client';
import { formatCallError } from '../../shared/i18n/format';
import type { State } from '../../shared/workbench';
import { SettingsGroup, SettingsRow } from '../../shared/ui/settings-group';

interface PluginInfo { name: string; path: string; format: string }

// 插件在独立宿主进程里试载，避免第三方代码运行在界面或主服务中。
export function PluginSettings({ state }: { state: State }) {
  const t = useTranslator();
  const [capture, setCapture] = useState<PluginInfo | null>(null);
  const [presetName, setPresetName] = useState('');
  const [instrument, setInstrument] = useState('');
  const [program, setProgram] = useState('');
  const [presetPath, setPresetPath] = useState('');
  const [provenance, setProvenance] = useState('');
  const [articulation, setArticulation] = useState('normal');
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [startedAt, setStartedAt] = useState(0);
  const capturing = state.activity?.kind === 'pluginCapture' && state.activity.status === 'running';
  useEffect(() => { const activity = state.activity; if (!startedAt || activity?.kind !== 'pluginCapture' || Date.parse(activity.startedAt ?? '') < startedAt) return; if (activity.status === 'completed') { setSaved(true); setStartedAt(0); } else if (activity.status === 'failed' || activity.status === 'cancelled') { setError(activity); setStartedAt(0); } }, [state.activity, startedAt]);
  const [plugins, setPlugins] = useState<PluginInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [testing, setTesting] = useState('');
  const [installing, setInstalling] = useState(false);
  const [audition, setAudition] = useState<{ path: string; url: string; name: string } | null>(null);
  const refresh = useCallback(async () => {
    setLoading(true); setError(null);
    try { setPlugins(await call<PluginInfo[]>('plugin.scan')); }
    catch (cause) { setError(cause); }
    finally { setLoading(false); }
  }, []);
  const testPlugin = async (plugin: PluginInfo) => {
    setTesting(plugin.path); setError(null); setAudition(null);
    try {
      const result = await call<{ name: string; fileName: string; peak: number }>('plugin.probe', { path: plugin.path });
      setAudition({ path: plugin.path, name: result.name, url: await mediaUrl('outputs', result.fileName) });
    } catch (cause) { setError(cause); }
    finally { setTesting(''); }
  };
  const openEditor = async (plugin: PluginInfo) => {
    setError(null);
    try { await call('plugin.openEditor', { path: plugin.path }); }
    catch (cause) { setError(cause); }
  };
  const installPlugin = async (kind: 'file' | 'folder') => {
    setInstalling(true); setError(null);
    try {
      await call<PluginInfo | null>('plugin.install', { kind });
      await refresh();
    } catch (cause) { setError(cause); }
    finally { setInstalling(false); }
  };
  const savePreset = async (editor: boolean) => {
    if (!capture) return;
    setSaving(true); setSaved(false); setError(null); setStartedAt(Date.now());
    try {
      await call('plugin.captureInstrument', { path: capture.path, name: presetName.trim(), instrument: instrument.trim(), program: program.trim(), presetPath: presetPath.trim(), provenance, articulation, editor });
      setCapture(null);
    } catch (cause) { setError(cause); }
    finally { setSaving(false); }
  };
  useEffect(() => { void refresh(); }, [refresh]);
  return <VStack className="settings-panel" gap={5} id="plugins-panel" role="tabpanel" aria-label={t('@yovoice.settings.tabPlugins')}>
    <SettingsGroup title={t('@yovoice.plugins.title')} description={t('@yovoice.plugins.intro')} actions={<HStack gap={2}>
      <Button size="sm" variant="secondary" icon={<FolderPlus size={15} />} label={t('@yovoice.plugins.install')} isLoading={installing} isDisabled={!isDesktop || loading || installing} onClick={() => void installPlugin('file')} />
      <Button size="sm" variant="secondary" label={t('@yovoice.plugins.installFolder')} isDisabled={!isDesktop || loading || installing} onClick={() => void installPlugin('folder')} />
      <Button size="sm" variant="secondary" icon={<RefreshCw size={15} />} label={t('@yovoice.plugins.scan')} isLoading={loading} isDisabled={installing} onClick={() => void refresh()} />
    </HStack>}>
      {plugins.length ? plugins.map(plugin => <SettingsRow key={plugin.path} label={plugin.name} hint={plugin.path}>
        <HStack gap={2} vAlign="center"><small>{plugin.format}</small>
          <Button size="sm" variant="secondary" icon={<SlidersHorizontal size={15} />}
            label={t('@yovoice.plugins.openEditor')} onClick={() => void openEditor(plugin)} />
          <Button size="sm" variant="secondary" icon={<Play size={15} />}
            label={t('@yovoice.plugins.test')} isLoading={testing === plugin.path} isDisabled={!!testing} onClick={() => void testPlugin(plugin)} /></HStack>
        <Button size="sm" variant="secondary" label={t('@yovoice.plugins.saveInstrument')} isDisabled={saving || capturing} onClick={() => { setCapture(plugin); setPresetName(plugin.name); setSaved(false); }} />
        {audition?.path === plugin.path ? <VStack gap={1}><small>{t('@yovoice.plugins.testPassed', { name: audition.name })}</small><audio controls src={audition.url} /></VStack> : null}
      </SettingsRow>)
        : <HStack className="settings-row" gap={2}><small>{loading ? t('@yovoice.plugins.scanning') : t('@yovoice.plugins.empty')}</small></HStack>}
    </SettingsGroup>
    {capture ? <SettingsGroup title={t('@yovoice.plugins.saveInstrument')} description={t('@yovoice.plugins.presetHint')}>
      <VStack className="settings-row" gap={3}>
        <TextInput label={t('@yovoice.plugins.presetName')} value={presetName} onChange={setPresetName} />
        <TextInput label={t('@yovoice.arrangement.instrument')} value={instrument} onChange={setInstrument} />
        <TextInput label={t('@yovoice.plugins.program')} value={program} onChange={setProgram} isOptional />
        <TextInput label={t('@yovoice.plugins.presetPath')} value={presetPath} onChange={setPresetPath} isOptional />
        <Selector label={t('@yovoice.plugins.provenance')} value={provenance} options={[{ value: 'recorded', label: t('@yovoice.plugins.recorded') }, { value: 'synthesized', label: t('@yovoice.plugins.synthesized') }]} onChange={setProvenance} />
        <Selector label={t('@yovoice.instruments.articulation')} value={articulation} options={['normal', 'sustain', 'pluck', 'tremolo', 'pizzicato', 'spiccato'].map(value => ({ value, label: value }))} onChange={setArticulation} />
        <HStack gap={2} wrap="wrap"><Button size="sm" variant="primary" label={t('@yovoice.plugins.editAndSave')} isLoading={saving} isDisabled={saving || !presetName.trim() || !instrument.trim() || !provenance} onClick={() => void savePreset(true)} />
          <Button size="sm" variant="secondary" label={t('@yovoice.plugins.captureProgram')} isDisabled={saving || !presetName.trim() || !instrument.trim() || !provenance} onClick={() => void savePreset(false)} />
          <Button size="sm" variant="ghost" label={t('@yovoice.action.cancel')} isDisabled={saving || capturing} onClick={() => setCapture(null)} /></HStack>
      </VStack>
    </SettingsGroup> : null}
    {capturing ? <small role="status">{t('@yovoice.plugins.capturing')}</small> : null}
    {saved ? <small role="status">{t('@yovoice.plugins.instrumentSaved')}</small> : null}
    <small>{t('@yovoice.plugins.installHint')}</small>
    <small>{t('@yovoice.plugins.hostHint')}</small>
    {error ? <p role="alert">{formatCallError(t, error)}</p> : null}
  </VStack>;
}
