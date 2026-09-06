# Avahi Web Manager 技术方案

## 1. 项目定位

开发一个运行于 Linux 宿主机的 Avahi 图形化管理平台。

技术栈：

* Backend：Go
* Frontend：React + TypeScript
* Database：SQLite
* Runtime：systemd
* Avahi 通信：D-Bus
* 系统服务控制：systemd D-Bus
* 网络接口读取：Linux Netlink
* Avahi 持久配置：Avahi 原生配置文件
* 部署形式：独立 Linux 原生程序，不使用 Docker

项目不重新实现 mDNS/DNS-SD 协议。

项目负责：

```text
安装状态检测
        ↓
Avahi 配置管理
        ↓
Avahi 生命周期管理
        ↓
服务发布管理
        ↓
mDNS 服务发现
        ↓
网络接口管理
        ↓
日志 / 更新 / 备份 / 恢复
```

Avahi 本身仍然作为真正的 mDNS/DNS-SD Engine。

---

# 2. 核心设计原则

整体关系：

```text
                    Browser
                       │
                  HTTP / WS
                       │
                       ▼
               avahi-managerd
                     Go
                       │
       ┌───────────────┼─────────────────┐
       │               │                 │
       ▼               ▼                 ▼
   Avahi D-Bus      systemd D-Bus      Netlink
       │               │                 │
       │               │                 │
 Discovery         Start/Stop        NIC / Address
 Resolve           Restart           Link State
 Runtime State     Enable
       │               │
       └───────────────┬┘
                       │
                       ▼
                Config Manager
                       │
         ┌─────────────┼─────────────┐
         ▼             ▼             ▼
avahi-daemon.conf    hosts      services/*.service
         │             │             │
         └─────────────┼─────────────┘
                       ▼
                 avahi-daemon
                       │
                       ▼
                 mDNS / DNS-SD
```

Avahi 官方本身就把 `/etc/avahi/avahi-daemon.conf`、`/etc/avahi/hosts` 和 `/etc/avahi/services/*.service` 作为持久配置入口，同时提供丰富的 D-Bus IPC。

因此不能把项目设计成：

```text
React
  ↓
Go
  ↓
Jinja Template
  ↓
覆盖 avahi.conf
```

而应该设计成：

```text
React
  ↓
Go Control Plane
  │
  ├── Config
  ├── D-Bus
  ├── systemd
  ├── Netlink
  ├── journald
  ├── APT
  └── SQLite
```

---

# 3. 配置事实来源

## Avahi 配置

以下文件始终是真实配置来源：

```text
/etc/avahi/avahi-daemon.conf
/etc/avahi/hosts
/etc/avahi/services/*.service
```

不要把这些配置复制进 SQLite 后再以 SQLite 为唯一来源。

原因：

```text
Avahi Manager 被停止
        ↓
Avahi 仍然可以正常运行

Avahi Manager 被卸载
        ↓
Avahi 配置仍然完整

管理员使用 CLI 查看配置
        ↓
看到的就是实际生效配置
```

SQLite 只负责 Manager 自己的数据。

---

# 4. SQLite 职责

数据库：

```text
/var/lib/avahi-manager/manager.db
```

SQLite 保存：

```text
app_settings
users
sessions
managed_files
config_snapshots
audit_logs
operation_jobs
```

建议 Schema：

```text
app_settings
-------------
key
value
updated_at


users
-------------
id
username
password_hash
created_at
last_login_at


sessions
-------------
id
user_id
token_hash
expires_at
created_at


managed_files
-------------
path
sha256
last_seen_at
externally_modified


config_snapshots
-------------
id
reason
manifest_json
created_at


audit_logs
-------------
id
actor
action
resource
detail_json
result
created_at


operation_jobs
-------------
id
type
state
progress
result
created_at
finished_at
```

不要设计：

```text
services
hosts
avahi_config
```

作为 Avahi 的主配置数据库。

这些信息实时从 `/etc/avahi/` 读取。

---

# 5. 首次启动和自动接管

Manager 启动后执行 Bootstrap。

流程：

