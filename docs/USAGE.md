# Avahi Manager 使用指南

本文档对应当前开发版本，介绍从构建到部署到启动的完整手动流程，不代表 PLAN.md 全部完成。APT 安装/更新、manager.toml、systemd 部署文件、Debian 包和完整目录备份尚未交付。建议先在可恢复的测试机联调，不要直接用于生产网络。

---

## 1. 环境要求

### 1.1 构建环境

| 依赖 | 最低版本 | 用途 |
| --- | --- | --- |
| Linux | 内核 3.x+ | 目标平台，不支持 macOS/Windows |
| Go | 1.26+ | 编译后端二进制 |
| Node.js | 24+ | 构建 React 前端（仅构建时需要，运行时不需要） |
| npm | 随 Node.js 24 | 安装前端依赖 |
| make | GNU Make | 执行构建命令 |

隔离 D-Bus 集成测试需要 `dbus-daemon`；Playwright 浏览器端到端测试需要 Chromium 及其系统依赖。

### 1.2 运行环境

| 依赖 | 说明 |
| --- | --- |
| Avahi | `avahi-daemon` 已安装并可运行 |
| systemd | 目标系统使用 systemd 作为 init |
| D-Bus | 系统 D-Bus 可用 |
| `/etc/avahi` | Avahi 原生配置目录存在 |

当前 Helper 无法在缺少 `/etc/avahi` 配置目录时启动。请先由系统管理员完成 Avahi 安装，再用 `detect` 检查：

```bash
make check
./bin/avahi-manager detect
```

`detect` 只读检测 OS、systemd service/socket、Avahi D-Bus 和原生配置，输出 JSON，不修改任何系统状态。D-Bus 查询使用 NoAutoStart，查看停止的服务不会触发启动。

---

## 2. 构建项目

### 2.1 从源码构建

在仓库根目录，以普通用户执行：

```bash
make check
```

该命令依次执行：

1. `make frontend` — 安装前端依赖并构建到 `frontend/dist/`
2. `make test` — 执行 Go 竞态检测测试（`go test -race -timeout 90s ./...`）
3. `make vet` — 静态分析（`go vet ./...`）
4. `make build` — 编译最终二进制到 `bin/avahi-manager`

最终产出 `bin/avahi-manager`，这是一个内嵌 React 前端的单一 Go 可执行文件。运行时不需要 Node.js、npm 或任何 Web 服务器。

### 2.2 构建过程中的依赖下载

首次构建会自动通过 Go module proxy 下载依赖，包括：

- `github.com/coreos/go-systemd/v22` — systemd D-Bus 控制
- `github.com/godbus/dbus/v5` — Avahi D-Bus 通信
- `github.com/vishvananda/netlink` — 网络接口/地址读取
- `golang.org/x/crypto` — Argon2id 密码哈希
- `modernc.org/sqlite` — 纯 Go SQLite（WAL 模式）
- `github.com/fsnotify/fsnotify` — 文件监听

如果网络受限，可配置 Go module proxy：

```bash
export GOPROXY=https://goproxy.cn,direct
make check
```

### 2.3 前端单独构建

仅修改前端代码后，只需重新构建前端并重新编译二进制：

```bash
make frontend
make build
```

不需要重新运行测试。

### 2.4 仅运行测试

```bash
make test   # Go 竞态检测测试
make vet    # 静态分析
```

### 2.5 浏览器端到端测试

```bash
cd frontend
npm ci --no-audit --no-fund
npx playwright install chromium
cd ..
make e2e
```

测试使用临时配置、SQLite、Unix Helper 和模拟系统控制器，不修改宿主机 Avahi。浏览器及系统依赖需满足 Playwright 运行要求。

---

## 3. 诊断命令

以下命令只读，不修改宿主机配置：

### 3.1 系统检测

```bash
./bin/avahi-manager detect
```

输出 JSON，包含 OS、systemd service/socket、Avahi D-Bus 和配置文件状态。

### 3.2 网络接口列表

```bash
./bin/avahi-manager interfaces
```

通过 Netlink 读取本机网络接口和地址信息。

### 3.3 服务发现

```bash
./bin/avahi-manager discover --duration 10s
```

使用 Avahi D-Bus 信号和 ResolveService 在指定时长后输出发现快照。没有发现记录可能只是网络中没有服务广播。

添加 `--events` 输出全部缓存变化的流式更新：

```bash
./bin/avahi-manager discover --duration 30s --events
```

这些命令不调用 `avahi-browse`，不修改系统状态。

---

## 4. 仅预览界面（开发模式）

