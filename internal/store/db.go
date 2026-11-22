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
	"time"

	"modernc.org/sqlite"

	"nib/internal/domain"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// initPragmas 按 AGENTS.md §3.2 规定顺序在每条连接初始化时执行（先设 busy_timeout 防止并发开连接即撞锁）。
var initPragmas = []string{
	"PRAGMA busy_timeout = 5000;",
	"PRAGMA journal_mode = WAL;",
	"PRAGMA foreign_keys = ON;",
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
	db            *sql.DB
	path          string
	writeMu       sync.Mutex
	schemaVersion int
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
	var currentVersion sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&currentVersion); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("read schema version: %w", err)
	}
	s.schemaVersion = int(currentVersion.Int64)
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

// BackupToFile 通过 SQLite VACUUM INTO 将当前一致性快照原子备份至目标路径（不受并发读连接阻塞）。
func (s *Store) BackupToFile(ctx context.Context, targetPath string) error {
	if s == nil || s.db == nil {
		return errors.New("database not initialized")
	}
	if isMemoryPath(s.path) {
		return fmt.Errorf("%w: 内存数据库不支持文件备份", domain.ErrValidation)
	}
	cleanTarget := filepath.Clean(strings.TrimSpace(targetPath))
	if cleanTarget == "" || cleanTarget == "." {
		return fmt.Errorf("%w: 备份目标路径不能为空", domain.ErrValidation)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.backupToFileLocked(ctx, cleanTarget)
}

func (s *Store) backupToFileLocked(ctx context.Context, cleanTarget string) error {
	dir := filepath.Dir(cleanTarget)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create backup target dir: %w", err)
		}
	}
	tmp, err := os.CreateTemp(dir, ".nib-db-bak-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(tmpPath)
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := s.db.ExecContext(ctx, `VACUUM INTO `+quoteSQLiteStringLiteral(tmpPath)); err != nil {
		return fmt.Errorf("vacuum backup into %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, cleanTarget); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func quoteSQLiteStringLiteral(val string) string {
	return `'` + strings.ReplaceAll(val, `'`, `''`) + `'`
}

// CreateAutoBackup 在指定目录创建当日滚动备份；同日已存在则跳过，并只保留最近 keep 份。
func (s *Store) CreateAutoBackup(ctx context.Context, backupDir string, keep int) (string, error) {
	if s == nil || s.db == nil {
		return "", errors.New("database not initialized")
	}
	if isMemoryPath(s.path) {
		return "", nil
	}
	if keep <= 0 {
		keep = 7
	}
	dir := strings.TrimSpace(backupDir)
	if dir == "" {
		return "", fmt.Errorf("%w: 备份目录不能为空", domain.ErrValidation)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	target := filepath.Join(dir, "nib-"+time.Now().Format("20060102")+".db")
	if fi, err := os.Stat(target); err == nil && fi.Size() > 0 {
		return target, nil
	}
	if err := s.BackupToFile(ctx, target); err != nil {
		return "", err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return target, nil
	}
	backups := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "nib-") || !strings.HasSuffix(name, ".db") {
			continue
		}
		backups = append(backups, filepath.Join(dir, name))
	}
	sort.Strings(backups)
	for len(backups) > keep {
		remove := backups[0]
		backups = backups[1:]
		_ = os.Remove(remove)
	}
	return target, nil
}

// RestoreDataFromFile 校验备份文件并把其中数据整体恢复进当前库。
// 恢复前会先把当前库热备为 <db>.pre-restore-*.db；恢复通过 ATTACH + 事务整表复制完成，
// 当前 Store 的连接池与上层引擎无需重建即可看到恢复后的数据。
func (s *Store) RestoreDataFromFile(ctx context.Context, backupPath string) (string, error) {
	if s == nil || s.db == nil {
		return "", errors.New("database not initialized")
	}
	if isMemoryPath(s.path) {
		return "", fmt.Errorf("%w: 内存数据库不支持恢复", domain.ErrValidation)
	}
	source := filepath.Clean(strings.TrimSpace(backupPath))
	if source == "" || source == "." {
		return "", fmt.Errorf("%w: 备份文件路径不能为空", domain.ErrValidation)
	}
	if fi, err := os.Stat(source); err != nil || fi.IsDir() || fi.Size() == 0 {
		return "", fmt.Errorf("%w: 备份文件不存在或为空", domain.ErrValidation)
	}
	if samePath, err := filepath.Abs(source); err == nil {
		if current, err := filepath.Abs(s.path); err == nil && strings.EqualFold(samePath, current) {
			return "", fmt.Errorf("%w: 不能恢复当前正在使用的数据库", domain.ErrValidation)
		}
	}

	stageParent := filepath.Dir(s.path)
	stageDir, err := os.MkdirTemp(stageParent, ".nib-restore-stage-*")
	if err != nil {
		return "", fmt.Errorf("create restore staging dir: %w", err)
	}
	defer os.RemoveAll(stageDir)
	stagePath := filepath.Join(stageDir, "stage.db")

	if err := copyFileAtomic(source, stagePath); err != nil {
		return "", fmt.Errorf("stage backup file: %w", err)
	}

	// 先把备份升级到当前最新 schema，旧备份也能安全恢复（附属 .bak/-wal/-shm 均随 stageDir 一并清理）。
	staged, err := Open(stagePath)
	if err != nil {
		return "", fmt.Errorf("validate backup database: %w", err)
	}
	if staged.schemaVersion > s.schemaVersion {
		_ = staged.Close()
		return "", fmt.Errorf("%w: 备份来自更新版本的 Nib，请先升级应用", domain.ErrValidation)
	}
	if _, err := staged.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		_ = staged.Close()
		return "", fmt.Errorf("checkpoint staged backup: %w", err)
	}
	if err := staged.Close(); err != nil {
		return "", err
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	preRestorePath := fmt.Sprintf("%s.pre-restore-%s.db", s.path, time.Now().Format("20060102-150405"))
	if err := s.backupToFileLocked(ctx, preRestorePath); err != nil {
		return "", fmt.Errorf("backup current database before restore: %w", err)
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	cleanCtx := context.WithoutCancel(ctx)
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return "", err
	}
	defer func() { _, _ = conn.ExecContext(cleanCtx, `PRAGMA foreign_keys = ON`) }()

	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS restore`, stagePath); err != nil {
		return "", fmt.Errorf("attach restore database: %w", err)
	}
	defer func() { _, _ = conn.ExecContext(cleanCtx, `DETACH DATABASE restore`) }()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return "", err
	}
	inTx := true
	defer func() {
		if inTx {
			_, _ = conn.ExecContext(cleanCtx, `ROLLBACK`)
		}
	}()
	if _, err := conn.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return "", err
	}

	names, err := tableNamesForRestore(ctx, conn)
	if err != nil {
		return "", err
	}
	for _, name := range names {
		quoted := quoteSQLiteIdentifier(name)
		cols, colErr := commonTableColumns(ctx, conn, name)
		if colErr != nil {
			return "", colErr
		}
		if len(cols) == 0 {
			continue
		}
		quotedCols := make([]string, len(cols))
		for i, c := range cols {
			quotedCols[i] = quoteSQLiteIdentifier(c)
		}
		colList := strings.Join(quotedCols, ", ")
		if _, err := conn.ExecContext(ctx, `DELETE FROM main.`+quoted); err != nil {
			return "", fmt.Errorf("clear table %s: %w", name, err)
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO main.`+quoted+` (`+colList+`) SELECT `+colList+` FROM restore.`+quoted); err != nil {
			return "", fmt.Errorf("copy table %s: %w", name, err)
		}
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO main.chapters_fts(chapters_fts) VALUES ('rebuild')`); err != nil {
		return "", fmt.Errorf("rebuild chapters_fts: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO main.entities_fts(entities_fts) VALUES ('rebuild')`); err != nil {
		return "", fmt.Errorf("rebuild entities_fts: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return "", err
	}
	inTx = false
	return preRestorePath, nil
}

