# Go 迁移后实施清单

## 已完成

### 1. 分类范围收敛与 paper-level topic tags 下线

状态：已在 `codex/tagging-and-thinking` 完成并合入 `main`

当前实现：

- [internal/classifier/classifier.go](/D:/Codes/Projects/SciRSSAgent/internal/classifier/classifier.go)
  - 收紧 `baseClassificationInstructions`，明确不能扩展用户兴趣范围
  - `batchClassificationPrompt()` 不再要求模型返回 paper-level `topic_tags`
  - `profilePromptPayload()` 不再把 `topic_taxonomy` 送进当前分类 prompt
  - `decodeClassification()` 统一返回空的 `topic_tags`
- [web/src/App.tsx](/D:/Codes/Projects/SciRSSAgent/web/src/App.tsx)
- [web/src/components/review/PaperCard.tsx](/D:/Codes/Projects/SciRSSAgent/web/src/components/review/PaperCard.tsx)
- [web/src/components/review/DetailPanel.tsx](/D:/Codes/Projects/SciRSSAgent/web/src/components/review/DetailPanel.tsx)
  - 评审 UI 已同步去掉对 paper-level topic tags 的依赖

后续说明：

- 如果未来要恢复或重做 paper-level tags，需要作为一个新的产品设计和数据结构议题重开，不应沿用这份旧方案。

### 2. Thinking 超时自动降级

状态：后端主体已完成并合入 `main`

当前实现：

- [internal/classifier/classifier.go](/D:/Codes/Projects/SciRSSAgent/internal/classifier/classifier.go)
  - 分类请求在 provider timeout / gateway / reasoning 相关错误时，会自动重试一次 `thinking=disabled`
- [internal/profile/generation.go](/D:/Codes/Projects/SciRSSAgent/internal/profile/generation.go)
  - profile generation 请求也支持同样的 fallback
- [internal/config/config.go](/D:/Codes/Projects/SciRSSAgent/internal/config/config.go)
  - 已提供 classifier/profile 两个 role 的 thinking 配置项
- [internal/classifier/classifier_test.go](/D:/Codes/Projects/SciRSSAgent/internal/classifier/classifier_test.go)
  - 已覆盖首次 thinking 失败后自动降级的测试

剩余收尾：

- [web/src/app/messages.ts](/D:/Codes/Projects/SciRSSAgent/web/src/app/messages.ts)
  - 可选：补一条更明确的 UI 提示，说明本次请求已自动退回 non-thinking 模式
- [web/src/components/admin/SettingsConfigEditor.tsx](/D:/Codes/Projects/SciRSSAgent/web/src/components/admin/SettingsConfigEditor.tsx)
  - 可选：补文案说明 thinking 可能更慢，但系统会自动做一次降级重试

## 待实施

### 1. Profile 可编辑

#### 目标

允许用户直接编辑当前 profile，而不是只能走 proposal apply。

#### 后端

- [internal/api/server.go](/D:/Codes/Projects/SciRSSAgent/internal/api/server.go)
  - 增加 `PUT /api/profile/current`
- [internal/profile/profile.go](/D:/Codes/Projects/SciRSSAgent/internal/profile/profile.go)
  - 复用现有 validate / write 逻辑
  - 新增“保留 `meta.created_at`、更新 `meta.updated_at`、`version +1`”的写入辅助函数

#### 前端

- [web/src/reportData.ts](/D:/Codes/Projects/SciRSSAgent/web/src/reportData.ts)
  - 增加 `saveCurrentProfile()`
- [web/src/components/profile/ProfileRulesDocument.tsx](/D:/Codes/Projects/SciRSSAgent/web/src/components/profile/ProfileRulesDocument.tsx)
  - 从只读文档升级为可编辑文档
- [web/src/components/admin/AdminPanel.tsx](/D:/Codes/Projects/SciRSSAgent/web/src/components/admin/AdminPanel.tsx)
  - 增加编辑入口和保存按钮
- [web/src/App.tsx](/D:/Codes/Projects/SciRSSAgent/web/src/App.tsx)
  - 接保存逻辑和刷新逻辑

#### 测试

- [internal/profile/profile_test.go](/D:/Codes/Projects/SciRSSAgent/internal/profile/profile_test.go)
  - 校验编辑后 `version/update_at` 变化
- [internal/api/server_test.go](/D:/Codes/Projects/SciRSSAgent/internal/api/server_test.go)
  - 校验 `PUT /api/profile/current`

### 2. AI 简化 Profile

#### 目标

在不改 research scope 的前提下，压缩冗余 rules 和 tag。

#### 后端

- [internal/profile/generation.go](/D:/Codes/Projects/SciRSSAgent/internal/profile/generation.go)
  - 新增 `GenerateProfileSimplificationProposal()`
  - prompt 明确要求：
    - 去重
    - 合并相近规则
    - 不扩大 scope
    - 不删除核心 topic
- [internal/jobs/profile.go](/D:/Codes/Projects/SciRSSAgent/internal/jobs/profile.go)
  - 增加新的 simplify job
- [internal/api/server.go](/D:/Codes/Projects/SciRSSAgent/internal/api/server.go)
  - 增加新接口，例如 `POST /api/profile/proposals/simplify`

