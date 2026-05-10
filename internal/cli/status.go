package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	cli "github.com/urfave/cli/v3"

	"github.com/lunguini/coggo/internal/config"
)

const (
	minDiskCritical = 64 * 1024 * 1024
	minDiskWarning  = 256 * 1024 * 1024
	minMemCritical  = 128 * 1024 * 1024
	minMemWarning   = 256 * 1024 * 1024
)

func cmdStatus() *cli.Command {
	return &cli.Command{
		Name:   "status",
		Usage:  "Show server health and local resource checks",
		Action: actionStatus,
	}
}

func actionStatus(ctx context.Context, cmd *cli.Command) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}

	dbPath := config.ResolvedDBPath(cfg)
	dataDir := config.DataDir(cfg)
	endpoint := statusEndpointURL(cfg.Server.ListenAddress)

	fmt.Println("Coggo status")
	fmt.Printf("  config:   %s\n", cmd.String("config"))
	fmt.Printf("  data dir: %s\n", dataDir)
	fmt.Printf("  db:       %s\n", dbPath)
	fmt.Printf("  endpoint: %s\n", endpoint)

	httpStatus, httpDetail := probeStatusEndpoint(ctx, endpoint)
	fmt.Printf("  server:   %s\n", httpStatus)
	if httpDetail != "" {
		fmt.Printf("            %s\n", httpDetail)
	}

	if pidStatus := probePidfile(); pidStatus != "" {
		fmt.Printf("  process:  %s\n", pidStatus)
	}

	diskStatus, diskDetail := probeDisk(dbPath)
	fmt.Printf("  storage:  %s\n", diskStatus)
	if diskDetail != "" {
		fmt.Printf("            %s\n", diskDetail)
	}

	memStatus, memDetail := probeMemory()
	fmt.Printf("  memory:   %s\n", memStatus)
	if memDetail != "" {
		fmt.Printf("            %s\n", memDetail)
	}

	return nil
}

func statusEndpointURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + strings.TrimRight(addr, "/") + "/mcp"
	}
	if host == "" || host == "localhost" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/mcp"
}

func probeStatusEndpoint(ctx context.Context, endpoint string) (string, string) {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "invalid", err.Error()
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "down", err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return "unhealthy", fmt.Sprintf("HTTP %d from /mcp", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return "up", "HTTP 401 from /mcp; auth is enforced"
	}
	return "up", fmt.Sprintf("HTTP %d from /mcp", resp.StatusCode)
}

func probePidfile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".coggo", "run", "coggo.pid")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "no pidfile at " + path
	}
	if err != nil {
		return "pidfile unreadable: " + err.Error()
	}
	pidText := strings.TrimSpace(string(b))
	pid, err := strconv.Atoi(pidText)
	if err != nil || pid <= 0 {
		return "pidfile has invalid pid " + strconv.Quote(pidText)
	}
	if processRunning(pid) {
		return fmt.Sprintf("pid %d from %s is running", pid, path)
	}
	return fmt.Sprintf("pid %d from %s is not running", pid, path)
}

func processRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func probeDisk(dbPath string) (string, string) {
	path := nearestExistingPath(dbPath)
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return "unknown", err.Error()
	}
	free := st.Bavail * uint64(st.Bsize)
	total := st.Blocks * uint64(st.Bsize)
	detail := fmt.Sprintf("%s free of %s at %s", formatBytes(free), formatBytes(total), path)
	switch {
	case free < minDiskCritical:
		return "critical", detail
	case free < minDiskWarning:
		return "low", detail
	default:
		return "ok", detail
	}
}

func nearestExistingPath(path string) string {
	if path == "" {
		return "."
	}
	for {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}

func probeMemory() (string, string) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return "unknown", "MemAvailable is not exposed at /proc/meminfo"
	}
	defer f.Close()
	return probeMemoryFromReader(f)
}

func probeMemoryFromReader(r io.Reader) (string, string) {
	available, ok := parseMemAvailable(r)
	if !ok {
		return "unknown", "MemAvailable not found in /proc/meminfo"
	}
	detail := formatBytes(available) + " available"
	switch {
	case available < minMemCritical:
		return "critical", detail
	case available < minMemWarning:
		return "low", detail
	default:
		return "ok", detail
	}
}

func parseMemAvailable(r io.Reader) (uint64, bool) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || fields[0] != "MemAvailable:" {
			continue
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, false
		}
		return kb * 1024, true
	}
	return 0, false
}

func formatBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
}
