package main

import (
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
	)

	var shutdownOnce sync.Once
	runShutdown := func() {
		shutdownOnce.Do(func() {
			if err := rt.Close(); err != nil {
				log.Printf("nib: close runtime failed: %v", err)
			}
		})
	}

	mygo.App.WhenReady(func() {
		dataDir, err := mygo.App.Path(mygo.PathUserData)
		if err != nil {
			log.Printf("nib: resolve user data dir failed: %v", err)
		} else {
			dbPath := filepath.Join(dataDir, "nib.db")
			if err := initRuntime(rt, dbPath); err != nil {
				log.Printf("nib: init runtime (%s) failed: %v", dbPath, err)
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
			BackgroundColor: "light-dark(#F6F3EC, #141416)",
		})
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

// initRuntime 在 WhenReady 回调内打开并迁移本地 SQLite，装配全部 Phase 1–3 纯 Go 核心引擎。
func initRuntime(rt *app.Runtime, dbPath string) error {
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}

	storeExp := exporter.NewStoreExporter(st)
	pipeExp := exporter.New()
	llmClient := llm.NewClient()
	comp := compiler.New(st, compiler.WithMemorySource(st), compiler.WithStyleSource(st))
	runner := pipeline.New(st, comp, llmClient, pipeExp, pipeline.WithStyleStore(st))
	extractor := memory.NewExtractor(st, llmClient)

	rt.Init(app.RuntimeDeps{
		Store:          st,
		Exporter:       storeExp,
		LLM:            llmClient,
		Compiler:       comp,
		Pipeline:       runner,
		MemoryStore:    st,
		DeltaExtractor: extractor,
		StyleStore:     st,
		StyleEngine:    runner,
	})
	return nil
}
