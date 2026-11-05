//go:build windows

package procs

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/berthkeep/berthkeep/internal/port"
)

// processCommandLineInformation: NtQueryInformationProcess class (Win 8.1+).
const processCommandLineInformation = 60

// Lister uses native APIs, falling back to CIM.
type Lister struct{}

// New returns the Windows ProcessLister.
func New() *Lister { return &Lister{} }

// List implements port.ProcessLister.
func (Lister) List(ctx context.Context, names []string) ([]port.Process, error) {
	out, err := listNative(names)
	if err == nil {
		return out, nil
	}
	return listCIM(ctx, names)
}

func listNative(names []string) ([]port.Process, error) {
	want := map[string]bool{}
	for _, n := range names {
		want[strings.ToLower(n)] = true
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	if err := windows.Process32First(snap, &e); err != nil {
		return nil, err
	}
	var out []port.Process
	for {
		name := windows.UTF16ToString(e.ExeFile[:])
		if want[strings.ToLower(name)] {
			cmd, cerr := commandLine(e.ProcessID)
			if cerr == nil {
				out = append(out, port.Process{PID: int(e.ProcessID), PPID: int(e.ParentProcessID), Name: name, CommandLine: cmd})
			}
		}
		if err := windows.Process32Next(snap, &e); err != nil {
			break
		}
	}
	return out, nil
}

func commandLine(pid uint32) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	size := uint32(4096)
	for i := 0; i < 4; i++ {
		buf := make([]byte, size)
		var ret uint32
		err := windows.NtQueryInformationProcess(h, processCommandLineInformation, unsafe.Pointer(&buf[0]), size, &ret)
		if err == nil {
			us := (*windows.NTUnicodeString)(unsafe.Pointer(&buf[0]))
			return us.String(), nil
		}
		if ret > size {
			size = ret
			continue
		}
		return "", err
	}
	return "", fmt.Errorf("command line too large for pid %d", pid)
}

func listCIM(ctx context.Context, names []string) ([]port.Process, error) {
	var filters []string
	for _, n := range names {
		if strings.HasSuffix(strings.ToLower(n), ".exe") {
			filters = append(filters, fmt.Sprintf("Name='%s'", strings.ReplaceAll(n, "'", "")))
		}
	}
	if len(filters) == 0 {
		return nil, nil
	}
	script := fmt.Sprintf(`$p = Get-CimInstance Win32_Process -Filter "%s" | Select-Object ProcessId,ParentProcessId,Name,CommandLine; ConvertTo-Json -InputObject @($p) -Compress`, strings.Join(filters, " or "))
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	b, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ProcessID       int    `json:"ProcessId"`
		ParentProcessID int    `json:"ParentProcessId"`
		Name            string `json:"Name"`
		CommandLine     string `json:"CommandLine"`
	}
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, err
	}
	out := make([]port.Process, 0, len(rows))
	for _, r := range rows {
		out = append(out, port.Process{PID: r.ProcessID, PPID: r.ParentProcessID, Name: r.Name, CommandLine: r.CommandLine})
	}
	return out, nil
}
