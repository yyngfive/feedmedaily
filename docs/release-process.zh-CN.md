# FeedMeDaily 固定发布流程

每次正式发布按本页顺序、在同一个 PowerShell 会话中执行命令。**GitHub Release 发布、DNS 切换、客户端更新检查均通过后，才算发布完成。** 发布记录留在本页末尾；不要记录凭据。

## 1. 准备发布内容

1. 设定本版版本号（下文用 `X.Y.Z` 表示）。检查 `web/package.json`、`CHANGELOG.md`、`AGENTS.md` 中的版本约定，确认本版变更相对于上一正式版。
2. 每次运行 `./tools/update_feed_catalog.ps1`，检查生成的目录和来源信息；有变化就与本版代码一起提交。正式打包时使用 `-SkipFeedCatalogUpdate`，避免 tag 之后再次拉取导致安装包内容偏离 tag。
3. 准备 `docs/release-notes-vX.Y.Z.md`：标题 `# FeedMeDaily vX.Y.Z`，`## 更新` 下写简短中文用户可见变更；只有当前仍存在且已确认的问题才写 `## 已知问题`。详细变更以 `CHANGELOG.md` 为准。
4. 检查安装脚本、图标和版本号；确认阿里云 DNS 凭据可从本机环境变量或忽略的 `.env` 读取。`update_release_dns.ps1 -DryRun` **不会**检验凭据或 DNS 权限。

## 2. 验证、提交和打 tag

```powershell
$releaseVersion = 'X.Y.Z'
$releaseTag = "v$releaseVersion"
corepack pnpm --dir web test
corepack pnpm --dir web build
go test ./cmd/... ./internal/...
git diff --check
git status --short
```

修复失败项后重新运行受影响的检查。检查待提交变更和暂存区，排除 API key、`.env`、本地数据库、日志及生成缓存。提交本版代码后，按顺序推送并确认工作树干净，再创建指向该提交的 tag：

```powershell
git push origin main
if (git status --porcelain) { throw 'Working tree is not clean.' }
git tag -a $releaseTag -m "Release FeedMeDaily $releaseTag"
git push origin $releaseTag
```

若使用发布分支，先完成合并及推送，再打 tag。tag 发布后不要移动；需要更正已发布代码时，另发补丁版本。

## 3. 从 tag 打包并校验

在干净的 tag 检出中运行；从这里直到 DNS 验证完成都保持在该 tag，避免随后从别的提交上传文件：

```powershell
git switch --detach $releaseTag
./tools/build_release.ps1 -SkipFeedCatalogUpdate
$installer = "dist/installer/FeedMeDaily-v$releaseVersion.exe"
if (-not (Test-Path $installer)) { throw 'Installer missing; check Inno Setup output.' }
$manifest = Get-Content dist/update.json -Raw | ConvertFrom-Json
if ($manifest.version -ne $releaseVersion) { throw 'update.json version mismatch.' }
Get-FileHash -Algorithm SHA256 $installer,dist/update.json
```

核对 `update.json` 的 `download_url` 指向本版安装包，`release_notes_url` 指向本版 Release；核对安装包版本，并在测试环境用安装包做一次安装或升级冒烟检查。记录两项文件的 SHA256。打包脚本若跳过 Inno Setup，安装包缺失，此步不得通过。

## 4. 创建草稿并正式发布 GitHub Release

先上传两项资产为草稿，用上一步的本地文件与 GitHub 显示的文件名、大小和 SHA256 核对；确认 release notes、tag 和安装包下载地址正确后再发布：

```powershell
gh release create $releaseTag $installer dist/update.json --draft --verify-tag --title "FeedMeDaily $releaseTag" --notes-file "docs/release-notes-$releaseTag.md"
gh release view $releaseTag --json tagName,isDraft,isPrerelease,assets,url
gh release edit $releaseTag --draft=false --latest
gh release view $releaseTag --json tagName,isDraft,isPrerelease,assets,url
```

发布后确认 `isDraft=false`、`isPrerelease=false`，安装包和 `update.json` 都能从公开 Release 获取，SHA256 与本地一致。旧版客户端仍通过 Release 的 `update.json` 检查更新，因此不能漏传。

## 5. 切换 DNS 并验证客户端

**只有上一步全部通过后才切换 DNS**：

```powershell
./tools/update_release_dns.ps1 -Version $releaseVersion -DryRun
./tools/update_release_dns.ps1 -Version $releaseVersion
Resolve-DnsName feedmedaily-update.stassenger.top -Type TXT -Server dns31.hichina.com
Resolve-DnsName feedmedaily-update.stassenger.top -Type TXT -Server dns32.hichina.com
Resolve-DnsName feedmedaily-update.stassenger.top -Type TXT -Server 223.5.5.5
```

预演值及解析结果都应为 `version=X.Y.Z;url=https://github.com/yyngfive/feedmedaily/releases/tag/vX.Y.Z`，权威 DNS 的 TTL 为 600 秒。缓存解析器可能需等待旧记录的 TTL 到期。对运行中的客户端请求 `GET /api/app/update?force=1`：旧版应返回 `latest_version=X.Y.Z`、`has_update=true`；本版应返回 `latest_version=X.Y.Z`、`status=up_to_date`。记录 DNS 记录 ID 和检查结果。

如切换失败，发布状态保持“未完成”，修复后重试同一版本脚本（脚本可重复执行）；不要在未核验资产时把 DNS 指向新版本。若已切换后发现严重问题，可用同一脚本把 DNS 指回上一个**已发布且资产有效**的版本，并记录原因，再准备补丁版本。

## 6. 收尾与发布记录

先运行 `git switch main` 返回主分支。删除临时 `docs/release-notes-vX.Y.Z.md`；在 `CHANGELOG.md` 开始下一版 unreleased section，并更新 `AGENTS.md` 的最新已发布版本及下一版指引。提交、推送这些收尾改动，确认工作树干净。

每次在下方增加一条记录：发布日期和版本、tag 对应提交、Release 地址、安装包及 `update.json` 的 SHA256、DNS 切换结果及记录 ID、权威与公共 DNS 结果、客户端 API 检查、收尾提交。凭据和用户本地数据不得写入记录。

### v0.7.1 · 2026-09-29

- Tag 提交：`9f5ebf3be24ee7caec70cb4bce930b4a01cbc7b4`；Release：<https://github.com/yyngfive/feedmedaily/releases/tag/v0.7.1>。
- 安装包 SHA256：`70aa590612f66c5af5bd79fc7948ca674a8ecca18eb4f5ced6b3161927c29d8f`；`update.json` SHA256：`d86ec8321ee6cd9a93022ef99ee47a057ad7725e3a125888987a293fa6e8354a`。
- 首次发布遗漏 DNS，TXT 仍为 0.7.0；当天 22:46（北京时间）补更新至 `version=0.7.1;url=https://github.com/yyngfive/feedmedaily/releases/tag/v0.7.1`，记录 ID `2094020894707601408`。
- `dns31.hichina.com`、`dns32.hichina.com`、Google DNS 和阿里公共 DNS 均返回 0.7.1，TTL 为 600 秒；本版客户端的 `GET /api/app/update?force=1` 返回 `latest_version=0.7.1`、`status=up_to_date`。收尾提交：`333555d`；DNS 补记提交：`7b245e7`。
