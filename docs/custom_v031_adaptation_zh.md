# 官方 v0.3.1 与智能编曲定制版适配

版本：`0.3.1-preview.3`。此分支是 yingchao4399 的定制发行，原作者为 [leemysw/yovoice](https://github.com/leemysw/yovoice)，不代表官方发行。

## 本次合并范围

以官方发布标签 `v0.3.1`（`27e052f10065dda9562fe58e4c9229581589d811`）为准，完整合入该标签的增量。未混入官方 main 分支上尚未发布的功能。

- macOS 26 标题栏侧边栏按钮使用固定尺寸容器，修复启动布局崩溃。
- Windows 关闭滚动边缘渐隐蒙版，保留细滚动条，避免长页面的黑色遮挡。
- 将细滚动条延伸到定制智能编曲和曲谱滚动区域。
- 同步官方日常测试与发布打包的分工、封存用例及全量浏览器运行入口。
- 保留智能编曲、音源库与乐器、来源替换、分轨拼合、VST3 管理和 Ocho 共用接口。
- 保留近期 AI 素材播放、采样位置、曲谱区高度与收起、新建作品状态隔离、分秒时长显示等修复。

本次主要调整官方增量的兼容性，不将既有的实验性编曲功能标为全部完成。

## 数据与安装

- 使用同一个 Windows 安装标识，不创建第二套 yovoice。
- 更新包不含音频内核，覆盖应用并保留已有 CPU/CUDA 内核；首次安装使用完整安装包。
- 数据库仍为定制结构版本 6，本次适配不增加数据库迁移或重写作品。
- 模型、用户作品、录音、数据库、API 凭证、插件和付费音色不随源码发布。
- 更新前保存作品、正常退出软件并备份数据。窗口右上角 × 会隐藏到托盘。
- 定制版继续拦截不兼容的官方自动覆盖更新，需使用适配后的定制包。

## 发布验证入口

```powershell
go test -race ./...
pnpm --dir web run lint
pnpm --dir web run build
$env:PW_ALL='1'
pnpm --dir web run test:all
dotnet run --project desktop/windows/ServiceChecks
dotnet run --project desktop/windows/UpdateChecks
python scripts/test-release-notes.py
python scripts/release-notes.py v0.3.1-preview.3
```

Windows 的 Go 竞态检查需要可用的 C 工具链。发布前运行全量浏览器检查，包括官方日常跳过的封存用例。具体执行结果和安装包 SHA-256 随 GitHub Release 提供；源码构建成功不能替代真实模型与插件的推理验收。

## 已知边界

- YuE2 输出不保证严格逐音符跟谱或精确秒数，生成后仍需检查、试听和对齐。
- VST3 兼容性取决于具体插件、架构及预设；发现插件不等于插件已通过实际演奏验证。Windows 宿主按当前实现打包，第三方插件需自行安装。
- 未下载的模型不算本机已验证；ACE-Step、SheetSage2 等需安装后再做实际验收。
- 本次没有新增音乐理解模型或云端听音 API。
- macOS 修复保留官方实现，由 macOS 测试流程验证；本次手动发布的安装文件面向 Windows x64。

## 源码和发布维护

源码位于 `intelligent-arrangement-v031` 分支，保留官方历史及独立的整合提交。仓库早期安装包和 main 分支内容保留。

Windows 包含独立 VST3 宿主及其源码和第三方许可；完整第三方信息见 [THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md)。模型和采样的许可分别核对，应用源码许可不替代模型或采样许可。

上游全平台发布工作流依赖原作者签名证书，仅在官方仓库自动执行。本定制仓库通过经过验证的 Windows 产物手动发布；普通分支的测试仍保留。Windows 发布环境显式安装 uv，用于构建 VST3 宿主。
