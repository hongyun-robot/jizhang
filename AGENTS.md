# 仓库贡献指南

- 本地开发启动本机的 MySQL 和 redis

## 项目结构与模块组织

- `backend/cmd/server/`：Go API 启动入口。`backend/internal/` 按处理器、服务、仓储、模型、中间件及基础设施分层；业务规则放在服务层，数据库访问放在仓储层。
- `backend/migrations/`：按编号排列的 SQL 迁移，成对提供升级与回滚文件，如 `000011_personal_liability_accounts.up.sql` 和 `.down.sql`。
- `frontend/src/`：React 页面、公共组件、API 客户端、Hooks、状态管理及样式；静态资源位于 `frontend/public/`，浏览器验收脚本位于 `frontend/e2e/`。
- `docker/` 与 `scripts/`：部署、备份及本地工具。`openspec/specs/` 保存功能规范，`openspec/changes/` 保存变更计划与归档；修改业务行为前先阅读相关规范。

## 构建、测试与本地开发

环境要求：Go 1.26.1+、Node 20+、Docker Compose。

- 仓库根目录：首次配置时执行 `Copy-Item .env.example .env`，再用 `docker compose up -d` 启动 MySQL 与 Redis；启动 API 前按 `README.md` 执行数据库迁移。
- 在 `backend/`：`go run ./cmd/server` 启动 API，端口为 8080；`go build ./cmd/server` 编译后端。
- 在 `backend/`：`go test ./...` 运行测试；`go vet ./...` 执行 Go 静态检查。
- 在 `frontend/`：执行 `npm install` 后，用 `npm run dev` 启动 Vite，端口为 5600，`/api` 请求代理到后端。
- 在 `frontend/`：`npm run build` 执行类型检查并构建生产资源；`npm run preview` 在本地预览构建结果。

## 代码风格与命名约定

Go 使用 `gofmt` 格式化，以制表符缩进；包名使用小写，文件名采用 `snake_case`，导出标识符采用首字母大写的驼峰命名。前端沿用两空格缩进、单引号及分号；组件和页面采用 `PascalCase`，Hooks 以 `use` 开头。保持 TypeScript 严格检查通过。当前未配置 ESLint 或 Prettier 命令。

## 测试规范

Go 测试与实现放在同一目录，文件名为 `*_test.go`，使用标准库 `testing`，测试函数以 `Test` 开头。浏览器验收使用 Playwright。当前未强制覆盖率门槛；涉及资金变动、权限、重试或回滚时，应覆盖对应行为。

MySQL 集成测试需设置 `VOUCHER_MYSQL_DSN` 或 `LIABILITY_MYSQL_DSN`，测试会创建独立数据库。浏览器验收按 `README.md` 启动专用测试服务，再在 `frontend/` 执行对应脚本，如 `node e2e/vouchers.mjs`。按需配置浏览器可执行文件路径；会写入数据的验收使用临时测试账本。

## 提交与拉取请求规范

历史提交通常采用 `type(scope): summary`，如 `feat(ui): ...`、`fix(e2e): ...`、`docs(openspec): ...`，摘要常用中文。每次提交聚焦一个目的。拉取请求应说明行为变化，关联相关问题或 OpenSpec 变更，列出验证结果及跳过的检查；界面修改附桌面和移动端截图，数据库修改说明迁移影响。

## 安全与配置

密钥保存在已被 Git 忽略的 `.env` 文件中。部署前替换开发凭据；数据库结构变更前备份财务数据，并记录回滚限制。
