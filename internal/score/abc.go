package score

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"yovoice/internal/schema"
)

// YuE2 的旋律谱是受限的 ABC 方言（见 YuE2 官方 yue2-music skill 的 abc-editing.md）：
// 固定表头、Vocal/Ins 两个单声部、每组 1–4 小节先 Vocal 后 Ins、`% verse` 注释分段、和弦只写在 Vocal。
// 这里按官方 abc_tools.py 的规则解析，遇到方言之外的写法直接报错，不猜测时值。

// ABCTicksPerWhole 是内部时间单位：一个全音符 4096 tick，L:1/1024 也能整除。
const ABCTicksPerWhole = 4096

const abcQuarter = ABCTicksPerWhole / 4

// ABCMaxLength 是旋律谱文本的长度上限；YuE2 规划阶段最多 4096 个 token，正常谱远小于此。
const ABCMaxLength = 64 << 10

var (
	abcVoices      = [2]string{"Vocal", "Ins"}
	abcVoiceHeader = [2]string{`V: Vocal clef=treble name="Vocal Melody" snm="Vocal"`, `V: Ins clef=treble name="Ins Melody" snm="Inst."`}
	abcDurations   = []int{1, 2, 3, 4, 6, 8, 12, 16, 24, 32, 48}
	abcNatural     = map[byte]int{'C': 0, 'D': 2, 'E': 4, 'F': 5, 'G': 7, 'A': 9, 'B': 11}
	// abcQualities 是官方方言允许的和弦性质及其音程，空串为大三和弦。
	abcQualities = map[string][]int{
		"": {0, 4, 7}, "m": {0, 3, 7}, "dim": {0, 3, 6}, "aug": {0, 4, 8}, "7": {0, 4, 7, 10}, "maj7": {0, 4, 7, 11},
		"m7": {0, 3, 7, 10}, "dim7": {0, 3, 6, 9}, "m7b5": {0, 3, 6, 10}, "sus4": {0, 5, 7}, "sus2": {0, 2, 7},
		"6": {0, 4, 7, 9}, "m6": {0, 3, 7, 9}, "7sus4": {0, 5, 7, 10}, "m(maj7)": {0, 3, 7, 11},
	}
	abcChord = regexp.MustCompile(`^([A-G](?:bb|##|b|#)?)(maj7|m\(maj7\)|m7b5|7sus4|dim7|dim|aug|sus4|sus2|m7|m6|m|7|6)?(?:/([A-G](?:bb|##|b|#)?))?$`)
	abcToken = regexp.MustCompile(`^(?:"([^"\n]*)"|\[K:([^\]\n]+)\]|(\^\^|__|\^|_|=)?([A-Ga-gz])([,']*)([0-9]*)(-?))`)
	abcMeter = regexp.MustCompile(`^([1-9][0-9]*)/([1-9][0-9]*)$`)
	abcRests = regexp.MustCompile(`^Z([2-4])?$`)
	abcKeys  = func() map[string]int {
		keys := map[string]int{}
		for i, k := range []string{"Cb", "Gb", "Db", "Ab", "Eb", "Bb", "F", "C", "G", "D", "A", "E", "B", "F#", "C#"} {
			keys[k] = i - 7
		}
		for i, k := range []string{"Abm", "Ebm", "Bbm", "Fm", "Cm", "Gm", "Dm", "Am", "Em", "Bm", "F#m", "C#m", "G#m", "D#m", "A#m"} {
			keys[k] = i - 7
		}
		return keys
	}()
)

// ABCIssue 是谱面问题：Code 供界面本地化，Line 是 1 起的行号，Bar 是 1 起的小节号，Detail 用英文写给模型修正。
type ABCIssue struct {
	Code   string `json:"code"`
	Line   int    `json:"line,omitempty"`
	Bar    int    `json:"bar,omitempty"`
	Voice  string `json:"voice,omitempty"`
	Detail string `json:"detail"`
}

func (i *ABCIssue) Error() string {
	where := ""
	if i.Line > 0 {
		where += fmt.Sprintf("line %d", i.Line)
	}
	if i.Voice != "" {
		where += ", " + i.Voice
	}
	if i.Bar > 0 {
		where += fmt.Sprintf(", bar %d", i.Bar)
	}
	if where = strings.TrimPrefix(where, ", "); where != "" {
		return where + ": " + i.Detail
	}
	return i.Detail
}

