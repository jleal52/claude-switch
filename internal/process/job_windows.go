//go:build windows

package process

import (
	"fmt"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Job is a handle to a Windows Job Object configured with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE. When the process holding this handle
// exits (for any reason), every process assigned to the job is killed.
type Job struct {
	handle windows.Handle
}

// NewKillOnCloseJob creates and configures a fresh Job Object. The caller
// must hold the Job struct for the lifetime of the wrapper; closing it
// (or wrapper exit) kills every assigned child.
func NewKillOnCloseJob() (*Job, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}

	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE

	if _, err := windows.SetInformationJobObject(
		h,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("set job information: %w", err)
	}
	return &Job{handle: h}, nil
}

// Assign adds cmd.Process to the job. Must be called AFTER cmd.Start().
// To avoid the child running briefly outside the job, callers should
// start the process suspended and resume it only after this returns.
func (j *Job) Assign(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return fmt.Errorf("cmd has no Process (not started)")
	}
	procHandle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		return fmt.Errorf("open child process: %w", err)
	}
	defer windows.CloseHandle(procHandle)
	return windows.AssignProcessToJobObject(j.handle, procHandle)
}

// Close destroys the job, killing every assigned process.
func (j *Job) Close() error {
	return windows.CloseHandle(j.handle)
}
