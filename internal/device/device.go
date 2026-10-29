// Package device collects hardware information by reading /proc and /sys directly.
package device

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
)

type Disk struct {
	Path       string `json:"path"`
	TotalBytes uint64 `json:"totalBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
}

type Info struct {
	Hostname       string   `json:"hostname"`
	Model          string   `json:"model,omitempty"`
	Batocera       string   `json:"batoceraVersion,omitempty"`
	Kernel         string   `json:"kernel,omitempty"`
	Arch           string   `json:"arch"`
	CPUs           int      `json:"cpus"`
	UptimeSeconds  int64    `json:"uptimeSeconds"`
	LoadAvg        []string `json:"loadAvg,omitempty"`
	TemperatureC   *float64 `json:"temperatureC,omitempty"`
	MemTotalKB     uint64   `json:"memTotalKB"`
	MemAvailableKB uint64   `json:"memAvailableKB"`
	Disk           *Disk    `json:"disk,omitempty"`
}

// Collect gathers whatever is available; anything the machine lacks is left out.
func Collect(diskPath string) Info {
	info := Info{Arch: runtime.GOARCH, CPUs: runtime.NumCPU()}
	info.Hostname, _ = os.Hostname()
	info.Model = readTrim("/proc/device-tree/model")
	info.Batocera = readTrim("/usr/share/batocera/batocera.version")
	info.Kernel = readTrim("/proc/sys/kernel/osrelease")

	if f := strings.Fields(readTrim("/proc/uptime")); len(f) > 0 {
		if v, err := strconv.ParseFloat(f[0], 64); err == nil {
			info.UptimeSeconds = int64(v)
		}
	}
	if f := strings.Fields(readTrim("/proc/loadavg")); len(f) >= 3 {
		info.LoadAvg = f[:3]
	}
	if raw := readTrim("/sys/class/thermal/thermal_zone0/temp"); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			t := v / 1000
			info.TemperatureC = &t
		}
	}
	info.MemTotalKB, info.MemAvailableKB = memInfo()
	if d, err := diskUsage(diskPath); err == nil {
		info.Disk = d
	}
	return info
}

func readTrim(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimRight(string(b), "\x00"))
}

func memInfo() (total, available uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			available = v
		}
	}
	return total, available
}