type ABCNote struct {
	Start  int `json:"start"`
	Length int `json:"length"`
	Pitch  int `json:"pitch"`
}

type ABCChord struct {
	At   int    `json:"at"`
	Name string `json:"name"`
}

type ABCBar struct {
	Start  int    `json:"start"`
	Length int    `json:"length"`
	Meter  [2]int `json:"meter"`
}

type ABCVoice struct {
	Notes  []ABCNote  `json:"notes"`
	Chords []ABCChord `json:"chords,omitempty"`
	Bars   []ABCBar   `json:"bars"`
	keys   []string
	meter  [2]int
	key    string
	time   int
	// pending 是连音线等待接续的音：实际音高与书写音高。
	pending *[2]int
}

// ABCSection 记录 `% name` 注释开始的段落，Bar 是该段第一个小节（1 起）。
type ABCSection struct {
	Name string `json:"name"`
	Bar  int    `json:"bar"`
}

// ABCScore 是解析后的旋律谱，时间均以 tick 计。
type ABCScore struct {
	Tempo    int          `json:"tempo"`
	Meter    [2]int       `json:"meter"`
	Key      string       `json:"key"`
	Unit     int          `json:"unit"`
	Voices   [2]*ABCVoice `json:"-"`
	Sections []ABCSection `json:"sections,omitempty"`
	// musicLines 是音符行的下标（0 起）到声部名的映射，供去和弦等文本改写使用。
	musicLines map[int]string
}

func (s *ABCScore) Vocal() *ABCVoice { return s.Voices[0] }
func (s *ABCScore) Ins() *ABCVoice   { return s.Voices[1] }

// Bars 是小节数，两个声部相同。
func (s *ABCScore) Bars() int { return len(s.Voices[0].Bars) }

// Seconds 是谱面时长；YuE2 实际演唱会有出入。
func (s *ABCScore) Seconds() float64 {
	return float64(s.Voices[0].time) / abcQuarter * 60 / float64(s.Tempo)
}

// HasChords 决定按谱生成时用 full（带和弦）还是 melody（伴奏自由发挥）。
func (s *ABCScore) HasChords() bool { return len(s.Voices[0].Chords) > 0 }

func issue(code string, line int, format string, args ...any) *ABCIssue {
	return &ABCIssue{Code: code, Line: line, Detail: fmt.Sprintf(format, args...)}
}

func abcKeyAccidentals(key string) (map[byte]int, bool) {
	count, ok := abcKeys[key]
	if !ok {
		return nil, false
	}
	result := map[byte]int{}
	order := "FCGDAEB"
	sign := 1
	if count < 0 {
		order, sign, count = "BEADGCF", -1, -count
	}
	for i := 0; i < count; i++ {
		result[order[i]] = sign
	}
	return result, true
}

func abcMeterValue(text string) ([2]int, bool) {
	m := abcMeter.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return [2]int{}, false
	}
	n, _ := strconv.Atoi(m[1])
	d, _ := strconv.Atoi(m[2])
	if d > 1024 || d&(d-1) != 0 || n > 64 {
		return [2]int{}, false
	}
	return [2]int{n, d}, true
}

func powerOfTwo(n int) bool { return n > 0 && n <= 1024 && n&(n-1) == 0 }

