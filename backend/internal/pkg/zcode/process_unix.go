//go:build !windows

package zcode

import "os/exec"

func hideProcess(_ *exec.Cmd) {}
