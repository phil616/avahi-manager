import { useResource } from "./useResource";
import React, { useEffect, useState } from "react";
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Collapse,
  Drawer,
  Input,
  Select,
  Skeleton,
  Space,
  Statistic,
  Switch,
  Tag,
} from "antd";
import { SearchOutlined } from "@ant-design/icons";
import {
  api,
  date,
  type Config,
  type Group,
  type Host,
  type Discovery,
  type Found,
  type Service,
} from "./api";
import {
  actionLabels,
  actionHelp,
  displayValue,
  sectionLabels,
  settingLabels,
  settingHelp,
} from "./zh";
import {
  DataTable,
  Empty,
  Field,
  Message,
  StateTag,
  useConfirm,
  useDirty,
  type Run,
  type Perform,
} from "./ui";
const blankService = (): Service => ({
  type: "_http._tcp",
  protocol: "any",
  port: 80,
  subtypes: [],
  txt: [],
});
const blankGroup = (): Group => ({
  name: { value: "", replaceWildcards: "no" },
  services: [blankService()],
});
const foundKey = (s: Found) =>
  JSON.stringify([s.interface, s.protocol, s.name, s.type, s.domain]);
export function Dashboard({
  config,
  discovery,
}: {
  config: Config | null;
  discovery: Discovery;
}) {
  const {
    data: status,
    error,
    loading,
    retry,
  } = useResource<any>("/avahi/status", config?.revision);
  if (loading && !status)
    return (
      <Card className="card">
        <Skeleton active paragraph={{ rows: 5 }} />
      </Card>
    );
  return (
    <>
      <Message error={error} />
      {error && <Button onClick={retry}>重试获取状态</Button>}
      <Card className="hero">
        <div>
          <span className="eyebrow">守护进程状态</span>
          <h2>{status ? displayValue(status.state) : "正在检查…"}</h2>
          <p>{status?.avahi?.hostname ?? "无法获取主机名"}</p>
        </div>
        <span className="hero-icon">◉</span>
      </Card>
      <div className="stats">
        <Card>
          <Statistic
            title="已发布服务"
            value={config?.services.length ?? "—"}
          />
          <small>持久化服务组</small>
        </Card>
        <Card>
          <Statistic title="已发现服务" value={discovery.services.length} />
          <small>
            {discovery.connected ? "实时局域网发现" : "服务发现已断开"}
          </small>
        </Card>
        <Card>
          <Statistic
            title="配置"
            value={
              config
                ? config.drift.length
                  ? "存在外部修改"
                  : "已纳入管理"
                : "不可用"
            }
          />
          <small>Avahi 原生配置文件</small>
        </Card>
      </div>
      <Card className="card">
        <h2>主机概况</h2>
        <dl className="details">
          <dt>版本</dt>
          <dd>{status?.avahi?.version || "—"}</dd>
          <dt>域名</dt>
          <dd>{status?.avahi?.domain || "—"}</dd>
          <dt>服务</dt>
          <dd>{displayValue(status?.systemd?.service?.activeState)}</dd>
          <dt>套接字</dt>
          <dd>{displayValue(status?.systemd?.socket?.activeState)}</dd>
          <dt>开机启动</dt>
          <dd>{displayValue(status?.systemd?.service?.unitFileState)}</dd>
          <dt>D-Bus</dt>
          <dd>
            {status?.avahi?.available
              ? "正常"
              : status?.avahi?.error || "不可用"}
          </dd>
        </dl>
      </Card>
    </>
  );
}

