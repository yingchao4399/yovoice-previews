# Changelog

各版本的重要更新记录。日常改动记入 `Unreleased`；发布前合并重复项，整理为简短的功能、改进和修复说明。

## [Unreleased]

## [0.3.1-preview.5] - 2026-10-21

### English

- Review each generated AI candidate directly in Smart Arrangement with a seekable player, adoption, regeneration and custom feedback; generation continues while the user listens.
- Keep track dragging within existing lanes when edge scrolling is active, preserving grouped moves and locked tracks.
- Persist per-item regeneration requests and process them after the current item, retain completed assets, and reject stale requests after score changes. Continue missing sample preparation after a representative sound is confirmed.
- Add per-part AI generation settings and an arrangement-only duration contract, keeping ordinary song generation unchanged.
- Separate instrumental and wordless-vocal requests; preserve key aliases and reject unsupported modes instead of silently using C major.
- Retain original candidates and measured checks, require audition and explicit region fitting before adoption, and prepare exact-pitch AI sample mappings for score playback.
- Align single-note ABC duration with the requested sustain, preserve tempo and dynamics in sampler phrases, and invalidate relevant caches when timing changes.
- Write release rests into score conditions, detect still-active choir endings, preserve confirmed phrases while new candidates await review, and keep checking failures recoverable.
- Prevent overlapping original/fitted audition playback and explain unsupported score-conditioned generation before starting a task.
- Resume checks from intact saved AI originals after restart, and allow failed or damaged candidates to be prepared again instead of blocking on an existing file.

### 简体中文

- 智能编曲页逐项弹出 AI 音源试听确认卡，支持拖动试听、确认采用、重新生成与自定义意见；试听期间后续素材继续生成。
- 修复片段拖到音轨区边缘时自动滚动导致目标轨号越界、跨轨移动失效的问题，保留分组移动和锁轨保护。
- 单项重做请求持久保存，在正在生成的下一项完成后优先处理，保留已有成果；曲谱变更后拒绝执行旧请求，代表采样确认后继续准备所缺音色。
- 新增声部二级生成设置和编曲专用长度合同，普通歌曲生成继续使用原路径。
- 分开器乐与无词人声条件，正确归一调性别名；无法表示的调式明确提示，不再静默改为 C 大调。
- 保留原始候选及本地测量结果，经区段适配和试听确认后采用；为采样器建立精确音高的 AI 音色映射。
- 单音 ABC 与所需持续时长一致；采样演奏保留段落速度与力度变化，相关时间条件变化后旧候选正确失效。
- 谱条件明确写入收尾休止，检查合唱末尾是否仍在发声；新候选不遮蔽已确认版本，检查异常可恢复并保留原片。
- 原片与适配片互斥试听；不支持的谱条件生成组合在任务开始前说明原因与可选路径。
- 重启后从完整保存的 AI 原片继续检查；失败或损坏候选可重新备料，不再因已有文件而卡住。

## [0.3.1-preview.4] - 2026-10-21

### English

- Add quick history switching beside the full history page, with uniform collapsed rows and per-item or bulk expansion.
- Keep the music editor scrollable above the resizable audio panel; add hand-tool panning and horizontal scrolling to track views.
- Add persistent Ocho question cards with selectable and custom answers, deferred replies and original-session continuation.
- Introduce shared resource inventory queries covering individual samples, generated audio, band presets, music models and audited CC0 candidates.
- Reuse local library audio as editable timeline material while preserving the original; respect real/AI source constraints.
- Require a scoped confirmation for AI-requested CC0 downloads, show per-item and byte progress, and resume confirmed batches while retaining completed items.
- Unify target duration with AI automatic and custom values; add male/female singing guidance and previewed lyric generation or optimization with undo.
- Pick and preview library audio or individual samples as vocal/accompaniment references, import local references without speech resampling, and explicitly add accompaniment as editable material. Preserve source and license metadata when reusing audio; reuse suitable local AI samples and band combinations.

### 简体中文

