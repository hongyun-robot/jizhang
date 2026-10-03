# identity/user-auth Specification

## Purpose

让用户安全地登录自用记账站点并维持会话有效，覆盖双 token 认证、刷新轮换、失败限流与停用账号的访问拒绝。

## Requirements

### Requirement: 用户名密码登录
系统 SHALL 支持用户名 + 密码登录，成功后签发短期 access token 与长期 refresh token；失败时 MUST 返回统一错误提示，不泄露账号是否存在。

#### Scenario: 登录成功
- **WHEN** 用户提交正确的用户名和密码
- **THEN** 系统返回 access token（约 30 分钟有效）与 refresh token（约 30 天有效），前端据此进入应用

#### Scenario: 凭据错误
- **WHEN** 用户提交错误的用户名或密码
- **THEN** 系统返回统一的"用户名或密码错误"提示

### Requirement: token 刷新轮换
系统 SHALL 在 access token 过期时允许用 refresh token 换取新 token；refresh token MUST 存储于服务端 Redis，且新签发后同一旧 refresh token MUST 立即失效（轮换）。

#### Scenario: 用过期 access token 请求
- **WHEN** 客户端携带过期 access token 调用受保护接口
- **THEN** 接口返回 401 并标记可刷新，客户端自动用 refresh token 换新 token 后重试，用户无感知

#### Scenario: refresh token 失效
- **WHEN** refresh token 已过期或已被轮换作废
- **THEN** 刷新请求失败，客户端跳转登录页

### Requirement: 登录失败限流
系统 SHALL 基于 Redis 对同一用户名与同一来源 IP 的连续登录失败计数，超过阈值（如 5 次）后在冷却期（如 10 分钟）内拒绝该用户的登录尝试。

#### Scenario: 暴力破解防护
- **WHEN** 同一用户名连续登录失败达到阈值
- **THEN** 冷却期内该用户的登录请求被直接拒绝并提示稍后再试

### Requirement: 停用账号拒绝访问
被管理员停用的账号 MUST 无法登录，其此前签发的 token 在服务端校验时 MUST 被拒绝。

#### Scenario: 停用后登录
- **WHEN** 停用账号提交正确凭据
- **THEN** 登录失败并提示账号已停用

#### Scenario: 停用后的在线会话
- **WHEN** 停用前已登录的会话继续携带有效 token 请求
- **THEN** 服务端校验时发现停用状态并返回 401，会话立即失效

### Requirement: 登出
系统 SHALL 支持登出，登出后该会话的 refresh token MUST 立即失效。

#### Scenario: 用户登出
- **WHEN** 用户点击登出
- **THEN** 服务端删除该 refresh token，此后无法再刷新
