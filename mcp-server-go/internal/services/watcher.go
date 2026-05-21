package services

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileWatcher 监控项目目录变更，收集变更文件路径，标记索引为脏。
// 设计为懒更新：文件变更只标记 stale，下次查询时才触发增量索引。
type FileWatcher struct {
	watcher    *fsnotify.Watcher
	projectRoot string
	debounceMs int

	mu         sync.Mutex
	staleFiles map[string]struct{}
	timer      *time.Timer
	stopped    bool

	onStale func(changedFiles []string)
}

// WatcherConfig 文件监控配置
type WatcherConfig struct {
	DebounceMs int                   // 防抖毫秒数，默认 2000
	OnStale    func(changedFiles []string) // 变更回调（标记脏）
}

// 需要排除的目录名
var excludeDirNames = map[string]bool{
	".git":         true,
	".mpm-data":    true,
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
	".cache":       true,
	".sass-cache":  true,
	"dist":         true,
	"build":        true,
	"target":       true,
	"bin":          true,
	".idea":        true,
	".vscode":      true,
	".claude":      true,
}

// NewFileWatcher 创建文件监控器
func NewFileWatcher(projectRoot string, cfg WatcherConfig) (*FileWatcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	debounceMs := cfg.DebounceMs
	if debounceMs <= 0 {
		debounceMs = 2000
	}

	w := &FileWatcher{
		watcher:     fw,
		projectRoot: projectRoot,
		debounceMs:  debounceMs,
		staleFiles:  make(map[string]struct{}),
		onStale:     cfg.OnStale,
	}

	return w, nil
}

// Start 启动监控。递归添加项目子目录，启动事件消费 goroutine。
func (w *FileWatcher) Start() error {
	// 递归添加目录
	if err := w.addWatchDirs(w.projectRoot); err != nil {
		return err
	}

	go w.consumeEvents()
	return nil
}

// Stop 停止监控
func (w *FileWatcher) Stop() {
	w.mu.Lock()
	w.stopped = true
	if w.timer != nil {
		w.timer.Stop()
	}
	w.mu.Unlock()
	w.watcher.Close()
}

// IsStale 返回是否有未同步的变更文件
func (w *FileWatcher) IsStale() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.staleFiles) > 0
}

// DrainStaleFiles 取出并清空变更文件列表
func (w *FileWatcher) DrainStaleFiles() []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	files := make([]string, 0, len(w.staleFiles))
	for f := range w.staleFiles {
		files = append(files, f)
	}
	w.staleFiles = make(map[string]struct{})
	return files
}

// addWatchDirs 递归添加需要监控的目录
func (w *FileWatcher) addWatchDirs(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 跳过不可访问的目录
		}
		if !info.IsDir() {
			return nil
		}

		name := info.Name()
		if excludeDirNames[name] {
			return filepath.SkipDir
		}

		// fsnotify 内部已做去重，直接 Add 即可
		_ = w.watcher.Add(path)
		return nil
	})
}

// consumeEvents 消费文件变更事件
func (w *FileWatcher) consumeEvents() {
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)

		case _, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// handleEvent 处理单个文件变更事件
func (w *FileWatcher) handleEvent(event fsnotify.Event) {
	// 只关注写、创建、删除、重命名
	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
		return
	}

	path := event.Name

	// 新建目录时动态添加监控
	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			// 检查排除目录
			if !excludeDirNames[filepath.Base(path)] {
				_ = w.addWatchDirs(path)
			}
			return
		}
	}

	// 转为相对路径
	rel, err := filepath.Rel(w.projectRoot, path)
	if err != nil {
		return
	}
	rel = filepath.ToSlash(rel)

	// 排除不关心的文件
	if w.shouldIgnore(rel) {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped {
		return
	}

	w.staleFiles[rel] = struct{}{}
	w.scheduleFlush()
}

// shouldIgnore 判断文件是否需要忽略
func (w *FileWatcher) shouldIgnore(relPath string) bool {
	// 排除 .mpm-data 下的所有文件
	if strings.HasPrefix(relPath, ".mpm-data/") {
		return true
	}
	if strings.HasPrefix(relPath, ".git/") {
		return true
	}

	// 只关注源代码文件（按扩展名粗筛）
	ext := strings.ToLower(filepath.Ext(relPath))
	if ext == "" {
		return true // 无扩展名文件忽略
	}

	// 排除非代码文件
	nonCodeExts := map[string]bool{
		".log": true, ".tmp": true, ".bak": true, ".swp": true,
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
		".ico": true, ".svg": true, ".woff": true, ".woff2": true,
		".ttf": true, ".eot": true, ".mp3": true, ".mp4": true,
		".zip": true, ".tar": true, ".gz": true, ".exe": true,
		".dll": true, ".so": true, ".dylib": true, ".db": true,
		".db-wal": true, ".db-shm": true,
	}
	return nonCodeExts[ext]
}

// scheduleFlush 防抖调度，静默期结束后触发回调
func (w *FileWatcher) scheduleFlush() {
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(time.Duration(w.debounceMs)*time.Millisecond, func() {
		w.mu.Lock()
		defer w.mu.Unlock()

		if w.stopped || len(w.staleFiles) == 0 {
			return
		}

		files := w.DrainStaleFiles()
		if w.onStale != nil {
			w.onStale(files)
		}
	})
}