export function DiscoveryPage({ state }: { state: Discovery }) {
  const [search, setSearch] = useState(""),
    [protocol, setProtocol] = useState("all"),
    [type, setType] = useState("all"),
    [iface, setIface] = useState("all"),
    [selectedKey, selectKey] = useState<string | null>(null);
  const selected = state.services.find((s) => foundKey(s) === selectedKey);
  const select = (s: Found | null) => selectKey(s ? foundKey(s) : null);
  const rows = state.services.filter(
    (s) =>
      (protocol === "all" || s.protocol === Number(protocol)) &&
      (iface === "all" || s.interface === Number(iface)) &&
      (type === "all" || s.type === type) &&
      `${s.name} ${s.host} ${s.address}`
        .toLowerCase()
        .includes(search.toLowerCase()),
  );
  return (
    <>
      <Message error={state.error ?? ""} />
      <div className="toolbar">
        <Input
          placeholder="搜索服务、主机名或地址"
          aria-label="搜索发现的服务"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <Select
          aria-label="IP 协议"
          value={protocol}
          onChange={(selectedValue) => setProtocol(selectedValue)}
        >
          <Select.Option value="all">全部协议</Select.Option>
          <Select.Option value="0">IPv4</Select.Option>
          <Select.Option value="1">IPv6</Select.Option>
        </Select>
        <Select
          aria-label="服务类型"
          value={type}
          onChange={(selectedValue) => setType(selectedValue)}
        >
          <Select.Option value="all">全部服务类型</Select.Option>
          {[...new Set(state.services.map((s) => s.type))].sort().map((t) => (
            <Select.Option key={t} value={t}>
              {t}
            </Select.Option>
          ))}
        </Select>
        <Select
          aria-label="网络接口"
          value={iface}
          onChange={(selectedValue) => setIface(selectedValue)}
        >
          <Select.Option value="all">全部接口</Select.Option>
          {[...new Set(state.services.map((s) => s.interface))]
            .sort((a, b) => a - b)
            .map((i) => (
              <Select.Option key={i} value={i}>
                接口 {i}
              </Select.Option>
            ))}
        </Select>
      </div>
      <Card className="card table-wrap">
        <DataTable
          dataSource={rows}
          rowKey={foundKey}
          locale={{
            emptyText: (
              <Empty>
                没有匹配的服务，发现结果会自动更新。请确认设备位于可通信的局域网，且目标正在广播服务。
              </Empty>
            ),
          }}
          columns={[
            {
              title: "名称",
              key: "0",
              sorter: (a, b) => a.name.localeCompare(b.name),
              render: (_, s) => (
                <>
                  <Button type="link" onClick={() => select(s)}>
                    {s.name}
                  </Button>
                </>
              ),
            },
            {
              title: "类型",
              key: "1",
              render: (_, s) => (
                <>
                  <code>{s.type}</code>
                </>
              ),
            },
            {
              title: "主机名",
              key: "2",
              render: (_, s) => <>{s.host || "正在解析…"}</>,
            },
            {
              title: "地址",
              key: "3",
              render: (_, s) => (
                <>
                  <code>{s.address || "—"}</code>
                </>
              ),
            },
            { title: "端口", key: "4", render: (_, s) => <>{s.port}</> },
          ]}
        />
      </Card>
      {selectedKey && (
        <Drawer
          open
          title={selected?.name ?? "服务已离线"}
          onClose={() => select(null)}
          size={600}
        >
          {!selected ? (
            <Empty>此服务已从发现列表移除。</Empty>
          ) : (
            <>
              <div className="section-heading">
                <h2>{selected.name}</h2>
                <Button onClick={() => select(null)}>关闭</Button>
              </div>
              <Message error={selected.error ?? ""} />
              <dl className="details">
                {Object.entries({
                  类型: selected.type,
                  域名: selected.domain,
                  网络接口: selected.interface,
                  "IP 协议": selected.protocol === 0 ? "IPv4" : "IPv6",
                  主机名: selected.host,
                  地址: selected.address,
                  端口: selected.port,
                  首次发现: date(selected.firstSeen),
                  最近发现: date(selected.lastSeen),
                }).map(([k, v]) => (
                  <React.Fragment key={k}>
                    <dt>{k}</dt>
                    <dd>{v || "—"}</dd>
                  </React.Fragment>
                ))}
              </dl>
              <h3>TXT 记录</h3>
              <pre>
                {selected.txt
                  ?.map((t) => {
                    try {
                      return new TextDecoder().decode(
                        Uint8Array.from(atob(t), (c) => c.charCodeAt(0)),
                      );
                    } catch {
                      return t;
                    }
                  })
                  .join("\n") || "无 TXT 记录"}
              </pre>
            </>
          )}
        </Drawer>
      )}
    </>
  );
}

export function Services({
  config,
  busy,
  perform,
}: {
  config: Config;
  busy: boolean;
  perform: Perform;
}) {
  const confirm = useConfirm();
  const [search, setSearch] = useState("");
  const [saveError, setSaveError] = useState("");
  const [edit, setEdit] = useState<{
    id?: string;
    revision: string;
    group: Group;
  } | null>(null);
  useDirty(edit !== null);
  function update(g: Group) {
    setEdit((e) => (e ? { ...e, group: g } : e));
  }
  return (
    <>
      <div className="section-heading">
        <Input
          className="table-search"
          prefix={<SearchOutlined />}
          aria-label="搜索列表"
          placeholder="按名称或地址搜索"
          allowClear
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <span>{config.services.length} 个服务组</span>
        <Button
          type="primary"
          onClick={async () =>
            setEdit({ revision: config.revision, group: blankGroup() })
          }
        >
          + 添加服务
        </Button>
      </div>
      {edit && (
        <Drawer
          open
          title={edit.id ? "编辑服务组" : "新建服务组"}
          size={760}
          onClose={async () => {
            if (
              !busy &&
              (await confirm("放弃尚未保存的表单内容？", "放弃修改"))
            )
              setEdit(null);
          }}
        >
          <Message error={saveError} />

          <form
            onSubmit={(e) => {
              e.preventDefault();
              void perform(() =>
                api(
                  edit.id ? `/services/${edit.id}` : "/services",
                  edit.id ? "PUT" : "POST",
                  edit,
                ),
              )
                .then(() => setEdit(null))
                .catch((e) => setSaveError(String(e)));
            }}
          >
            <Field>
              显示名称
              <Input
                required
                maxLength={63}
                value={edit.group.name.value}
                onChange={(e) =>
                  update({
                    ...edit.group,
                    name: { ...edit.group.name, value: e.target.value },
                  })
                }
              />
            </Field>
            <Field className="check">
              <Checkbox
                checked={edit.group.name.replaceWildcards === "yes"}
                onChange={(e) =>
                  update({
                    ...edit.group,
                    name: {
                      ...edit.group.name,
                      replaceWildcards: e.target.checked ? "yes" : "no",
                    },
                  })
                }
              />
              将名称中的 %h 替换为主机名
            </Field>
            {edit.group.services.map((service, i) => (
              <ServiceEditor
                key={i}
                value={service}
                onChange={(v) =>
                  update({
                    ...edit.group,
                    services: edit.group.services.map((s, j) =>
                      j === i ? v : s,
                    ),
                  })
                }
                onRemove={() =>
                  update({
                    ...edit.group,
                    services: edit.group.services.filter((_, j) => j !== i),
                  })
                }
                removable={edit.group.services.length > 1}
              />
            ))}
            <div className="actions">
              <Button
                htmlType="button"
                onClick={() =>
                  update({
                    ...edit.group,
                    services: [...edit.group.services, blankService()],
                  })
                }
              >
                + 添加协议 / 服务
              </Button>
              <Button htmlType="submit" type="primary" loading={busy}>
                保存并应用
              </Button>
              <Button
                htmlType="button"
                onClick={async () => {
                  if (await confirm("放弃尚未保存的表单内容？", "放弃修改"))
                    setEdit(null);
                }}
              >
                取消
              </Button>
            </div>
          </form>
        </Drawer>
      )}
      <Card className="card table-wrap">
        <DataTable
          dataSource={config.services.filter((s) =>
            `${s.group.name?.value} ${s.filename} ${s.group.services?.map((x) => x.type).join(" ")}`
              .toLowerCase()
              .includes(search.toLowerCase()),
          )}
          rowKey={"id"}
          locale={{ emptyText: <Empty>暂无匹配的持久化服务。</Empty> }}
          columns={[
            {
              title: "名称",
              key: "0",
              render: (_, s) => (
                <>
                  {s.group.name?.value || s.filename}
                  {s.error && <Message error={s.error} />}
                </>
              ),
            },
            {
              title: "服务类型",
              key: "1",
              render: (_, s) => (
                <>
                  {s.group.services?.map((x) => (
                    <div key={x.type + x.port}>
                      <code>{x.type}</code> ·{" "}
                      {displayValue(x.protocol || "any")}
                    </div>
                  ))}
                </>
              ),
            },
            {
              title: "端口",
              key: "2",
              render: (_, s) => (
                <>{s.group.services?.map((x) => x.port).join(", ")}</>
              ),
            },
            {
              title: "操作",
              key: "3",
              width: 230,
              render: (_, s) => (
                <>
                  <div className="actions">
                    <Button
                      onClick={() =>
                        setEdit({
                          id: s.id,
                          revision: config.revision,
                          group: {
                            ...structuredClone(s.group),
                            services: s.group.services?.length
                              ? structuredClone(s.group.services)
                              : [blankService()],
                          },
                        })
                      }
                    >
                      编辑
                    </Button>
                    <Button
                      danger
                      disabled={busy}
                      onClick={async () => {
                        if (
                          await confirm(
                            `删除服务组“${s.group.name?.value || s.filename}”？这会移除其持久化声明并重新加载服务。`,
                          )
                        )
                          void perform(() =>
                            api(`/services/${s.id}`, "DELETE", {
                              revision: config.revision,
                            }),
                          ).catch(() => {});
                      }}
                    >
                      删除
                    </Button>
                  </div>
                </>
              ),
            },
          ]}
        />
      </Card>
    </>
  );
}

