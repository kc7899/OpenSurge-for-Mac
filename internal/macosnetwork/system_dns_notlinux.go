//go:build !linux

package macosnetwork

import (
	"context"
	"fmt"
	"open-mihomo-gateway/internal/runtime"
)

func linuxPrepareDNS(context.Context, string) (runtime.SystemDNSSnapshot, error) {
	return runtime.SystemDNSSnapshot{}, fmt.Errorf("Linux-only DNS adapter")
}
func linuxEnableDNS(context.Context, runtime.SystemDNSSnapshot) error {
	return fmt.Errorf("Linux-only DNS adapter")
}
func linuxRestoreDNS(context.Context, runtime.SystemDNSSnapshot) (bool, error) {
	return false, fmt.Errorf("Linux-only DNS adapter")
}
func linuxVerifyDNS(context.Context, runtime.SystemDNSSnapshot) error {
	return fmt.Errorf("Linux-only DNS adapter")
}
