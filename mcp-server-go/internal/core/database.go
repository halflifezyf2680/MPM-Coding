package core

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// DatabaseManager 数据库连接管理器
type DatabaseManager struct {
	dbPath string
	db     *sql.DB
	mu     sync.Mutex
}

var (
	instances = make(map[string]*DatabaseManager)
	instLock  sync.Mutex
)

// GetDBForProject 获取指定项目的数据库管理器实例
func GetDBForProject(projectRoot string) (*DatabaseManager, error) {
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, err
	}

	instLock.Lock()
	defer instLock.Unlock()

	if mgr, ok := instances[absRoot]; ok {
		return mgr, nil
	}

	// 验证项目路径
	if !ValidateProjectPath(absRoot) {
		return nil, fmt.Errorf("invalid project path: %s", absRoot)
	}

	// 使用 GetDataDir 获取数据目录（自动处理迁移）
	dataDir, err := GetDataDir(absRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to get data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "mcp_memory.db")
	mgr := &DatabaseManager{
		dbPath: dbPath,
	}

	if err := mgr.init(); err != nil {
		return nil, err
	}

	instances[absRoot] = mgr
	return mgr, nil
}

// NewDatabaseManager 创建一个新的数据库管理器实例（用于非项目级数据库，如全局 Prompt 库）
func NewDatabaseManager(dbPath string) (*DatabaseManager, error) {
	mgr := &DatabaseManager{
		dbPath: dbPath,
	}
	if err := mgr.init(); err != nil {
		return nil, err
	}
	return mgr, nil
}

func (m *DatabaseManager) init() error {
	// 确保目录存在
	dir := filepath.Dir(m.dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	db, err := sql.Open("sqlite", m.dbPath)
	if err != nil {
		return err
	}

	// SQLite 单写多读场景下，限制到单连接可显著降低本进程内锁竞争。
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// 性能与并发优化 (WAL 模式)
	pragmas := []string{
		"PRAGMA busy_timeout = 30000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
	}

	for _, p := range pragmas {
		if err := exec_sql_with_retry(db, p); err != nil {
			db.Close()
			return err
		}
	}

	m.db = db

	// 执行 Schema 自愈
	if err := with_sqlite_busy_retry(func() error { return m.healSchema() }); err != nil {
		fmt.Fprintf(os.Stderr, "[DB][WARN] Schema healing failed: %v\n", err)
	}

	return nil
}

func exec_sql_with_retry(db *sql.DB, query string) error {
	return with_sqlite_busy_retry(func() error {
		_, err := db.Exec(query)
		return err
	})
}

func with_sqlite_busy_retry(fn func() error) error {
	const maxAttempts = 6
	const baseDelay = 200 * time.Millisecond

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := fn(); err != nil {
			lastErr = err
			if !is_sqlite_busy_error(err) || attempt == maxAttempts {
				break
			}
			time.Sleep(time.Duration(attempt) * baseDelay)
			continue
		}
		return nil
	}

	return lastErr
}

func is_sqlite_busy_error(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "sqlite_busy")
}

func (m *DatabaseManager) healSchema() error {
	// 1. 确保核心表存在
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS memos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			category TEXT,
			entity TEXT,
			act TEXT,
			path TEXT,
			content TEXT,
			session_id TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			task_id TEXT PRIMARY KEY,
			description TEXT,
			task_type TEXT,
			parent_task_id TEXT,
			understanding TEXT,
			execution_plan TEXT,
			status TEXT DEFAULT 'in_progress',
			meta_data TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME,
			summary TEXT,
			pitfalls TEXT,
			current_focus TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS known_facts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT,
			summarize TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS system_state (
			key TEXT PRIMARY KEY,
			value TEXT,
			category TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS pending_hooks (
			hook_id TEXT PRIMARY KEY,
			description TEXT,
			priority TEXT DEFAULT 'medium',
			context TEXT,
			result_summary TEXT,
			related_task_id TEXT,
			expires_at DATETIME,
			status TEXT DEFAULT 'open',
			tag TEXT,
			summary TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS task_chains (
			task_id TEXT PRIMARY KEY,
			description TEXT,
			protocol TEXT DEFAULT 'linear',
			status TEXT DEFAULT 'running',
			phases_json TEXT,
			current_phase TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS task_chain_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL,
			phase_id TEXT,
			sub_id TEXT,
			event_type TEXT NOT NULL,
			payload TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (task_id) REFERENCES task_chains(task_id)
		)`,
	}

	for _, s := range schemas {
		if _, err := m.db.Exec(s); err != nil {
			return err
		}
	}

	// 2. 索引优化
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_memos_entity ON memos(entity)",
		"CREATE INDEX IF NOT EXISTS idx_memos_category ON memos(category)",
		"CREATE INDEX IF NOT EXISTS idx_memos_timestamp ON memos(timestamp DESC)",
		"CREATE INDEX IF NOT EXISTS idx_task_chain_events_task ON task_chain_events(task_id, created_at)",
	}
	for _, idx := range indexes {
		if _, err := m.db.Exec(idx); err != nil {
			return err
		}
	}

	// 3. 数据迁移（ADD COLUMN）
	migrations := []struct {
		sql  string
		name string
	}{
		{"ALTER TABLE task_chains ADD COLUMN reinit_count INTEGER DEFAULT 0", "reinit_count"},
		{"ALTER TABLE task_chains ADD COLUMN plan_state TEXT", "plan_state"},
	}
	for _, mig := range migrations {
		err := with_sqlite_busy_retry(func() error {
			_, err := m.db.Exec(mig.sql)
			return err
		})
		if err != nil {
			// 检查是否是"列已存在"错误（SQLite 返回 "duplicate column name"）
			errMsg := strings.ToLower(err.Error())
			if strings.Contains(errMsg, "duplicate column") {
				fmt.Fprintf(os.Stderr, "[DB][INFO] Column %s already exists, skip\n", mig.name)
			} else {
				// 其他错误打印警告但不中断（兼容旧库升级）
				fmt.Fprintf(os.Stderr, "[DB][WARN] Migration %s failed: %v\n", mig.name, err)
			}
		} else {
			fmt.Fprintf(os.Stderr, "[DB][INFO] Migration %s applied\n", mig.name)
		}
	}

	return nil
}

// Exec 执行写操作
func (m *DatabaseManager) Exec(query string, args ...interface{}) (sql.Result, error) {
	return m.db.Exec(query, args...)
}

// QueryRow 执行单行查询
func (m *DatabaseManager) QueryRow(query string, args ...interface{}) *sql.Row {
	return m.db.QueryRow(query, args...)
}

// Query 执行多行查询
func (m *DatabaseManager) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return m.db.Query(query, args...)
}

// WithTx 在单个事务中执行数据库写入逻辑。
func (m *DatabaseManager) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	return with_sqlite_busy_retry(func() error {
		tx, err := m.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := fn(tx); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			_ = tx.Rollback()
			return err
		}
		return nil
	})
}

// Close 关闭连接
func (m *DatabaseManager) Close() error {
	if m.db != nil {
		return m.db.Close()
	}
	return nil
}
