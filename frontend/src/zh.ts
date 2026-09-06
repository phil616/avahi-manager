// Only presentation text is localized. Native configuration and API values stay unchanged.
export const actionLabels: Record<string, string> = {
  start: "启动 Avahi",
  stop: "停止 Avahi",
  restart: "重启 Avahi",
  reload: "重新加载服务声明",
  enable: "启用开机启动",
  disable: "禁用开机启动",
};
export const actionHelp: Record<string, string> = {
  start: "启动 Avahi 服务及套接字。",
  stop: "停止 Avahi 服务及套接字，本机服务发布与发现将中断；不会停止此管理面板。",
  restart: "本机服务发布与发现可能短暂中断。",
  reload: "重新读取服务声明，不等同于重启或应用所有守护进程设置。",
  enable: "设置 Avahi 随系统启动，不会立即启动服务。",
  disable: "取消 Avahi 开机启动，不会立即停止正在运行的服务。",
};
const states: Record<string, string> = {
  NOT_INSTALLED: "未安装",
  STOPPED: "已停止",
  RUNNING: "运行中",
  DEGRADED: "运行异常",
  CONFIG_ERROR: "配置错误",
  DBUS_UNAVAILABLE: "D-Bus 不可用",
  UPDATE_AVAILABLE: "有可用更新",
  active: "运行中",
  inactive: "未运行",
  failed: "失败",
  activating: "正在启动",
  deactivating: "正在停止",
  enabled: "已启用",
  disabled: "已禁用",
  static: "静态单元",
  masked: "已屏蔽",
  indirect: "间接启用",
  "enabled-runtime": "临时启用",
  "masked-runtime": "临时屏蔽",
  up: "已连接",
  down: "未连接",
  unknown: "未知",
  dormant: "休眠",
  lowerlayerdown: "底层链路断开",
  LAN: "局域网",
  "Public candidate": "可能面向公网",
  Loopback: "回环接口",
  WireGuard: "WireGuard 隧道",
  Docker: "Docker 容器网络",
  VPN: "VPN 隧道",
  device: "物理设备",
  bridge: "网桥",
  veth: "虚拟以太网",
  tun: "隧道",
  wireguard: "WireGuard 隧道",
  success: "成功",
  failure: "失败",
  rejected: "已拒绝",
  started: "已开始",
  pending: "待处理",
  any: "不限",
  ipv4: "IPv4",
  ipv6: "IPv6",
  all: "全部",
  info: "信息",
  warning: "警告",
  error: "错误",
  "Manual backup": "手动备份",
  "Save & Apply": "保存并应用前的配置",
  "Managed configuration": "管理基线配置",
  "Initial takeover": "首次接管配置",
  "Reload from disk": "接受磁盘配置",
};
export function displayValue(value?: string): string {
  return value ? (states[value] ?? value) : "—";
}
export const sectionLabels: Record<string, string> = {
  server: "服务器",
  "wide-area": "广域 DNS-SD",
  publish: "服务发布",
  reflector: "mDNS 反射器",
  rlimits: "资源限制",
};
export const settingLabels: Record<string, string> = {
  "host-name": "主机名",
  "domain-name": "域名",
  "host-name-from-machine-id": "使用机器标识生成主机名",
  "browse-domains": "浏览域名列表",
  "use-ipv4": "启用 IPv4",
  "use-ipv6": "启用 IPv6",
  "allow-interfaces": "允许的网络接口",
  "deny-interfaces": "禁止的网络接口",
  "check-response-ttl": "检查响应 TTL",
  "use-iff-running": "检查接口运行标志",
  "enable-dbus": "启用 D-Bus",
  "disallow-other-stacks": "禁止其他 mDNS 协议栈",
  "allow-point-to-point": "允许点对点接口",
  "cache-entries-max": "最大缓存条目数",
  "clients-max": "最大客户端数",
  "objects-per-client-max": "每个客户端最大对象数",
  "entries-per-entry-group-max": "每组最大记录数",
  "ratelimit-interval-usec": "速率限制间隔（微秒）",
  "ratelimit-burst": "速率限制突发数量",
  "enable-wide-area": "启用广域 DNS-SD",
  "disable-publishing": "启用服务发布",
  "disable-user-service-publishing": "禁止用户发布服务",
  "add-service-cookie": "添加服务 Cookie",
  "publish-addresses": "发布主机地址",
  "publish-hinfo": "发布主机硬件信息",
  "publish-workstation": "发布工作站服务",
  "publish-domain": "发布域名",
  "publish-dns-servers": "发布指定 DNS 服务器",
  "publish-resolv-conf-dns-servers": "发布 resolv.conf 中的 DNS 服务器",
  "publish-aaaa-on-ipv4": "通过 IPv4 发布 IPv6 地址记录",
  "publish-a-on-ipv6": "通过 IPv6 发布 IPv4 地址记录",
  "enable-reflector": "启用 mDNS 反射器",
  "reflect-ipv": "跨 IP 协议反射",
  "reflect-filters": "反射过滤规则",
  "rlimit-as": "虚拟地址空间上限",
  "rlimit-core": "核心转储大小上限",
  "rlimit-data": "数据段大小上限",
  "rlimit-fsize": "文件大小上限",
  "rlimit-nofile": "打开文件数量上限",
  "rlimit-stack": "栈大小上限",
  "rlimit-nproc": "进程数量上限",
};
export const settingHelp: Record<string, string> = {
  "enable-reflector":
    "会在接口之间转发 mDNS，可能暴露原本隔离网络的设备信息。仅在明确需要且网络可信时启用。",
  "enable-dbus":
    "面板依赖 D-Bus 获取状态及发现服务；禁用后相关功能将不可用。warn 表示连接失败时仅警告。",
  "disable-publishing":
    "此处“是”表示允许发布，写入时自动转换为 disable-publishing=no。",
  "allow-interfaces": "接口名用英文逗号分隔；留空不是禁用所有接口。",
  "deny-interfaces":
    "禁止列表优先于允许列表；网络接口页保存白名单时会清除此项。",
  "host-name":
    "单段主机名，仅使用英文字母、数字和连字符，例如 nas；不要填写 .local 后缀。",
  "domain-name": "通常使用 local。修改前请确认客户端的域名解析需求。",
  "reflect-ipv": "跨 IPv4 / IPv6 转发可能扩大服务可见范围，请谨慎启用。",
  "publish-hinfo": "可能向局域网公开主机硬件与操作系统信息。",
};

// Preserve original diagnostics for support while adding actionable Chinese context.
export function errorHint(message: string): string {
  if (/helper.*sock|dial unix|http:\/\/helper/i.test(message)) {
    if (/no such file or directory/i.test(message))
      return "特权 Helper 套接字不存在：请先启动 Helper，并确认 Web 与 Helper 的 socket 路径一致。不要手动创建空 socket 文件。";
    if (/permission denied/i.test(message))
      return "无法访问 Helper：请检查 Web 运行账户、套接字所属组及目录权限，不要将权限改为 777。";
    return "无法连接特权 Helper：请检查进程是否运行、套接字路径是否一致，并查看 Helper 日志。";
  }
  if (/failed to fetch|networkerror|load failed/i.test(message))
    return "网络请求失败，请检查面板服务和网络连接后重试。";
  return "";
}
