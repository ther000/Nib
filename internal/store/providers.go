package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"nib/internal/domain"
)

// ListProviders 返回全部 API 供应商配置（含原始 APIKey，供内部 LLM 客户端使用；
// 向前端返回前必须在 app 层转换为 ProviderPublic）。
func (s *Store) ListProviders(ctx context.Context) ([]domain.Provider, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, protocol, base_url, api_key, models_json, fetched_models_json, enabled, created_at, updated_at
		FROM providers
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	out := make([]domain.Provider, 0)
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetProvider 按 ID 查询单个供应商配置。
func (s *Store) GetProvider(ctx context.Context, id int64) (domain.Provider, error) {
	p, err := getProviderRow(ctx, s.db, id)
	if err != nil {
		return domain.Provider{}, err
	}
	return p, nil
}

// UpsertProvider 新增或更新供应商配置。
// 更新既有供应商（in.ID > 0）且 in.APIKey 为空时，强制保留数据库中原有的 api_key。
func (s *Store) UpsertProvider(ctx context.Context, in domain.UpsertProviderInput) (domain.Provider, error) {
	if err := in.Validate(); err != nil {
		return domain.Provider{}, err
	}

	name := strings.TrimSpace(in.Name)
	baseURL := strings.TrimSpace(in.BaseURL)
	apiKey := strings.TrimSpace(in.APIKey)
	modelsJSON, err := marshalStringSlice(in.Models)
	if err != nil {
		return domain.Provider{}, fmt.Errorf("marshal provider models: %w", err)
	}
	var fetchedJSON string
	hasFetchedUpdate := in.FetchedModels != nil
	if hasFetchedUpdate {
		fetchedJSON, err = marshalStringSlice(in.FetchedModels)
		if err != nil {
			return domain.Provider{}, fmt.Errorf("marshal provider fetched models: %w", err)
		}
	} else {
		fetchedJSON = "[]"
	}

	var result domain.Provider
	err = s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if in.ID <= 0 {
			res, err := tx.ExecContext(ctx, `
				INSERT INTO providers (name, protocol, base_url, api_key, models_json, fetched_models_json, enabled)
				VALUES (?, ?, ?, ?, ?, ?, ?)
			`, name, string(in.Protocol), baseURL, apiKey, modelsJSON, fetchedJSON, boolToInt(in.Enabled))
			if err != nil {
				return mapSQLiteError(err)
			}
			newID, err := res.LastInsertId()
			if err != nil {
				return err
			}
			result, err = getProviderRow(ctx, tx, newID)
			return err
		}

		existing, err := getProviderRow(ctx, tx, in.ID)
		if err != nil {
			return err
		}
		effectiveKey := apiKey
		if effectiveKey == "" {
			effectiveKey = existing.APIKey
		}
		effectiveFetchedJSON := fetchedJSON
		if !hasFetchedUpdate {
			effectiveFetchedJSON, err = marshalStringSlice(existing.FetchedModels)
			if err != nil {
				return fmt.Errorf("marshal existing fetched models: %w", err)
			}
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE providers
			SET name = ?,
			    protocol = ?,
			    base_url = ?,
			    api_key = ?,
			    models_json = ?,
			    fetched_models_json = ?,
			    enabled = ?,
			    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?
		`, name, string(in.Protocol), baseURL, effectiveKey, modelsJSON, effectiveFetchedJSON, boolToInt(in.Enabled), in.ID); err != nil {
			return mapSQLiteError(err)
		}

		result, err = getProviderRow(ctx, tx, in.ID)
		return err
	})
	if err != nil {
		return domain.Provider{}, err
	}
	return result, nil
}

// DeleteProvider 按 ID 删除供应商（关联的 model_routes.provider_id 通过外键自动置 NULL）。
func (s *Store) DeleteProvider(ctx context.Context, id int64) error {
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM providers WHERE id = ?`, id)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("provider %d: %w", id, domain.ErrNotFound)
		}
		return nil
	})
}

