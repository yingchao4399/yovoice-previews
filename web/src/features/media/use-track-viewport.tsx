import { useEffect, useState, type RefObject } from 'react';
import { Button } from '@astryxdesign/core/Button';
import { useTranslator } from '@astryxdesign/core/i18n';
import { Hand } from 'lucide-react';

// 捕获阶段处理平移，避免手型模式被音块移动、裁剪或波形进度输入抢走。
export function useTrackViewport(ref: RefObject<HTMLElement | null>) {
  const [hand, setHand] = useState(false);
  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    element.dataset.handTool = String(hand);
    let gesture: { id: number; x: number; y: number; left: number; top: number } | undefined;
    let suppressClick = false;
    let suppressionTimer: ReturnType<typeof setTimeout> | undefined;
    const wheel = (event: WheelEvent) => {
      if (event.shiftKey && !event.ctrlKey && !event.metaKey && Math.abs(event.deltaY) > Math.abs(event.deltaX) && element.scrollWidth > element.clientWidth) {
        event.preventDefault(); element.scrollLeft += event.deltaY;
      }
    };
    const down = (event: PointerEvent) => {
      if (event.button !== 1 && !(hand && event.button === 0)) return;
      const target = event.target instanceof Element ? event.target : null;
      if (target?.closest('textarea,select,input:not([type="range"]),[contenteditable="true"],.multitrack-label,.score-track-head')) return;
      event.preventDefault(); event.stopImmediatePropagation();
      clearTimeout(suppressionTimer);
      gesture = { id: event.pointerId, x: event.clientX, y: event.clientY, left: element.scrollLeft, top: element.scrollTop };
      suppressClick = true; element.setPointerCapture(event.pointerId); element.dataset.panning = 'true';
    };
    const move = (event: PointerEvent) => {
      if (!gesture || gesture.id !== event.pointerId) return;
      event.preventDefault(); event.stopImmediatePropagation();
      element.scrollLeft = gesture.left + gesture.x - event.clientX;
      element.scrollTop = gesture.top + gesture.y - event.clientY;
    };
    const up = (event: PointerEvent) => {
      if (!gesture || gesture.id !== event.pointerId) return;
      event.preventDefault(); event.stopImmediatePropagation();
      if (element.hasPointerCapture(event.pointerId)) element.releasePointerCapture(event.pointerId);
      gesture = undefined; delete element.dataset.panning;
      // 浏览器在 pointerup 后同步发出 click；中键不发 click，不能吞掉之后的普通点击。
      suppressionTimer = setTimeout(() => { suppressClick = false; }, 0);
    };
    const click = (event: MouseEvent) => {
      if (!suppressClick) return;
      suppressClick = false; event.preventDefault(); event.stopImmediatePropagation();
    };
    element.addEventListener('wheel', wheel, { passive: false });
    element.addEventListener('pointerdown', down, true); element.addEventListener('pointermove', move, true);
    element.addEventListener('pointerup', up, true); element.addEventListener('pointercancel', up, true);
    element.addEventListener('click', click, true);
    return () => {
      clearTimeout(suppressionTimer);
      if (gesture && element.hasPointerCapture(gesture.id)) element.releasePointerCapture(gesture.id);
      delete element.dataset.handTool; delete element.dataset.panning;
      element.removeEventListener('wheel', wheel); element.removeEventListener('pointerdown', down, true);
      element.removeEventListener('pointermove', move, true); element.removeEventListener('pointerup', up, true);
      element.removeEventListener('pointercancel', up, true); element.removeEventListener('click', click, true);
    };
  }, [ref, hand]);
  return { hand, toggle: () => setHand(value => !value) };
}

export function TrackPanButton({ active, toggle }: { active: boolean; toggle: () => void }) {
  const t = useTranslator();
  return <Button size="sm" variant={active ? 'secondary' : 'ghost'} isIconOnly icon={<Hand />} label={t('@yovoice.player.panCanvas')}
    tooltip={t('@yovoice.player.panHint')} aria-pressed={active} onClick={toggle} />;
}