- 历史详情页旁新增快捷版本切换；历史列表统一收起高度，支持单条和全部展开。
- 音乐正文可滚动到旋律谱，底部音频区可调高度；音轨视图新增手型平移和横向滚动。
- Ocho 新增持久问答卡，支持点选、自定义、稍后回答及原会话续接。
- 新增共用库存查询，覆盖单个采样、已生成音频、乐队组合、音乐模型与已审核 CC0 候选。
- 库内音频可复用为独立的可编辑时间线素材，保留原文件并遵守真实／AI 来源限制。
- AI 请求 CC0 音源时先展示固定清单供确认，提供逐项与字节进度；批量任务可恢复，失败保留已完成项目。
- 统一目标时长，支持 AI 自动与自定义；增加男／女声演唱方向、歌词生成与优化预览及撤销。
- 音色与配乐参考可搜索试听库内音频和单个采样，音乐上传保留原采样率与声道；参考可显式加入可编辑音轨。复用时保留来源与许可，并优先使用适用的本地 AI 单音采样及乐队组合。

## [0.3.1-preview.3] - 2026-10-20

### English

- Merge the official v0.3.1 release, including the macOS 26 title-bar startup fix, Windows scrolling fix and updated verification workflow, while retaining custom Smart Arrangement, Instrument Library and VST3 management.
- Prevent old score tracks remaining after creating a new work; add score-area resizing and collapse controls with a shared dock height budget.
- Separate generated AI audio and model catalogs, and include the official v0.3.1 Windows scrolling-mask fix.
- Keep AI-material playback and seeking available when moving between library lists, searches and details; retry failed media loads.
- Show the current storage location and individual file paths in instrument sample and AI-material details.
- Restore the Smart Arrangement target-duration choices in minutes and seconds, matching the existing Music interface.

### 简体中文

- 完整合入官方 v0.3.1，包括 macOS 26 标题栏启动修复、Windows 滚动修复和验证流程更新；保留定制智能编曲、音源库与乐器及 VST3 管理。
- 修复新建作品后残留旧曲谱轨道；曲谱区支持调整高度和收起，底部面板共同限制高度以保留作品信息。
- 区分 AI 生成音源与模型目录，并纳入官方 v0.3.1 的 Windows 滚动蒙版修复。
- 修复 AI 素材在音源库列表、搜索与详情切换后无法播放的问题，保留试听进度并支持失败后重试。
- 音源采样与 AI 素材详情新增当前存储位置及各文件的完整路径，可换行和选中复制。
- 修复智能编曲目标时长误显示内部翻译键，恢复与音乐界面一致的分秒选项。

## [0.3.1-preview.2] - 2026-10-20

### English

- Integrate Smart Arrangement, Instrument Library and VST3 management with the official v0.3.0 interface, preserving existing Speech, Story, Music and ordinary Score flows.
- Diagnose instrument identity, source files, articulation, velocity and pitch before binding or assembly. Support explicit substitutes, undoable source replacement, locked source choices and compatible legacy-project migration.
- Compose against phrase-specific source capabilities with server-owned part identities; retain validated phrase progress and persist preparation jobs, completed audio and retry state.
- Connect YuE2 melody conditions, ACE-Step free audio, real sample stems and saved VST3 presets to an editable timeline. Record quality checks, audition confirmation, model choices and plugin dependencies; preserve manual tracks and prior results on reassembly.
- Add reference-score analysis through SheetSage2, separate performance imports, shared Ocho arrangement tools and protection against incompatible official updates. Fix missing-source repair in both sample catalogs.
- Fix preparation progress snapshot races and occasional editor displacement after focus changes.

### 简体中文

- 在官方 v0.3.0 界面中整合智能编曲、音源库与乐器及 VST3 管理；保留既有语音、故事、音乐与普通编曲路径。
- 绑定与拼合前统一检查乐器身份、文件、奏法、力度层及音域；支持显式替代、可撤销换源、锁定选择和兼容旧工程的声部迁移。
- 写谱按乐句使用实际来源约束，由服务端管理声部身份；保存通过校验的乐句进度、持久备料任务、成功素材与恢复状态。
- 接通 YuE2 旋律条件、ACE-Step 自由音频、真实采样分轨和已保存 VST3 预设到可编辑时间线；记录质量检查、试听确认、模型选择和插件依赖，重新拼合保留手工轨道与历史结果。
- 增加 SheetSage2 参考谱分析和独立演奏素材导入，接通 Ocho 共用编曲接口，防止不兼容官方更新覆盖；修复两套采样目录的缺失文件修复逻辑。
- 修复主编辑区在切换焦点或关闭浮层后偶尔整体上移一小段的问题。

## [0.3.1] - 2026-10-19

### English

- Fix a crash on launch on macOS 26 caused by the sidebar button in the title bar.
- Fix long pages on Windows being covered by a large black block; main scroll areas now show a thin, light scrollbar.