```text
启动 Manager
     ↓
检测 OS
     ↓
检测 systemd
     ↓
检测 avahi-daemon
     ↓
检测 Avahi D-Bus
     ↓
检测配置文件
     ↓
读取当前配置
     ↓
建立初始 Snapshot
     ↓
记录文件 SHA256
     ↓
进入 Managed 状态
```

检测内容包括：

```text
/usr/sbin/avahi-daemon

/etc/avahi/avahi-daemon.conf
/etc/avahi/hosts
/etc/avahi/services/

avahi-daemon.service
avahi-daemon.socket

org.freedesktop.Avahi
```

Debian 当前包实际上同时安装 `avahi-daemon.service`、`avahi-daemon.socket` 和完整的 Avahi D-Bus interface XML。

状态模型：

```text
NOT_INSTALLED

INSTALLED_STOPPED

RUNNING

RUNNING_DEGRADED

CONFIG_ERROR

DBUS_UNAVAILABLE

UPDATE_AVAILABLE
```

如果没有安装 Avahi：

```text
Avahi is not installed
```

GUI 提供：

```text
Install Avahi
```

但**不要自动安装**。

涉及 APT 修改必须由管理员主动确认。

---

# 6. 配置接管策略

第一次检测到已有配置时：

```text
/etc/avahi/*
       ↓
完整备份
       ↓
Parse
       ↓
建立配置模型
       ↓
记录 SHA256
```

Manager 不应该第一次启动就重新生成配置。

只有用户第一次：

```text
Save & Apply
```

时才进行规范化写入。

这样可以避免安装管理器本身就改变生产服务器。

---

# 7. 外部修改检测

必须考虑：

```bash
vim /etc/avahi/avahi-daemon.conf
```

或者：

```bash
apt upgrade avahi-daemon
```

导致文件变化。

Manager 使用：

```text
fsnotify
+
SHA256
```

监控：

```text
/etc/avahi/avahi-daemon.conf
/etc/avahi/hosts
/etc/avahi/services/
```

发现变化：

```text
Configuration changed outside Avahi Manager
```

GUI 提供：

```text
Reload from disk
View changes
Restore managed version
```

默认：

> 磁盘内容优先。

不要后台自动覆盖管理员手工修改。

---

# 8. Backend 模块划分

建议：

```text
internal/

├── avahi/
│   ├── client.go
│   ├── browser.go
│   ├── resolver.go
│   └── state.go
│
├── config/
│   ├── daemon.go
│   ├── hosts.go
│   ├── service.go
│   ├── validator.go
│   ├── writer.go
│   └── snapshot.go
│
├── systemd/
│   ├── manager.go
│   └── status.go
│
├── network/
│   ├── interfaces.go
│   └── addresses.go
│
├── package/
│   └── apt.go
│
├── journal/
│   └── reader.go
│
├── database/
│   ├── sqlite.go
│   ├── migrations/
│   └── repository/
│
├── api/
│   ├── router.go
│   ├── middleware/
│   └── handlers/
│
├── auth/
│
├── helper/
│
└── model/
```

---

# 9. Avahi D-Bus 模块

正式程序不要依赖解析：

```bash
avahi-browse
```

作为核心实现。

Avahi 本身已经提供 D-Bus：

```text
org.freedesktop.Avahi.Server
org.freedesktop.Avahi.ServiceBrowser
org.freedesktop.Avahi.ServiceResolver
org.freedesktop.Avahi.ServiceTypeBrowser
org.freedesktop.Avahi.EntryGroup
org.freedesktop.Avahi.HostNameResolver
org.freedesktop.Avahi.AddressResolver
```

这些接口文件目前直接随 Debian Avahi 包安装。

Go 使用：

```text
github.com/godbus/dbus/v5
```

主要承担：

```text
Daemon 状态检测

Hostname 查询

Domain 查询

Service Browser

Service Type Browser

Service Resolver

Host Resolver
```

实时服务发现：

```text
LAN
 ↓
Avahi
 ↓
D-Bus Signal
 ↓
Go
 ↓
WebSocket
 ↓
React
```

不需要轮询。

---

# 10. CLI 的定位

支持：

```text
avahi-browse
avahi-resolve
avahi-publish
avahi-daemon
```

但只作为：

```text
诊断
Fallback
开发测试
```

而不是正式控制 API。

