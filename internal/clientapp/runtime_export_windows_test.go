//go:build windows

package clientapp

import "github.com/endless-net/client/internal/localipc"

// PipeConfig is only needed by isolated native IPC test clients.
func (r *WindowsRuntime) PipeConfig() localipc.PipeConfig { return r.pipe }