// ParseABC 校验并解析旋律谱；出错时返回 *ABCIssue。
func ParseABC(text string) (*ABCScore, error) {
	if len(text) > ABCMaxLength {
		return nil, issue("length", 0, "score is longer than %d bytes", ABCMaxLength)
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) < 12 {
		return nil, issue("header", 0, "incomplete native two-voice ABC: expected the 8 header lines followed by Vocal and Ins blocks")
	}
	if lines[0] != "X:1" || strings.TrimSpace(lines[1]) != "T:" {
		return nil, issue("header", 1, "expected native X:1 and a blank T: header")
	}
	if !strings.HasPrefix(lines[2], "M:") {
		return nil, issue("header", 3, "missing header M:")
	}
	meter, ok := abcMeterValue(lines[2][2:])
	if !ok {
		return nil, issue("meter", 3, "unsupported meter %q; write an explicit fraction such as 4/4", lines[2][2:])
	}
	unit, e := strconv.Atoi(strings.TrimPrefix(lines[3], "L:1/"))
	if !strings.HasPrefix(lines[3], "L:1/") || e != nil || !powerOfTwo(unit) {
		return nil, issue("header", 4, "expected L:1/<power of two>, usually L:1/32")
	}
	tempo, e := strconv.Atoi(strings.TrimPrefix(lines[4], "Q:1/4="))
	if !strings.HasPrefix(lines[4], "Q:1/4=") || e != nil || tempo < 30 || tempo > 300 {
		return nil, issue("tempo", 5, "expected an integer quarter-note tempo Q:1/4=<30-300>")
	}
	if lines[5] != abcVoiceHeader[0] || lines[6] != abcVoiceHeader[1] {
		return nil, issue("header", 6, "keep the native Vocal and Ins voice definitions")
	}
	if !strings.HasPrefix(lines[7], "K:") {
		return nil, issue("header", 8, "missing header K:")
	}
	key := strings.TrimSpace(lines[7][2:])
	if _, ok := abcKeyAccidentals(key); !ok {
		return nil, issue("key", 8, "unsupported key %q; use a standard major or minor key such as G or Em", key)
	}
	s := &ABCScore{Tempo: tempo, Meter: meter, Key: key, Unit: unit, musicLines: map[int]string{}}
	for i := range s.Voices {
		s.Voices[i] = &ABCVoice{meter: meter, key: key, keys: []string{"0:" + key}}
	}
	cursor, section := 8, ""
	for cursor < len(lines) {
		for cursor < len(lines) && (strings.HasPrefix(lines[cursor], "%") || strings.TrimSpace(lines[cursor]) == "") {
			if name := strings.TrimSpace(strings.TrimPrefix(lines[cursor], "%")); name != "" {
				section = name
			}
			cursor++
		}
		if cursor == len(lines) {
			if section != "" {
				return nil, issue("group", cursor, "section comment %q has no music after it", section)
			}
			break
		}
		if section != "" {
			s.Sections = append(s.Sections, ABCSection{Name: section, Bar: s.Bars() + 1})
			section = ""
		}
		counts := [2]int{}
		for vi, name := range abcVoices {
			voice := s.Voices[vi]
			if cursor >= len(lines) || strings.TrimSpace(lines[cursor]) != "V: "+name {
				return nil, &ABCIssue{Code: "group", Line: cursor + 1, Voice: name, Bar: len(voice.Bars) + 1, Detail: "expected a V: " + name + " line; every group lists Vocal then Ins"}
			}
			cursor++
			fields := map[byte]bool{}
			for cursor < len(lines) && (strings.HasPrefix(lines[cursor], "M:") || strings.HasPrefix(lines[cursor], "K:")) {
				field, value := lines[cursor][0], strings.TrimSpace(lines[cursor][2:])
				if fields[field] {
					return nil, &ABCIssue{Code: "group", Line: cursor + 1, Voice: name, Detail: fmt.Sprintf("duplicate %c: field", field)}
				}
				fields[field] = true
				if field == 'M' {
					m, ok := abcMeterValue(value)
					if !ok {
						return nil, &ABCIssue{Code: "meter", Line: cursor + 1, Voice: name, Detail: fmt.Sprintf("unsupported meter %q", value)}
					}
					voice.meter = m
				} else {
					if _, ok := abcKeyAccidentals(value); !ok {
						return nil, &ABCIssue{Code: "key", Line: cursor + 1, Voice: name, Detail: fmt.Sprintf("unsupported key %q", value)}
					}
					voice.key = value
					voice.keys = append(voice.keys, strconv.Itoa(voice.time)+":"+value)
				}
				cursor++
			}
			if cursor >= len(lines) {
				return nil, &ABCIssue{Code: "group", Line: cursor, Voice: name, Detail: "missing music line"}
			}
			line := strings.TrimSpace(lines[cursor])
			if !strings.HasSuffix(line, "|") {
				return nil, &ABCIssue{Code: "barline", Line: cursor + 1, Voice: name, Bar: len(voice.Bars) + 1, Detail: "a music line must end with a plain barline |"}
			}
			s.musicLines[cursor] = name
			var bars []string
			for _, bar := range strings.Split(line[:len(line)-1], "|") {
				bar = strings.TrimSpace(bar)
				if bar == "" {
					return nil, &ABCIssue{Code: "barline", Line: cursor + 1, Voice: name, Bar: len(voice.Bars) + len(bars) + 1, Detail: "empty measure or unsupported double/repeat barline"}
				}
				if m := abcRests.FindStringSubmatch(bar); m != nil {
					n := 1
					if m[1] != "" {
						n, _ = strconv.Atoi(m[1])
					}
					for range n {
						bars = append(bars, "Z")
					}
				} else {
					bars = append(bars, bar)
				}
			}
			if len(bars) > 4 {
				return nil, &ABCIssue{Code: "group", Line: cursor + 1, Voice: name, Bar: len(voice.Bars) + 1, Detail: "a group holds 1-4 measures after expanding Z rests"}
			}
			counts[vi] = len(bars)
			for _, bar := range bars {
				if e := parseABCBar(bar, voice, unit); e != nil {
					e.Line, e.Voice, e.Bar = cursor+1, name, len(voice.Bars)+1
					return nil, e
				}
			}
			cursor++
		}
		if counts[0] != counts[1] {
			return nil, &ABCIssue{Code: "group", Line: cursor, Bar: s.Bars(), Detail: fmt.Sprintf("Vocal has %d measures but Ins has %d in this group", counts[0], counts[1])}
		}
	}
	if s.Bars() == 0 {
		return nil, issue("group", 9, "the score has no measures")
	}
	for vi, voice := range s.Voices {
		if voice.pending != nil {
			return nil, &ABCIssue{Code: "tie", Voice: abcVoices[vi], Bar: len(voice.Bars), Detail: "unresolved tie at the end of the score"}
		}
	}
	if len(s.Ins().Chords) > 0 {
		return nil, &ABCIssue{Code: "chord", Voice: "Ins", Detail: "chord symbols belong in Vocal, not Ins"}
	}
	if !slices.Equal(s.Vocal().Bars, s.Ins().Bars) {
		return nil, issue("grid", 0, "Vocal and Ins meter/time grids differ")
	}
	if !slices.Equal(s.Vocal().keys, s.Ins().keys) {
		return nil, issue("key", 0, "Vocal and Ins must change key at the same time")
	}
	if s.Seconds() > schema.ScoreMaxSeconds {
		return nil, issue("length", 0, "the score is longer than %d seconds", int(schema.ScoreMaxSeconds))
	}
	return s, nil
}