Manager 的核心控制路径：

```text
D-Bus
systemd D-Bus
filesystem
Netlink
```

---

# 11. Avahi 生命周期管理

GUI 必须提供：

```text
Start
Stop
Restart

Enable on boot
Disable on boot

Reload Services
```

这里需要同时考虑：

```text
avahi-daemon.service
avahi-daemon.socket
```

Debian 官方文档在完全禁用 Avahi 时也是同时 stop/disable service 和 socket。

因此定义：

### Start

启动：

```text
avahi-daemon.socket
avahi-daemon.service
```

### Stop

停止：

```text
avahi-daemon.service
avahi-daemon.socket
```

### Enable

enable：

```text
avahi-daemon.service
avahi-daemon.socket
```

### Disable

disable：

```text
avahi-daemon.service
avahi-daemon.socket
```

不要默认提供：

```text
Mask
```

可以放进 Advanced Maintenance。

---

# 12. Reload 与 Restart 必须区分

这是 GUI 很重要的 UX。

修改：

```text
/etc/avahi/services/*.service
```

可以：

```text
Reload
```

但是修改：

```text
/etc/avahi/avahi-daemon.conf
```

必须：

```text
Restart
```

因为 Avahi 官方明确规定 `--reload` 会重新读取静态 service 文件，但不会重新读取 `avahi-daemon.conf`。

因此 Backend 应该自动计算：

```text
ChangeSet
```

例如：

```text
ServiceChanged
    → Reload

HostsChanged
    → Restart 或按实现确定刷新行为

DaemonConfigChanged
    → Restart
```

用户不用自己判断。

按钮统一叫：

```text
Save & Apply
```

Backend 自动选择正确动作。

---

# 13. systemd 控制

Go 不应该通过：

```bash
exec("systemctl " + userInput)
```

操作。

使用 systemd D-Bus。

推荐：

```text
github.com/coreos/go-systemd/v22/dbus
```

调用：

```text
StartUnit
StopUnit
RestartUnit
ReloadUnit
EnableUnitFiles
DisableUnitFiles
```

systemd 当前 D-Bus API原生提供 Start、Stop、Reload、Restart 等 unit 操作。

---

# 14. Interfaces 模块

通过 Netlink 获取：

```text
Interface Name
Index
MAC
Flags
Link State
IPv4
IPv6
MTU
Interface Type
```

GUI：

```text
Interfaces

enp1s0
203.0.113.20
Public candidate
mDNS: OFF

enp2s0
192.168.1.20
LAN
mDNS: ON

wg0
10.10.0.1
WireGuard
mDNS: OFF

docker0
172.17.0.1
Docker
mDNS: OFF
```

用户勾选：

```text
☑ enp2s0
```

生成：

```ini
allow-interfaces=enp2s0
```

Avahi 原生支持 `allow-interfaces` 和 `deny-interfaces`；设置 allow list 后其他接口流量会被忽略。

推荐 GUI 默认采用：

```text
Allow List
```

而不是 Deny List。

---

# 15. Settings 页面

Basic Settings：

```text
Hostname

Domain

IPv4
IPv6

Allowed Interfaces

Publishing Enabled

Publish Addresses

Publish Workstation

Reflector Enabled
```

Advanced：

对应：

```text
[server]
[wide-area]
[publish]
[reflector]
[rlimits]
```

不要直接让用户编辑原始 INI。

提供：

```text
Advanced → Raw configuration
```

只读查看即可。

---

# 16. Service 管理

管理：

```text
/etc/avahi/services/*.service
```

Avahi 官方 `.service` 就是 XML DNS-SD 静态服务定义，并且一个 service-group 可以包含多个 service。

内部模型：

```text
ServiceGroup

ID
Name
ReplaceWildcards

Services[]
    Type
    Protocol
    Domain
    Host
    Port
    Subtypes[]
    TXT[]
```

GUI：

```text
Published Services

Gitea
_http._tcp
3000
IPv4
Enabled

SSH
_ssh._tcp
22
Any
Enabled
```

Add Service：

```text
Display Name
Service Type
Protocol
Port
Hostname
Domain
TXT Records
```

TXT 使用 Key/Value 编辑器。

---