#### 前端

- [web/src/reportData.ts](/D:/Codes/Projects/SciRSSAgent/web/src/reportData.ts)
  - 增加 simplify proposal 调用
- [web/src/components/admin/AdminPanel.tsx](/D:/Codes/Projects/SciRSSAgent/web/src/components/admin/AdminPanel.tsx)
  - 增加 `Simplify profile` 按钮
- [web/src/App.tsx](/D:/Codes/Projects/SciRSSAgent/web/src/App.tsx)
  - 接 job 注册和 proposal 刷新

#### 测试

- [internal/jobs/profile_test.go](/D:/Codes/Projects/SciRSSAgent/internal/jobs/profile_test.go)
- [internal/api/server_test.go](/D:/Codes/Projects/SciRSSAgent/internal/api/server_test.go)

### 3. Zotero 集合树修正

#### 目标

确保集合列表完整、层级路径正确、默认集合稳定。

#### 后端

- [internal/zotero/zotero.go](/D:/Codes/Projects/SciRSSAgent/internal/zotero/zotero.go)
  - 检查 `limit=1000` 是否需要分页
  - 检查 `parentCollection` 为空、缺失、异常 key 的处理
  - 检查 `PathLabel` 构造是否会断链
  - 必要时改成分页拉全量 collection
- [internal/zotero/zotero_test.go](/D:/Codes/Projects/SciRSSAgent/internal/zotero/zotero_test.go)
  - 增加嵌套多层、缺失父节点、默认集合、group library 用例

#### 前端

- [web/src/components/modals/ZoteroSaveModal.tsx](/D:/Codes/Projects/SciRSSAgent/web/src/components/modals/ZoteroSaveModal.tsx)
  - 保持平铺选择，但确认 label 用 `path_label`
- [web/src/types.ts](/D:/Codes/Projects/SciRSSAgent/web/src/types.ts)
  - 如后端返回结构变化则同步

### 4. 任务进度条

#### 目标

把现在的 job message 升级成阶段进度，而不是纯文字提示。

#### 后端

- [internal/jobs/run_once.go](/D:/Codes/Projects/SciRSSAgent/internal/jobs/run_once.go)
- [internal/jobs/reclassify.go](/D:/Codes/Projects/SciRSSAgent/internal/jobs/reclassify.go)
- [internal/jobs/profile.go](/D:/Codes/Projects/SciRSSAgent/internal/jobs/profile.go)
  - 统一 progress key
- [internal/api/jobs.go](/D:/Codes/Projects/SciRSSAgent/internal/api/jobs.go)
  - 如需要，给 job 增加 `step`, `step_index`, `step_total`

#### 前端

- [web/src/app/messages.ts](/D:/Codes/Projects/SciRSSAgent/web/src/app/messages.ts)
- [web/src/components/common/AppStatusBar.tsx](/D:/Codes/Projects/SciRSSAgent/web/src/components/common/AppStatusBar.tsx)
- [web/src/components/common/StatusBanner.tsx](/D:/Codes/Projects/SciRSSAgent/web/src/components/common/StatusBanner.tsx)
- [web/src/App.tsx](/D:/Codes/Projects/SciRSSAgent/web/src/App.tsx)
  - 轮询 job 时把 message 映射成 stepper/progress UI

### 5. 筛选、排序、批量 read

#### 目标

提升 review 页高频操作效率。

#### 前端

- [web/src/App.tsx](/D:/Codes/Projects/SciRSSAgent/web/src/App.tsx)
  - 增加 `mark wrong` filter
  - `journal` 从单选变多选
  - 增加排序状态
  - 增加“当前结果全部 read”
- [web/src/components/review/FiltersSidebar.tsx](/D:/Codes/Projects/SciRSSAgent/web/src/components/review/FiltersSidebar.tsx)
  - 多选期刊
  - feedback filter
  - sort 控件
- [web/src/components/review/PaperListSection.tsx](/D:/Codes/Projects/SciRSSAgent/web/src/components/review/PaperListSection.tsx)
  - 批量 read 按钮
- [web/src/app/constants.ts](/D:/Codes/Projects/SciRSSAgent/web/src/app/constants.ts)
  - 增加 sort/filter 常量
- [web/src/app/utils.ts](/D:/Codes/Projects/SciRSSAgent/web/src/app/utils.ts)
  - 增加排序函数和新筛选函数

#### 后端

- [internal/api/server.go](/D:/Codes/Projects/SciRSSAgent/internal/api/server.go)
  - 新增批量 read API
- [internal/store/sqlite/store.go](/D:/Codes/Projects/SciRSSAgent/internal/store/sqlite/store.go)
  - 增加 bulk mark read

## 推荐执行顺序

1. `profile 可编辑`
2. `profile simplify proposal`
3. `zotero collection 修正`
4. `任务进度条`
5. `筛选/排序/批量 read`
6. `thinking fallback UI 提示收尾`

## 建议分支拆分

- `codex/profile-editing`
- `codex/profile-simplify`
- `codex/zotero-and-progress`
- `codex/review-filters`
- `codex/fallback-ui-messaging`
