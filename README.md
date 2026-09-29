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

安装完成页默认勾选“启动托盘并打开 Web UI”。之后双击桌面或开始菜单的 FeedMeDaily 快捷方式：托盘未运行时启动托盘，托盘已运行时打开 Web UI。首次使用需要填写兴趣描述、选择分类模型并录入对应 API key，然后生成初始 Profile；也可以先点击 `Save Settings` 单独保存模型和其他设置，再生成 Profile。正式界面的 `Settings → Model` 可以随时修改同一组模型配置。

在设置的Feed界面可以添加期刊的RSS订阅地址：参见[部分出版社期刊RSS订阅地址汇总](https://github.com/yyngfive/sci-rss-list)

说明：

- 分类和 Profile 角色共享同一份固定模型目录：`deepseek-flash`、`deepseek-v4-pro`、`glm-5.3-flash`、`glm-5.3`、`qwen3.8-flash`、`qwen3.8-max-0902`、`mimo-v2.6-flash`、`mimo-v2.6-pro`。两个角色分别选择默认模型，API key 按供应商共享；每个 sync/reclassify job 使用入队时的分类默认模型。旧 MiMo V2.5 配置会迁移到对应的 V2.6 模型。

### 模型设置

分类和 Profile 模型均使用各供应商官方的 OpenAI 兼容 API。在 `Settings → Model`（或首次引导）中录入对应 API Key 即可启用该供应商的模型；源码模式下也可以通过 `.env` 中的 `SCIRSS_DEEPSEEK_API_KEY`、`SCIRSS_GLM_API_KEY`、`QWEN_API_KEY`、`MIMO_API_KEY` 提供。各供应商 API Key 的获取方式：

模型 token 价格由应用内置并按 provider/时段自动选择，不在 Settings 或环境变量中保存，也不允许修改。历史 usage 记录会保留实际采用的价格快照。

#### DeepSeek V4.1 Flash 和 V4 Pro

1. 在 [DeepSeek Platform](https://platform.deepseek.com/) 注册并登录
2. 在 [API Keys](https://platform.deepseek.com/api_keys) 页面创建 API Key

#### GLM-5.3-Flash 和 GLM-5.3

1. 在 [智谱 BigModel 开放平台](https://open.bigmodel.cn/) 注册并登录
2. 在 [API Keys](https://bigmodel.cn/usercenter/proj-mgmt/apikeys) 页面创建 API Key

#### Qwen3.8-Flash 和 Qwen3.8-Max-0902

1. 在 [阿里云百炼控制台](https://bailian.console.aliyun.com/) 注册并登录
2. 按照[获取与配置 API Key](https://help.aliyun.com/zh/model-studio/get-api-key/)的指引创建 API Key；本应用固定使用中国大陆百炼端点，国际版 Key 无法使用

#### MiMo-V2.6-Flash 和 MiMo-V2.6-Pro

1. 在 [小米 MiMo API 开放平台](https://mimo.mi.com/) 注册并登录
2. 在控制台的 API Keys 页面申请按量付费 API Key；Token Plan 套餐专属 Key（`tp-` 开头）与本应用的按量付费端点不通用

## Zotero 设置

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

Windows 全新安装默认启用每天本地时间 `09:00` 的定时同步；可在 Settings → App → Scheduled sync 修改时间或关闭。已有设置中的启用状态和时间不会被覆盖。自动执行需要托盘程序正在运行。

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
