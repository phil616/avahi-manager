# 开发与验收记录

目标保持为 PLAN.md 的完整 V1：编码、测试、React UI、特权隔离和原生部署均须完成。本记录不替代或缩减 PLAN.md；基础模块通过测试不表示项目已经交付。

## 当前状态（2026-09-06）

仓库由只有 PLAN.md 的状态开始建立。当前已实现系统与配置基础模块、SQLite、认证、Helper RPC、应用 baseline、REST API、React 八页界面、内嵌资源和日志。没有留驻的测试 Web 服务或后台修改任务。

已经执行的验证：

- Go 全包测试及竞态检测，覆盖解析、输入攻击、原子替换、快照、保留数量、回滚、取消请求、崩溃恢复、SHA256 冲突、并发写入、符号链接、快照篡改。
- systemd mock 验证精确的 service/socket 参数、操作顺序、失败任务和超时；停止 service 失败仍尝试停止 socket。
- 私有 `dbus-daemon` 集成测试实际传输 D-Bus 消息，覆盖状态、发现、Resolve、移除、owner 丢失与取消退出。
- fsnotify 集成测试覆盖编辑器 rename 保存以及 services 目录重建。
- SQLite 验证 WAL、外键、迁移重开、权限、审计、外部修改检测、任务中断状态。
- 认证验证 Argon2id、单管理员、会话哈希/注销/过期、登录限流。
- 宿主机只读实测：Ubuntu 24.04，Avahi 0.8，service/socket active，D-Bus 状态 RUNNING；Netlink 返回 10 个接口。没有修改系统配置、控制服务或运行 APT。
- 本机实时发现实测返回 41 条服务记录，41 条全部 Resolve 成功。解析器 10 秒 fuzz 执行约 25 万次输入，无失败；当前 31 个顶层测试及其子测试通过，`make check`（race tests / vet / build）通过。
- 后续增加 Helper Unix socket 集成测试：配置读取、服务新增/编辑、hosts 保存、快照恢复、健康失败回滚、过期 revision、客户端/服务端 UID 拒绝、未知业务方法/字段及路径伪造拒绝。全包 race tests / vet / build 再次通过。Helper 测试仍只使用临时目录和注入运行时，未对宿主机执行特权修改。
- API 集成测试已覆盖真实 Unix Helper + SQLite：认证 Cookie、CSRF/Origin/Host、审计失败阻止副作用、配置/服务/hosts CRUD、重启保留外部漂移、显式接受磁盘；配置 SSE 测试实际观察 fsnotify 外部修改。
- baseline 测试验证保留策略保护、禁止误删、失败回滚保持旧 baseline，以及模拟“新 baseline 已写入但事务未提交”时的崩溃恢复。
- Chromium 浏览器端到端测试连接真实 Go API + SQLite + Unix Helper（仅系统动作使用测试运行时），覆盖登录、创建服务/TXT、添加主机、连续两次保存设置、发现空态、备份/审计、移动端无页面横向溢出、注销；最终直接读取文件确认 browser-second 主机名已写入。已查看桌面与移动端截图。

## 按计划阶段继续

| 阶段 | 当前证据 | 仍需完成 |
| --- | --- | --- |
| 1 检测 | detect CLI、本机只读检测、serve 初始化 baseline 和 SQL 哈希、重启保留漂移 | 无 Avahi 安装引导及安装后的初始化闭环 |
| 2 配置模型 | daemon / hosts / service 解析和语义测试 | 用发行版合法配置扩大兼容验证；配置错误在 GUI 可见 |
| 3 Safe Apply | Store、Helper 启动恢复、20 快照、保护 baseline、故障测试、SQL 索引、漂移 UI | 完整 `/etc/avahi/*` 辅助文件归档；所有变更文件的精确 diff（目前 Settings 只展示 daemon raw 对照）；更完整的异常目录恢复 |
| 4 systemd | 控制适配器、精确参数测试 | Helper/API 接线；隔离环境中真实 systemd/Avahi 写入验收 |
| 5 Avahi D-Bus | 状态、Resolve、事件缓存、私有总线/过期 Resolve 测试、SSE、React 筛选与详情 | 多协议/多接口浏览器验收、详情随缓存变更同步与事件高负载验证 |
| 6 Netlink | 实机接口读取、allow/deny 测试 | 将真实 daemon allow/deny/point-to-point/use-iff-running 设置接入接口状态与 GUI |
| 7 SQLite | bootstrap/snapshot/audit/auth 已接线并测试、Job 仓库 | APT 异步任务生命周期接线，审计/快照失败恢复继续审计 |
| 8 REST API | config/services/hosts/interfaces/status/discovery/logs/snapshots/audit/jobs/auth；认证强制、安全中间件、审计、SSE、集成测试 | updates 全接口；更严格的 JSON 重复键处理和全面边界测试 |
| 9 React GUI | 八页导航、结构化编辑器、发现筛选/详情、日志、维护/备份/审计、认证、Vite/go:embed、桌面与移动浏览器验证 | 更新/安装 UI、接口有效策略、完整 diff、剩余字段及异常状态的浏览器验收、状态告警完整性 |
| 10 运维 | journald 固定 argv 搜索/级别/时间/Follow、备份查看/恢复/删除和审计 UI | APT 确认/版本预览/snapshot/更新后检测、日志真实权限与 Follow 中断完整验证 |
| 11 Helper | 同一程序 helper 入口、socket activation、SO_PEERCRED 双向校验、有限配置/服务/快照/生命周期 RPC、启动恢复、单写者、集成与拒绝测试 | APT 固定业务 RPC；缺少 Avahi 时的安装初始化路径；生产目录/socket/备份权限硬化与打包验收；真实隔离 systemd 写入验证 |
| 12 部署 | Makefile、内嵌 UI、serve 非 root 检查、init-admin、默认本地监听与强制认证 | manager.toml、systemd service/helper/socket、Debian 包、安装卸载文档、权限硬化、真实系统端到端验收 |