function ServiceEditor({
  value: s,
  onChange,
  removable,
  onRemove,
}: {
  value: Service;
  onChange: (v: Service) => void;
  removable: boolean;
  onRemove: () => void;
}) {
  return (
    <fieldset>
      <legend>服务定义</legend>
      <p>
        服务类型使用 _http._tcp 等 DNS-SD
        标识，端口应与实际应用一致。主机名留空使用本机，域名留空使用默认域。TXT
        记录用于描述服务，请勿填写密码或密钥；单条编码后的记录不得超过 255
        字节，通常选择文本格式。
      </p>
      <div className="form-grid">
        <Field>
          服务类型
          <Input
            required
            placeholder="_http._tcp"
            value={s.type}
            onChange={(e) => onChange({ ...s, type: e.target.value })}
          />
        </Field>
        <Field>
          IP 协议
          <Select
            value={s.protocol || "any"}
            onChange={(selectedValue) =>
              onChange({ ...s, protocol: selectedValue })
            }
          >
            <Select.Option value="any">不限（IPv4 / IPv6）</Select.Option>
            <Select.Option value="ipv4">IPv4</Select.Option>
            <Select.Option value="ipv6">IPv6</Select.Option>
          </Select>
        </Field>
        <Field>
          端口
          <Input
            type="number"
            required
            min={0}
            max={65535}
            value={s.port}
            onChange={(e) => onChange({ ...s, port: Number(e.target.value) })}
          />
        </Field>
        <Field>
          主机名（可选，完整域名）
          <Input
            value={s.host ?? ""}
            onChange={(e) => onChange({ ...s, host: e.target.value })}
          />
        </Field>
        <Field>
          域名（可选）
          <Input
            value={s.domain ?? ""}
            onChange={(e) => onChange({ ...s, domain: e.target.value })}
          />
        </Field>
        <Field>
          子类型（每行一个）
          <Input.TextArea
            value={s.subtypes?.join("\n") ?? ""}
            onChange={(e) =>
              onChange({
                ...s,
                subtypes: e.target.value.split("\n").filter(Boolean),
              })
            }
          />
        </Field>
      </div>
      <h3>TXT 记录</h3>
      {s.txt?.map((t, i) => {
        const split = t.value.indexOf("="),
          key = split < 0 ? t.value : t.value.slice(0, split),
          val = split < 0 ? "" : t.value.slice(split + 1);
        return (
          <div className="txt-row" key={i}>
            <Input
              aria-label="TXT 键"
              placeholder="键"
              required
              value={key}
              onChange={(e) =>
                onChange({
                  ...s,
                  txt: s.txt.map((x, j) =>
                    j === i
                      ? {
                          ...x,
                          value: e.target.value + (split < 0 ? "" : "=" + val),
                        }
                      : x,
                  ),
                })
              }
            />
            <Input
              aria-label="TXT 值"
              placeholder="值"
              value={val}
              onChange={(e) =>
                onChange({
                  ...s,
                  txt: s.txt.map((x, j) =>
                    j === i ? { ...x, value: key + "=" + e.target.value } : x,
                  ),
                })
              }
            />
            <Select
              aria-label="TXT 编码格式"
              value={t.format || "text"}
              onChange={(selectedValue) =>
                onChange({
                  ...s,
                  txt: s.txt.map((x, j) =>
                    j === i ? { ...x, format: selectedValue } : x,
                  ),
                })
              }
            >
              <Select.Option value="text">文本</Select.Option>
              <Select.Option value="binary-hex">十六进制</Select.Option>
              <Select.Option value="binary-base64">Base64</Select.Option>
            </Select>
            <Button
              htmlType="button"
              onClick={() =>
                onChange({ ...s, txt: s.txt.filter((_, j) => j !== i) })
              }
            >
              移除
            </Button>
          </div>
        );
      })}
      <div className="actions">
        <Button
          htmlType="button"
          onClick={() =>
            onChange({
              ...s,
              txt: [...(s.txt ?? []), { value: "key=value", format: "text" }],
            })
          }
        >
          + TXT 记录
        </Button>
        {removable && (
          <Button htmlType="button" danger onClick={onRemove}>
            移除此服务定义
          </Button>
        )}
      </div>
    </fieldset>
  );
}

