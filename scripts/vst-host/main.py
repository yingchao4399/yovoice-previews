"""在独立进程里试载 VST3、打开编辑器或渲染一枚 MIDI 测试音。"""

import argparse
import json
import math
import struct
import sys
import tempfile
import wave
from pathlib import Path

import numpy as np
from mido import Message, MidiFile
from pedalboard import load_plugin


def make_test_sample(path: Path) -> None:
    with wave.open(str(path), "wb") as wav:
        wav.setnchannels(1)
        wav.setsampwidth(2)
        wav.setframerate(44100)
        wav.writeframes(b"".join(struct.pack("<h", int(10000 * math.sin(2 * math.pi * 440 * i / 44100))) for i in range(44100)))


def load_decent_test_sample(plugin, sample: Path) -> None:
    # Decent Sampler 空载时本来没有声音；试音暂时载入一个合成测试样本。
    state = plugin.preset_data
    list_offset = struct.unpack_from("<Q", state, 40)[0]
    component_size = struct.unpack_from("<Q", state, list_offset + 20)[0]
    old = b"<groups/>"
    replacement = f'<groups><group><sample path="{sample.as_posix()}" rootNote="69" loNote="0" hiNote="127"/></group></groups>'.encode()
    position = state.index(old)
    delta = len(replacement) - len(old)
    state = bytearray(state[:position] + replacement + state[position + len(old):])
    struct.pack_into("<I", state, 52, struct.unpack_from("<I", state, 52)[0] + delta)
    struct.pack_into("<Q", state, 40, list_offset + delta)
    struct.pack_into("<Q", state, list_offset + delta + 20, component_size + delta)
    struct.pack_into("<Q", state, list_offset + delta + 32, list_offset + delta)
    plugin.preset_data = bytes(state)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--plugin", required=True)
    parser.add_argument("--output")
    parser.add_argument("--editor", action="store_true")
    parser.add_argument("--state-in")
    parser.add_argument("--save-state")
    parser.add_argument("--program")
    parser.add_argument("--midi")
    parser.add_argument("--validate-notes", action="store_true")
    parser.add_argument("--duration", type=float, default=1.5)
    args = parser.parse_args()
    plugin = load_plugin(args.plugin)
    if args.state_in:
        plugin.preset_data = Path(args.state_in).read_bytes()
    if args.program:
        plugin.program = args.program
    if args.editor:
        # show_editor 必须运行在宿主进程的主线程；窗口关闭后进程自然退出。
        plugin.show_editor()
        if args.save_state:
            Path(args.save_state).write_bytes(plugin.preset_data)
        return
    if args.save_state:
        Path(args.save_state).write_bytes(plugin.preset_data)
        if not args.output:
            print(json.dumps({"name": plugin.name}, ensure_ascii=False))
            return
    if not args.output:
        raise ValueError("试音模式需要 --output")
    if not plugin.is_instrument:
        raise ValueError("插件不是 MIDI 乐器")
    with tempfile.TemporaryDirectory(prefix="yovoice-vst-") as temporary:
        if not args.midi and not args.state_in and plugin.name.lower().replace(" ", "") == "decentsampler":
            sample = Path(temporary) / "test.wav"
            make_test_sample(sample)
            load_decent_test_sample(plugin, sample)
        events = [Message("note_on", note=69, velocity=110, time=0), Message("note_off", note=69, time=0.75)]
        if args.midi:
            events = []
            at = 0.0
            for message in MidiFile(args.midi):
                at += message.time
                if not message.is_meta and message.type != "program_change":
                    events.append(message.copy(time=at))
        if not 0 < args.duration <= 610:
            raise ValueError("渲染时长必须在 0 到 610 秒之间")
        if args.validate_notes:
            # 按实际 MIDI 音高、力度和通道检查，避免有声整轨漏掉预设键区以外的音符。
            opened = {}
            lengths = {}
            for event in events:
                if event.type == "note_on" and event.velocity:
                    opened.setdefault((event.channel, event.note), []).append((event.time, event.velocity))
                elif event.type in ("note_off", "note_on"):
                    key = (event.channel, event.note)
                    if opened.get(key):
                        start, velocity = opened[key].pop(0)
                        pair = (event.channel, event.note, velocity)
                        lengths[pair] = max(lengths.get(pair, 0), event.time - start)
            missing = []
            for (channel, note, velocity), length in lengths.items():
                duration = min(1.5, max(0.12, length) + 0.12)
                def sounding(seconds):
                    probe = [Message("control_change", channel=channel, control=7, value=127, time=0), Message("control_change", channel=channel, control=11, value=127, time=0), Message("note_on", channel=channel, note=note, velocity=velocity, time=0), Message("note_off", channel=channel, note=note, time=max(.01, seconds - .1))]
                    return float(np.max(np.abs(plugin(probe, sample_rate=44100, duration=seconds, num_channels=2))))
                peak = sounding(duration)
                if peak < 0.0000001 and length + .12 > duration:
                    peak = sounding(length + .12)
                if not math.isfinite(peak) or peak < 0.0000001:
                    missing.append({"note": note, "velocity": velocity, "channel": channel})
            if missing:
                print(json.dumps({"name": plugin.name, "missingNotes": missing}, ensure_ascii=False))
                sys.exit(3)
        audio = plugin(events, sample_rate=44100, duration=args.duration, num_channels=2)
        peak = float(np.max(np.abs(audio)))
        if not math.isfinite(peak) or peak < 0.001:
            raise ValueError("插件已加载，但 MIDI 测试音没有产生可听音频")
        if peak > 0.89:
            audio *= 0.89 / peak
        with wave.open(args.output, "wb") as wav:
            wav.setnchannels(2)
            wav.setsampwidth(2)
            wav.setframerate(44100)
            wav.writeframes((audio.T * 32767).astype("<i2").tobytes())
        print(json.dumps({"name": plugin.name, "peak": peak}, ensure_ascii=False))


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    sys.stderr.reconfigure(encoding="utf-8")
    main()
