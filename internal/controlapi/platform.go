package controlapi

import "runtime"

func hostPlatform() string { return runtime.GOOS }