export function Hosts({
  config,
  busy,
  perform,
}: {
  config: Config;
  busy: boolean;
  perform: Perform;
}) {
  const confirm = useConfirm();
  const [search, setSearch] = useState("");
  const [saveError, setSaveError] = useState("");
  const { data, error, loading } = useResource<{ hosts: Host[] }>(
    "/hosts",
    config.revision,
  );
  const hosts = data?.hosts ?? [];
  const [edit, setEdit] = useState<{ host: Host; revision: string } | null>(
    null,
  );
  useDirty(edit !== null);

  return (
    <>
      <div className="section-heading">
        <Input
          className="table-search"
          prefix={<SearchOutlined />}
          aria-label="搜索列表"
          placeholder="按名称或地址搜索"
          allowClear
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <span>
          {hosts.length} 条静态记录
          <Message error={error} />
        </span>
        <Button
          type="primary"
          onClick={async () =>
            setEdit({
              revision: config.revision,
              host: { address: "", hostname: "" },
            })
          }
        >
          + 添加主机
        </Button>
      </div>
      {edit && (
        <Drawer
          open
          title={edit.host.id ? "编辑静态主机" : "添加静态主机"}
          size={600}
          onClose={async () => {
            if (
              !busy &&
              (await confirm("放弃尚未保存的表单内容？", "放弃修改"))
            )
              setEdit(null);
          }}
        >
          <Message error={saveError} />
          <form
            className="card"
            onSubmit={(e) => {
              e.preventDefault();
              const { id, ...host } = edit.host;
              void perform(() =>
                api(id ? `/hosts/${id}` : "/hosts", id ? "PUT" : "POST", {
                  revision: edit.revision,
                  host,
                }),
              )
                .then(() => setEdit(null))
                .catch((e) => setSaveError(String(e)));
            }}
          >
            <div className="form-grid">
              <Field>
                主机名（完整域名）
                <Input
                  required
                  placeholder="nas.local"
                  value={edit.host.hostname}
                  onChange={(e) =>
                    setEdit({
                      ...edit,
                      host: { ...edit.host, hostname: e.target.value },
                    })
                  }
                />
              </Field>
              <Field>
                IP 地址
                <Input
                  required
                  placeholder="192.168.1.10"
                  value={edit.host.address}
                  onChange={(e) =>
                    setEdit({
                      ...edit,
                      host: { ...edit.host, address: e.target.value },
                    })
                  }
                />
              </Field>
            </div>
            <div className="actions">
              <Button htmlType="submit" type="primary" loading={busy}>
                保存并应用
              </Button>
              <Button
                htmlType="button"
                onClick={async () => {
                  if (await confirm("放弃尚未保存的表单内容？", "放弃修改"))
                    setEdit(null);
                }}
              >
                取消
              </Button>
            </div>
          </form>
        </Drawer>
      )}
      <Card className="card table-wrap">
        <DataTable
          loading={loading}
          dataSource={hosts.filter((h) =>
            `${h.hostname} ${h.address}`
              .toLowerCase()
              .includes(search.toLowerCase()),
          )}
          rowKey={"id"}
          locale={{ emptyText: <Empty>暂无匹配的静态主机记录。</Empty> }}
          columns={[
            {
              title: "主机名",
              key: "0",
              sorter: (a, b) => a.hostname.localeCompare(b.hostname),
              render: (_, h) => <>{h.hostname}</>,
            },
            {
              title: "地址",
              key: "1",
              render: (_, h) => (
                <>
                  <code>{h.address}</code>
                </>
              ),
            },
            {
              title: "操作",
              key: "2",
              width: 230,
              render: (_, h) => (
                <>
                  <div className="actions">
                    <Button
                      onClick={() =>
                        setEdit({ revision: config.revision, host: { ...h } })
                      }
                    >
                      编辑
                    </Button>
                    <Button
                      danger
                      disabled={busy}
                      onClick={async () => {
                        if (
                          await confirm(
                            `删除静态主机记录“${h.hostname}”？应用时会重启 Avahi。`,
                          )
                        )
                          void perform(() =>
                            api(`/hosts/${h.id}`, "DELETE", {
                              revision: config.revision,
                            }),
                          ).catch(() => {});
                      }}
                    >
                      删除
                    </Button>
                  </div>
                </>
              ),
            },
          ]}
        />
      </Card>
    </>
  );
}

