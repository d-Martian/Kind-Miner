//go:build windows

package engine

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

const (
	idlePriorityClass    = 0x00000040
	processSetInfo       = 0x00000200 // PROCESS_SET_INFORMATION
	processSuspendResume = 0x00000800 // PROCESS_SUSPEND_RESUME
)

func setPriority(p *os.Process) error {
	h, err := windows.OpenProcess(processSetInfo, false, uint32(p.Pid))
	if err != nil {
		return fmt.Errorf("OpenProcess: %w", err)
	}
	defer windows.CloseHandle(h)
	if err := windows.SetPriorityClass(h, idlePriorityClass); err != nil {
		return fmt.Errorf("SetPriorityClass: %w", err)
	}
	return nil
}

var (
	ntdll            = windows.NewLazySystemDLL("ntdll.dll")
	ntSuspendProcess = ntdll.NewProc("NtSuspendProcess")
	ntResumeProcess  = ntdll.NewProc("NtResumeProcess")
)

func suspendProcess(p *os.Process) error {
	h, err := windows.OpenProcess(processSuspendResume, false, uint32(p.Pid))
	if err != nil {
		return fmt.Errorf("OpenProcess: %w", err)
	}
	defer windows.CloseHandle(h)
	// NtSuspendProcess returns NTSTATUS; 0 = STATUS_SUCCESS
	if r1, _, _ := ntSuspendProcess.Call(uintptr(h)); r1 != 0 {
		return fmt.Errorf("NtSuspendProcess returned 0x%x", r1)
	}
	return nil
}

func resumeProcess(p *os.Process) error {
	h, err := windows.OpenProcess(processSuspendResume, false, uint32(p.Pid))
	if err != nil {
		return fmt.Errorf("OpenProcess: %w", err)
	}
	defer windows.CloseHandle(h)
	if r1, _, _ := ntResumeProcess.Call(uintptr(h)); r1 != 0 {
		return fmt.Errorf("NtResumeProcess returned 0x%x", r1)
	}
	return nil
}
