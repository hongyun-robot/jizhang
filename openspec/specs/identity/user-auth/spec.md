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
系统 SHALL 在 access token 过期时允许用 refresh token 换取新 token；refresh token MUST 存储于服务端 Redis，且新签发后同一旧 refresh token MUST 立即失效（轮换）。客户端收到受保护请求的 401 后 MUST 实际刷新或使用同一会话中已经完成刷新的新 token，MUST NOT 将被拒绝的旧 token 当作刷新结果。一个页面会话内针对同一旧 token 的并发请求 SHALL 共享一次刷新，每个原请求 MUST 最多重试一次；重试 SHALL 保留原请求内容与操作标识。403 MUST NOT 触发刷新。

#### Scenario: 用过期 access token 请求
- **WHEN** 客户端携带过期 access token 调用受保护接口
- **THEN** 接口返回 401，客户端自动用 refresh token 换新 token 后重试，用户无感知

#### Scenario: refresh token 失效
- **WHEN** refresh token 已过期或已被轮换作废
- **THEN** 刷新请求失败，客户端清理失效会话并跳转登录页，不循环刷新

#### Scenario: 并发请求同时过期
- **WHEN** 同一页面会话中的多个请求携带同一个过期 access token 返回 401
- **THEN** 只执行一次刷新，所有可重试请求使用新 token 且各自最多重试一次

#### Scenario: 刷新后迟到的旧请求
- **WHEN** 新 token 已生效后，另一个携带旧 token 的在途请求才返回 401
- **THEN** 该请求直接使用当前会话的新 token 重试，不再次轮换 refresh token

#### Scenario: 重试仍然未授权
- **WHEN** 原请求使用新 token 重试后仍返回 401
- **THEN** 客户端结束会话并展示登录入口，不发起第二次重试

#### Scenario: 资金请求的认证重试
- **WHEN** 带操作标识的资金请求因 token 过期被拒绝，刷新后重新发送
- **THEN** 重试保留相同操作标识与提交内容，不将认证重试解释为新的资金操作

#### Scenario: 权限不足
- **WHEN** 已登录用户请求无权限资源并收到 403
- **THEN** 展示权限错误且不调用刷新接口

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
系统 SHALL 支持登出，登出后该会话的 refresh token MUST 立即失效。客户端结束会话或切换登录账号时 MUST 清理前一会话的数据缓存、导航筛选记忆和未提交业务草稿；前一会话的迟到读取或刷新结果 MUST NOT 恢复已退出的客户端会话、写入新账号状态或展示前一账号的个人数据。外观主题选择 SHALL 保持既有持久化行为。

#### Scenario: 用户登出
- **WHEN** 用户点击登出
- **THEN** 服务端删除该 refresh token，此后无法再刷新，客户端清理该会话的业务状态并进入登录页

#### Scenario: 登出期间刷新返回
- **WHEN** token 刷新或会话恢复尚未结束时用户登出，随后旧请求返回成功
- **THEN** 客户端仍保持登出状态，旧结果不重新设置 token、用户或业务草稿

#### Scenario: 换号后的迟到读取
- **WHEN** 用户从账号甲切换到账号乙后，账号甲的个人负债请求才返回
- **THEN** 账号乙的页面与缓存不展示账号甲的个人负债结果

#### Scenario: 同一账号重新登录
- **WHEN** 用户退出后再次登录同一账号
- **THEN** 不自动恢复上一会话的未提交草稿或导航记忆，用户主动打开的站内链接仍可恢复其显式查询条件
