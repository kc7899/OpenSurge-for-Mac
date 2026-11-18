//go:build !linux

package sysctl

const keyIPForwarding = "net.inet.ip.forwarding"

func NewForInterface(_ string) Manager { return New() }
