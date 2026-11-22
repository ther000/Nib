import { useCallback, useEffect, useState } from "react";
import { RefreshCw } from "lucide-react";
import { SettingsService, type UsageSummary } from "../../mygo";
import { useApp } from "../../stores/AppContext";
import { UsageHeatmap } from "../UsageHeatmap";
import {
  PIPELINE_STAGES,
  formatCompactTokens,
  formatErrorMessage,
  formatLatency,
  formatNumber,
} from "../../lib/constants";

export function UsageSettingsTab() {
  const { books, notify } = useApp();
  const [usageBookId, setUsageBookId] = useState<number>(0);
  const [usageSummary, setUsageSummary] = useState<UsageSummary | null>(null);
  const [usageLoading, setUsageLoading] = useState(false);
  const [usageDetailTab, setUsageDetailTab] = useState<"stage" | "model">("stage");

  const loadUsage = useCallback(
    async (bookId: number) => {
      setUsageLoading(true);
      try {
        const res = await SettingsService.getUsageSummary(bookId);
        setUsageSummary(res);
      } catch (err) {
        notify({
          tone: "danger",
          message: "加载用量统计失败",
          detail: formatErrorMessage(err),
        });
      } finally {
        setUsageLoading(false);
      }
    },
    [notify]
  );

  useEffect(() => {
    if (usageBookId > 0 && !books.some((b) => b.id === usageBookId)) {
      setUsageBookId(0);
    }
  }, [books, usageBookId]);

  useEffect(() => {
    void loadUsage(usageBookId);
  }, [usageBookId, loadUsage]);

  return (
    <div className="flex-1 overflow-y-auto px-5 py-4">
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span className="text-xs text-ink-secondary">
            统计本地通过模型服务发起的写作、审校与设定分析调用记录
          </span>
          <div className="flex items-center gap-2">
            <select
              aria-label="统计范围"
              value={usageBookId}
              onChange={(e) => setUsageBookId(Number(e.target.value) || 0)}
              className="rounded-lg border border-border-subtle bg-canvas px-2.5 py-1 text-xs text-ink-primary focus:border-accent"
            >
              <option value={0}>全部作品</option>
              {books.map((b) => (
                <option key={b.id} value={b.id}>
                  《{b.title}》
                </option>
              ))}
            </select>

            <button
              type="button"
              disabled={usageLoading}
              onClick={() => void loadUsage(usageBookId)}
              className="flex items-center gap-1 rounded-lg border border-border-subtle bg-surface px-2.5 py-1 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-50"
            >
              <RefreshCw className={`h-3 w-3 ${usageLoading ? "animate-spin text-accent" : ""}`} />
              <span>刷新</span>
            </button>
          </div>
        </div>

        {/* KPI Strip */}
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <div className="rounded-xl border border-border-subtle bg-surface p-3.5">
            <div className="text-xs text-ink-muted">调用次数</div>
            <div className="nib-mono mt-1 text-lg font-medium text-ink-primary">
              {formatNumber(usageSummary?.totalCalls ?? 0)}
              <span className="ml-1 font-ui text-xs font-normal text-ink-muted">次</span>
            </div>
          </div>
          <div
            title={`输入 ${formatNumber(usageSummary?.totalPromptTokens ?? 0)} Token`}
            className="rounded-xl border border-border-subtle bg-surface p-3.5"
          >
            <div className="text-xs text-ink-muted">输入 Token</div>
            <div className="nib-mono mt-1 text-lg font-medium text-ink-primary">
              {formatCompactTokens(usageSummary?.totalPromptTokens ?? 0)}
            </div>
          </div>
          <div
            title={`输出 ${formatNumber(usageSummary?.totalCompletionTokens ?? 0)} Token`}
            className="rounded-xl border border-border-subtle bg-surface p-3.5"
          >
            <div className="text-xs text-ink-muted">输出 Token</div>
            <div className="nib-mono mt-1 text-lg font-medium text-ink-primary">
              {formatCompactTokens(usageSummary?.totalCompletionTokens ?? 0)}
            </div>
          </div>
          <div
            title={`总计 ${formatNumber(usageSummary?.totalTokens ?? 0)} Token`}
            className="rounded-xl border border-accent/30 bg-accent-soft/45 p-3.5"
          >
            <div className="text-xs text-accent">总用量</div>
            <div className="nib-mono mt-1 text-lg font-medium text-accent">
              {formatCompactTokens(usageSummary?.totalTokens ?? 0)}
            </div>
            {((usageSummary?.estimatedCostCny ?? 0) > 0 || (usageSummary?.estimatedCostUsd ?? 0) > 0) && (
              <div className="nib-mono mt-1 text-xs text-ink-secondary">
                估算：
                {(usageSummary?.estimatedCostCny ?? 0) > 0 && `¥${(usageSummary?.estimatedCostCny ?? 0).toFixed(2)} `}
                {(usageSummary?.estimatedCostUsd ?? 0) > 0 && `$${(usageSummary?.estimatedCostUsd ?? 0).toFixed(3)}`}
              </div>
            )}
          </div>
        </div>

        {/* Annual Daily Token Heatmap */}
        <UsageHeatmap daily={usageSummary?.daily ?? []} />

        {/* Breakdown Table */}
        {(usageSummary?.totalCalls ?? 0) > 0 && (
          <div className="overflow-hidden rounded-xl border border-border-subtle bg-surface">
            <div className="flex items-center justify-between border-b border-border-subtle px-4 py-2.5">
              <div className="flex items-center gap-1 rounded-lg border border-border-subtle bg-canvas p-0.5 text-xs">
                <button
                  type="button"
                  onClick={() => setUsageDetailTab("stage")}
                  className={`rounded-[6px] px-2.5 py-1 transition-colors ${
                    usageDetailTab === "stage"
                      ? "bg-accent-soft font-medium text-accent"
                      : "text-ink-secondary hover:text-ink-primary"
                  }`}
                >
                  按写作环节
                </button>
                <button
                  type="button"
                  onClick={() => setUsageDetailTab("model")}
                  className={`rounded-[6px] px-2.5 py-1 transition-colors ${
                    usageDetailTab === "model"
                      ? "bg-accent-soft font-medium text-accent"
                      : "text-ink-secondary hover:text-ink-primary"
                  }`}
                >
                  按模型服务
                </button>
              </div>
              <span className="text-xs text-ink-muted">单位：Token</span>
            </div>

            {usageDetailTab === "stage" ? (
              <table className="w-full text-left text-xs">
                <thead className="border-b border-border-subtle bg-canvas/50 text-ink-muted">
                  <tr>
                    <th className="px-4 py-2 font-medium">环节</th>
                    <th className="px-4 py-2 text-right font-medium">调用</th>
                    <th className="px-4 py-2 text-right font-medium">输入</th>
                    <th className="px-4 py-2 text-right font-medium">输出</th>
                    <th className="px-4 py-2 text-right font-medium">合计</th>
                    <th className="px-4 py-2 text-right font-medium">平均耗时</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border-subtle">
                  {(usageSummary?.byStage ?? []).map((row) => {
                    const label =
                      PIPELINE_STAGES.find((s) => s.stage === row.stage)?.label ??
                      (row.stage === "style" ? "文风分析" : row.stage);
                    return (
                      <tr key={row.stage}>
                        <td className="px-4 py-2 text-[13px] font-medium text-ink-primary">
                          {label}
                        </td>
                        <td className="nib-mono px-4 py-2 text-right text-ink-secondary">
                          {formatNumber(row.calls)}
                        </td>
                        <td className="nib-mono px-4 py-2 text-right text-ink-secondary">
                          {formatCompactTokens(row.promptTokens)}
                        </td>
                        <td className="nib-mono px-4 py-2 text-right text-ink-secondary">
                          {formatCompactTokens(row.completionTokens)}
                        </td>
                        <td className="nib-mono px-4 py-2 text-right font-medium text-ink-primary">
                          {formatCompactTokens(row.totalTokens)}
                        </td>
                        <td className="nib-mono px-4 py-2 text-right text-ink-muted">
                          {formatLatency(row.avgLatencyMs)}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            ) : (
              <table className="w-full text-left text-xs">
                <thead className="border-b border-border-subtle bg-canvas/50 text-ink-muted">
                  <tr>
                    <th className="px-4 py-2 font-medium">模型服务</th>
                    <th className="px-4 py-2 font-medium">模型</th>
                    <th className="px-4 py-2 text-right font-medium">调用</th>
                    <th className="px-4 py-2 text-right font-medium">合计</th>
                    <th className="px-4 py-2 text-right font-medium">估算花费</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border-subtle">
                  {(usageSummary?.byModel ?? []).map((row, idx) => (
                    <tr key={`${row.providerName}-${row.modelName}-${idx}`}>
                      <td className="px-4 py-2 text-[13px] text-ink-primary">
                        {row.providerName || "—"}
                      </td>
                      <td className="nib-mono px-4 py-2 text-ink-secondary">
                        {row.modelName || "—"}
                      </td>
                      <td className="nib-mono px-4 py-2 text-right text-ink-secondary">
                        {formatNumber(row.calls)}
                      </td>
                      <td className="nib-mono px-4 py-2 text-right font-medium text-ink-primary">
                        {formatCompactTokens(row.totalTokens)}
                      </td>
                      <td className="nib-mono px-4 py-2 text-right text-ink-secondary">
                        {row.estimatedCost > 0
                          ? `${row.currency === "USD" ? "$" : "¥"}${row.estimatedCost.toFixed(row.currency === "USD" ? 4 : 2)}`
                          : "—"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