export function Interfaces({
  config,
  busy,
  run,
}: {
  config: Config;
  busy: boolean;
  run: Run;
}) {
  const confirm = useConfirm();
  const [dirty, setDirty] = useState(false),
    [revision, setRevision] = useState(config.revision),
    [loading, setLoading] = useState(true);
  useDirty(dirty);
  const [items, setItems] = useState<any[]>([]),
    [selected, setSelected] = useState<string[]>([]),
    [error, setError] = useState("");
  useEffect(() => {
    if (dirty) return;
    let active = true;
    setLoading(true);
    api<any[]>("/interfaces")
      .then((v) => {
        if (!active) return;
        setError("");
        setRevision(config.revision);
        setItems(v);
        setSelected(v.filter((i) => i.mdns).map((i) => i.name));
      })
      .catch((e) => {
        if (active) setError(String(e));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [config.revision, dirty]);
  return (
    <>
      <Message error={error} />
      {dirty && revision !== config.revision && (
        <Alert
          type="warning"
          showIcon
          className="feedback"
          title="配置已在其他位置更新，请记录本次修改后刷新再编辑。"
        />
      )}
      <div className="notice">
        白名单将 mDNS 限制在所选接口。保存会清除现有
        deny-interfaces（接口黑名单）并重启 Avahi。空白名单在 Avahi
        中表示允许所有符合条件的接口，不是禁用全部接口，因此本页禁止空选保存。请勿将不可信公网或
        VPN 接口加入白名单。
      </div>
      <Card className="card table-wrap">
        <DataTable
          loading={loading}
          dataSource={items}
          rowKey={"index"}
          locale={{ emptyText: <Empty>暂无网络接口。</Empty> }}
          columns={[
            {
              title: "mDNS",
              key: "0",
              render: (_, i) => (
                <>
                  <Checkbox
                    aria-label={`在 ${i.name} 上启用 mDNS`}

                    checked={selected.includes(i.name)}
                    onChange={(e) => {
                      setDirty(true);
                      setSelected(
                        e.target.checked
                          ? [...selected, i.name]
                          : selected.filter((n) => n !== i.name),
                      );
                    }}
                  />
                </>
              ),
            },
            {
              title: "网络接口",
              key: "1",
              render: (_, i) => (
                <>
                  <strong>{i.name}</strong>
                  <small>
                    {i.mac}
                    <br />
                    {displayValue(i.category)}
                  </small>
                </>
              ),
            },
            {
              title: "地址",
              key: "2",
              render: (_, i) => (
                <>
                  {i.addresses.map((a: string) => (
                    <div key={a}>
                      <code>{a}</code>
                    </div>
                  ))}
                </>
              ),
            },
            {
              title: "类型 / 状态",
              key: "3",
              render: (_, i) => (
                <>
                  {displayValue(i.type)}
                  <small>
                    {displayValue(i.state)} · {i.flags}
                  </small>
                </>
              ),
            },
            { title: "MTU", key: "4", render: (_, i) => <>{i.mtu}</> },
          ]}
        />
      </Card>
      <Button
        type="primary"
        disabled={
          busy ||
          loading ||
          !!error ||
          !dirty ||
          selected.length === 0 ||
          revision !== config.revision
        }
        onClick={async () => {
          if (
            !(await confirm(
              "将清除接口黑名单并重启 Avahi。请确认所选接口均可信。",
              "应用接口白名单",
            ))
          )
            return;
          const daemon = structuredClone(config.daemon);
          daemon.server = {
            ...daemon.server,
            "allow-interfaces": selected.join(","),
          };
          delete daemon.server["deny-interfaces"];
          run(async () => {
            await api("/config", "PUT", { revision, config: daemon });
            setDirty(false);
          });
        }}
      >
        保存并应用接口白名单
      </Button>
    </>
  );
}

export function Settings({
  config,
  busy,
  run,
}: {
  config: Config;
  busy: boolean;
  run: Run;
}) {
  const confirm = useConfirm();
  const [draft, setDraft] = useState(() => structuredClone(config.daemon)),
    [revision, setRevision] = useState(config.revision),
    [dirty, setDirty] = useState(false),
    [error, setError] = useState(""),
    [schema, setSchema] = useState<Record<string, Record<string, string>>>({}),
    [managed, setManaged] = useState("");
  useDirty(dirty);
  useEffect(() => {
    api<Record<string, Record<string, string>>>("/config/schema")
      .then(setSchema)
      .catch((e) => setError(String(e)));
    api<{ configuration: { raw: string } }>("/config/managed")
      .then((v) => setManaged(v.configuration.raw))
      .catch((e) => setError(String(e)));
  }, [config.managedSnapshot]);
  useEffect(() => {
    if (!dirty) {
      setDraft(structuredClone(config.daemon));
      setRevision(config.revision);
    }
  }, [config.revision, dirty]);
  const save = async () => {
    if (
      !(await confirm(
        "将写入配置并重启 Avahi，服务发现可能短暂中断。",
        "保存并应用配置",
      ))
    )
      return;
    run(async () => {
      await api("/config", "PUT", { revision, config: draft });
      setDirty(false);
    });
  };
  function field(section: string, key: string, kind: string) {
    const value = draft[section]?.[key] ?? "";
    const change = (value: string) => {
      setDirty(true);
      setDraft((old) => {
        const next = structuredClone(old);
        next[section] = { ...next[section] };
        if (value === "") delete next[section][key];
        else next[section][key] = value;
        return next;
      });
    };
    return (
      <Field key={key}>
        {settingLabels[key] ?? key}
        <small>
          <code>{key}</code>
        </small>
        {settingHelp[key] && <small>{settingHelp[key]}</small>}
        <small>
          {kind === "interfaces"
            ? "接口名称用英文逗号分隔"
            : kind === "bool"
              ? "可使用默认值，或显式指定"
              : kind === "uint"
                ? "非负整数"
                : kind === "domain"
                  ? "DNS 域名"
                  : kind === "label"
                    ? "单段主机名，不含域名后缀"
                    : kind === "domains"
                      ? "域名列表，用英文逗号分隔"
                      : kind === "ips"
                        ? "IP 地址列表，用英文逗号分隔"
                        : "使用原生 Avahi 配置格式；不确定时请保留默认值"}
        </small>
        {kind === "bool" || kind === "dbus" ? (
          <Select
            value={
              key === "disable-publishing" && value !== ""
                ? value === "yes"
                  ? "no"
                  : "yes"
                : value
            }
            onChange={(selectedValue) =>
              change(
                key === "disable-publishing" && selectedValue !== ""
                  ? selectedValue === "yes"
                    ? "no"
                    : "yes"
                  : selectedValue,
              )
            }
          >
            <Select.Option value="">使用 Avahi 默认值</Select.Option>
            <Select.Option value="yes">是</Select.Option>
            <Select.Option value="no">否</Select.Option>
            {kind === "dbus" && (
              <Select.Option value="warn">仅警告</Select.Option>
            )}
          </Select>
        ) : (
          <Input
            type={kind === "uint" ? "number" : "text"}
            min={kind === "uint" ? 0 : undefined}
            placeholder="使用 Avahi 默认值"
            value={value}
            onChange={(e) => change(e.target.value)}
          />
        )}
      </Field>
    );
  }
  const basic: Record<string, string[]> = {
    server: [
      "host-name",
      "domain-name",
      "use-ipv4",
      "use-ipv6",
      "allow-interfaces",
    ],
    publish: ["disable-publishing", "publish-addresses", "publish-workstation"],
    reflector: ["enable-reflector"],
  };
  return (
    <>
      {dirty && revision !== config.revision && (
        <Alert
          className="feedback"
          type="warning"
          showIcon
          title="配置版本已变更，本次草稿已保留。请记录修改后刷新并合并，避免覆盖外部配置。"
        />
      )}
      <Card className="card">
        <h2>基本设置</h2>
        <Message error={error} />
        <p>
          留空会移除此项显式配置，使用 Avahi 默认值。“保存并应用”会重启
          Avahi，服务发现可能短暂中断。若出现版本冲突，请先记录未保存的修改，刷新并核对配置后再提交。
        </p>
        <div className="form-grid">
          {Object.entries(basic).flatMap(([section, keys]) =>
            keys.map((key) =>
              field(section, key, schema[section]?.[key] ?? "text"),
            ),
          )}
        </div>
        <Button
          disabled={
            busy ||
            !dirty ||
            !Object.keys(schema).length ||
            revision !== config.revision
          }
          type="primary"
          onClick={() => save()}
        >
          保存并应用
        </Button>
      </Card>
      <Card className="card">
        <h2>高级设置</h2>
        <Collapse
          ghost
          items={Object.entries(schema).map(([section, fields]) => ({
            key: section,
            label: `${sectionLabels[section] ?? section}（${section}）`,
            children: (
              <div className="form-grid">
                {Object.entries(fields)
                  .filter(([key]) => !basic[section]?.includes(key))
                  .map(([key, kind]) => field(section, key, kind))}
              </div>
            ),
          }))}
        />
        <Button disabled={busy} onClick={() => save()}>
          保存并应用高级设置
        </Button>
      </Card>
      <div className="save-bar">
        <span>
          {dirty
            ? "有未保存的修改 · 应用时将重启 Avahi"
            : "当前配置无未保存修改"}
        </span>
        <Button
          disabled={busy || !dirty}
          onClick={async () => {
            if (
              await confirm("恢复为当前服务器配置，放弃本次草稿？", "放弃修改")
            ) {
              setDirty(false);
              setDraft(structuredClone(config.daemon));
              setRevision(config.revision);
            }
          }}
        >
          放弃修改
        </Button>
      </div>
      <Card className="card">
        <h2>原始配置 · 只读</h2>
        {config.drift.length > 0 ? (
          <div className="form-grid">
            <div>
              <h3>管理基线版本</h3>
              <pre>{managed}</pre>
            </div>
            <div>
              <h3>当前磁盘版本</h3>
              <pre>{config.raw}</pre>
            </div>
          </div>
        ) : (
          <pre>{config.raw}</pre>
        )}
      </Card>
    </>
  );
}

type LogEntry = {
  cursor: string;
  timestamp: string;
  priority: number;
  message: string;
};
export function Logs({ refreshTick }: { refreshTick: number }) {
  const [entries, setEntries] = useState<LogEntry[]>([]),
    [search, setSearch] = useState(""),
    [priority, setPriority] = useState("all"),
    [since, setSince] = useState(""),
    [until, setUntil] = useState(""),
    [follow, setFollow] = useState(false),
    [query, setQuery] = useState(""),
    [attempt, setAttempt] = useState(0),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(true),
    [connected, setConnected] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    let stream: EventSource | undefined,
      live: LogEntry[] = [];
    setLoading(true);
    setError("");
    setEntries([]);
    setConnected(false);
    const merge = (items: LogEntry[]) =>
      Array.from(
        new Map(
          items.map((item) => [
            item.cursor || item.timestamp + item.message,
            item,
          ]),
        ).values(),
      ).slice(-1000);
    api<LogEntry[]>("/logs?" + query, "GET", undefined, controller.signal)
      .then((items) => {
        if (!controller.signal.aborted) setEntries(merge([...items, ...live]));
      })
      .catch((e) => {
        if (!controller.signal.aborted) setError(String(e));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    if (follow) {
      stream = new EventSource("/api/v1/logs/events?" + query);
      stream.onopen = () => {
        if (!controller.signal.aborted) setConnected(true);
      };
      stream.addEventListener("log", (e) => {
        if (controller.signal.aborted) return;
        try {
          const item = JSON.parse((e as MessageEvent).data) as LogEntry;
          live = merge([...live, item]);
          setEntries((old) => merge([...old, item]));
        } catch {
          setError("日志事件无法解析，请重新搜索。");
        }
      });
      stream.addEventListener("failure", (e) => {
        try {
          setError(JSON.parse((e as MessageEvent).data).message);
        } catch {
          setError("日志读取失败，请检查权限。");
        }
        setConnected(false);
        stream?.close();
      });
      stream.onerror = () => {
        if (!controller.signal.aborted) setConnected(false);
      };
    }
    return () => {
      controller.abort();
      stream?.close();
    };
  }, [query, refreshTick, follow, attempt]);
  return (
    <>
      <Message error={error} />
      <form
        className="toolbar"
        onSubmit={(e) => {
          e.preventDefault();
          if (since && until && new Date(since) > new Date(until)) {
            setError("开始时间不能晚于结束时间。");
            return;
          }
          const q = new URLSearchParams({ search, priority });
          if (since) q.set("since", new Date(since).toISOString());
          if (until) q.set("until", new Date(until).toISOString());
          setQuery(q.toString());
          setAttempt((n) => n + 1);
        }}
      >
        <Input
          prefix={<SearchOutlined />}
          allowClear
          aria-label="搜索日志"
          placeholder="搜索日志内容"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <Select
          aria-label="日志级别"
          value={priority}
          onChange={setPriority}
          options={["all", "info", "warning", "error"].map((value) => ({
            value,
            label: displayValue(value),
          }))}
        />
        <Field>
          开始时间
          <Input
            type="datetime-local"
            value={since}
            onChange={(e) => setSince(e.target.value)}
          />
        </Field>
        <Field>
          结束时间
          <Input
            type="datetime-local"
            value={until}
            onChange={(e) => setUntil(e.target.value)}
          />
        </Field>
        <Button htmlType="submit" type="primary" loading={loading}>
          搜索
        </Button>
        <Space>
          <Switch aria-label="实时跟踪" checked={follow} onChange={setFollow} />
          <span>实时跟踪</span>
          <Tag color={connected ? "success" : "default"}>
            {follow ? (connected ? "已连接" : "连接中 / 等待重连") : "已暂停"}
          </Tag>
        </Space>
      </form>
      <Card className="card logs">
        <p>
          修改筛选条件后点击“搜索”生效；实时跟踪沿用已提交的条件，最多保留最近
          1000 条记录。若提示权限不足，请检查 Web 账户的 journal
          读取权限。日志原文不会翻译。
        </p>
        {loading && !entries.length ? (
          <Skeleton active />
        ) : (
          entries.map((l, i) => (
            <div className="log-line" key={l.cursor || i}>
              <time>{date(l.timestamp)}</time>
              <span>
                <Tag
                  color={
                    l.priority <= 3
                      ? "error"
                      : l.priority <= 4
                        ? "warning"
                        : "default"
                  }
                >
                  {l.priority <= 3 ? "错误" : l.priority <= 4 ? "警告" : "信息"}
                </Tag>
              </span>
              <code>{l.message}</code>
            </div>
          ))
        )}
        {!loading && !entries.length && (
          <Empty>
            {error
              ? "日志读取失败，请检查上方错误后重新搜索。"
              : "没有匹配的日志记录。"}
          </Empty>
        )}
      </Card>
    </>
  );
}

export function Maintenance({
  config,
  busy,
  run,
}: {
  config: Config | null;
  busy: boolean;
  run: Run;
}) {
  const confirm = useConfirm();
  const [auditFilter, setAuditFilter] = useState("all");
  const statusResource = useResource<any>("/avahi/status", config?.revision);
  const snapshotsResource = useResource<any[]>("/snapshots", config?.revision);
  const auditResource = useResource<any[]>("/audit", config?.revision);
  const status = statusResource.data,
    snapshots = snapshotsResource.data ?? [],
    audit = auditResource.data ?? [];
  const [view, setView] = useState<any>(null),
    [viewOpen, setViewOpen] = useState(false),
    [viewLoading, setViewLoading] = useState(false),
    [error, setError] = useState("");
  const reload = async () => {
    statusResource.retry();
    snapshotsResource.retry();
    auditResource.retry();
  };
  return (
    <>
      <Message
        error={
          error ||
          statusResource.error ||
          snapshotsResource.error ||
          auditResource.error
        }
      />
      <Card className="card">
        <h2>Avahi 服务控制</h2>
        <p>
          <StateTag value={status?.state} /> {status?.avahi?.version}
        </p>
        <p>
          停止或重启会影响本机的服务发布与发现，不会关闭管理面板。“开机启动”仅改变下次开机行为；“重新加载服务声明”不等同于应用全部守护进程配置。
        </p>
        <div className="actions">
          {["start", "stop", "restart", "reload", "enable", "disable"].map(
            (action) => (
              <Button
                key={action}
                title={actionHelp[action]}
                danger={action === "stop" || action === "restart"}
                disabled={busy || statusResource.loading}
                onClick={async () => {
                  if (
                    await confirm(
                      `确认${actionLabels[action]}？${actionHelp[action]}`,
                    )
                  )
                    run(async () => {
                      await api("/avahi/" + action, "POST", {});
                      await reload();
                    }, `已完成：${actionLabels[action]}`);
                }}
              >
                {
                  {
                    start: "启动",
                    stop: "停止",
                    restart: "重启",
                    reload: "重新加载服务声明",
                    enable: "启用开机启动",
                    disable: "禁用开机启动",
                  }[action]
                }
              </Button>
            ),
          )}
        </div>
      </Card>
      <Card className="card">
        <div className="section-heading">
          <h2>配置快照</h2>
          <Button
            disabled={busy}
            onClick={() =>
              run(async () => {
                await api("/snapshots", "POST", { reason: "手动备份" });
                await reload();
              }, "快照已创建")
            }
          >
            创建快照
          </Button>
        </div>
        <p>
          快照仅包含 avahi-daemon.conf、hosts 和 services/*.service，不包含整个
          /etc/avahi 目录或面板数据库。最多保留 20
          份，当前管理基线受保护；恢复会覆盖受管配置并应用，建议先创建快照。删除不可撤销。
        </p>
        <div className="table-wrap">
          <DataTable
            loading={snapshotsResource.loading}
            dataSource={snapshots}
            rowKey={"id"}
            locale={{ emptyText: <Empty>暂无配置快照。</Empty> }}
            columns={[
              {
                title: "创建时间",
                key: "0",
                render: (_, s) => (
                  <>
                    {date(s.createdAt)}
                    {s.id === config?.managedSnapshot && (
                      <small>当前管理基线</small>
                    )}
                  </>
                ),
              },
              {
                title: "原因",
                key: "1",
                render: (_, s) => <>{displayValue(s.reason)}</>,
              },
              {
                title: "操作",
                key: "2",
                width: 230,
                render: (_, s) => (
                  <>
                    <div className="actions">
                      <Button
                        onClick={async () => {
                          setViewOpen(true);
                          setView(null);
                          setViewLoading(true);
                          setError("");
                          try {
                            setView(await api(`/snapshots/${s.id}`));
                          } catch (e) {
                            setError(String(e));
                          } finally {
                            setViewLoading(false);
                          }
                        }}
                      >
                        查看
                      </Button>
                      <Button
                        disabled={busy || !config}
                        onClick={async () => {
                          if (
                            config &&
                            (await confirm(
                              "恢复此快照并应用到 Avahi？当前受管配置将被覆盖，可能短暂中断服务发现。建议先创建当前配置的快照。",
                            ))
                          )
                            run(async () => {
                              await api(`/snapshots/${s.id}/restore`, "POST", {
                                revision: config.revision,
                              });
                              await reload();
                            });
                        }}
                      >
                        恢复
                      </Button>
                      <Button
                        danger
                        disabled={busy || s.id === config?.managedSnapshot}
                        onClick={async () => {
                          if (await confirm("永久删除此快照？删除后无法恢复。"))
                            run(async () => {
                              await api(`/snapshots/${s.id}`, "DELETE", {});
                              await reload();
                            }, "快照已删除");
                        }}
                      >
                        删除
                      </Button>
                    </div>
                  </>
                ),
              },
            ]}
          />
        </div>
      </Card>
      <Drawer
        open={viewOpen}
        title="快照详情"
        size={720}
        onClose={() => setViewOpen(false)}
      >
        <Message error={error} />
        {viewLoading ? (
          <Skeleton active />
        ) : (
          <pre>{JSON.stringify(view, null, 2)}</pre>
        )}
      </Drawer>
      <Card className="card table-wrap">
        <h2>操作审计</h2>
        <p>
          以下为接口返回的近期操作记录，分页与筛选仅作用于已加载数据。资源路径和
          HTTP 操作名保留原文。
        </p>
        <DataTable
          loading={auditResource.loading}
          title={() => (
            <Select
              aria-label="审计结果"
              value={auditFilter}
              onChange={setAuditFilter}
              options={["all", "success", "failed", "started"].map((value) => ({
                value,
                label: value === "all" ? "全部结果" : displayValue(value),
              }))}
            />
          )}
          dataSource={audit.filter(
            (item) => auditFilter === "all" || item.result === auditFilter,
          )}
          rowKey={"id"}
          locale={{ emptyText: <Empty>暂无操作审计记录。</Empty> }}
          columns={[
            {
              title: "时间",
              key: "0",
              sorter: (a, b) => a.createdAt.localeCompare(b.createdAt),
              defaultSortOrder: "descend",
              render: (_, a) => <>{date(a.createdAt)}</>,
            },
            { title: "操作者", key: "1", render: (_, a) => <>{a.actor}</> },
            {
              title: "操作 / 资源",
              key: "2",
              render: (_, a) => (
                <>
                  {a.action} <code>{a.resource}</code>
                </>
              ),
            },
            {
              title: "结果",
              key: "3",
              render: (_, a) => (
                <>
                  <StateTag value={a.result} />
                </>
              ),
            },
          ]}
        />
      </Card>
    </>
  );
}