### 简体中文

- 修复 macOS 26 上标题栏侧边栏按钮导致应用启动后闪退的问题。
- 修复 Windows 上内容超过一屏的页面被一大片黑色遮住的问题；主要滚动区域显示细的浅色滚动条。

## [0.3.0] - 2026-10-19

### English

- Meet Ocho, the built-in AI assistant (⌘J / Ctrl+J): create and edit projects, write lyrics and scores, pick voices, tune parameters and start generations by chatting. It sees your selection and recent results, remembers preferences, and asks before anything destructive.
- YuE2 songs can start from a melody score: write or extract the melody and chords, view them as staff notation, tracks or ABC, preview with a sound font (notes light up as they play), edit, undo, export `.abc`, then generate from the score. Adds instrumental songs, covers via SheetSage2, and song forms that vary from song to song.
- Novel import for story narration (TXT, Markdown, EPUB), SRT/WebVTT subtitle export, LRC lyrics export, and side-by-side comparison of song versions on the playback page.
- New settings pages: Resources to find and clean up unused audio, Logs with diagnostic bundles, separate Speech and Music model pages with one row per model and a precision switch, and ChatGPT (Codex) as an AI provider.
- The interface now speaks Traditional Chinese, Japanese, Korean, Spanish, French, German, Portuguese (Brazil), Russian, Italian and Vietnamese, with an opening animation, a feature tour and smoother motion throughout.
- New projects default to downloaded models; `yovoice serve` serves interactive API docs at `/docs/`; Score projects move under Music as "Score generation".

### 简体中文

- 新增 AI 助手 Ocho（⌘J / Ctrl+J）：用对话新建和修改作品、写词写谱、挑音色、调参数、发起生成；能看到你的选中内容和最近结果，记住偏好，删除等操作先确认再执行。
- YuE2 歌曲可以先写旋律谱：按歌词写谱或从歌曲提取，用五线谱、轨道或 ABC 查看，用音色库试听（谱面跟随高亮）、修改、撤回、导出 `.abc`，再按谱生成整首歌；新增纯音乐、SheetSage2 翻唱，写词时按曲风选择不同曲式。
- 故事配音支持导入小说（TXT、Markdown、EPUB）和导出 SRT/WebVTT 字幕；音乐播放页可导出 LRC 歌词，并在同一作品的各版本间对比试听。
- 设置新增“资源”页（查找并清理未使用的音频）、“日志”页（可导出诊断包），模型分为“语音模型”和“音乐模型”两页，每个模型一行、切换精度下载；AI 服务新增 ChatGPT (Codex)。
- 界面新增繁體中文、日本語、한국어、Español、Français、Deutsch、Português (Brasil)、Русский、Italiano 和 Tiếng Việt，加入开场动画、功能引导和更顺滑的动效。
- 新建作品默认使用已下载的模型；`yovoice serve` 在 `/docs/` 提供交互式 API 文档；「编曲」更名为「编曲生成」并入「音乐生成」。

## [0.2.1] - 2026-10-16

### English

- Add direct Kokoro-82M 1.1-zh downloads from Hugging Face and ModelScope, and simplify model management to downloads from the app or CLI.
- Add first-run preparation guidance, faster speech generation, batch reference-audio deletion, readable generated-audio filenames, and safer shared-file cleanup.
- Add a full music playback page with synchronized lyrics, vocal alignment and calibration, plus improved score limits, AI naming, and style editing.
- Move projects, voices, models and settings to transactional SQLite storage with backups, migration from `state.json`, storage relocation, recovery, and lifecycle cleanup.
- Improve desktop reliability across Windows and macOS, including service recovery, process cleanup, tray behavior, narrow-window layouts, localized Windows UI, signing checks, and automatic updates.
- Add Breeze TTS 2, text and subtitle file import, local MCP and API integration improvements, OrcaRouter, and clearer audio, migration, disk, and save-error handling.

### 简体中文

