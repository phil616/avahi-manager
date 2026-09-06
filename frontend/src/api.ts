export type Session = { username: string; csrf: string; expiresAt: string };
export type Host = { address: string; hostname: string; id?: string };
export type Service = {
  type: string;
  protocol: string;
  port: number;
  host?: string;
  domain?: string;
  subtypes: string[];
  txt: { value: string; format?: string }[];
};
export type Group = {
  name: { value: string; replaceWildcards: string };
  services: Service[];
};
export type Published = {
  id: string;
  filename: string;
  group: Group;
  error?: string;
};
export type Config = {
  revision: string;
  daemon: Record<string, Record<string, string>>;
  raw: string;
  hosts: Host[];
  services: Published[];
  errors: string[];
  drift: { path: string; managedHash: string; diskHash: string }[];
  managedSnapshot: string;
};
export type Found = {
  name: string;
  type: string;
  domain: string;
  interface: number;
  protocol: number;
  host: string;
  address: string;
  port: number;
  txt: string[];
  firstSeen: string;
  lastSeen: string;
  error?: string;
};
export type Discovery = {
  services: Found[];
  connected: boolean;
  error?: string;
};
let csrf = "";
export function setSession(s: Session | null) {
  csrf = s?.csrf ?? "";
}
export class APIError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}
export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const r = await fetch("/api/v1" + path, {
    signal,
    method,
    credentials: "same-origin",
    headers: {
      ...(body !== undefined ? { "Content-Type": "application/json" } : {}),
      ...(method !== "GET" ? { "X-CSRF-Token": csrf } : {}),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await r.json();
  if (!r.ok) {
    if (r.status === 401) window.dispatchEvent(new Event("session-expired"));
    const hints: Record<number, string> = {
      400: "请求参数无效，请核对填写内容。",
      401:
        path === "/auth/login"
          ? "登录失败，请检查用户名和密码。"
          : "登录已失效，请重新登录。",
      403: "请求被拒绝，请检查访问地址、来源配置与账户权限；必要时重新登录。",
      409: "配置版本冲突或当前状态不允许此操作。请保留未保存的修改，刷新并核对后再试。",
      429: "请求过于频繁，请稍后重试。",
      503: "服务暂不可用，请检查 Helper、Avahi 和系统日志。",
    };
    throw new APIError(
      r.status,
      `${hints[r.status] ?? "操作失败，请查看详细信息。"}（HTTP ${r.status}）${data.message ? "\n原始信息：" + data.message : ""}`,
    );
  }
  return data;
}
export function date(s: string) {
  return s ? new Date(s).toLocaleString("zh-CN") : "—";
}
