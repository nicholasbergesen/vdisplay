// vdisplay gives a headless Windows PC a virtual monitor at a real resolution.
//
// With no monitor attached, Windows runs the console at 640x480, which breaks anything that needs a
// normal screen: browser automation, remote-control tools, and dialogs taller than the screen. vdisplay
// uses Parsec's free virtual display driver (parsec-vdd, signed by Parsec Cloud, Inc.) to add one
// virtual display, makes it the only active screen at the size you ask for, and keeps it alive.
//
//	vdisplay [-w 1920] [-h 1080] [-log <file>]    add (or reuse) the display and keep it alive; runs until killed
//	vdisplay -status                               print the active screens and exit
//
// It needs administrator rights to open the driver. The driver drops its displays some time after its
// keep-alive pings stop, so vdisplay pings every 100 ms for as long as it runs; run it at logon (see
// register-task.ps1). If a Parsec display is already attached when it starts (left by an earlier run),
// it reuses that one instead of adding a second.
//
// The protocol (device interface GUID, IOCTL codes, the 32-byte input buffer, the big-endian index on
// remove) follows the parsec-vdd header published by nomi-san/parsec-vdd (MIT License).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var adapterGUID = windows.GUID{Data1: 0x00b41627, Data2: 0x04c4, Data3: 0x429e, Data4: [8]byte{0xa2, 0x6e, 0x02, 0x65, 0xcf, 0x50, 0xc8, 0xfa}}

const (
	ioctlAdd     = 0x0022e004
	ioctlRemove  = 0x0022a008
	ioctlUpdate  = 0x0022a00c
	ioctlVersion = 0x0022e010
)

var (
	user32                   = windows.NewLazySystemDLL("user32.dll")
	procEnumDisplayDevices   = user32.NewProc("EnumDisplayDevicesW")
	procEnumDisplaySettings  = user32.NewProc("EnumDisplaySettingsW")
	procChangeDisplaySetting = user32.NewProc("ChangeDisplaySettingsExW")
)

func openAdapter() windows.Handle {
	paths, err := windows.CM_Get_Device_Interface_List("", &adapterGUID, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT)
	if err != nil || len(paths) == 0 || paths[0] == "" {
		log.Fatalf("Parsec virtual display adapter not found (is parsec-vdd installed?): %v", err)
	}
	path := paths[0]
	h, err := windows.CreateFile(windows.StringToUTF16Ptr(path), windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_NO_BUFFERING|windows.FILE_FLAG_OVERLAPPED|windows.FILE_FLAG_WRITE_THROUGH, 0)
	if err != nil {
		log.Fatalf("open adapter: %v", err)
	}
	return h
}

// ioctl sends a code with a 32-byte input buffer and returns the driver's 32-bit answer.
func ioctl(h windows.Handle, code uint32, in []byte) (uint32, error) {
	var input [32]byte
	copy(input[:], in)
	var out uint32
	ev, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(ev)
	ov := windows.Overlapped{HEvent: ev}
	err = windows.DeviceIoControl(h, code, &input[0], uint32(len(input)), (*byte)(unsafe.Pointer(&out)), 4, nil, &ov)
	if err != nil && err != windows.ERROR_IO_PENDING {
		return 0, err
	}
	var n uint32
	if err := windows.GetOverlappedResult(h, &ov, &n, true); err != nil {
		return 0, err
	}
	return out, nil
}

// displayDevice mirrors DISPLAY_DEVICEW.
type displayDevice struct {
	cb           uint32
	DeviceName   [32]uint16
	DeviceString [128]uint16
	StateFlags   uint32
	DeviceID     [128]uint16
	DeviceKey    [128]uint16
}

// devMode mirrors DEVMODEW (display fields).
type devMode struct {
	DeviceName       [32]uint16
	SpecVersion      uint16
	DriverVersion    uint16
	Size             uint16
	DriverExtra      uint16
	Fields           uint32
	PositionX        int32
	PositionY        int32
	DisplayOrient    uint32
	DisplayFixed     uint32
	Color            int16
	Duplex           int16
	YResolution      int16
	TTOption         int16
	Collate          int16
	FormName         [32]uint16
	LogPixels        uint16
	BitsPerPel       uint32
	PelsWidth        uint32
	PelsHeight       uint32
	DisplayFlags     uint32
	DisplayFrequency uint32
	ICMMethod        uint32
	ICMIntent        uint32
	MediaType        uint32
	DitherType       uint32
	Reserved1        uint32
	Reserved2        uint32
	PanningWidth     uint32
	PanningHeight    uint32
}

const (
	attachedToDesktop = 0x1
	primaryDevice     = 0x4
	enumCurrent       = 0xFFFFFFFF
	dmPelsWidth       = 0x80000
	dmPelsHeight      = 0x100000
	cdsUpdateRegistry = 0x1
)