普通用户在仓库根目录执行，不启动 Helper，不修改系统配置：

```bash
mkdir -p .dev
./bin/avahi-manager init-admin --database "$PWD/.dev/manager.db"
./bin/avahi-manager serve --database "$PWD/.dev/manager.db"
```

### 4.1 初始化管理员

`init-admin` 交互输入密码，至少 12 字节，建议使用至少 12 个 ASCII 字符的强密码：

```
Administrator password (at least 12 bytes): ********
```

- 默认用户名 `admin`，可通过 `--username` 修改
- 没有默认密码
- 已有管理员时跳过并提示 `administrator already exists`
- 此模式和生产模式使用不同数据库，需要独立初始化

### 4.2 启动 Web 服务

`serve` 默认监听 `127.0.0.1:8053`，打开浏览器访问：

```
http://127.0.0.1:8053
```

用初始化时设置的密码登录。会话约 12 小时到期，到期后重新登录。

### 4.3 预览模式的限制

此模式没有 root Helper：

- 可登录、查看界面
- 部分只读状态和发现功能取决于系统环境
- 配置读取/保存、快照、服务控制不可用
- 相关页面可能报错或显示连接失败

这不是完整功能启动方式，仅用于开发和界面验证。

### 4.4 注意事项

- 不要把密码放进命令行参数
- 不要将 `.dev` 中的数据库、WAL 等敏感文件提交或分享
- 如果 8053 端口被占用，先停止之前的预览服务（Ctrl+C）
- 预览模式停止后数据库保留在 `.dev/manager.db`

---

## 5. 连接真实 Avahi：手动部署

以下步骤会创建系统账户和目录、安装本项目二进制。后续网页保存、恢复和服务控制会真正修改 `/etc/avahi`、重载或重启 Avahi。

### 5.0 前提确认

- 目标使用 systemd
- 已安装并配置 Avahi，`/etc/avahi` 存在
- 系统 D-Bus 可用
- 有 root 权限
- 用 `detect` 检查过目标机器状态

### 5.1 备份现有 Avahi 配置

部署前先独立备份整个 `/etc/avahi`：

```bash
sudo cp -a /etc/avahi /etc/avahi.bak.$(date +%Y%m%d%H%M%S)
```

### 5.2 检查现有账户和目录

先检查是否已有部署，不要覆盖：

```bash
getent passwd avahi-manager
getent group avahi-manager
ls -ld /var/lib/avahi-manager /var/lib/avahi-manager-helper /run/avahi-manager
```

不存在时检查会返回非零状态。

### 5.3 创建系统账户

仅在同名账户和组均不存在时创建：

```bash
sudo useradd --system --user-group --no-create-home \
  --home-dir /var/lib/avahi-manager \
  --shell /usr/sbin/nologin avahi-manager
```

参数说明：

- `--system` — 创建系统账户（非交互式）
- `--user-group` — 创建同名用户组
- `--no-create-home` — 不创建 home 目录（使用指定的 home-dir）
- `--shell /usr/sbin/nologin` — 禁止直接登录

### 5.4 创建目录并安装二进制

确认以下目标路径不是符号链接、没有其他应用文件冲突，再执行：

```bash
# 安装二进制
sudo install -d -o root -g root -m 0755 /usr/local/libexec
sudo install -o root -g root -m 0755 ./bin/avahi-manager /usr/local/libexec/avahi-manager

# 数据库和运行时目录
sudo install -d -o avahi-manager -g avahi-manager -m 0700 /var/lib/avahi-manager

# Helper 备份目录（root 控制）
sudo install -d -o root -g root -m 0700 /var/lib/avahi-manager-helper
sudo install -d -o root -g root -m 0700 /var/lib/avahi-manager-helper/backups

# 运行时 socket 目录
sudo install -d -o root -g avahi-manager -m 0750 /run/avahi-manager
```

目录权限说明：

| 路径 | 所有者 | 权限 | 用途 |
| --- | --- | --- | --- |
| `/usr/local/libexec/avahi-manager` | root:root | 0755 | 二进制文件 |
| `/var/lib/avahi-manager/` | avahi-manager:avahi-manager | 0700 | SQLite 数据库和会话 |
| `/var/lib/avahi-manager-helper/` | root:root | 0700 | Helper 备份（root 控制） |
| `/var/lib/avahi-manager-helper/backups/` | root:root | 0700 | 配置快照 |
| `/run/avahi-manager/` | root:avahi-manager | 0750 | Helper Unix socket |

安全原则：