- 新增从 Hugging Face 和 ModelScope 直接下载 Kokoro-82M 1.1-zh，模型统一通过应用或 CLI 下载，并自动识别模型目录中的完整模型。
- 新增首次生成准备清单、快速语音合成、参考音频批量删除、可读的生成音频文件名和更安全的共享文件清理。
- 新增带歌词同步、人声定位与校准的音乐播放页，改进编曲时长限制、AI 自动命名和风格编辑。
- 作品、音色、模型和设置迁移到带事务与备份的 SQLite，支持从 `state.json` 迁移、存储位置迁移、故障恢复和生命周期清理。
- 提升 Windows 与 macOS 桌面可靠性，涵盖本地服务恢复、进程清理、托盘行为、窄窗口布局、Windows 界面本地化、签名校验和自动更新。
- 新增 Breeze TTS 2、文本与字幕文件导入，改进本机 MCP 与 API、OrcaRouter，以及音频、迁移、磁盘和保存失败提示。

## [0.2.0] - 2026-10-11

### English

- New Music projects: set the style with tags, write lyrics with section markers or go instrumental, and generate full songs with ACE-Step 1.5 Turbo / XL Turbo or YuE2 3B (vocal songs whose length follows the lyrics; CC BY-NC 4.0, non-commercial). Each take is kept as a version you can preview, compare, reuse and export. Models made of several files can now be downloaded.
- New Score projects: write notes per track by bar and beat, import MIDI, or let an agent compose, then render every track with a General MIDI sound font and mix by role. The track area spans the bottom of the window with sections on the timeline, zoom, scrolling and a playhead; export MIDI or score JSON. Choose MuseScore General, GeneralUser GS or SGM-V2.01. The CLI adds `yovoice score render|midi|from-midi` with stems, MCP adds `render_score`, and the Agent Skill adds a score guide.
- Optional AI providers (DeepSeek, Qwen, Zhipu, Kimi, Doubao, MiniMax, OpenAI, Anthropic, OpenRouter, coding plans, local and custom endpoints) write a whole score or song style, lyrics and title from one sentence, run in the background, and can switch models on the spot.
- A refreshed interface: ticket-style project lists with record and cassette covers, grouped settings, a separate sound font page, side panel toggles in the title bar (the right panel toggle only appears in a workspace), and hover hints only for truncated text or icon buttons.
- The audio.cpp engine is upgraded to v0.9.1 and updates separately from the app; new Windows CUDA 12.4 and 13.3 installers.
- Fixes for long AI requests timing out, saving on exit, Windows time-zone saves, timeline menus, deleting the last project, and clearer error messages and diagnostic logs.

### 简体中文

- 新增“音乐生成”：用标签定风格，写带段落标记的歌词或改为纯音乐，由 ACE-Step 1.5 Turbo / XL Turbo 或 YuE2 3B（带人声、长度随歌词，CC BY-NC 4.0 仅限非商业）生成整首歌曲；每次生成保留为版本，可试听、对比、复用和导出。支持下载由多个文件组成的模型。
- 新增“编曲”：按小节和拍写各声部音符，或导入 MIDI、让 Agent 写谱，用 General MIDI 音色库逐声部渲染并按角色混音。轨道区横跨窗口底部，段落标在时间轴上，可缩放、滚动并显示播放指针；可导出 MIDI 或乐谱 JSON。音色库可选 MuseScore General、GeneralUser GS 或 SGM-V2.01。CLI 新增 `yovoice score render|midi|from-midi`（含分轨），MCP 新增 `render_score`，Agent Skill 新增编曲参考。
- 可选接入 AI 服务（DeepSeek、通义千问、智谱、Kimi、豆包、MiniMax、OpenAI、Anthropic、OpenRouter、编程套餐、本地与自定义接口），一句话写出整份编曲或歌曲的风格、歌词和标题，在后台进行，可随时切换模型。
- 界面焕新：票根式作品列表配唱片与磁带封面，设置页分组，音色库单独成页，左右面板开关集成到标题栏（右侧面板开关只在工作区显示），悬浮提示只在文字被截断或图标按钮需要说明时出现。
- 推理内核升级到 audio.cpp v0.9.1，与应用分开更新；新增 Windows CUDA 12.4 与 13.3 安装包。
- 修复长 AI 请求超时、退出保存、Windows 时区作品保存、时间线菜单、删除最后一个作品等问题，错误提示和诊断日志更具体。

## [0.1.7] - 2026-10-05

### English

- Run a remote speech service with HTTP API and MCP support. Submit, query, cancel, and download generation tasks with authentication, retry deduplication, and configurable timeouts.
- Use the Agent Skill with either a local CLI or a remote service. User guides and CLI packages now include separate English and Chinese documentation.
- Fix Windows save-error reporting and allow a recovery backup when saving on exit fails. Improve diagnostic logs for saving and generation failures.