type screen struct {
	name, adapter string
	attached      bool
	primary       bool
	w, h          uint32
}

func screens() []screen {
	var out []screen
	for i := uint32(0); ; i++ {
		var d displayDevice
		d.cb = uint32(unsafe.Sizeof(d))
		if r, _, _ := procEnumDisplayDevices.Call(0, uintptr(i), uintptr(unsafe.Pointer(&d)), 0); r == 0 {
			break
		}
		s := screen{name: windows.UTF16ToString(d.DeviceName[:]), adapter: windows.UTF16ToString(d.DeviceString[:]),
			attached: d.StateFlags&attachedToDesktop != 0, primary: d.StateFlags&primaryDevice != 0}
		var m devMode
		m.Size = uint16(unsafe.Sizeof(m))
		if r, _, _ := procEnumDisplaySettings.Call(uintptr(unsafe.Pointer(&d.DeviceName[0])), enumCurrent, uintptr(unsafe.Pointer(&m))); r != 0 {
			s.w, s.h = m.PelsWidth, m.PelsHeight
		}
		out = append(out, s)
	}
	return out
}

func printScreens() {
	for _, s := range screens() {
		if s.attached {
			fmt.Printf("%s %dx%d primary=%v (%s)\n", s.name, s.w, s.h, s.primary, s.adapter)
		}
	}
}

func parsecScreen() *screen {
	for _, s := range screens() {
		if s.attached && len(s.adapter) >= 6 && s.adapter[:6] == "Parsec" {
			return &s
		}
	}
	return nil
}

func setSize(name string, w, h uint32) error {
	var m devMode
	m.Size = uint16(unsafe.Sizeof(m))
	m.Fields = dmPelsWidth | dmPelsHeight
	m.PelsWidth, m.PelsHeight = w, h
	r, _, _ := procChangeDisplaySetting.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(name))), uintptr(unsafe.Pointer(&m)), 0, cdsUpdateRegistry, 0)
	if int32(r) != 0 {
		return fmt.Errorf("ChangeDisplaySettingsEx returned %d", int32(r))
	}
	return nil
}

func main() {
	w := flag.Uint("w", 1920, "width")
	h := flag.Uint("h", 1080, "height")
	status := flag.Bool("status", false, "print the active screens and exit")
	logPath := flag.String("log", "", "append the log to this file instead of stderr")
	flag.Parse()
	if *status {
		printScreens()
		return
	}
	log.SetFlags(log.LstdFlags)
	if *logPath != "" {
		if f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			log.SetOutput(f)
		}
	}
	vdd := openAdapter()
	if v, err := ioctl(vdd, ioctlVersion, nil); err == nil {
		log.Printf("parsec-vdd driver version %d.%d", v>>16, v&0xFFFF)
	}
	added := false
	var idx uint32
	if ps := parsecScreen(); ps != nil {
		log.Printf("reusing the virtual display already attached (%s)", ps.name)
	} else {
		var err error
		if idx, err = ioctl(vdd, ioctlAdd, nil); err != nil {
			log.Fatalf("add display: %v", err)
		}
		added = true
		ioctl(vdd, ioctlUpdate, nil)
		log.Printf("added virtual display %d", idx)
	}

	// Keep pinging from now on; the driver drops the display about a second without pings.
	go func() {
		for {
			ioctl(vdd, ioctlUpdate, nil)
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// Wait for Windows to attach it, show only it ("second screen only"), then set the size.
	var ps *screen
	for i := 0; i < 50 && ps == nil; i++ {
		time.Sleep(200 * time.Millisecond)
		ps = parsecScreen()
	}
	if ps == nil {
		log.Printf("virtual display not attached yet; switching projection anyway")
	}
	exec.Command(`C:\Windows\System32\DisplaySwitch.exe`, "/external").Run()
	time.Sleep(3 * time.Second)
	if ps = parsecScreen(); ps != nil {
		if ps.w != uint32(*w) || ps.h != uint32(*h) {
			if err := setSize(ps.name, uint32(*w), uint32(*h)); err != nil {
				log.Printf("set size: %v", err)
			}
		}
	}
	for _, s := range screens() {
		if s.attached {
			log.Printf("screen %s %dx%d primary=%v (%s)", s.name, s.w, s.h, s.primary, s.adapter)
		}
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	if added {
		in := []byte{byte(idx >> 8), byte(idx)} // index as 16-bit big-endian
		ioctl(vdd, ioctlRemove, in)
		ioctl(vdd, ioctlUpdate, nil)
		log.Printf("removed virtual display %d", idx)
	}
}