- SQLite/WAL 目录需要管理器写权限
- 特权备份目录及其父目录必须由 root 控制
- 不能允许 Web 账户替换 root Helper 的存储路径
- 不要 `chmod 777`
- 不要把 `/etc/avahi` 交给 Web 账户所有
- 不要从普通用户可替换的路径长期运行 root Helper

此布局用于手动联调，不替代尚未完成的生产权限审计与服务加固。

### 5.5 初始化管理员

```bash
sudo -u avahi-manager /usr/local/libexec/avahi-manager init-admin \
  --database /var/lib/avahi-manager/manager.db
```

交互输入密码；已有管理员时跳过。这与预览模式的 `.dev/manager.db` 是不同数据库，需要独立初始化。

应用登录密码不是 root 或 Linux `avahi-manager` 账户密码。

### 5.6 启动 Helper 进程

Helper 必须以 root 身份运行，负责特权操作（写 `/etc/avahi`、systemd 控制、快照恢复）：

```bash
sudo /usr/local/libexec/avahi-manager helper \
  --manager-user avahi-manager \
  --config-dir /etc/avahi \
  --backups /var/lib/avahi-manager-helper/backups \
  --socket /run/avahi-manager/helper.sock
```

参数说明：

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--manager-user` | `avahi-manager` | 只允许此 UID 调用 Helper |
| `--config-dir` | `/etc/avahi` | Avahi 原生配置目录 |
| `--backups` | `/var/lib/avahi-manager/backups` | 快照存储目录（root 控制） |
| `--socket` | `/run/avahi-manager/helper.sock` | Unix socket 路径 |

Helper 启动后：

- 监听 Unix socket，等待 Web 服务的 RPC 请求
- 启动时可能恢复未完成事务（不是纯只读进程）
- 双向 `SO_PEERCRED` UID 校验，只接受指定 UID 的调用
- 支持 systemd socket activation（通过 `activation.Listeners()`）

保持终端运行。在另一终端验证 socket：

```bash
sudo ls -l /run/avahi-manager/helper.sock
sudo ss -xlpn | grep helper
```

Socket 应为 `root:avahi-manager`、权限 `0660`。不要用 `touch` 创建 socket，必须由 Helper 监听。

### 5.7 启动 Web 服务

Web 服务必须以非 root 身份运行（程序会拒绝 root 执行）：

```bash
sudo -u avahi-manager /usr/local/libexec/avahi-manager serve \
  --database /var/lib/avahi-manager/manager.db \
  --config-dir /etc/avahi \
  --helper /run/avahi-manager/helper.sock \
  --listen 127.0.0.1:8053
```

`serve` 参数说明：

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--listen` | `127.0.0.1:8053` | HTTP 监听地址 |
| `--origins` | `http://127.0.0.1:8053,http://localhost:8053` | 允许的浏览器 Origin（精确匹配） |
| `--database` | `/var/lib/avahi-manager/manager.db` | SQLite 数据库路径 |
| `--helper` | `/run/avahi-manager/helper.sock` | Helper Unix socket 路径 |
| `--config-dir` | `/etc/avahi` | Avahi 配置目录 |

访问 `http://127.0.0.1:8053` 并登录。

### 5.8 启动顺序总结

```
1. sudo ... helper    ← 先启动 Helper（root）
2. sudo -u ... serve   ← 再启动 Web（非 root）
```

停止顺序相反：

```
1. Ctrl+C serve        ← 先停止 Web
2. Ctrl+C helper       ← 再停止 Helper
```

两个终端必须保持运行。两端 socket 路径、配置目录必须一致；Web 实际用户必须匹配 `--manager-user`。不能以 root 运行 `serve`/`init-admin`，不能以普通用户运行 Helper。

---

## 6. 日志读取权限（可选）

Logs 页面使用 `journalctl`，管理器需要 journal 读取权限。先检查：

```bash
sudo -u avahi-manager journalctl --unit avahi-daemon.service --no-pager --lines 20
```

权限不足可能表现为空列表、内容不全或错误。在提供 `systemd-journal` 组的系统上，管理员可选择授权：

```bash
getent group systemd-journal
sudo usermod -aG systemd-journal avahi-manager
```

该组通常可读取其他系统日志，不只 Avahi，需权衡信息访问范围。变更后重启 Web 进程使组权限生效。

---

## 7. 日常使用

### 7.1 Dashboard

首页显示关键状态：Avahi 运行状态、版本、主机名、接口、已发布/已发现服务数、D-Bus 状态、配置管理状态、更新状态。同时显示重要告警：Restart Required、Configuration Drift、Update Available、D-Bus Error。

### 7.2 Discovery

