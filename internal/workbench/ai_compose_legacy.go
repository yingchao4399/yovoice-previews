package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"yovoice/internal/llm"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

// ScoreBrief 是“AI 写谱”的输入：一句话描述、目标时长（秒，0 表示由模型决定）和界面语言。
type ScoreBrief struct {
	ProjectID   string                  `json:"projectId"`
	Brief       string                  `json:"brief"`
	Seconds     int                     `json:"seconds"`
	Locale      schema.UiLocale         `json:"locale"`
	Arrangement *schema.ArrangementPlan `json:"arrangement,omitempty"`
}

const scorePrompt = `You are a film and video composer who writes arrangements as JSON for yovoice. yovoice renders every track with a General MIDI sound font and mixes tracks by role.

Return ONLY one JSON object, no prose, no Markdown:
{"title":"Night Drive","tempo":100,"timeSignature":[4,4],"key":"D minor","sections":[{"name":"Intro","start":1,"end":4}],
 "tracks":[{"id":"piano","name":"Piano","role":"piano","program":0,"reverb":0.3,"pan":-0.1,
   "humanize":{"velocity":8,"timingMs":6},"dynamics":[{"start":1,"end":4,"from":0.5,"to":1}],
   "notes":[[1,1,62,1,80],[1,2,65,0.5,72]],"repeats":[{"from":1,"to":4,"at":[9,13]}]}]}

Rules:
- notes are arrays [bar, beat, pitch, length, velocity]. bar and beat start at 1; beat may be fractional (1.5 = the off-beat of beat 1); beats are quarter notes (6/8 has 3 beats per bar); length is in beats; pitch is MIDI (60 = C4); velocity 1-127.
- program is a General MIDI number: 0 piano, 4 electric piano, 24 nylon guitar, 32 acoustic bass, 33 finger bass, 40 violin, 41 viola, 42 cello, 46 harp, 48 string ensemble, 52 choir, 56 trumpet, 60 french horn, 71 clarinet, 73 flute, 81 saw lead, 88 new age pad, 89 warm pad, 95 sweep pad. For drums set "drums":true and use pitches 36 kick, 38 snare, 37 side stick, 42 closed hat, 46 open hat, 49 crash, 51 ride.
- role is one of melody, piano, strings, bass, drums, pad, arp, other; it sets the default level (melody and piano in front, pad and arp behind). Do not set level unless needed.
- One instrument per track, 1-8 tracks, unique ids. Honor a requested instrument list and solo passages; do not invent extra instruments for a constrained ensemble. Optional: pan -1..1, reverb 0..1, humanize {velocity 0-40, timingMs 0-50}, dynamics ramps {start bar, end bar, from 0-1, to 0-1}. Sections may set their own tempo for a slowdown.
- Plan a chord progression first, then write bass, pad, melody and rhythm parts that follow it. Shape the piece with sections: introduce and drop parts, use dynamics for builds.
- Keep it compact: write at most 1500 notes in total. Sustained pads and long notes are cheaper than many short ones.
- repeats copy a track's own notes from bars from..to so the copy starts at each bar listed in at. Write a loop, groove or recurring phrase once and repeat it instead of writing it again; this is how long pieces stay within the note budget. Notes written directly in the target bars are kept too.
- Seconds = bars x beats per bar x 60 / tempo. Match the requested length within about 10%%, and never exceed %d seconds.
- title is a short name for the piece, at most 20 characters.
- Write the title, section and track names in %s.`

// ComposedScore 是 AI 写谱的结果：乐谱和顺带起的曲名，曲名只在作品尚未命名时由界面采用。
type ComposedScore struct {
	schema.Score
	Title string `json:"title,omitempty"`
}

// decodeLegacyScore 把独立智能编曲的 JSON 乐谱转换为官方新版的乐谱结构。
func decodeLegacyScore(text string) (ComposedScore, error) {
	raw := llm.ExtractJSON(text)
	if raw == "" {
		return ComposedScore{}, errors.New("no JSON object found")
	}
	var value struct {
		scoreInput
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return ComposedScore{}, fmt.Errorf("invalid JSON: %w", err)
	}
	score, err := buildScore(value.scoreInput)
	if err != nil {
		return ComposedScore{}, err
	}
	return ComposedScore{Score: score, Title: truncateRunes(strings.TrimSpace(value.Title), 120)}, nil
}

