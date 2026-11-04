package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"
)

// Set by the portable Windows build; command-line/server builds keep existing defaults.
var defaultDesktop = "false"

func main() {
	if defaultDesktop == "true" {
		if exe, err := os.Executable(); err == nil {
			_ = os.Chdir(filepath.Dir(exe))
		}
	}
	desktop := flag.Bool("desktop", defaultDesktop == "true", "以 Windows 客戶端視窗啟動")
	addr := flag.String("addr", "127.0.0.1:8080", "监听地址（仅本机）")
	root := flag.String("data-dir", "data", "数据目录")
	assets := flag.String("web-dir", "frontend/dist", "前端构建目录")
	font := flag.String("font", "assets/fonts/NotoSansSC-Regular.ttf", "PDF中文字体")
	organize := flag.Bool("organize-documents", false, "按内容整理凭证、自动命名并清理重复副本，然后退出")
	restore := flag.String("restore-backup", "", "將備份ZIP恢復至空白帳簿後退出")
	envFile := flag.String("env-file", ".env", "可選接口配置文件；空值禁用")
	flag.Parse()
	if *desktop {
		prepareDesktopLog(*root)
		explicitAddr := false
		flag.Visit(func(f *flag.Flag) {
			if f.Name == "addr" {
				explicitAddr = true
			}
		})
		if !explicitAddr {
			*addr = "127.0.0.1:0"
		}
		release, already, err := acquireDesktop(*root)
		if err != nil {
			startupError(true, err)
		}
		if already {
			return
		}
		defer release()
	}
	explicitEnv := false
	configLocked := map[string]bool{}
	for key := range ocrEnvKeys {
		if _, exists := os.LookupEnv(key); exists {
			configLocked[key] = true
		}
	}
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "env-file" {
			explicitEnv = true
		}
	})
	if e := loadEnvFile(*envFile, !explicitEnv); e != nil {
		startupError(*desktop, e)
	}
	host, _, e := net.SplitHostPort(*addr)
	if e != nil || host != "127.0.0.1" && host != "localhost" && host != "::1" {
		startupError(*desktop, "只允许绑定本机地址")
	}
	absRoot, e := filepath.Abs(*root)
	if e != nil {
		startupError(*desktop, e)
	}
	s, e := openStore(absRoot)
	if e != nil {
		startupError(*desktop, e)
	}
	defer s.db.Close()
	if *restore != "" {
		result, err := s.restoreBackup(context.Background(), *restore)
		if err != nil {
			startupError(*desktop, err)
		}
		json.NewEncoder(os.Stdout).Encode(result)
		return
	}
	if e = s.retireReferenceIssues(); e != nil {
		startupError(*desktop, "参考问题模块清理失败: ", e)
	}
	if *organize {
		result, err := s.organizeDocuments()
		if err != nil {
			startupError(*desktop, err)
		}
		json.NewEncoder(os.Stdout).Encode(result)
		return
	}
	if e = s.upgradeReviewPolicy(); e != nil {
		startupError(*desktop, "核验规则升级失败: ", e)
	}
	s.ocr = ocrFromEnv()
	a := &server{s: s, assets: *assets, font: *font, envFile: *envFile, configLocked: configLocked}
	srv := &http.Server{Addr: *addr, Handler: a.routes(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute, WriteTimeout: 10 * time.Minute, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		srv.Shutdown(c)
	}()
	listener, e := net.Listen("tcp", *addr)
	if e != nil {
		startupError(*desktop, e)
	}
	log.Printf("港賬通 http://%s · 資料目錄 %s", listener.Addr(), absRoot)
	if *desktop {
		serveErrors := make(chan error, 1)
		go func() { serveErrors <- srv.Serve(listener) }()
		if e = runDesktop("http://"+listener.Addr().String(), absRoot); e != nil {
			showDesktopError(e.Error())
		}
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if e = srv.Shutdown(c); e != nil {
			log.Print(e)
		}
		// An already-started reorganisation must finish before its database closes.
		a.mu.Lock()
		a.mu.Unlock()
		if e = <-serveErrors; e != nil && !errors.Is(e, http.ErrServerClosed) {
			log.Print(e)
		}
		return
	}
	if e = srv.Serve(listener); e != nil && !errors.Is(e, http.ErrServerClosed) {
		startupError(false, e)
	}
}

func startupError(desktop bool, values ...any) {
	message := fmt.Sprint(values...)
	if desktop {
		showDesktopError(message)
	}
	log.Fatal(message)
}
