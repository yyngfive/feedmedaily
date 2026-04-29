# SciRSSAgent 工程工具链方案

## Summary
推荐把项目做成一个小型 monorepo：**Python 后端管线 + Vite/React/Tailwind 前端报告 + Git 版本管理**。  
环境管理优先用 **uv**，mamba 作为备用，不作为主路径。

本机已确认可用：`git 2.44.0`、`uv 0.9.27`、`mamba 2.2.0`、`conda 25.1.1`、`node v25.5.0`、`pnpm 10.33.0`。  
注意：当前 Node 是 25，开发大概率可用，但建议工程目标锁定到 Node 22 LTS；Vite 官方当前要求 Node `20.19+` 或 `22.12+`。

参考：Vite [Getting Started](https://vite.dev/guide/)、Tailwind [Vite 安装](https://tailwindcss.com/docs/installation/using-vite)、uv [project docs](https://docs.astral.sh/uv/guides/projects/)、mamba [user guide](https://mamba.readthedocs.io/en/stable/user_guide/mamba.html)。

## Toolchain Decisions
- 版本管理：
  - 使用 Git。
  - 初始化仓库后第一批提交只包含工程骨架、配置、`RSS.txt`、示例配置和说明文档。
  - `.gitignore` 排除 `.venv/`、`node_modules/`、`.env`、`data/*.sqlite`、`logs/`、`reports/`、前端 build 产物。
- Python 环境：
  - 主方案使用 `uv`。
  - 使用 `pyproject.toml` 管理依赖，`uv.lock` 提交进 Git。
  - 使用 `.python-version` 锁定 Python 版本，建议 Python `3.12`。
  - 常用命令：
    - `uv sync`
    - `uv run scirssagent run --once`
    - `uv run pytest`
    - `uv run ruff check`
- mamba 评估：
  - 不作为第一版主环境。
  - 适合后期需要复杂科学计算依赖、Jupyter、GPU、本地 embedding 模型时使用。
  - 当前项目主要是 RSS、HTTP、SQLite、LLM API、JSON 和报告生成，用 uv 更轻、更可复现。
- 前端工具链：
  - 使用 `Vite + React + TypeScript + Tailwind CSS`。
  - 包管理建议用 `pnpm`。
  - Tailwind 使用当前官方推荐的 Vite plugin 方式：`tailwindcss` + `@tailwindcss/vite`。
  - 前端只做本地静态 dashboard，不需要 Next.js、后端服务或登录系统。
- Node 策略：
  - 工程文档声明目标 Node：`22 LTS`。
  - 当前本机 Node 25 可先用于开发；若 Vite 或依赖出现兼容警告，再切换到 Node 22。
  - `package.json` 加 `engines.node`：`>=22.12 <23 || >=20.19 <21`，保持稳定。

## Architecture
- 推荐目录结构：
  - `src/scirssagent/`：Python 核心逻辑。
  - `tests/`：Python 测试。
  - `web/`：Vite/React/Tailwind 前端。
  - `RSS.txt`：第一版 feed 输入。
  - `data/`：本地 SQLite，默认不入 Git。
  - `reports/`：生成的静态报告，默认不入 Git。
  - `logs/`：运行日志，默认不入 Git。
- Python 后端负责：
  - 读取 `RSS.txt`。
  - 抓取 RSS。
  - 补充 DOI、摘要和元数据。
  - 去重并写入 SQLite。
  - 调用 LLM 输出固定 JSON 分类。
  - 导出前端可读的 `reports/data/latest.json` 和按日期归档 JSON。
- React 前端负责：
  - 读取静态 JSON。
  - 展示 direct / indirect / unrelated。
  - 支持标签筛选、期刊筛选、搜索标题、摘要折叠。
  - 单独突出 `proximity_labeling` 标签。
- 报告生成方式：
  - 第一版构建为静态页面。
  - Python 每次运行后更新 JSON，并复制或引用前端 build 产物。
  - 打开 `reports/latest/index.html` 即可查看。

## Classification Update
- `direct` 增加 proximity labeling 方法学：
  - BioID、APEX、TurboID、miniTurbo、HRP 等邻近标记体系开发。
  - 邻近标记酶改造。
  - 底物、探针、标记反应、时空分辨标记策略优化。
- 普通 proximity labeling 生物学应用归入 `indirect`，除非文章本身有明显方法学创新。
- 前端标签中固定保留 `proximity_labeling`，方便单独筛选。

## Test Plan
- Python：
  - RSS 解析测试。
  - DOI/URL/title 去重测试。
  - metadata API 失败时的降级测试。
  - LLM JSON schema 校验测试。
  - proximity labeling 分类 prompt 测试样例。
- 前端：
  - 用 mock `latest.json` 测试 direct/indirect/unrelated 三组渲染。
  - 测试搜索、标签筛选、摘要折叠。
  - 测试无数据、API 失败记录、长标题、无摘要文章。
- 集成：
  - `uv run scirssagent run --once` 后生成 SQLite 和 JSON。
  - `pnpm --dir web build` 后能打开本地报告。
  - 重复运行不产生重复文献。
- 自动化：
  - Windows Task Scheduler 每天 10:00 运行 `uv run scirssagent run --once`。
  - 开启错过任务后补跑。
  - 日志记录到 `logs/YYYY-MM-DD.log`。
