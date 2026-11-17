//go:build !linux

package macosnetwork

import (
	"context"
	"fmt"
)

func linuxInterface(string) (string, error)       { return "", fmt.Errorf("Linux-only adapter") }
func linuxInterfaces() ([]InterfaceOption, error) { return nil, fmt.Errorf("Linux-only adapter") }
func linuxRoute(context.Context, string) (RouteSelection, error) {
	return RouteSelection{}, fmt.Errorf("Linux-only adapter")
}
func linuxDiscover(context.Context, string, string) (Snapshot, error) {
	return Snapshot{}, fmt.Errorf("Linux-only adapter")
}
func linuxNeighbors(context.Context, string) ([]Neighbor, error) {
	return nil, fmt.Errorf("Linux-only adapter")
}
func linuxVerifyTUN(context.Context, string, bool) error { return fmt.Errorf("Linux-only adapter") }