### 简体中文

- 新增 HTTP API 与 MCP 远程语音服务，支持任务提交、查询、取消和音频下载，提供认证、重试去重与可配置超时。
- Agent Skill 支持本地 CLI 和远程服务；用户指南与 CLI 安装包提供独立中英文文档。
- 修复 Windows 保存错误提示，退出保存失败时可备份作品；完善保存和生成失败的诊断日志。

## [0.1.6] - 2026-10-01

### Added

- 角色支持保存多种演绎方式，每种演绎可独立选择模型和参数，故事台词和语音项目可分别选用。

### Fixed

- 统一演绎选择框与新增、更多按钮的尺寸和对齐。
- 修复音轨面板调高区域遮挡播放按钮的问题，拖拽仅在顶部横线附近触发，横线贴近面板上边线外侧，并缩小播放控制图标。
- 移除应用图标底板边缘的光带，清理透明轮廓杂点，并校准 macOS 图标的主体边界和小尺寸留白。

## [0.1.5] - 2026-09-29

### Added

- 新增故事配音与语音生成两类作品，支持字幕导入、分段生成和可编辑音轨。
- 新增音轨裁剪、撤销与重做、混音及 WAV 导出，支持作品归档导入导出与状态恢复。
- 新增 Kokoro 官方 BF16 模型下载。
- 新增版本变更日志，发布时自动将对应版本的说明展示到 GitHub Release 页面。

### Changed

- 移除待处理台词批量生成，播放时正文始终自动跟随。

### Fixed

- 优化音轨缩放、滚动、片段拖动与磁吸裁剪交互，统一单人和多人播放指针样式。
- 修复旧版 WebKit 下拉浮层样式，通知展示不再挤动工作台布局。
- 改进 Windows 窗口尺寸适配与位置保存，以及 macOS 应用启动和图标边距。

## [0.1.4] - 2026-09-26

### Added

- 新增角色库，保存并复用音色与声音设置；声音库与创作流程共用编辑能力。
- 新增 Kokoro 官方 Q8 模型下载及 49 个内置音色，支持实验性多语言和中文版本导入。

### Changed

- 优化声音库、生成历史、参考音频片段与原文管理，统一搜索、头像、音频试听和弹窗交互。

### Fixed

- 修复 macOS 麦克风权限回调崩溃，兼容旧版 macOS 的下拉选择器。
- 修复 VoxCPM2 长参考音频的编码器容量问题。
- 修复服务退出时未等待清理完成的问题，并保留 Windows 界面兼容修复。

## [0.1.3] - 2026-09-19

### Changed

- 优化 Windows 窗口边框、滚动、原生菜单与应用对话框。
- 新增代理开关和设置自动保存，调整下拉菜单定位。

### Fixed

- 修复 Windows 下载归档目录路径处理，完善 .NET 8 构建兼容性。

## [0.1.2] - 2026-09-18

### Added

- 新增 OmniVoice 与 Qwen3-TTS 模型及对应的声音设计、克隆和生成参数。
- 新增英文界面，支持语言偏好及 macOS 菜单、提示本地化。

### Changed

- 整理偏好设置、模型目录操作与生成控制面板。

### Fixed

- 修复 macOS 自动更新时 `lipo` 架构校验参数顺序错误。

## [0.1.1] - 2026-09-16

### Added

- 新增 VoxCPM2 Q8 / BF16，支持声音设计、音色克隆和参考原文辅助克隆，App 与 CLI 均可使用。
- 新增声音描述、引导强度、推理步数和随机种子设置。

### Changed

- 优化模型管理布局与按钮，下载期间可移除其他模型的登记。
- 简化 `make app-run`，分离日常启动与安装包制作；采用完整生成后播放。

### Fixed

- 修复 macOS 麦克风权限归属，改用原生权限请求。

## [0.1.0] - 2026-09-16

### Added

- 首个公开版本，提供 macOS Apple Silicon 与 Windows 桌面安装包。
- 支持 IndexTTS 2.0 / 2.5 本地生成、模型下载及 GGUF 导入。
- 支持参考音色、参考演绎、情绪调节、文字引导和参考音频录制。
- 提供声音库、作品历史、版本管理与音频试听。
- 提供 macOS、Windows、Linux 独立 CLI 与 Agent Skill，内置精简 FFmpeg 处理常见音频格式。
