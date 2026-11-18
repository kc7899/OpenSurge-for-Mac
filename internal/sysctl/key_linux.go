package sysctl

const keyIPForwarding = "net.ipv4.ip_forward"

func NewForInterface(name string) Manager { return Manager{interfaceName: name} }
