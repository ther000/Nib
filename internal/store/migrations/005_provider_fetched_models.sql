-- 为供应商补充上游拉取的全量候选模型目录，models_json 专用于保存作者勾选启用的模型
ALTER TABLE providers ADD COLUMN fetched_models_json TEXT NOT NULL DEFAULT '[]';
