# FeedMeDaily v0.3.3

## 更新

- 新增 onboarding 的 `Save Settings` 按钮，可以先保存 API key 和高级设置，再生成初始 Profile
- 修复 release 首次 onboarding 保存 API key 后，生成 Profile 仍读到空 profile key 的问题
- 改进受保护 RSS 的站点级验证会话和 WebView2 verifier 稳定性

## 已知问题

- 某些站点即使使用持久化 verifier profile，仍可能比系统浏览器更容易触发 Cloudflare 循环验证
- PNAS 订阅仍可能无法稳定获取 RSS
- Zotero 仓库文件夹过多时仍可能显示不全