// ListModelRoutes 按固定流水线顺序返回全部 5 环节模型路由。
func (s *Store) ListModelRoutes(ctx context.Context) ([]domain.ModelRoute, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT stage, provider_id, model_name, temperature, max_tokens, updated_at
		FROM model_routes
		ORDER BY CASE stage
			WHEN 'outline' THEN 1
			WHEN 'draft'   THEN 2
			WHEN 'critic'  THEN 3
			WHEN 'polish'  THEN 4
			WHEN 'extract' THEN 5
			ELSE 99
		END
	`)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	out := make([]domain.ModelRoute, 0, 5)
	for rows.Next() {
		r, err := scanModelRoute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetModelRoute 按环节查询单条模型路由配置。
func (s *Store) GetModelRoute(ctx context.Context, stage domain.PipelineStage) (domain.ModelRoute, error) {
	if !stage.Valid() {
		return domain.ModelRoute{}, &domain.ValidationError{Field: "stage", Message: "无效的流水线阶段"}
	}
	return getModelRouteRow(ctx, s.db, stage)
}

// SaveModelRoute 保存指定环节的模型路由配置。
func (s *Store) SaveModelRoute(ctx context.Context, route domain.ModelRoute) (domain.ModelRoute, error) {
	if err := route.Validate(); err != nil {
		return domain.ModelRoute{}, err
	}

	maxTokens := route.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}
	modelName := strings.TrimSpace(route.ModelName)

	var providerID sql.NullInt64
	if route.ProviderID != nil {
		providerID = sql.NullInt64{Int64: *route.ProviderID, Valid: true}
	}

	var result domain.ModelRoute
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if providerID.Valid {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT 1 FROM providers WHERE id = ?`, providerID.Int64).Scan(&exists); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("provider %d: %w", providerID.Int64, domain.ErrNotFound)
				}
				return mapSQLiteError(err)
			}
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO model_routes (stage, provider_id, model_name, temperature, max_tokens, updated_at)
			VALUES (?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
			ON CONFLICT(stage) DO UPDATE SET
				provider_id = excluded.provider_id,
				model_name  = excluded.model_name,
				temperature = excluded.temperature,
				max_tokens  = excluded.max_tokens,
				updated_at  = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		`, string(route.Stage), providerID, modelName, route.Temperature, maxTokens); err != nil {
			return mapSQLiteError(err)
		}

		var err error
		result, err = getModelRouteRow(ctx, tx, route.Stage)
		return err
	})
	if err != nil {
		return domain.ModelRoute{}, err
	}
	return result, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProvider(sc rowScanner) (domain.Provider, error) {
	var (
		p           domain.Provider
		protocol    string
		modelsJSON  string
		fetchedJSON string
		enabled     int
	)
	if err := sc.Scan(&p.ID, &p.Name, &protocol, &p.BaseURL, &p.APIKey, &modelsJSON, &fetchedJSON, &enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return domain.Provider{}, mapSQLiteError(err)
	}
	p.Protocol = domain.ProviderProtocol(protocol)
	p.Enabled = enabled != 0
	p.Models = unmarshalStringSlice(modelsJSON)
	p.FetchedModels = unmarshalStringSlice(fetchedJSON)
	return p, nil
}

func getProviderRow(ctx context.Context, q queryRower, id int64) (domain.Provider, error) {
	row := q.QueryRowContext(ctx, `
		SELECT id, name, protocol, base_url, api_key, models_json, fetched_models_json, enabled, created_at, updated_at
		FROM providers
		WHERE id = ?
	`, id)
	p, err := scanProvider(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Provider{}, fmt.Errorf("provider %d: %w", id, domain.ErrNotFound)
		}
		return domain.Provider{}, err
	}
	return p, nil
}

func scanModelRoute(sc rowScanner) (domain.ModelRoute, error) {
	var (
		r          domain.ModelRoute
		stage      string
		providerID sql.NullInt64
	)
	if err := sc.Scan(&stage, &providerID, &r.ModelName, &r.Temperature, &r.MaxTokens, &r.UpdatedAt); err != nil {
		return domain.ModelRoute{}, mapSQLiteError(err)
	}
	r.Stage = domain.PipelineStage(stage)
	if providerID.Valid {
		id := providerID.Int64
		r.ProviderID = &id
	}
	return r, nil
}

func getModelRouteRow(ctx context.Context, q queryRower, stage domain.PipelineStage) (domain.ModelRoute, error) {
	row := q.QueryRowContext(ctx, `
		SELECT stage, provider_id, model_name, temperature, max_tokens, updated_at
		FROM model_routes
		WHERE stage = ?
	`, string(stage))
	r, err := scanModelRoute(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ModelRoute{}, fmt.Errorf("model route %s: %w", stage, domain.ErrNotFound)
		}
		return domain.ModelRoute{}, err
	}
	return r, nil
}

func marshalStringSlice(items []string) (string, error) {
	clean := make([]string, 0, len(items))
	for _, s := range items {
		t := strings.TrimSpace(s)
		if t != "" {
			clean = append(clean, t)
		}
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func unmarshalStringSlice(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []string{}
	}
	return out
}
