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

const entityColumns = `
	id, book_id, kind, name, aliases_json, is_invariant, summary, attributes_json, created_at, updated_at
`

// ListEntities 按条件查询指定书籍下的设定实体与世界铁律（铁律优先排序）。
func (s *Store) ListEntities(ctx context.Context, bookID int64, filter domain.EntityFilter) ([]domain.Entity, error) {
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	var (
		conds = []string{"book_id = ?"}
		args  = []any{bookID}
	)
	if filter.Kind != "" {
		conds = append(conds, "kind = ?")
		args = append(args, string(filter.Kind))
	}
	if filter.InvariantOnly {
		conds = append(conds, "is_invariant = 1")
	}
	if q := strings.TrimSpace(filter.Query); q != "" {
		pattern := "%" + escapeLikePattern(q) + "%"
		conds = append(conds, `(name LIKE ? ESCAPE '\' OR aliases_json LIKE ? ESCAPE '\' OR summary LIKE ? ESCAPE '\' OR attributes_json LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern, pattern, pattern)
	}

	query := `SELECT ` + entityColumns + ` FROM entities WHERE ` + strings.Join(conds, " AND ") + ` ORDER BY is_invariant DESC, kind ASC, id ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()
	return scanEntityRows(rows)
}

// GetEntitiesByIDs 按 ID 集合批量查询实体（自动去重并忽略 <= 0 的 ID）。
func (s *Store) GetEntitiesByIDs(ctx context.Context, ids []int64) ([]domain.Entity, error) {
	unique := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return []domain.Entity{}, nil
	}

	placeholders := make([]string, len(unique))
	args := make([]any, len(unique))
	for i, id := range unique {
		placeholders[i] = "?"
		args[i] = id
	}

	query := `SELECT ` + entityColumns + ` FROM entities WHERE id IN (` + strings.Join(placeholders, ",") + `) ORDER BY is_invariant DESC, id ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()
	return scanEntityRows(rows)
}

// ListInvariants 返回指定书籍的全部世界铁律（is_invariant = 1）。
func (s *Store) ListInvariants(ctx context.Context, bookID int64) ([]domain.Entity, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+entityColumns+`
		FROM entities
		WHERE book_id = ? AND is_invariant = 1
		ORDER BY id ASC
	`, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()
	return scanEntityRows(rows)
}

// UpsertEntity 新增或更新设定实体 / 世界铁律。
func (s *Store) UpsertEntity(ctx context.Context, in domain.UpsertEntityInput) (domain.Entity, error) {
	if err := in.Validate(); err != nil {
		return domain.Entity{}, err
	}

	name := strings.TrimSpace(in.Name)
	aliasesJSON, err := marshalStringSlice(in.Aliases)
	if err != nil {
		return domain.Entity{}, fmt.Errorf("marshal entity aliases: %w", err)
	}
	attrsJSON, err := marshalAttributes(in.Attributes)
	if err != nil {
		return domain.Entity{}, fmt.Errorf("marshal entity attributes: %w", err)
	}

	var result domain.Entity
	err = s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if in.ID <= 0 {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT 1 FROM books WHERE id = ?`, in.BookID).Scan(&exists); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("book %d: %w", in.BookID, domain.ErrNotFound)
				}
				return mapSQLiteError(err)
			}

			res, err := tx.ExecContext(ctx, `
				INSERT INTO entities (book_id, kind, name, aliases_json, is_invariant, summary, attributes_json)
				VALUES (?, ?, ?, ?, ?, ?, ?)
			`, in.BookID, string(in.Kind), name, aliasesJSON, boolToInt(in.IsInvariant), in.Summary, attrsJSON)
			if err != nil {
				return mapSQLiteError(err)
			}
			newID, err := res.LastInsertId()
			if err != nil {
				return err
			}
			result, err = getEntityRow(ctx, tx, newID)
			return err
		}

		existing, err := getEntityRow(ctx, tx, in.ID)
		if err != nil {
			return err
		}
		if existing.BookID != in.BookID {
			return &domain.ValidationError{Field: "bookId", Message: "实体不属于指定书籍"}
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE entities
			SET kind = ?,
			    name = ?,
			    aliases_json = ?,
			    is_invariant = ?,
			    summary = ?,
			    attributes_json = ?,
			    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?
		`, string(in.Kind), name, aliasesJSON, boolToInt(in.IsInvariant), in.Summary, attrsJSON, in.ID); err != nil {
			return mapSQLiteError(err)
		}

		result, err = getEntityRow(ctx, tx, in.ID)
		return err
	})
	if err != nil {
		return domain.Entity{}, err
	}
	return result, nil
}

// DeleteEntity 按 ID 删除设定实体。
func (s *Store) DeleteEntity(ctx context.Context, id int64) error {
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM entities WHERE id = ?`, id)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("entity %d: %w", id, domain.ErrNotFound)
		}
		return nil
	})
}

func scanEntity(sc rowScanner) (domain.Entity, error) {
	var (
		e           domain.Entity
		kind        string
		aliasesJSON string
		isInv       int
		attrsJSON   string
	)
	if err := sc.Scan(&e.ID, &e.BookID, &kind, &e.Name, &aliasesJSON, &isInv, &e.Summary, &attrsJSON, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return domain.Entity{}, mapSQLiteError(err)
	}
	e.Kind = domain.EntityKind(kind)
	e.IsInvariant = isInv != 0
	e.Aliases = unmarshalStringSlice(aliasesJSON)
	e.Attributes = unmarshalAttributes(attrsJSON)
	return e, nil
}

func scanEntityRows(rows *sql.Rows) ([]domain.Entity, error) {
	out := make([]domain.Entity, 0)
	for rows.Next() {
		e, err := scanEntity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func getEntityRow(ctx context.Context, q queryRower, id int64) (domain.Entity, error) {
	row := q.QueryRowContext(ctx, `SELECT `+entityColumns+` FROM entities WHERE id = ?`, id)
	e, err := scanEntity(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Entity{}, fmt.Errorf("entity %d: %w", id, domain.ErrNotFound)
		}
		return domain.Entity{}, err
	}
	return e, nil
}

func marshalAttributes(attrs map[string]string) (string, error) {
	if len(attrs) == 0 {
		return "{}", nil
	}
	clean := make(map[string]string, len(attrs))
	for k, v := range attrs {
		key := strings.TrimSpace(k)
		if key != "" {
			clean[key] = v
		}
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func unmarshalAttributes(raw string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return map[string]string{}
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return map[string]string{}
	}
	return out
}

func escapeLikePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(q)
}