func (in ScoreBrief) valid() bool {
	brief := strings.TrimSpace(in.Brief)
	return brief != "" && schema.TextLen(brief) <= 30000 && in.Seconds >= 0 && in.Seconds <= int(schema.ScoreMaxSeconds)
}

// StartScore 在后台写谱，立即返回任务。
func (w *Workbench) StartScore(in ScoreBrief) (schema.AITask, error) {
	if !in.valid() {
		return schema.AITask{}, msg.Err(msg.ErrRequestInvalid, nil)
	}
	return w.startAI("score", in.ProjectID, strings.TrimSpace(in.Brief), func(ctx context.Context) (any, error) { return w.ComposeScore(ctx, in) })
}

// ComposeScore 让模型写出乐谱和曲名并校验；不合规时把问题发回让模型修正一次。
func (w *Workbench) ComposeScore(ctx context.Context, in ScoreBrief) (ComposedScore, error) {
	if !in.valid() {
		return ComposedScore{}, msg.Err(msg.ErrRequestInvalid, nil)
	}
	if in.Arrangement != nil && len(in.Arrangement.Sections) > 0 && len(in.Arrangement.Sections[0].Phrases) > 0 && len(in.Arrangement.Sections[0].Phrases[0].Parts) > 0 && in.Arrangement.Sections[0].Phrases[0].Parts[0].ID != "" {
		return w.composePlannedPhrases(ctx, in)
	}
	brief := strings.TrimSpace(in.Brief)
	if in.Arrangement != nil {
		if issue := schema.ArrangementIssue(in.Arrangement); issue != "" || !in.Arrangement.Approved {
			return ComposedScore{}, fmt.Errorf("编曲计划尚未审核或无效：%s", issue)
		}
		var required []string
		for _, section := range in.Arrangement.Sections {
			for _, phrase := range section.Phrases {
				for _, part := range phrase.Parts {
					required = append(required, fmt.Sprintf("bars %d-%d: track instrument %q, role %q, technique %q must contain notes; attach planned partId %q in the track partIds array", phrase.StartBar, phrase.EndBar, part.Instrument, part.Role, part.Technique, part.ID))
				}
			}
		}
		if len(required) > 120 {
			return ComposedScore{}, fmt.Errorf("编曲包含 %d 个分句声部，超过单次 AI 写谱可可靠处理的 120 个；请拆分后写谱", len(required))
		}
		if len(required) > schema.ScoreMaxTracks {
			brief += fmt.Sprintf("\nOne track may cover several phrase parts of the same instrument and role; put every planned partId in that track's partIds array. Maximum tracks: %d.", schema.ScoreMaxTracks)
		}
		brief += "\nMANDATORY ARRANGEMENT CONTRACT: Keep tempo and meter exactly as planned. Every listed instrument and role needs playable notes in every listed bar range; do not invent extra tracks or leave any listed part empty. Track names must match the instrument names exactly. Every planned partId must be assigned to exactly one track through partIds; a track may list multiple partIds for different phrases. Never invent a partId.\n" + strings.Join(required, "\n")
	}
	ask := "Brief: " + brief
	if in.Seconds > 0 {
		ask += fmt.Sprintf("\nTarget length: about %d seconds.", in.Seconds)
	}
	system := fmt.Sprintf(scorePrompt, int(schema.ScoreMaxSeconds), scoreLanguage(in.Locale))
	if in.Arrangement != nil {
		system += fmt.Sprintf("\nFor the mandatory arrangement contract, the 1-8 track guideline is replaced by the score schema limit of %d tracks. Use only the listed instrument names and role values. Each track may include partIds: [\"exact-planned-part-id\", ...]. Validate that every planned phrase has corresponding notes and the correct partIds before returning JSON.", schema.ScoreMaxTracks)
	}
	request := llm.Request{System: system, Messages: []llm.Message{{Role: "user", Content: ask}}, MaxTokens: 16000, Temperature: 0.7}
	var issue string
	for attempt := 0; attempt < 2; attempt++ {
		result, e := w.complete(ctx, request)
		if e != nil {
			return ComposedScore{}, e
		}
		var s ComposedScore
		if result.Truncated {
			e = errors.New("output was cut off by the length limit")
		} else {
			s, e = decodeLegacyScore(result.Text)
		}
		if e == nil {
			if in.Arrangement != nil {
				_, e = arrangementUnits(*in.Arrangement, s.Score)
			}
			if e == nil {
				return s, nil
			}
		}
		issue = e.Error()
		w.logAIOutput("score", issue, result.Text)
		if attempt == 0 {
			w.setAIStage("repairing")
		}
		fix := "Your score is invalid: " + issue + ". Return the complete corrected JSON only."
		if result.Truncated {
			fix = "Your output was cut off. Write a shorter score with fewer notes (use longer notes and fewer bars) and return complete JSON only."
		}
		request.Messages = append(request.Messages, llm.Message{Role: "assistant", Content: result.Text}, llm.Message{Role: "user", Content: fix})
	}
	return ComposedScore{}, msg.Err(msg.ErrAIResultInvalid, msg.Params{"detail": issue})
}