## V1 完整验收清单

以下仅在真实应用路径和对应验证都存在后勾选，不以孤立单元测试替代完整能力。

- [ ] 1 Avahi 自动检测与初次接管
- [ ] 2 systemd 状态在 Dashboard/Maintenance 显示
- [ ] 3 Start / Stop / Restart 从认证 GUI 到 Helper
- [ ] 4 Enable / Disable 同时处理 service 和 socket
- [ ] 5 daemon 配置结构化 GUI，raw 只读
- [ ] 6 Interface 选择保存为 allow-interfaces
- [ ] 7 IPv4 / IPv6 设置
- [ ] 8 Hostname / Domain 设置
- [ ] 9 hosts 添加、编辑、删除
- [ ] 10 多服务 .service 创建、编辑、删除及安全 ID
- [ ] 11 TXT 键值编辑与 XML 安全编码
- [ ] 12 实时 D-Bus Discovery UI 与过滤
- [ ] 13 Resolve 详情（地址、TXT、时间、接口、协议）
- [ ] 14 journald 查看、搜索、时间范围、级别、实时 follow
- [ ] 15 初始/手动/变更/升级 Snapshot，查看、删除、恢复与保留策略
- [ ] 16 Safe Apply、动作判断、健康检查、失败回滚、崩溃恢复端到端
- [ ] 17 SQLite 管理器状态，原生配置始终为事实来源
- [ ] 18 所有修改请求（含失败）产生 Audit Log
- [ ] 19 APT 安装/更新由管理员明确操作与二次确认，只允许固定包/argv
- [ ] 20 React 八页 UI，Vite 构建嵌入单 Go binary，无 Node 运行依赖
- [ ] 21 内置单管理员认证、Argon2id、Cookie 安全、CSRF、限流

补充硬性验收：非 root HTTP + root 严格 Helper、默认 127.0.0.1:8053、非回环强制认证、fsnotify+SHA256 漂移提示/查看/重载/恢复、Avahi 停止或 Manager 卸载后配置仍完整、Debian/Ubuntu systemd 原生部署。禁止扩展 PLAN §34 排除的功能。

## 下一工作入口

接下来完成 APT 固定业务操作、缺少 Avahi 时的安装闭环、生产配置和原生打包；同时补全完整目录备份和仍缺的界面行为。`make check` 会先构建前端，再运行 race/vet/Go build；`make e2e` 使用已安装的 Playwright Chromium 执行隔离的真实浏览器测试。

浏览器保存必须提交读取时的 revision；服务修改触发 Reload，daemon/hosts 修改触发 Restart。HTTP 修改前后审计已实现；APT 任务待接。baseline.json 已实现，成功 Apply 和显式接受磁盘同步 baseline，崩溃恢复还原旧指针；ApplyResult.SnapshotID 仍表示修改前版本。生产备份父目录应防止非 root Web 进程替换 root Helper 的存储路径，须和 SQLite WAL 的目录写权限一起审计。当前目录信任、归档范围等问题还不能由浏览器测试证明已完成。

目前不存在后台待观察进程；验证命令均应以当前工具返回状态为准，不凭本记录重启或等待任务。工作目标仍 active，完整验收尚未达成。
