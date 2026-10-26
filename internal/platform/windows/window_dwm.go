package windows

import (
	"syscall"
	"unsafe"
)

var (
	modDwmapi                 = syscall.NewLazyDLL("dwmapi.dll")
	procDwmSetWindowAttribute = modDwmapi.NewProc("DwmSetWindowAttribute")
	procFindWindowW           = modUser32.NewProc("FindWindowW")
	procSetWindowRgn          = modUser32.NewProc("SetWindowRgn")
	procGetWindowRect         = modUser32.NewProc("GetWindowRect")
	procCreateRoundRectRgn    = modGdi32.NewProc("CreateRoundRectRgn")
)

type RECT struct {
	Left, Top, Right, Bottom int32
}

// ApplyHardwareRoundedRegion 使用 Windows 原生 GDI 区域裁切 (SetWindowRgn)
// 直接在操作系统管线裁除窗口矩形的4个尖角，实现 100% 真实镂空透明，绝无黑角底色！
func ApplyHardwareRoundedRegion(windowTitle string, radius int) {
	titlePtr, err := syscall.UTF16PtrFromString(windowTitle)
	if err != nil {
		return
	}

	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	if hwnd == 0 {
		return
	}

	var rc RECT
	ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	if ret == 0 {
		return
	}

	w := rc.Right - rc.Left
	h := rc.Bottom - rc.Top

	// 创建系统级圆角区域
	hrgn, _, _ := procCreateRoundRectRgn.Call(
		0,
		0,
		uintptr(w),
		uintptr(h),
		uintptr(radius*2),
		uintptr(radius*2),
	)

	if hrgn != 0 {
		// SetWindowRgn 成功后系统自动管理 hrgn 内存
		procSetWindowRgn.Call(hwnd, hrgn, 1)
	}
}