浏览和筛选网络 mDNS 服务、查看解析详情。数据仅保存在内存，不会自动将发现的服务变成本机发布配置。

### 7.3 Services

管理本机服务组、端口、协议、TXT。保存即应用到 `/etc/avahi/services/*.service`，不是只存网页草稿。服务文件使用 `encoding/xml` 生成，文件名使用 `awm-<UUID>.service` 格式。

### 7.4 Hosts

管理 `/etc/avahi/hosts` 静态映射。保存即写入原生文件。

### 7.5 Interfaces

选择发布接口，设置 `allow-interfaces`。接口限制会影响 mDNS 可见性。

### 7.6 Settings

修改主机名、域及 IPv4/IPv6 设置。`avahi-daemon.conf` 修改触发 Restart，`services/*.service` 修改触发 Reload，用户无需手动判断。

### 7.7 Logs

实时查看 Avahi 日志，支持搜索、时间范围、级别过滤和实时 Follow。数据来自 `journalctl`，使用固定 argv 执行。

### 7.8 Maintenance

- Avahi 生命周期控制（Start/Stop/Restart/Enable/Disable）
- 配置快照查看、恢复、删除
- 审计日志查看

### 7.9 外部修改与冲突

`/etc/avahi` 是配置事实来源。命令行修改后会提示漂移，不会自动覆盖外部修改。

- **Reload from disk**：接受磁盘内容为新基线
- **查看差异**：Settings 展示 daemon 原始内容对照
- **Restore**：恢复选定快照并应用，会覆盖快照后的部分修改
- 遇到 revision/409 冲突，重新读取并合并修改

### 7.10 快照边界

当前覆盖受管理的 `avahi-daemon.conf`、`hosts`、`services/*.service`，不是完整 `/etc/avahi` 目录归档。正常保留策略最多保留 20 个快照。不要手工编辑或删除 `baseline.json`、`pending.json`、快照清单和内容。

---

## 8. 停止、重启与保留数据

### 8.1 停止

先在 Web 终端 Ctrl+C，再停止 Helper。如正在应用配置，应等待完成或恢复，避免强制杀进程。停止 Manager 不会停止 Avahi，已写入的原生配置仍保留。

### 8.2 重启

再次启动跳过初始化，先 Helper 后 Web，沿用原数据库、备份和配置路径。`/run` 通常在重启后清空，需重新创建运行目录：

```bash
sudo install -d -o root -g avahi-manager -m 0750 /run/avahi-manager
```

### 8.3 更新项目

1. 构建并测试新版本：`make check`
2. 停止两个进程（先 Web 后 Helper）
3. 替换安装二进制：`sudo install -o root -g root -m 0755 ./bin/avahi-manager /usr/local/libexec/avahi-manager`
4. 按顺序启动（先 Helper 后 Web）
5. 刷新浏览器

已有账户无需重新初始化。

### 8.4 备份数据库

1. 停止 Web 进程
2. 将数据库及同目录中仍存在的 WAL/SHM 文件一并保留
3. 另行备份 root Helper 存储与整个 `/etc/avahi`

不要通过删除数据库解决普通登录/socket 问题，这会丢失管理员、审计和管理状态。

---

## 9. 远程访问

### 9.1 SSH 隧道（推荐）

优先保留回环监听，通过 SSH 隧道访问。在自己的电脑执行（替换服务器地址）：

```bash
ssh -N -L 8053:127.0.0.1:8053 your-user@your-server
```

本机访问 `http://127.0.0.1:8053`。

### 9.2 反向代理

自行部署反向代理时需满足：

- HTTPS（TLS 证书由外部代理提供，`serve` 本身不提供证书参数）
- 正确转发 Host 头
- 支持 SSE 长连接（日志实时 Follow）
- `--origins` 应为准确外部 HTTPS Origin，不能用通配符或带路径 URL

Nginx 示例：

```nginx
server {
    listen 443 ssl;
    server_name avahi.example.com;

    ssl_certificate     /etc/ssl/certs/avahi.example.com.pem;
    ssl_certificate_key /etc/ssl/private/avahi.example.com.key;

    location / {
        proxy_pass http://127.0.0.1:8053;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # SSE 支持
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 86400s;
    }
}
```

启动 serve 时设置对应的 Origin：

```bash
sudo -u avahi-manager /usr/local/libexec/avahi-manager serve \
  --listen 127.0.0.1:8053 \
  --origins https://avahi.example.com \
  ...
```

### 9.3 安全警告

- 不要将开发版 HTTP 端口直接暴露公网
- 不要混用 HTTP/HTTPS Origin，以免 Cookie 策略不一致
- 不要禁用安全校验

