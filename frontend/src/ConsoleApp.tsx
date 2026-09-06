import { useCallback, useEffect, useRef, useState } from "react";
import {
  Alert,
  App,
  Avatar,
  Badge,
  Breadcrumb,
  Button,
  Card,
  ConfigProvider,
  Drawer,
  Form,
  Grid,
  Input,
  Layout,
  Menu,
  Skeleton,
  Space,
  Tag,
} from "antd";
import {
  ApartmentOutlined,
  CloudServerOutlined,
  DashboardOutlined,
  DatabaseOutlined,
  FileSearchOutlined,
  GlobalOutlined,
  LockOutlined,
  LogoutOutlined,
  MenuFoldOutlined,
  MenuOutlined,
  MenuUnfoldOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
  ToolOutlined,
  UserOutlined,
} from "@ant-design/icons";
import {
  api,
  APIError,
  setSession,
  type Session,
  type Config,
  type Discovery,
} from "./api";
import {
  Dashboard,
  DiscoveryPage,
  Services,
  Hosts,
  Interfaces,
  Settings,
  Logs,
  Maintenance,
} from "./pages";
import { DirtyContext, Message, useConfirm } from "./ui";
const pages = [
  "Dashboard",
  "Discovery",
  "Services",
  "Hosts",
  "Interfaces",
  "Settings",
  "Logs",
  "Maintenance",
] as const;
type Page = (typeof pages)[number];
const labels: Record<Page, string> = {
  Dashboard: "概览",
  Discovery: "服务发现",
  Services: "服务发布",
  Hosts: "静态主机",
  Interfaces: "网络接口",
  Settings: "配置设置",
  Logs: "日志",
  Maintenance: "维护",
};
const descriptions: Record<Page, string> = {
  Dashboard: "本机运行状态、配置健康度与局域网服务，一站式掌握。",
  Discovery:
    "实时发现 mDNS / DNS-SD 服务。此页面只读，不会发布或修改网络中的服务。",
  Services:
    "管理持久化服务声明。保存会重新加载服务声明，但不会启动实际应用，请确认目标端口已有服务监听。",
  Hosts:
    "维护 Avahi 静态主机记录。保存后会重启 Avahi，不会修改系统 /etc/hosts。",
  Interfaces:
    "控制参与 mDNS 的网络接口。保存后重启 Avahi，服务发现可能短暂中断。",
  Settings: "管理 Avahi 原生配置。保存会写入配置并重启守护进程，请先确认影响。",
  Logs: "检索与跟踪 Avahi 系统日志，时间按浏览器本地时区显示。",
  Maintenance: "服务控制、配置快照与操作审计。修改操作依赖特权 Helper。",
};
const icons = [
  <DashboardOutlined />,
  <GlobalOutlined />,
  <CloudServerOutlined />,
  <DatabaseOutlined />,
  <ApartmentOutlined />,
  <SettingOutlined />,
  <FileSearchOutlined />,
  <ToolOutlined />,
];
export function ConsoleApp() {
  const { message } = App.useApp(),
    confirm = useConfirm(),
    screens = Grid.useBreakpoint();
  const [session, sessionState] = useState<Session | null>(null),
    [ready, setReady] = useState(false);
  const [page, setPage] = useState<Page>(
      () =>
        pages.find((p) => p.toLowerCase() === location.hash.slice(1)) ??
        "Dashboard",
    ),
    [collapsed, setCollapsed] = useState(false),
    [mobileOpen, setMobileOpen] = useState(false);
  const [config, setConfig] = useState<Config | null>(null),
    [discovery, setDiscovery] = useState<Discovery>({
      services: [],
      connected: false,
    });
  const [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false),
    [refreshing, setRefreshing] = useState(false),
    [available, setAvailable] = useState(false),
    [refreshTick, setRefreshTick] = useState(0),
    [editEpoch, setEditEpoch] = useState(0);
  const operation = useRef(false),
    dirtyFields = useRef(new Set<string>()),
    generation = useRef(0);
  const [dirty, setDirty] = useState(false);
  const reportDirty = useCallback((id: string, value: boolean) => {
    value ? dirtyFields.current.add(id) : dirtyFields.current.delete(id);
    setDirty(dirtyFields.current.size > 0);
  }, []);
  const establish = useCallback((s: Session | null) => {
    generation.current++;
    setSession(s);
    sessionState(s);
    setConfig(null);
    setAvailable(false);
    setError("");
    setNotice("");
    setRefreshing(false);
    setDiscovery({ services: [], connected: false });
  }, []);
  const refresh = useCallback(async () => {
    const requestGeneration = generation.current;
    setRefreshing(true);
    let ok = false;
    try {
      const next = await api<Config>("/config");
      if (requestGeneration === generation.current) {
        setConfig(next);
        setAvailable(true);
        setError("");
        ok = true;
      }
    } catch (e) {
      if (requestGeneration === generation.current) {
        setError(String(e));
        setAvailable(false);
      }
    } finally {
      if (requestGeneration === generation.current) {
        setRefreshing(false);
        setRefreshTick((n) => n + 1);
      }
    }
    return ok;
  }, []);
  useEffect(() => {
    api<Session>("/auth/session")
      .then(establish)
      .catch((e) => {
        if (!(e instanceof APIError && e.status === 401)) setError(String(e));
      })
      .finally(() => setReady(true));
    const expired = () => establish(null);
    window.addEventListener("session-expired", expired);
    return () => window.removeEventListener("session-expired", expired);
  }, [establish]);
  useEffect(() => {
    if (!session) return;
    let active = true;
    void refresh();
    const d = new EventSource("/api/v1/discovery/events"),
      c = new EventSource("/api/v1/config/events");
    d.addEventListener("discovery", (e) => {
      if (active) {
        try {
          setDiscovery(JSON.parse((e as MessageEvent).data));
        } catch {
          setError("无法解析服务发现事件，请刷新重试。");
        }
      }
    });
    d.onerror = () => {
      if (active)
        setDiscovery((old) => ({
          ...old,
          connected: false,
          error: "服务发现连接中断，正在重新连接；当前列表可能不是最新状态。",
        }));
    };
    c.addEventListener("configuration", (e) => {
      if (!active) return;
      try {
        const data = JSON.parse((e as MessageEvent).data);
        if (data.revision) {
          setConfig(data);
          setAvailable(true);
        } else if (data.message) {
          setError(data.message);
          setAvailable(false);
        }
      } catch {
        setError("无法解析配置事件，请刷新重试。");
      }
    });
    return () => {
      active = false;
      d.close();
      c.close();
    };
  }, [session, refresh]);
  useEffect(() => {
    const beforeUnload = (e: BeforeUnloadEvent) => {
      if (dirtyFields.current.size || operation.current) {
        e.preventDefault();
        e.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", beforeUnload);
    return () => window.removeEventListener("beforeunload", beforeUnload);
  }, []);
  async function canLeave() {
    if (operation.current) {
      void message.warning("操作正在执行，请等待完成后再切换页面。");
      return false;
    }
    return (
      dirtyFields.current.size === 0 ||
      (await confirm(
        "离开或刷新将丢失尚未保存的修改。是否继续？",
        "存在未保存的修改",
      ))
    );
  }
  async function navigate(next: Page) {
    if (next !== page && !(await canLeave())) return;
    setPage(next);
    window.history.replaceState(null, "", "#" + next.toLowerCase());
    setMobileOpen(false);
    setNotice("");
  }
  async function perform(work: () => Promise<unknown>, text = "更改已应用") {
    if (operation.current) throw new Error("操作正在执行，请勿重复提交。");
    operation.current = true;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await work();
      const refreshed = await refresh();
      setNotice(
        refreshed
          ? text
          : "操作已提交，但最新配置读取失败。请核对当前状态后再操作。",
      );
    } catch (e) {
      setError(String(e));
      void message.error("操作未完成，请查看错误详情。");
      throw e;
    } finally {
      operation.current = false;
      setBusy(false);
    }
  }
  const run = (work: () => Promise<unknown>, text?: string) => {
    void perform(work, text).catch(() => {});
  };
  const menu = (
    <Menu
      theme="dark"
      mode="inline"
      selectedKeys={[page]}
      items={pages.map((p, i) => ({
        key: p,
        icon: <span aria-hidden="true">{icons[i]}</span>,
        label: labels[p],
      }))}
      onClick={({ key }) => void navigate(key as Page)}
    />
  );
  if (!ready)
    return (
      <div className="boot-screen">
        <Card>
          <Skeleton active />
          <p>正在连接 Avahi 管理面板…</p>
        </Card>
      </div>
    );
  if (!session) return <Login onLogin={establish} initialError={error} />;
  const blocked = busy || !available;
  return (
    <DirtyContext.Provider value={reportDirty}>
      <Layout className="console-layout">
        {screens.lg && (
          <Layout.Sider
            className="console-sider"
            width={232}
            collapsed={collapsed}
            theme="dark"
          >
            <a
              className="brand"
              href="#dashboard"
              onClick={(e) => {
                e.preventDefault();
                void navigate("Dashboard");
              }}
              aria-label="Avahi 管理面板首页"
            >
              <span className="brand-mark">
                <ApartmentOutlined />
              </span>
              {!collapsed && (
                <span>
                  Avahi <small>网络服务管理</small>
                </span>
              )}
            </a>
            {!collapsed && <div className="nav-caption">管理控制台</div>}
            {menu}
            {!collapsed && (
              <div className="sider-status">
                <Badge
                  status={discovery.connected ? "success" : "warning"}
                  text={discovery.connected ? "服务发现已连接" : "服务发现离线"}
                />
                <small>Linux · 原生配置 · D-Bus</small>
              </div>
            )}
          </Layout.Sider>
        )}
        <Drawer
          title="Avahi 管理面板"
          placement="left"
          open={mobileOpen}
          onClose={() => setMobileOpen(false)}
          size={280}
          className="navigation-drawer"
        >
          {menu}
        </Drawer>
        <Layout className="console-workspace">
          <Layout.Header className="console-header">
            <Space size="middle">
              <Button
                type="text"
                aria-label={screens.lg ? "折叠导航" : "打开导航"}
                icon={
                  screens.lg ? (
                    collapsed ? (
                      <MenuUnfoldOutlined />
                    ) : (
                      <MenuFoldOutlined />
                    )
                  ) : (
                    <MenuOutlined />
                  )
                }
                onClick={() =>
                  screens.lg ? setCollapsed(!collapsed) : setMobileOpen(true)
                }
              />
              <Breadcrumb
                items={[{ title: "控制台" }, { title: labels[page] }]}
              />
            </Space>
            <Space>
              <Tag
                className="desktop-only"
                icon={<SafetyCertificateOutlined />}
              >
                管理员会话
              </Tag>
              <Avatar size="small" icon={<UserOutlined />} />
              <span className="account-name">{session.username}</span>
              <Button
                type="text"
                aria-label="退出登录"
                disabled={busy}
                icon={<LogoutOutlined />}
                onClick={async () => {
                  if (!(await canLeave())) return;
                  try {
                    await api("/auth/logout", "POST", {});
                    establish(null);
                  } catch (e) {
                    setError(String(e));
                  }
                }}
              />
            </Space>
          </Layout.Header>
          <Layout.Content className="console-content">
            <div className="page-heading">
              <div>
                <Space>
                  <h1>{labels[page]}</h1>
                  {dirty && <Tag color="warning">未保存修改</Tag>}
                </Space>
                <p>{descriptions[page]}</p>
              </div>
              <Button
                icon={<ReloadOutlined />}
                loading={refreshing}
                aria-label="刷新"
                disabled={busy}
                onClick={async () => {
                  if (!(await canLeave())) return;
                  setEditEpoch((n) => n + 1);
                  await refresh();
                }}
              >
                刷新
              </Button>
            </div>
            {busy && (
              <Alert
                className="feedback"
                showIcon
                type="info"
                title="正在执行操作，请勿关闭页面或重复提交。"
              />
            )}
            <Message error={error} />
            {notice && (
              <div role="status">
                <Alert
                  className="feedback"
                  type={available ? "success" : "warning"}
                  showIcon
                  title={notice}
                  closable
                  onClose={() => setNotice("")}
                />
              </div>
            )}
            {!!config?.drift.length && (
              <Alert
                className="feedback"
                type="warning"
                showIcon
                title="检测到面板之外的配置修改"
                description={
                  <>
                    <p>
                      “接受磁盘配置”仅更新管理基线，不会重新加载
                      Avahi。“恢复管理版本”将覆盖当前受管配置。
                    </p>
                    <p>{config.drift.map((d) => d.path).join("、")}</p>
                    <Space wrap>
                      <Button
                        disabled={busy}
                        onClick={() => void navigate("Settings")}
                      >
                        查看配置
                      </Button>
                      <Button
                        disabled={blocked || dirty}
                        onClick={() =>
                          run(
                            () =>
                              api("/config/reload", "POST", {
                                revision: config.revision,
                              }),
                            "已将磁盘配置接受为管理基线",
                          )
                        }
                      >
                        接受磁盘配置
                      </Button>
                      <Button
                        danger
                        disabled={blocked || dirty}
                        onClick={async () => {
                          if (
                            await confirm(
                              "恢复上次管理版本会覆盖当前受管配置，并应用到 Avahi。服务发现可能短暂中断。",
                              "恢复管理版本",
                            )
                          )
                            run(() =>
                              api(
                                `/snapshots/${config.managedSnapshot}/restore`,
                                "POST",
                                { revision: config.revision },
                              ),
                            );
                        }}
                      >
                        恢复管理版本
                      </Button>
                    </Space>
                  </>
                }
              />
            )}
            {config?.errors.map((e, i) => (
              <Message key={i} error={e} />
            ))}
            <ConfigProvider componentDisabled={busy}>
              {page === "Dashboard" && (
                <Dashboard
                  key={refreshTick}
                  config={config}
                  discovery={discovery}
                />
              )}
              {page === "Discovery" && <DiscoveryPage state={discovery} />}
              {page === "Services" && config && (
                <Services
                  key={editEpoch}
                  config={config}
                  busy={blocked}
                  perform={perform}
                />
              )}
              {page === "Hosts" && config && (
                <Hosts
                  key={editEpoch}
                  config={config}
                  busy={blocked}
                  perform={perform}
                />
              )}
              {page === "Interfaces" && config && (
                <Interfaces
                  key={refreshTick}
                  config={config}
                  busy={blocked}
                  run={run}
                />
              )}
              {page === "Settings" && config && (
                <Settings
                  key={editEpoch}
                  config={config}
                  busy={blocked}
                  run={run}
                />
              )}
              {page === "Logs" && <Logs refreshTick={refreshTick} />}
              {page === "Maintenance" && (
                <Maintenance
                  key={refreshTick}
                  config={config}
                  busy={busy}
                  run={run}
                />
              )}
            </ConfigProvider>
            {!config &&
              ["Services", "Hosts", "Interfaces", "Settings"].includes(page) &&
              (refreshing ? (
                <Card>
                  <Skeleton active paragraph={{ rows: 5 }} />
                </Card>
              ) : (
                <Card>
                  <Alert
                    showIcon
                    type="warning"
                    title="配置不可用"
                    description="配置不可用。请确认 Avahi 已安装，特权 Helper 已启动，且 Web 与 Helper 使用同一套接字路径。可在“维护”页查看状态。"
                  />
                  <Button
                    className="retry-button"
                    icon={<ReloadOutlined />}
                    onClick={() => void refresh()}
                  >
                    重新获取配置
                  </Button>
                </Card>
              ))}
            <footer className="console-footer">
              <span>Avahi Manager · 原生网络服务管理</span>
              <span>开发预览 · 尚未完成生产验收</span>
            </footer>
          </Layout.Content>
        </Layout>
      </Layout>
    </DirtyContext.Provider>
  );
}
function Login({
  onLogin,
  initialError,
}: {
  onLogin: (s: Session) => void;
  initialError: string;
}) {
  const [form] = Form.useForm(),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(initialError);
  return (
    <div className="login-layout">
      <section className="login-story">
        <div className="login-brand">
          <ApartmentOutlined /> Avahi Manager
        </div>
        <Tag color="blue">网络服务管理平台</Tag>
        <h1>
          让局域网服务
          <br />
          清晰、可见、可管理。
        </h1>
        <p>
          从实时发现到持久化发布，通过原生 Linux 接口统一管理
          Avahi，保留配置控制权与操作记录。
        </p>
        <div className="login-capabilities">
          <div>
            <GlobalOutlined />
            <span>
              实时服务发现<small>mDNS / DNS-SD 网络视图</small>
            </span>
          </div>
          <div>
            <DatabaseOutlined />
            <span>
              持久化配置<small>原生文件与版本冲突保护</small>
            </span>
          </div>
          <div>
            <SafetyCertificateOutlined />
            <span>
              可追踪的变更<small>配置快照与操作审计</small>
            </span>
          </div>
        </div>
        <small>开发预览版本 · 请在可信网络中使用</small>
      </section>
      <div className="login-panel">
        <Card className="login-card">
          <div className="login-icon">
            <LockOutlined />
          </div>
          <h2>欢迎登录</h2>
          <p>使用管理员账户进入管理控制台</p>
          <Message error={error} />
          <Form
            form={form}
            layout="vertical"
            initialValues={{ username: "admin" }}
            onFinish={async (values) => {
              if (busy) return;
              setBusy(true);
              setError("");
              try {
                onLogin(await api<Session>("/auth/login", "POST", values));
              } catch (e) {
                setError(String(e));
                form.setFieldValue("password", "");
              } finally {
                setBusy(false);
              }
            }}
          >
            <Form.Item
              name="username"
              label="用户名"
              rules={[{ required: true, message: "请输入管理员用户名" }]}
            >
              <Input
                prefix={<UserOutlined />}
                autoComplete="username"
                size="large"
                disabled={busy}
              />
            </Form.Item>
            <Form.Item
              name="password"
              label="密码"
              rules={[{ required: true, message: "请输入密码" }]}
            >
              <Input.Password
                prefix={<LockOutlined />}
                autoComplete="current-password"
                size="large"
                disabled={busy}
              />
            </Form.Item>
            <Button
              block
              type="primary"
              htmlType="submit"
              size="large"
              loading={busy}
            >
              登录
            </Button>
          </Form>
          <Alert
            type="info"
            showIcon
            className="login-help"
            title="首次使用？"
            description={
              <>
                没有默认密码。请在服务器使用 <code>init-admin</code>{" "}
                创建管理员；已有账户无需重复初始化。
              </>
            }
          />
        </Card>
        <p className="login-security">
          <LockOutlined /> 会话认证 · 同源保护 · 操作审计
        </p>
      </div>
    </div>
  );
}
