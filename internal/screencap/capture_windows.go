// Package screencap captures the screen for QR code scanning, with GDI and
// no cgo: one image per display, at physical pixels.
package screencap

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")

	procSetThreadDpiAwarenessContext = user32.NewProc("SetThreadDpiAwarenessContext")
	procEnumDisplayMonitors          = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW              = user32.NewProc("GetMonitorInfoW")
	procGetDC                        = user32.NewProc("GetDC")
	procReleaseDC                    = user32.NewProc("ReleaseDC")
	procCreateCompatibleDC           = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap       = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject                 = gdi32.NewProc("SelectObject")
	procBitBlt                       = gdi32.NewProc("BitBlt")
	procGetDIBits                    = gdi32.NewProc("GetDIBits")
	procDeleteObject                 = gdi32.NewProc("DeleteObject")
	procDeleteDC                     = gdi32.NewProc("DeleteDC")
)

const (
	srccopy    = 0x00CC0020
	captureblt = 0x40000000
	dibRGB     = 0
	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2: coordinates in physical
	// pixels on every monitor, whatever its scale.
	perMonitorAwareV2 = ^uintptr(3) // (DPI_AWARENESS_CONTEXT)-4
)

type rect struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width, Height int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

var (
	enumMu    sync.Mutex
	enumRects []rect
	enumCB    = windows.NewCallback(func(hmon, hdc, lprc, data uintptr) uintptr {
		mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
		if r, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi))); r != 0 {
			enumRects = append(enumRects, mi.Monitor)
		}
		return 1
	})
)

// CaptureAll returns one image per display, in physical pixels.
func CaptureAll() ([]image.Image, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if procSetThreadDpiAwarenessContext.Find() == nil {
		old, _, _ := procSetThreadDpiAwarenessContext.Call(perMonitorAwareV2)
		if old != 0 {
			defer procSetThreadDpiAwarenessContext.Call(old)
		}
	}

	enumMu.Lock()
	enumRects = nil
	procEnumDisplayMonitors.Call(0, 0, enumCB, 0)
	rects := append([]rect(nil), enumRects...)
	enumMu.Unlock()
	if len(rects) == 0 {
		return nil, errors.New("screencap: no displays")
	}

	screen, _, _ := procGetDC.Call(0)
	if screen == 0 {
		return nil, errors.New("screencap: no screen device context")
	}
	defer procReleaseDC.Call(0, screen)
	var out []image.Image
	for _, r := range rects {
		img, err := capture(screen, r)
		if err != nil {
			return out, err
		}
		out = append(out, img)
	}
	return out, nil
}

func capture(screen uintptr, r rect) (*image.RGBA, error) {
	w, h := int(r.Right-r.Left), int(r.Bottom-r.Top)
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("screencap: empty display %v", r)
	}
	mem, _, _ := procCreateCompatibleDC.Call(screen)
	if mem == 0 {
		return nil, errors.New("screencap: CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(mem)
	bmp, _, _ := procCreateCompatibleBitmap.Call(screen, uintptr(w), uintptr(h))
	if bmp == 0 {
		return nil, errors.New("screencap: CreateCompatibleBitmap failed")
	}
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(mem, bmp)
	defer procSelectObject.Call(mem, old)
	if ok, _, err := procBitBlt.Call(mem, 0, 0, uintptr(w), uintptr(h), screen,
		uintptr(int(r.Left)), uintptr(int(r.Top)), srccopy|captureblt); ok == 0 {
		return nil, fmt.Errorf("screencap: BitBlt: %v", err)
	}
	hdr := bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(w), Height: -int32(h),
		Planes: 1, BitCount: 32, Compression: dibRGB}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// GetDIBits needs the bitmap not selected into a DC.
	procSelectObject.Call(mem, old)
	if n, _, _ := procGetDIBits.Call(mem, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&img.Pix[0])),
		uintptr(unsafe.Pointer(&hdr)), dibRGB); n == 0 {
		return nil, errors.New("screencap: GetDIBits failed")
	}
	for i := 0; i < len(img.Pix); i += 4 { // BGRA → RGBA, opaque
		img.Pix[i], img.Pix[i+2], img.Pix[i+3] = img.Pix[i+2], img.Pix[i], 0xFF
	}
	return img, nil
}
