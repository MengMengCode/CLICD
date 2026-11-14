package lxc

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	InotifyInstancesKey  = "fs.inotify.max_user_instances"
	MinInotifyInstances  = 2048
	inotifyInstancesPath = "/proc/sys/fs/inotify/max_user_instances"
)

var inotifyInstancesMu sync.Mutex

func HostInotifyInstances() (int, error) {
	return readInotifyInstances(inotifyInstancesPath)
}

func readInotifyInstances(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid %s value %q", InotifyInstancesKey, strings.TrimSpace(string(data)))
	}
	return value, nil
}

// ensureInotifyInstances returns the previous limit and raises it only when
// needed. Root-owned LXC user namespaces can share the host's inotify budget.
func ensureInotifyInstances(path string) (int, error) {
	inotifyInstancesMu.Lock()
	defer inotifyInstancesMu.Unlock()

	limit, err := readInotifyInstances(path)
	if err != nil || limit >= MinInotifyInstances {
		return limit, err
	}
	return limit, os.WriteFile(path, []byte(strconv.Itoa(MinInotifyInstances)+"\n"), 0644)
}

func inotifyStartupHint(console string) string {
	for _, line := range strings.Split(console, "\n") {
		line = strings.ToLower(line)
		if strings.Contains(line, "control group inotify object") && strings.Contains(line, "too many open files") {
			return fmt.Sprintf("The host inotify instance quota may be exhausted. Check %s on the host and raise it to at least %d, or higher for this workload.", InotifyInstancesKey, MinInotifyInstances)
		}
	}
	return ""
}
