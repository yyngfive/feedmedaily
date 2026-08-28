# FeedMeDaily

![FeedMeDaily banner](./assets/branding/feedmedaily-icon.svg)

FeedMeDaily 是一个面向科研 RSS 的本地论文筛选工具。它可以抓取期刊 RSS、按 Profile 规则调用 LLM 做相关性分类、提供本地 Web 界面进行阅读和反馈，并支持把选中的论文保存到 Zotero。

- 当前架构说明见 [ARCHITECTURE.md](./ARCHITECTURE.md)
- 中文维护与开发手册见 [docs/maintenance-development.zh-CN.md](./docs/maintenance-development.zh-CN.md)
- 本地 API 文档见 [docs/api.zh-CN.md](./docs/api.zh-CN.md)
- 版本更新记录见 [CHANGELOG.md](./CHANGELOG.md)
- 开源协议为 [MIT](./LICENSE)

## 快速开始

FeedMeDaily 当前优先提供 Windows 安装版。请从 GitHub Releases 下载最新版安装包：

- [FeedMeDaily Releases](https://github.com/yyngfive/feedmedaily/releases/latest)

安装完成后启动托盘程序，再从托盘打开 FeedMeDaily。首次使用需要填写兴趣描述、选择分类模型并录入对应 API key，然后生成初始 Profile；也可以先点击 `Save Settings` 单独保存模型和其他设置，再生成 Profile。正式界面的 `Settings → Model` 可以随时修改同一组模型配置。

在设置的Feed界面可以添加期刊的RSS订阅地址：参见[部分出版社期刊RSS订阅地址汇总](https://github.com/yyngfive/sci-rss-list)

说明：

- 分类模型目前固定为 `DeepSeek V4 Flash (deepseek-v4-flash)` 和 `GLM-5.3-Flash (glm-5.3-flash)`；配置 key 即启用对应模型，默认模型菜单只显示已有 key 的模型，每个 sync/reclassify job 只使用入队时的默认模型。

### DeepSeek 设置

1. 在 [DeepSeek Platform](https://platform.deepseek.com/) 注册并登录
2. 在 [API Keys](https://platform.deepseek.com/api_keys) 页面创建 API Key

默认分类配置使用 `deepseek-v4-flash`、`thinking=disabled` 和 batch size `5`。DeepSeek 分类和标题翻译固定关闭 thinking；GLM 分类和标题翻译固定使用 `thinking=enabled`、`reasoning_effort=low` 及确定性采样设置。分类请求保留简短 `reason`，不要求模型生成内部决策轨迹或阅读动作；阅读动作由应用根据相关性标签确定。Profile 生成仍独立使用 DeepSeek V4 Pro key。

连接测试会在后台发送一个最小 JSON 请求，会消耗少量对应提供商额度；测试 key 不会被保存。停用模型不会删除其 key，必须单独点击 `Clear key` 才会删除。

### Zotero 设置

- 个人库：`ZOTERO_LIBRARY_TYPE=user`，`ZOTERO_LIBRARY_ID` 填写 Zotero `userID`
- 群组库：`ZOTERO_LIBRARY_TYPE=group`，`ZOTERO_LIBRARY_ID` 填对应 `groupID`
- `ZOTERO_COLLECTION_KEY` 可留空，表示每次保存时在应用内选择 collection

相关文档：

- [Zotero Web API Basics](https://www.zotero.org/support/dev/web_api/v3/basics)
- [Zotero API Keys](https://www.zotero.org/settings/keys)

## Linux 用户

Linux 目前可以直接使用源码模式运行后端和 Web UI，但当前不提供托盘程序。推荐使用仓库里的辅助脚本。

- `feedmedailyd` 可能可以在 Linux 上直接运行
- `feedmedaily-tray` 当前只支持 Windows
- 对于需要Cloudflare人类验证的 RSS 地址，手动验证功能只支持 Windows，Linux使用会以Warning提示

### 源码模式运行

```bash
git clone git@github.com:yyngfive/feedmedaily.git
cd feedmedaily
cp .env.example .env
corepack pnpm --dir web install
corepack pnpm --dir web build
bash ./tools/feedmedaily.sh serve
```

常用命令：

```bash
bash ./tools/feedmedaily.sh open #打开Web UI
bash ./tools/feedmedaily.sh sync #更新文献列表
bash ./tools/feedmedaily.sh paths #输出日志地址
```

如果只是想直接启动后台服务，可以运行：

```bash
go run ./cmd/feedmedailyd --root . --host 127.0.0.1 --port 8000
```

### 定时任务

当前应用内定时更新功能依赖托盘程序，目前只支持Windows。Linux 上推荐使用 `cron` 调用辅助脚本：

Windows 托盘首次启用定时同步时默认使用本地时间 `12:30`；在中国标准时间下，它位于 DeepSeek 当前的午间空闲计费窗口。已保存的用户自定义时间不会被覆盖，其他时区用户应按北京时间峰谷窗口自行调整。

手动同步采用单任务保护：已有 sync 处于排队、执行或等待订阅源验证状态时，再次点击 Dashboard 的 Sync 或从托盘/API 触发同步只会复用现有任务，不会重复抓取和分类。

Settings → Dashboard 会显示每个 sync、reclassify、首次 Profile 生成和 Profile proposal job 的 DeepSeek token 用量与估算人民币费用，并保留最近 3 天的明细视图。费用使用 API 返回的缓存命中、缓存未命中和输出 token 乘以调用时价格快照计算；北京时间周一至周五 9:00–12:00、14:00–18:00 使用高峰价，其余时间（包括周末）使用空闲价。Settings → Model 可以手动调整 Flash/Pro 的峰谷单价；保存后的价格只用于之后启动的 job，不回算历史记录。未知模型、非官方 endpoint 或缺少精确 usage 时只显示 token，不推测金额。

```cron
0 8 * * * cd /path/to/feedmedaily && bash /path/to/feedmedaily/tools/feedmedaily.sh sync >> /path/to/feedmedaily/logs/cron.log 2>&1
```

## 开发者

### 开发环境

- Go
- Node.js
- pnpm
- Windows（目前推荐）

### 获取源码

```powershell
git clone git@github.com:yyngfive/feedmedaily.git
cd feedmedaily
```

### 配置并运行 source mode

先复制本地配置模板：

```powershell
Copy-Item .env.example .env
```

安装前端依赖并构建：

```powershell
pnpm --dir web install
pnpm --dir web build
```

启动托盘程序：

```powershell
go run .\cmd\feedmedaily-tray --root .
```

直接启动后端服务可以自动拉起托盘程序：

```powershell
go run .\cmd\feedmedailyd --root . --host 127.0.0.1 --port 8000
```

### 打包

构建安装版可执行文件与安装包：

```powershell
.\tools\build_release.ps1
```

只构建 release 目录、不生成安装包：

```powershell
.\tools\build_release.ps1 -SkipInstaller
```

