//go:build windows

package audio

import (
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	wca "github.com/moutend/go-wca/pkg/wca"
	"golang.org/x/sys/windows"
)

const (
	th32csSnapProcess = 0x00000002

	// fastScan is the session scan interval right after a session this process
	// has not seen before shows up. One scan is a handful of in-process COM
	// calls and a lookup, not a walk of the machine.
	fastScan = 2 * time.Second
	// slowScan is the interval once several scans in a row turned up nothing
	// new. A session created during this window keeps its default name in the
	// volume mixer until the next scan.
	slowScan = 15 * time.Second
	// quietScansBeforeSlow is how many consecutive fruitless scans it takes to
	// drop back to slowScan.
	quietScansBeforeSlow = 8
)

var (
	// volPercent is the level applied to this process's sessions, 0-100.
	volPercent atomic.Int32
	// volWake nudges the labeler loop to re-apply the level at once.
	volWake = make(chan struct{}, 1)
)

// SetVolume stores a new target level and wakes the labeler so it is applied
// to the current sessions without waiting for the next scan.
func SetVolume(percent int) {
	volPercent.Store(int32(clampPercent(percent)))
	select {
	case volWake <- struct{}{}:
	default:
	}
}

func clampPercent(p int) int {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

// wait sleeps for d, but returns early if SetVolume pokes volWake.
func wait(d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-volWake:
	}
}

// applyVolume sets the session's master volume. Errors are ignored: a session
// that does not expose ISimpleAudioVolume simply keeps its default level.
func applyVolume(control *wca.IAudioSessionControl2, level float32) {
	var sav *wca.ISimpleAudioVolume
	if err := control.PutQueryInterface(wca.IID_ISimpleAudioVolume, &sav); err != nil || sav == nil {
		return
	}
	defer sav.Release()
	_ = sav.SetMasterVolume(level, nil)
}

// StartLabeler renames and volumes the audio sessions belonging to this
// process's WebView2 renderers, so the app's audio appears under label in the
// Windows volume mixer and plays at the configured level.
//
// The scan is deliberately lopsided. It enumerates audio sessions on every
// tick, but only walks the system process table when a session names a process
// it has not classified yet. That walk is a snapshot of every process on the
// machine, and running it every two seconds for the life of the app was the
// expensive half of the loop this replaced.
func StartLabeler(label string, volumePercent int) {
	volPercent.Store(int32(clampPercent(volumePercent)))
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
			return
		}
		defer ole.CoUninitialize()

		var (
			enumerator *wca.IMMDeviceEnumerator
			device     *wca.IMMDevice
			manager    *wca.IAudioSessionManager2
		)
		releaseMixer := func() {
			if manager != nil {
				manager.Release()
			}
			if device != nil {
				device.Release()
			}
			if enumerator != nil {
				enumerator.Release()
			}
			manager, device, enumerator = nil, nil, nil
		}
		defer releaseMixer()

		// ours is the set of WebView2 PIDs under this process. seen is the
		// session PIDs from the previous scan, which is how a genuinely new
		// session is told apart from one that has been there all along.
		var ours map[uint32]bool
		seen := map[uint32]bool{}
		quiet := 0

		for {
			if manager == nil {
				enumerator, device, manager = openMixer()
				if manager == nil {
					wait(slowScan)
					continue
				}
			}

			sessions, err := collectSessions(manager)
			if err != nil {
				// The endpoint went away: device switch or driver restart.
				releaseMixer()
				wait(fastScan)
				continue
			}

			current := make(map[uint32]bool, len(sessions))
			fresh := false
			for _, s := range sessions {
				current[s.pid] = true
				if !seen[s.pid] {
					fresh = true
				}
			}
			seen = current

			if fresh {
				ours = waGramDeskLiteWebViewProcesses(uint32(syscall.Getpid()))
			}
			for _, s := range sessions {
				if ours[s.pid] {
					name := label
					_ = s.control.SetDisplayName(&name, nil)
					applyVolume(s.control, float32(volPercent.Load())/100)
				}
				s.control.Release()
			}

			if fresh {
				quiet = 0
				wait(fastScan)
				continue
			}
			quiet++
			if quiet >= quietScansBeforeSlow {
				wait(slowScan)
			} else {
				wait(fastScan)
			}
		}
	}()
}

// openMixer activates the session manager for the current default playback
// device. The handles are held for the life of the process: they only change
// when the default device does, so rebuilding them on every tick was COM churn
// for a result that almost never changed.
func openMixer() (*wca.IMMDeviceEnumerator, *wca.IMMDevice, *wca.IAudioSessionManager2) {
	var enumerator *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(
		wca.CLSID_MMDeviceEnumerator,
		0,
		wca.CLSCTX_INPROC_SERVER,
		wca.IID_IMMDeviceEnumerator,
		&enumerator,
	); err != nil {
		return nil, nil, nil
	}

	var device *wca.IMMDevice
	if err := enumerator.GetDefaultAudioEndpoint(wca.ERender, wca.DEVICE_STATE_ACTIVE, &device); err != nil {
		enumerator.Release()
		return nil, nil, nil
	}

	var manager *wca.IAudioSessionManager2
	if err := device.Activate(wca.IID_IAudioSessionManager2, wca.CLSCTX_INPROC_SERVER, nil, &manager); err != nil {
		device.Release()
		enumerator.Release()
		return nil, nil, nil
	}
	return enumerator, device, manager
}

type audioSession struct {
	control *wca.IAudioSessionControl2
	pid     uint32
}

// collectSessions enumerates the current audio sessions. The caller owns the
// returned control pointers and must release each one.
func collectSessions(manager *wca.IAudioSessionManager2) ([]audioSession, error) {
	var sessions *wca.IAudioSessionEnumerator
	if err := manager.GetSessionEnumerator(&sessions); err != nil {
		return nil, err
	}
	defer sessions.Release()

	var count int
	if err := sessions.GetCount(&count); err != nil {
		return nil, err
	}

	found := make([]audioSession, 0, count)
	for i := 0; i < count; i++ {
		var control *wca.IAudioSessionControl
		if err := sessions.GetSession(i, &control); err != nil || control == nil {
			continue
		}

		var control2 *wca.IAudioSessionControl2
		if err := control.PutQueryInterface(wca.IID_IAudioSessionControl2, &control2); err != nil || control2 == nil {
			control.Release()
			continue
		}
		control.Release()

		var pid uint32
		if err := control2.GetProcessId(&pid); err != nil {
			control2.Release()
			continue
		}
		found = append(found, audioSession{control: control2, pid: pid})
	}
	return found, nil
}

func waGramDeskLiteWebViewProcesses(rootPID uint32) map[uint32]bool {
	parents := map[uint32]uint32{}
	entries := map[uint32]string{}

	snapshot, err := windows.CreateToolhelp32Snapshot(th32csSnapProcess, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil
	}
	for {
		parents[entry.ProcessID] = entry.ParentProcessID
		entries[entry.ProcessID] = syscall.UTF16ToString(entry.ExeFile[:])
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}

	result := make(map[uint32]bool)
	for pid, name := range entries {
		if !strings.EqualFold(name, "msedgewebview2.exe") {
			continue
		}
		for current := pid; current != 0; current = parents[current] {
			if current == rootPID {
				result[pid] = true
				break
			}
		}
	}
	return result
}
