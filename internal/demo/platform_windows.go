package demo

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")
var createMutex = kernel.NewProc("CreateMutexW")
var createEvent = kernel.NewProc("CreateEventW")
var openEvent = kernel.NewProc("OpenEventW")
var setEvent = kernel.NewProc("SetEvent")
var waitObject = kernel.NewProc("WaitForSingleObject")
var closeHandle = kernel.NewProc("CloseHandle")
var shellExecute = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")

type Instance struct{ mutex, event uintptr }

func objectName(base, suffix string) string {
	h := sha256.Sum256([]byte(base))
	return fmt.Sprintf("Local\\EdgeHybridDemo_%x_%s", h[:16], suffix)
}
func ProgramDataRoot() (string, error) {
	exe, e := os.Executable()
	if e != nil {
		return "", e
	}
	exe, e = filepath.EvalSymlinks(exe)
	if e != nil {
		return "", e
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, e = os.UserCacheDir()
		if e != nil {
			return "", e
		}
	}
	h := sha256.Sum256([]byte(filepath.Clean(filepath.Dir(exe))))
	return filepath.Abs(filepath.Join(base, "EdgeHybridDemo", fmt.Sprintf("%x", h[:8])))
}
func AcquireInstance(base string) (*Instance, error) {
	name, e := syscall.UTF16PtrFromString(objectName(base, "instance"))
	if e != nil {
		return nil, e
	}
	m, _, callErr := createMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if m == 0 {
		return nil, callErr
	}
	if callErr == syscall.Errno(183) {
		closeHandle.Call(m)
		return nil, fmt.Errorf("本包演示已经运行。请查看现有页面/启动窗口，或先双击停止演示.cmd")
	}
	stop, e := syscall.UTF16PtrFromString(objectName(base, "stop"))
	if e != nil {
		closeHandle.Call(m)
		return nil, e
	}
	event, _, callErr := createEvent.Call(0, 1, 0, uintptr(unsafe.Pointer(stop)))
	if event == 0 {
		closeHandle.Call(m)
		return nil, callErr
	}
	return &Instance{mutex: m, event: event}, nil
}
func SignalStop(base string) error {
	name, e := syscall.UTF16PtrFromString(objectName(base, "stop"))
	if e != nil {
		return e
	}
	h, _, callErr := openEvent.Call(2, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return fmt.Errorf("没有可停止的本包演示，或停止事件不可用: %w", callErr)
	}
	defer closeHandle.Call(h)
	r, _, callErr := setEvent.Call(h)
	if r == 0 {
		return callErr
	}
	return nil
}
func (i *Instance) WaitStop(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r, _, e := waitObject.Call(i.event, 200)
		if r == 0 {
			return nil
		}
		if r != 258 {
			return e
		}
	}
}
func (i *Instance) Close() error {
	var err error
	for _, h := range []uintptr{i.event, i.mutex} {
		r, _, e := closeHandle.Call(h)
		if r == 0 {
			err = e
		}
	}
	return err
}
func OpenBrowser(url string) error {
	op, e := syscall.UTF16PtrFromString("open")
	if e != nil {
		return e
	}
	value, e := syscall.UTF16PtrFromString(url)
	if e != nil {
		return e
	}
	r, _, callErr := shellExecute.Call(0, uintptr(unsafe.Pointer(op)), uintptr(unsafe.Pointer(value)), 0, 0, 1)
	if r <= 32 {
		return fmt.Errorf("浏览器打开失败 (%d): %w；请复制页面地址手动打开", r, callErr)
	}
	return nil
}