# 17. Service 文件生成

不要使用字符串拼接或 Jinja。

使用：

```text
encoding/xml
```

生成 XML。

流程：

```text
React JSON
    ↓
Go Struct
    ↓
Validate
    ↓
encoding/xml
    ↓
.service
```

例如：

```text
ServiceGroup
    ↓
XML Encoder
    ↓
/etc/avahi/services/<uuid>.service
```

文件名不要直接使用用户输入。

使用：

```text
awm-<UUID>.service
```

避免：

```text
../../example
```

等路径问题。

---

# 18. Hosts 管理

管理：

```text
/etc/avahi/hosts
```

页面：

```text
Static Hosts

nas.local
192.168.1.10

printer.local
192.168.1.20
```

提供：

```text
Add
Edit
Delete
```

并检查：

```text
IP 合法性
hostname 合法性
重复记录
```

---

# 19. Discovery 页面

这是 Manager 区别于普通配置 GUI 的重要功能。

页面：

```text
Discovery

Interface: All / enp2s0
Protocol: All / IPv4 / IPv6
Type: All / HTTP / SSH / SMB / IPP
```

实时表格：

```text
NAME        TYPE          HOST              ADDRESS        PORT

Gitea       _http._tcp    server.local      192.168.1.20   3000
SSH         _ssh._tcp     server.local      192.168.1.20   22
NAS         _smb._tcp     nas.local         192.168.1.30   445
Printer     _ipp._tcp     printer.local     192.168.1.40   631
```

点击展开：

```text
Instance
Type
Domain
Interface
Protocol
Hostname
IPv4 / IPv6
Port
TXT Records
First Seen
Last Seen
```

Discovery 数据仅保存在内存。

不需要长期写 SQLite。

---

# 20. Dashboard

首页只放关键状态：

```text
Avahi
RUNNING

Version
0.8

Hostname
server.local

Interfaces
1 active / 6 detected

Published Services
4

Discovered Services
23

D-Bus
Healthy

Configuration
Managed

Update
Up to date
```

同时显示：

```text
Restart Required
Configuration Drift
Update Available
D-Bus Error
```

等重要告警。

---

# 21. Logs

页面：

```text
Logs
```

数据主要来自：

```text
journalctl
```

V1 可以使用固定参数执行：

```text
/usr/bin/journalctl
```

因为日志读取不是安全敏感的动态 shell 操作。

必须采用固定 argv：

```text
journalctl
--unit
avahi-daemon.service
--output=json
```

禁止：

```text
sh -c
```

支持：

```text
INFO
WARNING
ERROR

搜索

时间范围

实时 Follow
```

实时日志通过 WebSocket/SSE 给 React。

---

# 22. Avahi 软件更新

V1 支持 Debian/Ubuntu APT。

维护页面：

```text
Installed Version
Available Version

Check Updates

Update Avahi
```

Backend 只允许固定操作：

```text
apt-get update

apt-get --only-upgrade install avahi-daemon
```

必须：

```text
用户主动点击
        ↓
检查
        ↓
显示版本变化
        ↓
再次确认
        ↓
建立配置 Snapshot
        ↓
更新
        ↓
重新检测 Avahi
        ↓
Health Check
```

不要实现：

```text
任意 package 安装
任意 apt 参数
任意 shell
```

V1 不负责 Linux 整机更新。

只负责 Avahi 相关包。

---

# 23. Safe Apply

所有配置操作必须使用事务式 Apply。

流程：

```text
New Config
    ↓
Schema Validation
    ↓
Semantic Validation
    ↓
Create Snapshot
    ↓
Write Temporary Files
    ↓
fsync
    ↓
Atomic Rename
    ↓
Reload / Restart
    ↓
Health Check
    ↓
Commit
```

失败：

```text
Health Check Failed
        ↓
Restore Snapshot
        ↓
Restart Avahi
        ↓
Report Failure
```

不要直接：

```text
os.WriteFile("/etc/avahi/avahi-daemon.conf")
```

覆盖生产配置。

---

# 24. Snapshot

目录：

```text
/var/lib/avahi-manager/backups/
```

例如：

```text
2026-09-06T130500Z/
├── manifest.json
├── avahi-daemon.conf
├── hosts
└── services/
```