// LyricsBrief 是“AI 写歌词”的输入。
type LyricsBrief struct {
	ProjectID    string          `json:"projectId"`
	Brief        string          `json:"brief"`
	Instrumental bool            `json:"instrumental"`
	Language     string          `json:"language"`
	Locale       schema.UiLocale `json:"locale"`
}

// Lyrics 是写给音乐作品的风格描述与带段落标记的歌词。
type Lyrics struct {
	Title    string `json:"title"`
	Style    string `json:"style"`
	Lyrics   string `json:"lyrics"`
	Language string `json:"language"`
}

const lyricsPrompt = `You write songs for ACE-Step, a music generation model. Return ONLY one JSON object, no prose, no Markdown:
{"title":"...","style":"...","lyrics":"...","language":"zh"}

- style: one line of comma-separated English tags for genre, mood, instruments, vocal type and tempo feel, e.g. "city pop, nostalgic, warm synths, slap bass, female vocals, 100 bpm". At most 300 characters.
- lyrics: lines grouped under section markers on their own line: [intro], [verse], [pre-chorus], [chorus], [bridge], [outro]. Keep it singable: short lines, a memorable repeated chorus. At most 2500 characters.%s
- language: the ISO code of the sung language, one of zh, en, yue, ja, ko, es, fr, de, pt, ru, it.
- title in the sung language.`

func (in LyricsBrief) valid() bool {
	brief := strings.TrimSpace(in.Brief)
	return brief != "" && schema.TextLen(brief) <= 2000
}

// StartLyrics 在后台写歌词，立即返回任务。
func (w *Workbench) StartLyrics(in LyricsBrief) (schema.AITask, error) {
	if !in.valid() {
		return schema.AITask{}, msg.Err(msg.ErrRequestInvalid, nil)
	}
	return w.startAI("lyrics", in.ProjectID, strings.TrimSpace(in.Brief), func(ctx context.Context) (any, error) { return w.WriteLyrics(ctx, in) })
}

// WriteLyrics 让模型写出风格描述与歌词；纯音乐只写风格与段落结构。
func (w *Workbench) WriteLyrics(ctx context.Context, in LyricsBrief) (Lyrics, error) {
	if !in.valid() {
		return Lyrics{}, msg.Err(msg.ErrRequestInvalid, nil)
	}
	brief := strings.TrimSpace(in.Brief)
	extra := ""
	if in.Instrumental {
		extra = "\n- This is an instrumental: leave lyrics empty except section markers describing the arrangement, e.g. [intro]\\n[verse]\\n[chorus]."
	}
	ask := "Song idea: " + brief
	if in.Language != "" && slices.Contains(schema.MusicLanguages, in.Language) {
		ask += "\nSung language: " + in.Language
	}
	result, e := w.complete(ctx, llm.Request{System: fmt.Sprintf(lyricsPrompt, extra), Messages: []llm.Message{{Role: "user", Content: ask}}, MaxTokens: 4000, Temperature: 0.9})
	if e != nil {
		return Lyrics{}, e
	}
	var l Lyrics
	if e = json.Unmarshal([]byte(llm.ExtractJSON(result.Text)), &l); e != nil || strings.TrimSpace(l.Style) == "" {
		w.logAIOutput("lyrics", "missing style", result.Text)
		return Lyrics{}, msg.Err(msg.ErrAIResultInvalid, msg.Params{"detail": "missing style"})
	}
	l.Title = truncateRunes(strings.TrimSpace(l.Title), 120)
	l.Style = truncateRunes(strings.TrimSpace(l.Style), 512)
	l.Lyrics = truncateRunes(strings.TrimSpace(l.Lyrics), 4000)
	if !slices.Contains(schema.MusicLanguages, l.Language) {
		l.Language = ""
	}
	return l, nil
}
