package simusb

// Shamelessly pilfering this from FBInk. Functions renamed to avoid clashing with FBIink versions

// #include "mount_states.c"
import "C"
import (
	"bytes"
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/mitchellh/go-ps"
	"github.com/shermp/go-fbink-v2/gofbink"
	"golang.org/x/sys/unix"
)

const nickelHWstatusPipe = "/tmp/nickel-hardware-status"
const onboardMntPoint = "/mnt/onboard"
const newOnboardMnt = "/mnt/tmponboard"

// Internal SD card device
const internalMemoryDev = "/dev/mmcblk0p3"

// USBMSsession contains the information required to manage entering
// and leaving Nickels USBMS mode
type USBMSsession struct {
	origWD             string
	currWD             string
	relWD              string
	CurOnboardMnt      string
	wifiEnabledInUSBMS bool
	partMounted        bool
	nickelVars         struct {
		platform       string
		wifiModule     string
		wifiModulePath string
		netInterface   string
		ldLibPath      string
	}
	fbI *gofbink.FBInk
}

func getPID(processName string) (int, error) {
	proc, _ := ps.Processes()
	for _, p := range proc {
		if strings.HasPrefix(p.Executable(), processName) {
			return p.Pid(), nil
		}
	}
	return -1, errors.New("process not found")
}

// New initilializes a USBMSsession struct
func New(fbI *gofbink.FBInk) (u *USBMSsession, err error) {
	u = &USBMSsession{}
	// Get the current working directory. We will need to restore this later
	u.origWD, err = os.Getwd()
	if err != nil {
		return u, nil
	}
	u.currWD = u.origWD
	// Compute the working directory relative to the mountpoint
	u.relWD, _ = filepath.Rel(onboardMntPoint, u.currWD)
	u.CurOnboardMnt = onboardMntPoint
	u.fbI = fbI
	u.wifiEnabledInUSBMS = false
	err = u.readNickelEnv()
	if err != nil {
		return u, err
	}
	return u, nil
}

// Start carries out the process of safely entering USBMS mode
// Whilst in USBMS mode, u.CurOnboardMnt is the new mountpoint
// for the Kobo internal memory
func (u *USBMSsession) Start(enableWifi, mountPart bool) error {
	err := error(nil)
	u.fbI.Println("Attempting to launch into USBMS session. Please wait...")
	onboardMntCstr := C.CString(onboardMntPoint)
	defer C.free(unsafe.Pointer(onboardMntCstr))
	// Check our mounts. /mnt/onboard should be mounted
	wantMounted := true
	mounted := bool(C.simusb_is_onboard_state(C.bool(wantMounted), onboardMntCstr))
	if !mounted {
		return errors.New("/mnt/onboard is not mounted")
	}

	os.Chdir("/")
	// Next, we fake the USB connection
	u.plugUSB()
	// And get FBInk to connect for us
	for {
		err := u.fbI.ButtonScan(true, false)
		if err != nil {
			switch err.Error() {
			case "EXIT_FAILURE":
				time.Sleep(500 * time.Millisecond)
				break
			case "ENODEV":
				return errors.New("button_scan touch failed")
			default:
				return err
			}
		} else {
			break
		}
	}
	// Let things settle for a bit. Might help stability
	time.Sleep(3 * time.Second)
	unmounted := bool(C.sim_usb_wait_for_onboard_state(C.bool(!wantMounted), onboardMntCstr))
	if !unmounted {
		return errors.New("/mnt/onboard never unmounted")
	}
	if mountPart {
		// Ready to try mounting the internal storage to a place of our choosing
		newOnboardMntCstr := C.CString(newOnboardMnt)
		defer C.free(unsafe.Pointer(newOnboardMntCstr))
		err = os.MkdirAll(newOnboardMnt, 0666)
		if err != nil {
			return err
		}
		unix.Mount(internalMemoryDev, newOnboardMnt, "vfat", 0, "")
		mounted = bool(C.sim_usb_wait_for_onboard_state(C.bool(wantMounted), newOnboardMntCstr))
		if !mounted {
			// Oh dear... Beter abort
			u.unplugUSB()
			return errors.New("could not mount temporary FS")
		}
		u.partMounted = true
		u.fbI.Println("Internal storage remounted!")
		// Now that we have a new mountpoint, change the current onboard mount point,
		// and change the current working directory for clients to use.
		u.CurOnboardMnt = newOnboardMnt
		u.currWD = filepath.Join(u.CurOnboardMnt, u.relWD)
		os.Chdir(u.currWD)
	}
	if enableWifi {
		// Note, we are checking this several times, as it can take a while for Nickel to kill the Wifi
		if u.wifiIsEnabled(5 * time.Second) {
			u.fbI.Println("Wifi Already enabled")
			u.wifiEnabledInUSBMS = true
		}
		if !u.wifiEnabledInUSBMS {
			u.fbI.Println("Attempting to enable Wifi")
			err = u.enableWifi()
			if err != nil {
				u.fbI.Println("Wifi not enabled!")
				u.fbI.Println("USBMS Success!")
			} else {
				u.fbI.Println("Wifi enabled!")
			}
		}
	}

	u.fbI.Println("USBMS Success!")
	return err
}

