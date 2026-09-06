import React, { createContext, useContext, useEffect, useId } from "react";
import {
  Alert,
  App,
  Button,
  Empty as AntEmpty,
  Form,
  Result,
  Table,
  Tag,
  type TableProps,
} from "antd";
import { errorHint, displayValue } from "./zh";

export type Perform = (
  work: () => Promise<unknown>,
  message?: string,
) => Promise<void>;
export type Run = (work: () => Promise<unknown>, message?: string) => void;
export const DirtyContext = createContext<(id: string, dirty: boolean) => void>(
  () => {},
);
export function useDirty(dirty: boolean) {
  const id = useId(),
    report = useContext(DirtyContext);
  useEffect(() => {
    report(id, dirty);
    return () => report(id, false);
  }, [id, dirty, report]);
}
export function useConfirm() {
  const { modal } = App.useApp();
  return (content: string, title = "请确认操作") =>
    modal.confirm({
      title,
      content,
      okText: "确认继续",
      cancelText: "取消",
      centered: true,
      autoFocusButton: "cancel",
      okButtonProps: { danger: true },
    });
}
export function Message({ error }: { error: string }) {
  return error ? (
    <Alert
      className="feedback"
      type="error"
      showIcon
      title={errorHint(error) || "操作未完成"}
      description={<div className="error-detail">{error}</div>}
    />
  ) : null;
}
export function Empty({ children }: { children: React.ReactNode }) {
  return (
    <AntEmpty image={AntEmpty.PRESENTED_IMAGE_SIMPLE} description={children} />
  );
}
export function StateTag({ value }: { value?: string }) {
  const color = /^(RUNNING|active|enabled|success|up)$/.test(value ?? "")
    ? "success"
    : /ERROR|UNAVAILABLE|DEGRADED|failed/.test(value ?? "")
      ? "error"
      : "default";
  return <Tag color={color}>{displayValue(value)}</Tag>;
}
export function DataTable<T extends object>(props: TableProps<T>) {
  return (
    <Table<T>
      size="middle"
      scroll={{ x: 680 }}
      pagination={{
        defaultPageSize: 10,
        showSizeChanger: true,
        pageSizeOptions: [10, 20, 50],
        showTotal: (total) => `共 ${total} 条`,
        hideOnSinglePage: false,
      }}
      {...props}
    />
  );
}
// Preserve explicit label associations when controls render their own DOM (Select, Checkbox).
export function Field({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  const id = useId();
  const parts = React.Children.toArray(children);
  const controls = parts.filter(
    (node) => React.isValidElement(node) && typeof node.type !== "string",
  );
  const help = parts.filter(
    (node) => React.isValidElement(node) && node.type === "small",
  );
  const text = parts.filter(
    (node) => !controls.includes(node) && !help.includes(node),
  );
  return (
    <Form component="div" layout="vertical" colon={false}>
      <Form.Item
        className={className}
        label={<span id={`${id}-label`}>{text}</span>}
        htmlFor={id}
        extra={help.length ? <>{help}</> : undefined}
      >
        {controls.map((control) =>
          React.cloneElement(
            control as React.ReactElement<Record<string, unknown>>,
            { id, "aria-labelledby": `${id}-label` },
          ),
        )}
      </Form.Item>
    </Form>
  );
}
export class ErrorBoundary extends React.Component<
  { children: React.ReactNode },
  { failed: boolean }
> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    return this.state.failed ? (
      <Result
        status="error"
        title="页面遇到异常"
        subTitle="请刷新页面重试；刷新会丢失未保存的修改。如果操作已提交，请先核对服务器状态，勿重复执行。"
        extra={
          <Button onClick={() => window.location.reload()}>重新加载页面</Button>
        }
      />
    ) : (
      this.props.children
    );
  }
}
