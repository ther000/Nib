package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"modernc.org/sqlite"

	"nib/internal/domain"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// initPragmas 按 AGENTS.md §3.2 规定顺序在每条连接初始化时执行。
var initPragmas = []string{
	"PRAGMA journal_mode = WAL;",
	"PRAGMA foreign_keys = ON;",
	"PRAGMA busy_timeout = 5000;",
	"PRAGMA synchronous = NORMAL;",
}

const (
	sqliteConstraintCode           = 19
	sqliteConstraintCheckCode      = 275
	sqliteConstraintForeignKeyCode = 787
	sqliteConstraintPrimaryKeyCode = 1555
	sqliteConstraintUniqueCode     = 2067
)

type migration struct {
	Version int
	Name    string
	SQL     string
}

type pragmaConnector struct {
	driver.Connector
}

func (c pragmaConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	execer, ok := conn.(driver.ExecerContext)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("sqlite connection does not implement driver.ExecerContext")
	}
	for _, stmt := range initPragmas {
		if _, err := execer.ExecContext(ctx, stmt, nil); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("apply %q: %w", stmt, err)
		}
	}
	return conn, nil
}

// Store 封装基于纯 Go modernc.org/sqlite 的本地仓储与迁移管理。
type Store struct {
	db      *sql.DB
	path    string
	writeMu sync.Mutex
}

// Open 打开 SQLite 数据库（支持磁盘路径或 ":memory:"），启用必备 PRAGMA，
// 运行版本化内嵌迁移，并幂等补齐 5 环节默认模型路由。
func Open(dbPath string) (*Store, error) {
	migs, err := loadEmbeddedMigrations()
	if err != nil {
		return nil, err
	}
	return openWithMigrations(dbPath, migs)
}

func openWithMigrations(dbPath string, migs []migration) (*Store, error) {
	trimmedPath := strings.TrimSpace(dbPath)
	if trimmedPath == "" {
		return nil, fmt.Errorf("db path is empty: %w", domain.ErrValidation)
	}

	isMemory := isMemoryPath(trimmedPath)
	preExistingNonEmpty := false

	if !isMemory {
		cleanPath := filepath.Clean(trimmedPath)
		trimmedPath = cleanPath
		if dir := filepath.Dir(cleanPath); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create db directory %q: %w", dir, err)
			}
		}
		if fi, err := os.Stat(cleanPath); err == nil && !fi.IsDir() && fi.Size() > 0 {
			preExistingNonEmpty = true
		}
	}

	baseConnector, err := sqlite.NewConnector(trimmedPath)
	if err != nil {
		return nil, fmt.Errorf("create sqlite connector: %w", err)
	}
	db := sql.OpenDB(pragmaConnector{Connector: baseConnector})

	if isMemory {
		// 内存库固定单连接且永不回收，防止释放连接导致表丢失。
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		db.SetConnMaxLifetime(0)
		db.SetConnMaxIdleTime(0)
	} else {
		// WAL 模式允许并发读连接；写操作由 writeMu 串行化以消除 SQLITE_BUSY。
		db.SetMaxOpenConns(4)
		db.SetMaxIdleConns(4)
		db.SetConnMaxLifetime(0)
		db.SetConnMaxIdleTime(0)
	}

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite %q: %w", trimmedPath, err)
	}

	s := &Store{
		db:   db,
		path: trimmedPath,
	}

	if err := s.runMigrations(ctx, migs, isMemory, preExistingNonEmpty); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.seedDefaultModelRoutes(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return s, nil
}

// Close 关闭底层 SQLite 连接池。
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Path 返回当前打开的数据库路径（或 ":memory:"）。
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func isMemoryPath(p string) bool {
	if p == ":memory:" {
		return true
	}
	lower := strings.ToLower(p)
	return strings.HasPrefix(lower, "file::memory:") || strings.Contains(lower, "mode=memory")
}

func loadEmbeddedMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	var migs []migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		ver, err := parseMigrationVersion(entry.Name())
		if err != nil {
			return nil, err
		}
		raw, err := fs.ReadFile(migrationsFS, "migrations/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		migs = append(migs, migration{
			Version: ver,
			Name:    entry.Name(),
			SQL:     string(raw),
		})
	}
	sort.Slice(migs, func(i, j int) bool {
		return migs[i].Version < migs[j].Version
	})
	return migs, nil
}

func parseMigrationVersion(filename string) (int, error) {
	idx := strings.IndexByte(filename, '_')
	if idx <= 0 {
		return 0, fmt.Errorf("invalid migration filename %q: missing version prefix", filename)
	}
	ver, err := strconv.Atoi(filename[:idx])
	if err != nil || ver <= 0 {
		return 0, fmt.Errorf("invalid migration version in %q", filename)
	}
	return ver, nil
}

func (s *Store) runMigrations(ctx context.Context, migs []migration, isMemory bool, preExistingNonEmpty bool) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version     INTEGER PRIMARY KEY,
			name        TEXT NOT NULL,
			applied_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		);
	`); err != nil {
		return fmt.Errorf("ensure schema_migrations table: %w", err)
	}

	applied := make(map[int]bool)
	currentVer := 0
	rows, err := s.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan migration version: %w", err)
		}
		applied[v] = true
		if v > currentVer {
			currentVer = v
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate applied migrations: %w", err)
	}
	_ = rows.Close()

	var pending []migration
	for _, m := range migs {
		if !applied[m.Version] {
			pending = append(pending, m)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	// 升级非空磁盘数据库到更高版本前，先刷盘 WAL 并创建 .bak.<ver> 备份（保留升级前版本号），出错则中止迁移。
	if !isMemory && (currentVer > 0 || preExistingNonEmpty) {
		if _, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE);`); err != nil {
			return fmt.Errorf("checkpoint WAL before backup: %w", err)
		}
		backupVer := currentVer
		if backupVer <= 0 {
			backupVer = pending[len(pending)-1].Version
		}
		backupPath := fmt.Sprintf("%s.bak.%d", s.path, backupVer)
		if err := copyFileAtomic(s.path, backupPath); err != nil {
			return fmt.Errorf("backup db to %q before migration v%d: %w", backupPath, pending[len(pending)-1].Version, err)
		}
	}

	for _, m := range pending {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", m.Name, err)
		}
		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("execute migration %s: %w", m.Name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, name) VALUES (?, ?)`,
			m.Version, m.Name,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", m.Name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.Name, err)
		}
	}

	return nil
}

func (s *Store) seedDefaultModelRoutes(ctx context.Context) error {
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		for _, r := range domain.DefaultModelRoutes() {
			if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO model_routes (stage, provider_id, model_name, temperature, max_tokens)
				VALUES (?, NULL, ?, ?, ?)
			`, string(r.Stage), r.ModelName, r.Temperature, r.MaxTokens); err != nil {
				return fmt.Errorf("seed model route %s: %w", r.Stage, err)
			}
		}
		return nil
	})
}

func copyFileAtomic(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dir := filepath.Dir(dstPath)
	tmp, err := os.CreateTemp(dir, ".nib-db-bak-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (s *Store) withWriteTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return mapSQLiteError(err)
	}
	return nil
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func mapSQLiteError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	var sqlErr *sqlite.Error
	if errors.As(err, &sqlErr) {
		switch sqlErr.Code() {
		case sqliteConstraintUniqueCode, sqliteConstraintPrimaryKeyCode:
			return fmt.Errorf("%w: %v", domain.ErrConflict, err)
		case sqliteConstraintForeignKeyCode:
			return fmt.Errorf("%w: %v", domain.ErrNotFound, err)
		case sqliteConstraintCheckCode, sqliteConstraintCode:
			return fmt.Errorf("%w: %v", domain.ErrValidation, err)
		}
	}
	return err
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