// End carries out the process of safely ending a USBMS session
func (u *USBMSsession) End(waitForContentImport bool) error {
	err := error(nil)
	// Turns out, if we fail to change our working directory, the unmount fails
	os.Chdir("/")
	u.fbI.Println("Ending USBMS session. Please wait...")
	if u.partMounted {
		u.fbI.Println("Checking filesystem mount status")
		// Let's see if our temporary FS is still mounted
		wantMounted := true
		newOnboardMntCstr := C.CString(newOnboardMnt)
		defer C.free(unsafe.Pointer(newOnboardMntCstr))
		mounted := bool(C.simusb_is_onboard_state(C.bool(wantMounted), newOnboardMntCstr))
		// Yes it is. Unmount the filesystem
		u.fbI.Println("Unmounting filesystem")
		unmountSuccess := false
		if mounted {
			for i := 0; i < 5; i++ {
				u.fbI.Println("Unmount iteration:", i)
				err := unix.Unmount(newOnboardMnt, 0)
				if err == nil {
					unmountSuccess = true
					break
				} else if err.(syscall.Errno) == syscall.EBUSY {
					u.fbI.Println(err)
					time.Sleep(2 * time.Second)
				}
			}
		}
		if !unmountSuccess {
			// Oh dear... The **** has really hit the proverbial fan
			// I think rebooting is the safest option here to hopefully
			// avoid corrupting our partition

			// The linux reboot man suggests that rebooting without calling
			// sync() could lead to data loss
			u.fbI.Println("Unount Failed...Rebooting")
			time.Sleep(1 * time.Second)
			unix.Sync()
			unix.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
		}
	}

	// We've done our best to unmount things. Let's check the networking
	if !u.wifiEnabledInUSBMS {
		u.disableWifi()
	}
	err = u.fbI.WaitForUSBMSprocessing(true)
	if err != nil {
		switch err.Error() {
		case "ENODATA":
			// No new content to import is not an error
			os.Chdir(u.origWD)
			return nil
		case "ETIME":
			os.Chdir(u.origWD)
			return errors.New("content import end not detected")
		case "EXIT_FAILURE":
			// Things turned to custard...
			return errors.New("there was an error detecting content import")
		}
	}
	return err
}

// plugUSB simulates pugging in a USB cable
func (u *USBMSsession) plugUSB() {
	nickelPipe, _ := os.OpenFile(nickelHWstatusPipe, os.O_RDWR, os.ModeNamedPipe)
	nickelPipe.WriteString("usb plug add")
	nickelPipe.Close()
}

// unplugUSB simulates unplugging a USB cable
func (u *USBMSsession) unplugUSB() {
	nickelPipe, _ := os.OpenFile(nickelHWstatusPipe, os.O_RDWR, os.ModeNamedPipe)
	nickelPipe.WriteString("usb plug remove")
	nickelPipe.Close()
}