SQLite 只记录：

```text
snapshot_id
reason
timestamp
manifest
```

真正配置文件保存在 filesystem。

保留最近：

```text
20
```

个 Snapshot。

支持：

```text
View
Restore
Delete
```

---

# 25. Privilege Architecture

不建议让整个 HTTP Server 直接以 root 身份运行。

使用同一个 Go Binary 的两个运行模式：

```text
/usr/bin/avahi-manager
```

### Manager

```text
avahi-manager serve
```

用户：

```text
avahi-manager
```

负责：

```text
HTTP
React
SQLite
D-Bus Browse
Configuration Model
```

### Privileged Helper

```text
avahi-manager helper
```

由 root 运行。

负责：

```text
写 /etc/avahi

systemd control

APT update

Snapshot restore
```

通信：

```text
/run/avahi-manager/helper.sock
```

架构：

```text
Browser
   ↓
Unprivileged Go Backend
   ↓
Unix Socket
   ↓
Root Helper
   ↓
Linux System
```

---

# 26. Helper 必须使用严格 RPC

只允许：

```text
WriteDaemonConfig

WriteHosts

WriteService

DeleteService

ReloadAvahi

RestartAvahi

StartAvahi

StopAvahi

EnableAvahi

DisableAvahi

CheckUpdate

UpgradeAvahi

RestoreSnapshot
```

绝对不要存在：

```text
RunCommand(string)
ExecuteShell(string)
WriteFile(path,data)
```

这种通用接口。

否则 Web 漏洞很容易变成 root RCE。

---

# 27. Web 服务安全

默认：

```text
127.0.0.1:8053
```

如果管理员主动切换：

```text
0.0.0.0
```

必须启用认证。

V1 只设计：

```text
单管理员
```

不需要复杂 RBAC。

密码：

```text
Argon2id
```

Session：

```text
HttpOnly
SameSite=Strict
Secure when HTTPS
```

同时：

```text
CSRF protection
login rate limit
audit log
```

---

# 28. React 页面结构

导航栏固定为：

```text
Dashboard

Discovery

Services

Hosts

Interfaces

Settings

Logs

Maintenance
```

其中 Maintenance：

```text
Avahi Status

Start
Stop
Restart

Enable
Disable

Version

Check Update
Update

Configuration Backups
```

---

# 29. REST API

统一：

```text
/api/v1/
```

主要接口：

```text
GET    /status

GET    /avahi/status
POST   /avahi/start
POST   /avahi/stop
POST   /avahi/restart
POST   /avahi/enable
POST   /avahi/disable

GET    /config
PUT    /config

GET    /interfaces

GET    /services
POST   /services
PUT    /services/:id
DELETE /services/:id

GET    /hosts
POST   /hosts
PUT    /hosts/:id
DELETE /hosts/:id

GET    /discovery
GET    /discovery/events

GET    /logs
GET    /logs/events

GET    /updates
POST   /updates/check
POST   /updates/install

GET    /snapshots
POST   /snapshots/:id/restore

GET    /audit
```

所有修改接口都必须产生：

```text
audit_log
```

---

# 30. Frontend 构建

React：

```text
React
TypeScript
Vite
```

Build：

```text
frontend/dist
```

Go 使用：

```text
go:embed
```

直接把 Web UI 嵌入 Go Binary。

最终部署：

```text
/usr/bin/avahi-manager
```

就是一个主要可执行文件。

不需要：

```text
Node.js
npm
nginx
```

作为运行时依赖。

---

# 31. 安装目录

推荐：

```text
/usr/bin/avahi-manager

/etc/avahi-manager/
└── manager.toml

/var/lib/avahi-manager/
├── manager.db
└── backups/

/run/avahi-manager/
└── helper.sock

/usr/lib/systemd/system/
├── avahi-manager.service
├── avahi-manager-helper.service
└── avahi-manager-helper.socket
```

Avahi 配置仍然保持：

```text
/etc/avahi/
```

---

# 32. Repository

建议：

