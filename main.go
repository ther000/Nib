package main

import (
	"context"
	"log"
	"path/filepath"
	"sync"

	"github.com/egoist/mygo"

	"nib/app"
	"nib/internal/compiler"
	"nib/internal/exporter"
	"nib/internal/llm"
	"nib/internal/memory"
	"nib/internal/pipeline"
	"nib/internal/store"
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
	rt := app.NewRuntime()
	mygo.Bind(
		app.NewLibraryService(rt),
		app.NewAtlasService(rt),
		app.NewSettingsService(rt),
		app.NewStudioService(rt),
		app.NewMemoryService(rt),
		app.NewStyleService(rt),
		app.NewBatchService(rt),
		app.NewAutopilotService(rt),
		app.NewDirectorService(rt),
	)

	var shutdownOnce sync.Once
	var closerMu sync.Mutex
	var batchCloser func()
	runShutdown := func() {
		shutdownOnce.Do(func() {
			closerMu.Lock()
			bc := batchCloser
			closerMu.Unlock()
			if bc != nil {
				bc()
			}
			if err := rt.Close(); err != nil {
				log.Printf("nib: close runtime failed: %v", err)
			}
		})
	}

	mygo.App.WhenReady(func() {
		dataDir, err := mygo.App.Path(mygo.PathUserData)
		if err != nil {
			log.Printf("nib: resolve user data dir failed: %v", err)
			rt.SetInitError(err)
		} else {
			dbPath := filepath.Join(dataDir, "nib.db")
			closer, initErr := initRuntime(rt, dbPath)
			if initErr != nil {
				log.Printf("nib: init runtime (%s) failed: %v", dbPath, initErr)
				rt.SetInitError(initErr)
			} else {
				closerMu.Lock()
				batchCloser = closer
				closerMu.Unlock()
			}
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
			TitleBarStyle:   mygo.TitleBarHidden,
			TitleBarHeight:  44,
			BackgroundColor: "light-dark(#F6F3EC, #141416)",
		})
		mainWin.SetWindowButtonsTheme(mygo.ThemeSystem)
		mainWin.OnReadyToShow(mainWin.Show)
	})

	mygo.App.OnBeforeQuit(func(_ *mygo.QuitEvent) {
		// 同步取消运行中流式任务、等待保底落库事务完成并关闭 SQLite 连接（兼容正常退出与 Windows SessionEnding）。
		runShutdown()
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	runShutdown()
}

// initRuntime 在 WhenReady 回调内打开并迁移本地 SQLite，恢复异常中断的微批队列，装配全部 Phase 1–4 纯 Go 核心引擎。
func initRuntime(rt *app.Runtime, dbPath string) (func(), error) {
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	if err := st.RecoverInterruptedBatchRuns(context.Background()); err != nil {
		log.Printf("nib: recover interrupted batch runs warning: %v", err)
	}
	if err := st.RecoverInterruptedAutopilotRuns(context.Background()); err != nil {
		log.Printf("nib: recover interrupted autopilot runs warning: %v", err)
	}

	storeExp := exporter.NewStoreExporter(st)
	pipeExp := exporter.New()
	llmClient := llm.NewClient()
	comp := compiler.New(st, compiler.WithMemorySource(st), compiler.WithStyleSource(st))
	extractor := memory.NewExtractor(st, llmClient)
	runner := pipeline.New(
		st,
		comp,
		llmClient,
		pipeExp,
		pipeline.WithStyleStore(st),
		pipeline.WithBatchStore(st),
		pipeline.WithDeltaExtractor(extractor),
		pipeline.WithProgressListener(app.EmitBatchProgress),
		pipeline.WithAutopilotListener(app.EmitAutopilotProgress),
	)

	rt.Init(app.RuntimeDeps{
		Store:           st,
		Exporter:        storeExp,
		LLM:             llmClient,
		Compiler:        comp,
		Pipeline:        runner,
		MemoryStore:     st,
		DeltaExtractor:  extractor,
		StyleStore:      st,
		StyleEngine:     runner,
		BatchStore:      st,
		BatchEngine:     runner,
		GenesisEngine:   runner,
		AutopilotEngine: runner,
		DirectorEngine:  runner,
	})

	// 每日滚动备份与过期回收站清理改为异步后台执行，避免大库阻塞首屏窗口弹出。
	backupDir := filepath.Join(filepath.Dir(dbPath), "backups")
	rt.RunBackground(func(bgCtx context.Context) {
		if _, err := st.CreateAutoBackup(bgCtx, backupDir, 7); err != nil && bgCtx.Err() == nil {
			log.Printf("nib: daily auto backup warning: %v", err)
		}
		if _, err := st.CleanupExpiredTrash(bgCtx); err != nil && bgCtx.Err() == nil {
			log.Printf("nib: cleanup expired trash warning: %v", err)
		}
	})

	return runner.Close, nil
}
