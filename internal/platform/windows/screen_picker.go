package windows

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var (
	modUser32 = syscall.NewLazyDLL("user32.dll")
	modGdi32  = syscall.NewLazyDLL("gdi32.dll")

	procGetDC            = modUser32.NewProc("GetDC")
	procReleaseDC        = modUser32.NewProc("ReleaseDC")
	procGetCursorPos     = modUser32.NewProc("GetCursorPos")
	procGetAsyncKeyState = modUser32.NewProc("GetAsyncKeyState")
	procGetPixel         = modGdi32.NewProc("GetPixel")
)

const (
	VK_LBUTTON = 0x01
	VK_ESCAPE  = 0x1B
)

type POINT struct {
	X, Y int32
}

// StartScreenColorSampling 进入吸管采样等待模式：
// 允许用户把鼠标移出阅读器窗口，点击屏幕任意目标软件；
// 无论阅读器是否处于焦点，只要按下左键即瞬间抓取该像素，按 ESC 取消。
func StartScreenColorSampling(timeoutSeconds int) (string, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 15 // 默认 15 秒超时
	}

	// 1. 先等待用户松开刚刚点击“吸色”按钮的鼠标左键（避免吸到当前的“吸色”按钮自身）
	releaseWaitStart := time.Now()
	for time.Since(releaseWaitStart) < 3*time.Second {
		btnState, _, _ := procGetAsyncKeyState.Call(uintptr(VK_LBUTTON))
		if (btnState & 0x8000) == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// 2. 消费并清除当前鼠标左键与 ESC 的残留按压状态
	time.Sleep(50 * time.Millisecond)
	procGetAsyncKeyState.Call(uintptr(VK_LBUTTON))
	procGetAsyncKeyState.Call(uintptr(VK_ESCAPE))

	// 3. 进入目标点击监听循环
	start := time.Now()
	for time.Since(start) < time.Duration(timeoutSeconds)*time.Second {
		// 检查 ESC 键（取消）
		escState, _, _ := procGetAsyncKeyState.Call(uintptr(VK_ESCAPE))
		if (escState & 0x8000) != 0 {
			return "", fmt.Errorf("吸色已由用户按 ESC 取消")
		}

		// 检查鼠标左键按下（用户已将光标移至目标窗口并点击）
		btnState, _, _ := procGetAsyncKeyState.Call(uintptr(VK_LBUTTON))
		if (btnState & 0x8000) != 0 {
			// 用户点击了目标！稍作等待以确保目标窗口光标与像素稳定，再抓取当前光标像素
			time.Sleep(25 * time.Millisecond)
			return PickColorAtCursor()
		}

		time.Sleep(15 * time.Millisecond)
	}

	return "", fmt.Errorf("吸色等待超时 (15秒内未点击)")
}

// PickColorAtCursor 获取当前光标位置的像素颜色
func PickColorAtCursor() (string, error) {
	var pt POINT
	ret, _, err := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	if ret == 0 {
		return "", fmt.Errorf("GetCursorPos 失败: %v", err)
	}

	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return "", fmt.Errorf("GetDC 失败")
	}
	defer procReleaseDC.Call(0, hdc)

	colorRef, _, _ := procGetPixel.Call(hdc, uintptr(pt.X), uintptr(pt.Y))
	if colorRef == 0xFFFFFFFF { // CLR_INVALID
		return "", fmt.Errorf("GetPixel 返回无效颜色")
	}

	// COLORREF: 0x00bbggrr
	r := byte(colorRef & 0xFF)
	g := byte((colorRef >> 8) & 0xFF)
	b := byte((colorRef >> 16) & 0xFF)

	return fmt.Sprintf("#%02X%02X%02X", r, g, b), nil
}