```text
avahi-manager/

├── cmd/
│   └── avahi-manager/
│
├── internal/
│   ├── api/
│   ├── auth/
│   ├── avahi/
│   ├── config/
│   ├── database/
│   ├── helper/
│   ├── journal/
│   ├── network/
│   ├── package/
│   ├── systemd/
│   └── model/
│
├── frontend/
│   ├── src/
│   └── package.json
│
├── migrations/
│
├── packaging/
│   ├── systemd/
│   └── debian/
│
├── go.mod
└── README.md
```

---

# 33. V1 功能范围

第一版必须完成：

1. Avahi 自动检测。
2. Avahi systemd 状态读取。
3. Start / Stop / Restart。
4. Enable / Disable。
5. `avahi-daemon.conf` GUI。
6. Interface 选择。
7. IPv4 / IPv6 设置。
8. Hostname / Domain 设置。
9. `/etc/avahi/hosts` 管理。
10. `.service` 创建、编辑、删除。
11. DNS-SD TXT Record。
12. D-Bus 服务发现。
13. 服务 Resolve。
14. journald 查看。
15. 配置 Snapshot。
16. Safe Apply + Rollback。
17. SQLite。
18. Audit Log。
19. Avahi 更新检查和升级。
20. React Web UI。
21. 内置管理员认证。

---

# 34. V1 不实现

明确不要让 Codex自行扩展：

```text
自己实现 mDNS

替代 Avahi

Docker 部署

Kubernetes

集群管理

多服务器集中管理

LDAP

OAuth

复杂 RBAC

内部 DNS Server

DHCP

Bonjour Gateway

自定义 mDNS Reflector

插件系统

Prometheus

Grafana

远程云管理
```

Avahi 自带的：

```text
enable-reflector
```

可以作为 Settings 中的配置项管理，但 Manager 不自己实现 reflector。

---

# 35. 第二阶段再考虑

以后可以增加：

```text
Runtime Service Publishing
```

使用：

```text
org.freedesktop.Avahi.EntryGroup
```

直接通过 D-Bus：

```text
AddService
Commit
Reset
```

Avahi 的发布 API本身能够指定 interface、protocol、service type、port 等。

但第一阶段应优先完成：

```text
Persistent Service
        ↓
/etc/avahi/services/*.service
```

因为这种方式即使 Manager 不运行，服务依然存在。

---

# 36. 开发顺序

Codex 应按照：

```text
Phase 1
OS / Avahi / systemd Detection

Phase 2
Avahi Config Parser

Phase 3
Safe Config Writer + Snapshot

Phase 4
systemd Control

Phase 5
Avahi D-Bus Client

Phase 6
Network Interface Detection

Phase 7
SQLite

Phase 8
REST API

Phase 9
React GUI

Phase 10
Logs / Update / Maintenance

Phase 11
Privilege Helper

Phase 12
Packaging + Hardening
```

实现。

不要先做漂亮 GUI 再补 Linux 系统控制层。

---

# 37. 最终架构

完整运行关系：

```text
                         Browser
                            │
                            │ HTTP / WebSocket
                            ▼
                  ┌────────────────────┐
                  │  avahi-managerd    │
                  │                    │
                  │ Go REST API        │
                  │ Embedded React     │
                  │ SQLite             │
                  └─────────┬──────────┘
                            │
              ┌─────────────┼──────────────┐
              │             │              │
              ▼             ▼              ▼
          Avahi D-Bus    Netlink      Helper Socket
              │             │              │
              │             │              ▼
              │             │      root helper
              │             │          │
              │             │     ┌────┼───────┐
              │             │     │    │       │
              │             │     ▼    ▼       ▼
              │             │   Files systemd APT
              │             │
              ▼             │
        avahi-daemon        │
              │             │
              └──────┬──────┘
                     │
              Linux Interfaces
                     │
          ┌──────────┼──────────┐
          ▼          ▼          ▼
        LAN         VPN       Docker
       enp2s0       wg0       docker0
          │
          ▼
        mDNS
```

这里的职责边界应该始终保持：

```text
Avahi
= mDNS / DNS-SD Engine

Avahi Manager
= Control Plane

SQLite
= Manager State

/etc/avahi
= Persistent Avahi State

D-Bus
= Runtime State

systemd
= Process Lifecycle

Netlink
= Network State
```

这是整个项目最重要的架构原则。
