import { ArrowRight, BookPlus, Check, Feather, SlidersHorizontal } from "lucide-react";
import { useApp } from "../stores/AppContext";

export function EmptyShelf() {
  const { providers, openSettings, openCreateBook } = useApp();
  const hasEnabledProvider = providers.some((p) => p.enabled);

  return (
    <main className="flex flex-1 items-center justify-center overflow-y-auto px-6 py-12">
      <div className="w-full max-w-[440px]">
        <div className="mb-10 flex flex-col items-center text-center">
          <div className="mb-4 flex h-11 w-11 items-center justify-center rounded-xl border border-border-strong bg-surface">
            <Feather className="h-5 w-5 text-accent" />
          </div>
          <h1 className="text-xl font-medium tracking-wide text-ink-primary">Nib</h1>
        </div>

        <div className="flex flex-col gap-3">
          <button
            type="button"
            onClick={() => openSettings("providers")}
            className="group flex w-full items-center justify-between rounded-lg border border-border-subtle bg-surface px-4 py-3.5 text-left transition-colors hover:border-border-strong hover:bg-elevated"
          >
            <div className="flex items-center gap-3">
              <div
                className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-xs font-medium nib-mono ${
                  hasEnabledProvider
                    ? "bg-ok-soft text-ok"
                    : "bg-elevated text-ink-secondary"
                }`}
              >
                {hasEnabledProvider ? <Check className="h-3.5 w-3.5" /> : "01"}
              </div>
              <div>
                <div className="text-[14px] font-medium text-ink-primary">
                  配置 API 供应商
                </div>
                <div className="mt-0.5 text-xs text-ink-secondary">
                  {hasEnabledProvider
                    ? `已配置 ${providers.length} 个供应商`
                    : "OpenAI 兼容 · Anthropic · Gemini"}
                </div>
              </div>
            </div>
            <SlidersHorizontal className="h-4 w-4 text-ink-muted transition-colors group-hover:text-ink-primary" />
          </button>

          <button
            type="button"
            onClick={openCreateBook}
            className="group flex w-full items-center justify-between rounded-lg border border-accent/35 bg-accent-soft px-4 py-3.5 text-left transition-colors hover:border-accent/60"
          >
            <div className="flex items-center gap-3">
              <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-accent text-xs font-semibold text-canvas nib-mono">
                02
              </div>
              <div>
                <div className="text-[14px] font-medium text-ink-primary">
                  创建第一本书
                </div>
                <div className="mt-0.5 text-xs text-ink-secondary">
                  玄幻 · 仙侠 · 都市 · 通用男频
                </div>
              </div>
            </div>
            <div className="flex items-center gap-1 text-accent">
              <BookPlus className="h-4 w-4" />
              <ArrowRight className="h-3.5 w-3.5 transition-transform group-hover:translate-x-0.5" />
            </div>
          </button>
        </div>
      </div>
    </main>
  );
}