func commonTableColumns(ctx context.Context, conn *sql.Conn, tableName string) ([]string, error) {
	readCols := func(schema string) ([]string, map[string]bool, error) {
		rows, err := conn.QueryContext(ctx, fmt.Sprintf(`PRAGMA %s.table_info(%s)`, schema, quoteSQLiteIdentifier(tableName)))
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()
		var list []string
		set := make(map[string]bool)
		for rows.Next() {
			var (
				cid       int
				name      string
				colType   string
				notNull   int
				dfltValue sql.NullString
				pk        int
			)
			if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
				return nil, nil, err
			}
			list = append(list, name)
			set[name] = true
		}
		return list, set, rows.Err()
	}

	mainCols, _, err := readCols("main")
	if err != nil {
		return nil, err
	}
	_, restoreSet, err := readCols("restore")
	if err != nil {
		return nil, err
	}
	common := make([]string, 0, len(mainCols))
	for _, col := range mainCols {
		if restoreSet[col] {
			common = append(common, col)
		}
	}
	return common, nil
}

func tableNamesForRestore(ctx context.Context, conn *sql.Conn) ([]string, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT name FROM main.sqlite_master
		WHERE type = 'table'
		  AND name NOT LIKE 'sqlite_%'
		  AND name NOT LIKE '%_fts%'
		  AND name <> 'schema_migrations'
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		var exists int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM restore.sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&exists); err != nil {
			return nil, err
		}
		if exists == 0 {
			continue
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func quoteSQLiteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
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

	// 升级非空磁盘数据库到更高版本前，先创建一致性 .bak.<ver> 备份（保留升级前版本号），出错则中止迁移。
	if !isMemory && (currentVer > 0 || preExistingNonEmpty) {
		backupVer := currentVer
		if backupVer <= 0 {
			backupVer = pending[len(pending)-1].Version
		}
		backupPath := fmt.Sprintf("%s.bak.%d", s.path, backupVer)
		if err := s.backupToFileLocked(ctx, backupPath); err != nil {
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