func parseABCBar(body string, voice *ABCVoice, unit int) *ABCIssue {
	length := ABCTicksPerWhole * voice.meter[0] / voice.meter[1]
	start, offset := voice.time, 0
	// 原生导出按字母传递临时记号，跨八度也生效，到小节线重置。
	local := map[byte]int{}
	if body == "Z" {
		if voice.pending != nil {
			return &ABCIssue{Code: "tie", Detail: "a tie runs into a full-measure rest"}
		}
		offset = length
	}
	for cursor := 0; body != "Z" && cursor < len(body); {
		if body[cursor] == ' ' || body[cursor] == '\t' {
			cursor++
			continue
		}
		m := abcToken.FindStringSubmatchIndex(body[cursor:])
		if m == nil {
			rest := body[cursor:]
			if len(rest) > 24 {
				rest = rest[:24]
			}
			return &ABCIssue{Code: "token", Detail: fmt.Sprintf("unsupported notation at %q (tuplets, grace notes, chords in brackets, slurs and decorations are outside the native dialect)", rest)}
		}
		base := cursor
		group := func(i int) (string, bool) {
			if m[2*i] < 0 {
				return "", false
			}
			return body[base+m[2*i] : base+m[2*i+1]], true
		}
		cursor += m[1]
		if offset >= length {
			return &ABCIssue{Code: "overflow", Detail: "an event comes after the end of the measure"}
		}
		if chord, ok := group(1); ok {
			if !abcChord.MatchString(chord) {
				return &ABCIssue{Code: "chord", Detail: fmt.Sprintf("unsupported chord %q; use a root with one of: major, m, dim, aug, 7, maj7, m7, dim7, m7b5, sus4, sus2, 6, m6, 7sus4, m(maj7), optionally /bass", chord)}
			}
			voice.Chords = append(voice.Chords, ABCChord{At: start + offset, Name: chord})
			continue
		}
		if key, ok := group(2); ok {
			if _, ok := abcKeyAccidentals(key); !ok {
				return &ABCIssue{Code: "key", Detail: fmt.Sprintf("unsupported key %q", key)}
			}
			voice.key = key
			voice.keys = append(voice.keys, strconv.Itoa(start+offset)+":"+key)
			local = map[byte]int{}
			continue
		}
		acc, _ := group(3)
		note, _ := group(4)
		octave, _ := group(5)
		digits, _ := group(6)
		tie, _ := group(7)
		units := 1
		if digits != "" {
			units, _ = strconv.Atoi(digits)
		}
		if !slices.Contains(abcDurations, units) {
			return &ABCIssue{Code: "duration", Detail: fmt.Sprintf("unsupported duration %d; use one of 1,2,3,4,6,8,12,16,24,32,48 and tie notes for other lengths", units)}
		}
		duration := units * ABCTicksPerWhole / unit
		if offset+duration > length {
			return &ABCIssue{Code: "overflow", Detail: fmt.Sprintf("notes add up to more than the %d/%d measure", voice.meter[0], voice.meter[1])}
		}
		if strings.Contains(octave, ",") && strings.Contains(octave, "'") {
			return &ABCIssue{Code: "token", Detail: "mixed octave marks"}
		}
		if note == "z" {
			if acc != "" || octave != "" || tie != "" {
				return &ABCIssue{Code: "rest", Detail: "a rest cannot carry accidentals, octave marks or ties"}
			}
			if voice.pending != nil {
				return &ABCIssue{Code: "tie", Detail: "a tie runs into a rest"}
			}
		} else {
			letter := strings.ToUpper(note)[0]
			written := 60 + abcNatural[letter] + 12*(strings.Count(octave, "'")-strings.Count(octave, ","))
			if note[0] >= 'a' {
				written += 12
			}
			signature, _ := abcKeyAccidentals(voice.key)
			alteration, ok := local[letter]
			if !ok {
				alteration = signature[letter]
			}
			if acc != "" {
				alteration = map[string]int{"=": 0, "_": -1, "__": -2, "^": 1, "^^": 2}[acc]
				local[letter] = alteration
			}
			pitch := written + alteration
			if voice.pending != nil {
				// 跨小节线接续的音不写记号时保持原音高，也不影响本小节后面的音。
				if acc == "" && written == voice.pending[1] {
					pitch = voice.pending[0]
				}
				if pitch != voice.pending[0] {
					return &ABCIssue{Code: "tie", Detail: fmt.Sprintf("a tie joins different pitches (%d to %d)", voice.pending[0], pitch)}
				}
				voice.Notes[len(voice.Notes)-1].Length += duration
			} else {
				if pitch < 0 || pitch > 127 {
					return &ABCIssue{Code: "pitch", Detail: fmt.Sprintf("pitch %d is outside the MIDI range", pitch)}
				}
				voice.Notes = append(voice.Notes, ABCNote{Start: start + offset, Length: duration, Pitch: pitch})
			}
			voice.pending = nil
			if tie != "" {
				voice.pending = &[2]int{pitch, written}
			}
		}
		offset += duration
	}
	if offset != length {
		return &ABCIssue{Code: "duration", Detail: fmt.Sprintf("the measure holds %s of %s quarter notes", quarters(offset), quarters(length))}
	}
	voice.Bars = append(voice.Bars, ABCBar{Start: start, Length: length, Meter: voice.meter})
	voice.time += length
	return nil
}

