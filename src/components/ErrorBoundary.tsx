import { Component, type ErrorInfo, type ReactNode } from "react";
import { AlertCircle, RotateCcw } from "lucide-react";

interface ErrorBoundaryProps {
  children: ReactNode;
}

interface ErrorBoundaryState {
  hasError: boolean;
  message: string;
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = {
    hasError: false,
    message: "",
  };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return {
      hasError: true,
      message: error?.message || "界面渲染遇到意外错误",
    };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("Nib ErrorBoundary caught:", error, info.componentStack);
  }

  handleReload = () => {
    this.setState({ hasError: false, message: "" });
  };

  render() {
    if (!this.state.hasError) {
      return this.props.children;
    }
    return (
      <div className="flex h-full w-full items-center justify-center bg-canvas px-6 text-ink-primary">
        <div className="flex max-w-md flex-col gap-3 rounded-xl border border-danger/35 bg-surface p-6 shadow-[var(--shadow-elevated)]">
          <div className="flex items-center gap-2.5 text-danger">
            <AlertCircle className="h-5 w-5 shrink-0" />
            <h2 className="text-sm font-semibold text-ink-primary">工作台界面发生异常</h2>
          </div>
          <p className="text-xs leading-relaxed text-ink-secondary">
            {this.state.message}
          </p>
          <p className="text-[11px] text-ink-muted">
            已落盘的章节正文与设定均安全保存在本地数据库。可点击下方按钮尝试恢复视图。
          </p>
          <div className="mt-1 flex items-center gap-2">
            <button
              type="button"
              onClick={this.handleReload}
              className="flex items-center gap-1.5 rounded-md bg-accent px-3.5 py-1.5 text-xs font-semibold text-canvas transition-colors hover:bg-accent-hover"
            >
              <RotateCcw className="h-3.5 w-3.5" />
              <span>重新载入界面</span>
            </button>
            <button
              type="button"
              onClick={() => window.location.reload()}
              className="rounded-md border border-border-subtle bg-elevated px-3 py-1.5 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary"
            >
              刷新整页
            </button>
          </div>
        </div>
      </div>
    );
  }
}
