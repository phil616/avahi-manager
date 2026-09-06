import { createRoot } from "react-dom/client";
import { App, ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import { ConsoleApp } from "./ConsoleApp";
import { ErrorBoundary } from "./ui";
import "antd/dist/reset.css";
import "./style.css";
const nonce = document.querySelector<HTMLMetaElement>(
  'meta[name="csp-nonce"]',
)?.content;
createRoot(document.getElementById("root")!).render(
  <ConfigProvider
    button={{ autoInsertSpace: false }}
    virtual={false}
    locale={zhCN}
    csp={nonce && nonce !== "__CSP_NONCE__" ? { nonce } : undefined}
    theme={{
      token: {
        colorPrimary: "#1765d1",
        colorInfo: "#1765d1",
        colorBgLayout: "#f3f5f9",
        colorText: "#172b4d",
        colorTextSecondary: "#5e6c84",
        borderRadius: 8,
        fontSize: 14,
        controlHeight: 36,
        fontFamily:
          '-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
      },
      components: {
        Layout: { siderBg: "#101e36", headerBg: "#fff" },
        Menu: { darkItemBg: "#101e36", darkItemSelectedBg: "#2259a0" },
        Card: { headerFontSize: 16 },
        Table: { headerBg: "#f7f9fc" },
      },
    }}
  >
    <App>
      <ErrorBoundary>
        <ConsoleApp />
      </ErrorBoundary>
    </App>
  </ConfigProvider>,
);
