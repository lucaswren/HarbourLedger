//go:build windows

package main

import (
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	webview "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

var desktopUser32 = windows.NewLazySystemDLL("user32.dll")
var desktopProperty string

//go:embed desktop-icon.png
var desktopIconPNG []byte

func setDesktopIcon(hwnd uintptr) uintptr {
	const wmSetIcon = 0x0080
	icon, _, err := desktopUser32.NewProc("CreateIconFromResourceEx").Call(
		uintptr(unsafe.Pointer(&desktopIconPNG[0])), uintptr(len(desktopIconPNG)), 1, 0x00030000, 32, 32, 0,
	)
	if icon == 0 {
		log.Printf("無法載入視窗圖示: %v", err)
		return 0
	}
	sendMessage := desktopUser32.NewProc("SendMessageW")
	sendMessage.Call(hwnd, wmSetIcon, 0, icon) // ICON_SMALL
	sendMessage.Call(hwnd, wmSetIcon, 1, icon) // ICON_BIG
	return icon
}

func prepareDesktopLog(root string) {
	if os.MkdirAll(root, 0755) != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(root, "客戶端.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		log.SetOutput(f)
	}
}
func showDesktopError(message string) {
	text, _ := windows.UTF16PtrFromString(message)
	title, _ := windows.UTF16PtrFromString("港賬通")
	desktopUser32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}
func acquireDesktop(root string) (func(), bool, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, false, err
	}
	if resolved, e := filepath.EvalSymlinks(absolute); e == nil {
		absolute = resolved
	}
	hash := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(absolute))))
	desktopProperty = fmt.Sprintf("HarbourLedger.%x", hash[:16])
	name, _ := windows.UTF16PtrFromString("Local\\" + desktopProperty)
	handle, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if handle != 0 {
			windows.CloseHandle(handle)
		}
		focusDesktop()
		return func() {}, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return func() { windows.CloseHandle(handle) }, false, nil
}
func focusDesktop() {
	property, _ := windows.UTF16PtrFromString(desktopProperty)
	found := false
	callback := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		value, _, _ := desktopUser32.NewProc("GetPropW").Call(hwnd, uintptr(unsafe.Pointer(property)))
		if value != 0 {
			desktopUser32.NewProc("ShowWindow").Call(hwnd, 9)
			desktopUser32.NewProc("SetForegroundWindow").Call(hwnd)
			found = true
			return 0
		}
		return 1
	})
	desktopUser32.NewProc("EnumWindows").Call(callback, 0)
	if !found {
		showDesktopError("此帳簿已在啟動或處理資料，請稍候。")
	}
}
func runDesktop(url, root string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 is the Win32 handle -4.
	// Set awareness before WebView2 creates its host window.
	setDPI := desktopUser32.NewProc("SetProcessDpiAwarenessContext")
	if setDPI.Find() == nil {
		setDPI.Call(^uintptr(3))
	}
	version, err := webviewloader.GetInstalledVersion()
	if err != nil || version == "" {
		return fmt.Errorf("系統未提供 Microsoft Edge WebView2 Runtime。請使用已包含 WebView2 的 Windows 10／11 系統，或由 Microsoft 官方安裝 Runtime。帳簿原件不受影響。")
	}
	w := webview.NewWithOptions(webview.WebViewOptions{DataPath: filepath.Join(root, "視窗快取"), AutoFocus: true, WindowOptions: webview.WindowOptions{Title: "港賬通 · Harbour Ledger", Width: 1440, Height: 960, Center: true}})
	if w == nil {
		return fmt.Errorf("無法建立客戶端視窗，請查看客戶端日誌")
	}
	icon := setDesktopIcon(uintptr(w.Window()))
	defer func() {
		w.Destroy()
		if icon != 0 {
			desktopUser32.NewProc("DestroyIcon").Call(icon)
		}
	}()
	property, _ := windows.UTF16PtrFromString(desktopProperty)
	desktopUser32.NewProc("SetPropW").Call(uintptr(w.Window()), uintptr(unsafe.Pointer(property)), 1)
	w.SetSize(1000, 700, webview.HintMin)
	w.Navigate(url)
	w.Run()
	return nil
}
