# FeedMeDaily v0.3.2

## 更新

- 新增受保护 RSS 的浏览器 fallback，可以在系统浏览器完成人类验证后手动粘贴 XML 继续同步
- 修复 Windows 打开受保护 feed 链接时查询参数被截断的问题
- verifier 改为按站点持久化 WebView2 profile，提升 Cloudflare 验证后的复用成功率

## 已知问题

- 某些站点即使使用持久化 verifier profile，仍可能比系统浏览器更容易触发 Cloudflare 循环验证
- PNAS 订阅仍可能无法稳定获取 RSS
- Zotero 仓库文件夹过多时仍可能显示不全
