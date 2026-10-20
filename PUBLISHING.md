# 源码发布方式

本分支为产品源码快照。原始构建提交为 `efa2d26f64d4037ed6b7c4dcf3930d3716b7e8e0`，其历史完整合入官方 v0.3.1。

当前 GitHub 登录凭证缺少 workflow 权限，因此此可浏览快照未启用 `.github/workflows` 自动化配置；完整配置位于 Release 的 `yovoice-v0.3.1-preview.3-source.zip`。完整官方历史与整合提交保存在同一 Release 的 `source.bundle`，可用 `git clone <bundle文件名> <目录>` 恢复。

安装包由原始构建提交生成，程序产品版本包含该提交摘要。公开快照的提交仅用于浏览与发布定位，内容差异为此说明文件及未启用的 CI 配置。以后启用远端 CI 需要为 GitHub 登录补充 workflow 权限。

用户作品、数据库、API 凭证和本机审计资料不在发布内容中。