// A basic check to see if Wifi is already enabled. A working network will have
// dhcpcd and wpa_supplicant running
func (u *USBMSsession) wifiIsEnabled(timeout time.Duration) bool {
	sdioPres, wifiModPres := false, false
	start := time.Now()
	for {
		if isKernelModLoaded("sdio_wifi_pwr") {
			sdioPres = true
		}
		if isKernelModLoaded(u.nickelVars.wifiModule) {
			wifiModPres = true
		}
		if !sdioPres && !wifiModPres {
			return false
		}
		if time.Since(start) > timeout {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func isKernelModLoaded(moduleName string) bool {
	cmd := exec.Command("lsmod")
	output, _ := cmd.Output()
	if len(output) > 0 && strings.Contains(string(output), moduleName) {
		return true
	}
	return false
}

// enableWifi attempts to bring the Wifi module up after Nickel kills it.
// Adapted from the Koreader shell script at
// https://github.com/koreader/koreader/blob/master/platform/kobo/enable-wifi.sh
func (u *USBMSsession) enableWifi() error {
	err := error(nil)
	u.fbI.Println("Loading kernel modules")
	// Is sdio_wifi_pwr loaded?
	if !isKernelModLoaded("sdio_wifi_pwr") {
		// module not loaded. Load it now
		sdioPath := "/drivers/" + u.nickelVars.platform + "/wifi/sdio_wifi_pwr.ko"
		cmd := exec.Command("insmod", sdioPath)
		err = cmd.Run()
		if err != nil {
			return err
		}
	}
	time.Sleep(250000 * time.Microsecond)
	// how about the wifi module?
	if !isKernelModLoaded(u.nickelVars.wifiModule) {
		// module not loaded. Load it now
		cmd := exec.Command("insmod", u.nickelVars.wifiModulePath)
		err = cmd.Run()
		if err != nil {
			return err
		}
	}
	// The Koreader devs tell us not to try and optimize this sleep...
	time.Sleep(1 * time.Second)

	u.fbI.Println("Bringing up interface")
	// Assuming we got this far, time to bring up the interface
	cmd := exec.Command("ifconfig", u.nickelVars.netInterface, "up")
	err = cmd.Run()
	if err != nil {
		u.disableWifi()
		return err
	}
	if !strings.Contains(u.nickelVars.wifiModule, "8189fs") || !strings.Contains(u.nickelVars.wifiModule, "8192es") {
		cmd = exec.Command("wlarm_le", "-i", u.nickelVars.netInterface, "up")
		err = cmd.Run()
		if err != nil {
			u.disableWifi()
			return nil
		}
	}
	u.fbI.Println("Starting wpa_supplicant")
	// WPA is next on the table...
	cmd = exec.Command("wpa_supplicant", "-D", "wext", "-s", "-i", u.nickelVars.netInterface,
		"-O", "/var/run/wpa_supplicant", "-c", "/etc/wpa_supplicant/wpa_supplicant.conf", "-B")
	ldEnv := []string{("LD_LIBRARY_PATH=" + u.nickelVars.ldLibPath)}
	cmd.Env = ldEnv
	err = cmd.Run()
	if err != nil {
		u.disableWifi()
		return err
	}
	u.fbI.Println("Obtaining IP address")
	// Last thing, obtain an IP address via DHCP
	cmd = exec.Command("udhcpc", "-S", "-i", u.nickelVars.netInterface, "-s",
		"/etc/udhcpc.d/default.script", "-t15", "-T10", "-A3", "-b", "-q")
	cmd.Env = ldEnv
	err = cmd.Run()
	if err != nil {
		u.disableWifi()
		return err
	}
	// Whew, if we've reached this point, we should have a working internet connection
	return err
}

func (u *USBMSsession) disableWifi() error {
	err := error(nil)
	// Nickel kept the Wifi alive during USBMS. This happens when running with debug services
	// and "Force Wifi Enabled" on. No need to kill it in this case
	if u.wifiEnabledInUSBMS {
		return err
	}
	cmd := exec.Command("killall", "udhcpc", "default.script", "wpa_supplicant")
	// Not checking for errors here, there's nothing we can do about them anyway, so hope for the best
	cmd.Run()

	// Next we'll bring down the interfaces
	if !strings.Contains(u.nickelVars.wifiModule, "8189fs") || !strings.Contains(u.nickelVars.wifiModule, "8192es") {
		cmd = exec.Command("wlarm_le", "-i", u.nickelVars.netInterface, "down")
		cmd.Run()
	}
	cmd = exec.Command("ifconfig", u.nickelVars.netInterface, "down")
	cmd.Run()

	// Finally, remove the kernel modules
	if isKernelModLoaded(u.nickelVars.wifiModule) {
		// module is loaded. Remove it now
		time.Sleep(250000 * time.Microsecond)
		cmd = exec.Command("rmmod", u.nickelVars.wifiModule)
		cmd.Run()
	}
	if isKernelModLoaded("sdio_wifi_pwr") {
		// module not loaded. Load it now
		time.Sleep(250000 * time.Microsecond)
		cmd = exec.Command("rmmod", "sdio_wifi_pwr")
		cmd.Run()
	}
	return err
}

// The environment variables required to enable Wifi are not present after Nickel starts
// We will need to get them from the running Nickel process instead
func (u *USBMSsession) readNickelEnv() error {
	// Get the PID of Nickel, we will need it later
	pid, err := getPID("nickel")
	if err != nil {
		return err
	}
	envPath := "/proc/" + strconv.Itoa(pid) + "/environ"
	// Nickel environments are stored in /proc/PID/environ
	env, err := ioutil.ReadFile(envPath)
	if err != nil {
		return err
	}
	if len(env) > 0 {
		envs := bytes.Split(env, []byte("\x00"))
		for _, e := range envs {
			if len(e) > 0 {
				envSplit := bytes.Split(e, []byte("="))
				varName := string(envSplit[0])
				val := string(envSplit[1])
				switch varName {
				case "WIFI_MODULE":
					u.nickelVars.wifiModule = val
				case "PLATFORM":
					u.nickelVars.platform = val
				case "WIFI_MODULE_PATH":
					u.nickelVars.wifiModulePath = val
				case "INTERFACE":
					u.nickelVars.netInterface = val
				case "LD_LIBRARY_PATH":
					u.nickelVars.ldLibPath = val
				}
			}
		}
	}
	return nil
}
