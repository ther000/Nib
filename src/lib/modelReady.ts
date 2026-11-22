import type { ModelRoute, PipelineStage, ProviderPublic } from "../mygo";

/**
 * 判断至少一条给定环节的路由是否已绑定启用供应商下的可用模型。
 */
export function hasReadyModelRoute(
  providers: ProviderPublic[],
  modelRoutes: ModelRoute[],
  stages: PipelineStage[]
): boolean {
  const stageSet = new Set<string>(stages);
  const enabledProviderIds = new Set(
    providers.filter((p) => p.enabled && (p.models?.length ?? 0) > 0).map((p) => p.id)
  );
  return modelRoutes.some(
    (r) =>
      stageSet.has(r.stage) &&
      r.providerId != null &&
      enabledProviderIds.has(r.providerId) &&
      r.modelName.trim().length > 0
  );
}

/**
 * 判断是否存在至少一个启用且已启用模型的供应商。
 */
export function hasAnyEnabledModel(providers: ProviderPublic[]): boolean {
  return providers.some((p) => p.enabled && (p.models?.length ?? 0) > 0);
}
