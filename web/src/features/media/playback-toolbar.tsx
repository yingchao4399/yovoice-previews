import { useEffect, useState, type ReactNode } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { HStack } from '@astryxdesign/core/Layout';
import { useTranslator } from '@astryxdesign/core/i18n';
import { Play, Pause, ZoomIn, ZoomOut } from 'lucide-react';
import { formatTime } from '../../shared/workbench';
import { Selector } from '../../shared/selector';
import { readLocal, writeLocal } from '../../shared/lib/local-store';

export type PlaybackMode = 'loop' | 'sequential' | 'shuffle';
const playbackModeKey = 'yovoice-playback-mode';
const modeListeners = new Set<(mode: PlaybackMode) => void>();

export function getPlaybackMode(): PlaybackMode {
  const value = readLocal(playbackModeKey);
  return value === 'sequential' || value === 'shuffle' ? value : 'loop';
}

export function setPlaybackMode(mode: PlaybackMode) {
  writeLocal(playbackModeKey, mode);
  modeListeners.forEach(listener => listener(mode));
}

export function usePlaybackMode() {
  const [mode, update] = useState<PlaybackMode>(getPlaybackMode);
  useEffect(() => { modeListeners.add(update); return () => { modeListeners.delete(update); }; }, []);
  return [mode, setPlaybackMode] as const;
}

export function PlaybackModeSelector({ disabled = false, compact = false }: { disabled?: boolean; compact?: boolean }) {
  const t = useTranslator();
  const [mode, change] = usePlaybackMode();
  return <Selector className="playback-mode-selector" label={t('@yovoice.player.playbackMode')} isLabelHidden size="sm" variant="ghost"
    width={compact ? '6.5rem' : '7.5rem'} value={mode} isDisabled={disabled}
    options={[
      { value: 'loop', label: t('@yovoice.player.playbackMode.loop') },
      { value: 'sequential', label: t('@yovoice.player.playbackMode.sequential') },
      { value: 'shuffle', label: t('@yovoice.player.playbackMode.shuffle') },
    ]}
    onChange={value => change(value as PlaybackMode)} />;
}

export function PlaybackToolbar({ time, duration, playing, disabled, loading, toggle, beforePlay, afterPlay, mode, children }: {
  time: number; duration: number; playing: boolean; disabled: boolean; loading?: boolean; toggle: () => void;
  beforePlay?: ReactNode; afterPlay?: ReactNode; mode?: PlaybackMode; children: ReactNode;
}) {
  const t = useTranslator();
  return <HStack className="transport" vAlign="center" gap={4} wrap="wrap">
    <small className="transport-time">{formatTime(time)} <em>/ {formatTime(duration)}</em></small>
    {mode ? <PlaybackModeSelector disabled={disabled} /> : null}
    <HStack className="transport-controls" gap={2} vAlign="center">
      {beforePlay}
      <Button label={t(playing ? '@yovoice.player.pause' : '@yovoice.player.play')} isIconOnly icon={playing ? <Pause /> : <Play fill="currentColor" />} size="sm" variant="primary" className="play-main" isDisabled={disabled} isLoading={loading} onClick={toggle} />
      {afterPlay}
    </HStack>
    <HStack className="transport-end" gap={3} vAlign="center" wrap="wrap">
      {children}
    </HStack>
  </HStack>;
}

export function TrackZoom({ value, change, disabled, min = 1, max = 8, fit }: { value: number; change: (value: number) => void; disabled: boolean; min?: number; max?: number; fit?: () => void }) {
  const t = useTranslator();
  return <HStack className="track-zoom" gap={0} vAlign="center" role="group" aria-label={t('@yovoice.player.zoomGroup')}>
    <Button label={t('@yovoice.player.zoomOut')} isIconOnly icon={<ZoomOut />} size="sm" variant="ghost" isDisabled={disabled || value <= min} onClick={() => change(Math.max(min, value / 2))} />
    <Button label={`${Number((value * 100).toFixed(2))}%`} aria-label={t('@yovoice.player.zoomFit')} size="sm" variant="ghost" isDisabled={disabled} onClick={() => fit ? fit() : change(1)} />
    <Button label={t('@yovoice.player.zoomIn')} isIconOnly icon={<ZoomIn />} size="sm" variant="ghost" isDisabled={disabled || value >= max} onClick={() => change(Math.min(max, value * 2))} />
  </HStack>;
}
