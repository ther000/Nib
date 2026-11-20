package main

import (
	"context"
	"log"
	"path/filepath"
	"sync"

	"github.com/egoist/mygo"
)

func main() {
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	var mainWin *mygo.Window
	mygo.App.OnSecondInstance(func(_ []string, _ string) {
		if mainWin != nil {
			mainWin.Restore()
			mainWin.Focus()
		}
	})

	// 仅在顶层注册 Service 绑定与生命周期回调；严禁在 main() 顶层打开 SQLite，
	// 避免 `mygo generate` 执行 main() 至 App.Run() 时触碰本地数据库。
	// Phase 1 各 Service（LibraryService / AtlasService / SettingsService / StudioService）
	// 落地后统一在此通过 mygo.Bind(...) 注册。

	appCtx, cancelApp := context.WithCancel(context.Background())
	var (
		shutdownOnce sync.Once
		onShutdown   func()
	)
	runShutdown := func() {
		shutdownOnce.Do(func() {
			cancelApp()
			if onShutdown != nil {
				onShutdown()
			}
		})
	}

	mygo.App.WhenReady(func() {
		dataDir, err := mygo.App.Path(mygo.PathUserData)
		if err != nil {
			log.Printf("nib: resolve user data dir failed: %v", err)
		} else {
			dbPath := filepath.Join(dataDir, "nib.db")
			onShutdown = initRuntime(appCtx, dbPath)
		}

		mainWin = mygo.NewWindow(mygo.WindowOptions{
			Title:           "Nib",
			URL:             "/",
			Width:           1360,
			Height:          860,
			MinWidth:        960,
			MinHeight:       640,
			StateKey:        "main",
			Hidden:          true,
			BackgroundColor: "light-dark(#F6F3EC, #141416)",
		})
		mainWin.OnReadyToShow(mainWin.Show)
	})

	mygo.App.OnBeforeQuit(func(_ *mygo.QuitEvent) {
		// 同步取消运行中任务并安全关闭数据库连接（兼容正常退出与 Windows SessionEnding）。
		runShutdown()
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	runShutdown()
}

// initRuntime 是 WhenReady 内的运行时装配钩子。
// 后续接入 internal/store 与 app/ 服务时在此打开 SQLite（filepath.Join(dataDir, "nib.db")）、
// 执行编号迁移并返回同步关闭函数。
func initRuntime(_ context.Context, _ string) func() {
	return func() {}
}