func quarters(ticks int) string {
	return strconv.FormatFloat(float64(ticks)/abcQuarter, 'f', -1, 64)
}

// StripABCChords 去掉全部和弦记号，旋律、休止和小节保持不变；按谱生成时随之改用 melody 模式。
func StripABCChords(text string) (string, error) {
	s, e := ParseABC(text)
	if e != nil {
		return "", e
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	quoted := regexp.MustCompile(`"[^"\n]*"`)
	for i := range s.musicLines {
		lines[i] = quoted.ReplaceAllString(lines[i], "")
	}
	out := strings.Join(lines, "\n")
	result, e := ParseABC(out)
	if e != nil {
		return "", e
	}
	for vi := range s.Voices {
		if !slices.Equal(s.Voices[vi].Notes, result.Voices[vi].Notes) {
			return "", issue("grid", 0, "removing chords changed the melody")
		}
	}
	return out, nil
}

// SetABCTempo 改写表头速度；拍子内的节奏不变，谱面时长随之变化。
func SetABCTempo(text string, tempo int) (string, error) {
	if _, e := ParseABC(text); e != nil {
		return "", e
	}
	if tempo < 30 || tempo > 300 {
		return "", issue("tempo", 5, "tempo must be 30-300")
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	lines[4] = "Q:1/4=" + strconv.Itoa(tempo)
	return strings.Join(lines, "\n"), nil
}

// ABCChanges 是两版谱之间按音符事件比较的结果，小节号从 1 起。
type ABCChanges struct {
	Tempo  bool   `json:"tempo,omitempty"`
	Key    bool   `json:"key,omitempty"`
	Vocal  []int  `json:"vocal,omitempty"`
	Ins    []int  `json:"ins,omitempty"`
	Chords []int  `json:"chords,omitempty"`
	Bars   [2]int `json:"bars"`
}

// CompareABC 按小节比较音高、起点、时值与和弦，而不是比较字符；连音线写法不同但发声相同视为未改。
func CompareABC(before, after *ABCScore) ABCChanges {
	c := ABCChanges{Tempo: before.Tempo != after.Tempo, Key: !slices.Equal(before.Vocal().keys, after.Vocal().keys), Bars: [2]int{before.Bars(), after.Bars()}}
	bars := max(before.Bars(), after.Bars())
	type barEvents struct {
		notes  [2][]ABCNote
		chords []ABCChord
	}
	split := func(s *ABCScore, bar int) (out barEvents, ok bool) {
		if bar >= s.Bars() {
			return out, false
		}
		b := s.Vocal().Bars[bar]
		for vi, voice := range s.Voices {
			for _, n := range voice.Notes {
				if n.Start >= b.Start && n.Start < b.Start+b.Length {
					out.notes[vi] = append(out.notes[vi], ABCNote{Start: n.Start - b.Start, Length: n.Length, Pitch: n.Pitch})
				}
			}
		}
		for _, ch := range s.Vocal().Chords {
			if ch.At >= b.Start && ch.At < b.Start+b.Length {
				out.chords = append(out.chords, ABCChord{At: ch.At - b.Start, Name: ch.Name})
			}
		}
		return out, true
	}
	for bar := range bars {
		a, okA := split(before, bar)
		b, okB := split(after, bar)
		if !okA || !okB || !slices.Equal(a.notes[0], b.notes[0]) {
			c.Vocal = append(c.Vocal, bar+1)
		}
		if !okA || !okB || !slices.Equal(a.notes[1], b.notes[1]) {
			c.Ins = append(c.Ins, bar+1)
		}
		if !okA || !okB || !slices.Equal(a.chords, b.chords) {
			c.Chords = append(c.Chords, bar+1)
		}
	}
	return c
}

// chordPitches 返回和弦的低音和三/七和弦音（低音落在 C2–B2，和弦音落在 C4 附近）。
func chordPitches(name string) (int, []int, bool) {
	m := abcChord.FindStringSubmatch(name)
	if m == nil {
		return 0, nil, false
	}
	pc := func(n string) int {
		v := abcNatural[n[0]]
		for _, r := range n[1:] {
			if r == '#' {
				v++
			} else {
				v--
			}
		}
		return (v%12 + 12) % 12
	}
	root := pc(m[1])
	bass := root
	if m[3] != "" {
		bass = pc(m[3])
	}
	var tones []int
	for _, interval := range abcQualities[m[2]] {
		p := 60 + root + interval
		for p > 71 {
			p -= 12
		}
		tones = append(tones, p)
	}
	slices.Sort(tones)
	return 36 + bass, tones, true
}

// Score 把旋律谱转换成编曲乐谱，供轨道视图显示：人声旋律、器乐旋律、和弦铺底与低音各一轨，空轨省略。
// 小节与拍按谱面小节换算，拍以四分音符计，与编曲乐谱一致。
func (s *ABCScore) Score() schema.Score {
	out := schema.Score{Tempo: float64(s.Tempo), TimeSignature: []int{s.Meter[0], s.Meter[1]}, Key: abcKeyName(s.Key)}
	bars := s.Vocal().Bars
	// 编曲乐谱只有一个拍号：谱中途改成更长的拍子（如 4/4 里夹一小节 9/8）时，改按绝对时间排到固定小节网格上，
	// 保证每个音都落在小节内、试听时间不变；只有这种谱的小节号会与谱面略有出入。
	grid := s.Meter[0] * ABCTicksPerWhole / s.Meter[1]
	regular := !slices.ContainsFunc(bars, func(b ABCBar) bool { return b.Length > grid })
	locate := func(at int) (int, float64) {
		if !regular {
			return at/grid + 1, 1 + float64(at%grid)/abcQuarter
		}
		i, _ := slices.BinarySearchFunc(bars, at, func(b ABCBar, t int) int {
			if t < b.Start {
				return 1
			}
			if t >= b.Start+b.Length {
				return -1
			}
			return 0
		})
		i = min(i, len(bars)-1)
		return i + 1, 1 + float64(at-bars[i].Start)/abcQuarter
	}
	// barEnd 是 at 所在小节的结束位置，和弦铺底在这里切开。
	barEnd := func(at int) int {
		if !regular {
			return (at/grid + 1) * grid
		}
		bar, _ := locate(at)
		b := bars[bar-1]
		return b.Start + b.Length
	}
	melody := func(id, name, role string, program, velocity int, notes []ABCNote) {
		if len(notes) == 0 {
			return
		}
		t := schema.ScoreTrack{ID: id, Name: name, Role: role, Program: program}
		for _, n := range notes {
			bar, beat := locate(n.Start)
			t.Notes = append(t.Notes, schema.ScoreNote{Bar: bar, Beat: beat, Pitch: n.Pitch, Length: float64(n.Length) / abcQuarter, Velocity: velocity})
		}
		out.Tracks = append(out.Tracks, t)
	}
	melody("vocal", "Vocal", "melody", 53, 100, s.Vocal().Notes)
	melody("ins", "Ins", "piano", 0, 90, s.Ins().Notes)
	// 和弦持续到下一个和弦或曲末，按小节切开，便于在轨道上对齐小节线。
	chords := s.Vocal().Chords
	pad := schema.ScoreTrack{ID: "chords", Name: "Chords", Role: "pad", Program: 89}
	bass := schema.ScoreTrack{ID: "bass", Name: "Bass", Role: "bass", Program: 33}
	end := s.Vocal().time
	for i, ch := range chords {
		stop := end
		if i+1 < len(chords) {
			stop = chords[i+1].At
		}
		low, tones, ok := chordPitches(ch.Name)
		if !ok || stop <= ch.At {
			continue
		}
		for at := ch.At; at < stop; {
			bar, beat := locate(at)
			until := min(stop, barEnd(at))
			length := float64(until-at) / abcQuarter
			for _, p := range tones {
				pad.Notes = append(pad.Notes, schema.ScoreNote{Bar: bar, Beat: beat, Pitch: p, Length: length, Velocity: 64})
			}
			bass.Notes = append(bass.Notes, schema.ScoreNote{Bar: bar, Beat: beat, Pitch: low, Length: length, Velocity: 80})
			at = until
		}
	}
	if len(pad.Notes) > 0 {
		out.Tracks = append(out.Tracks, pad, bass)
	}
	// 段落起止换算到同一套小节号上。
	at := func(bar int) int {
		if bar > len(bars) {
			return s.Vocal().time
		}
		return bars[bar-1].Start
	}
	lastBar, _ := locate(max(s.Vocal().time-1, 0))
	for i, section := range s.Sections {
		start, _ := locate(at(section.Bar))
		last := lastBar
		if i+1 < len(s.Sections) {
			next, _ := locate(at(s.Sections[i+1].Bar))
			last = next - 1
		}
		if last >= start {
			out.Sections = append(out.Sections, schema.ScoreSection{Name: section.Name, Start: start, End: last})
		}
	}
	return out
}

// abcKeyName 把 K: 字段写成乐谱里的调名，如 "Bb major"、"F# minor"。
func abcKeyName(key string) string {
	if strings.HasSuffix(key, "m") {
		return strings.TrimSuffix(key, "m") + " minor"
	}
	return key + " major"
}