---

## 10. 常见错误

| 现象 | 原因与处理 |
| --- | --- |
| `Post "http://helper/rpc/ListSnapshots": dial unix /run/avahi-manager/helper.sock: connect: no such file or directory` | 指定路径没有 socket。只启动 Web 时可预期，但快照不可用。按第 5 节创建目录并启动 Helper，检查其启动错误及两端路径。`http://helper` 是内部 Unix RPC URL，不需要配置 DNS。 |
| `configuration bootstrap unavailable` | 无法读取管理基线；Web 继续运行不代表 Helper 正常。先修复 Helper/目录权限，再刷新，必要时重启 Web。 |
| socket `permission denied` / UID 拒绝 | 检查目录遍历权限、socket 组和模式，Web 实际用户应匹配 `--manager-user`，Helper 必须为 root；不要改成全员可写。 |
| socket `address already in use` / `connection refused` | 用 `sudo ss -xlpn` 和进程信息确认已有 Helper 或残留 socket；只有确认没有监听进程后，管理员才可清理精确残留路径。不要删除活跃 socket。 |
| `administrator already exists` | 已初始化，直接启动并登录；当前没有交付密码重置命令。 |
| `administrator is not initialized` | 核对 `init-admin` 与 `serve` 是否使用同一数据库。 |
| `HTTP server must run as a non-root account` | 以指定普通账户运行 Web，不要直接 `sudo ... serve`。 |
| 403 / Origin 或 Host 被拒绝 | 用允许的地址访问；协议、主机和端口需匹配，修改地址后同步设置 `--origins`，不要禁用安全校验。 |
| 401 / 会话过期 | 重新登录；HTTPS Origin 搭配 HTTP 访问可能导致 Secure Cookie 无法发送，应统一协议。 |
| 登录失败后 429 | 限流生效，按响应提示等待，核对用户名、密码和数据库。 |
| 配置校验失败 | 检查错误及原文件；严格解析器可能尚不支持部分合法发行版扩展，不要为通过校验盲目删除原配置。 |
| 8053 占用 / 页面打不开 | 检查 Web 终端及是否重复启动；换端口时同时设置 `--listen` 和 `--origins`。 |

---

## 11. 一键启动脚本示例

以下是手动部署时的快速启动参考脚本：

```bash
#!/bin/bash
set -euo pipefail

BIN=/usr/local/libexec/avahi-manager
DB=/var/lib/avahi-manager/manager.db
SOCK=/run/avahi-manager/helper.sock
BACKUP=/var/lib/avahi-manager-helper/backups

# 确保运行目录存在
sudo install -d -o root -g avahi-manager -m 0750 /run/avahi-manager

# 启动 Helper（后台）
sudo "$BIN" helper \
  --manager-user avahi-manager \
  --config-dir /etc/avahi \
  --backups "$BACKUP" \
  --socket "$SOCK" &
HELPER_PID=$!
sleep 1

# 启动 Web（后台）
sudo -u avahi-manager "$BIN" serve \
  --database "$DB" \
  --config-dir /etc/avahi \
  --helper "$SOCK" \
  --listen 127.0.0.1:8053 &
WEB_PID=$!

echo "Helper PID: $HELPER_PID"
echo "Web PID: $WEB_PID"
echo "Access: http://127.0.0.1:8053"

# 等待任一进程退出
wait -n $HELPER_PID $WEB_PID 2>/dev/null || true

# 清理
kill $HELPER_PID $WEB_PID 2>/dev/null || true
wait 2>/dev/null
```

---

## 12. 验收测试

想测试完整网页操作但不修改宿主机，可在仓库根目录执行：

```bash
cd frontend
npm ci --no-audit --no-fund
npx playwright install chromium
cd ..
make e2e
```

测试使用临时配置、SQLite 和 Unix Helper，系统控制器为测试替身，不修改宿主机 Avahi。浏览器及系统依赖需满足 Playwright 运行要求。测试不会留下长期演示站点，也不能代替真实 systemd 写入与生产安全验收。

---

## 13. 当前未交付事项

以下功能在 PLAN.md 中规划但尚未实现，当前部署不包含：

- `manager.toml` 配置文件（位于 `/etc/avahi-manager/manager.toml`）
- systemd service/helper/socket 单元文件
- Debian 包和 APT 安装入口
- 完整 `/etc/avahi/*` 辅助文件归档
- 生产权限硬化与完整目录备份
- 密码重置命令

未交付事项及验证记录见 [DEVELOPMENT.md](../DEVELOPMENT.md)，原始要求见 [PLAN.md](../PLAN.md)。
