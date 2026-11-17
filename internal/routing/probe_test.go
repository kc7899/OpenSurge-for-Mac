package routing

import "testing"

func TestTraceRequiresValidCountryAndPublicAddress(t *testing.T) {
	for _, raw := range []string{"ip=127.0.0.1\nloc=US", "ip=192.168.0.1\nloc=US", "ip=1.1.1.1\nloc=USA", "ip=1.1.1.1\nloc=US\nloc=HK", "ip=bad\nloc=JP"} {
		c, ip := ParseTrace([]byte(raw))
		if c != "" || ip != "" {
			t.Fatal("invalid trace accepted")
		}
	}
	c, ip := ParseTrace([]byte("ip=1.1.1.1\nloc=US\n"))
	if c != "US" || ip != "1.1.1.1" {
		t.Fatal(c, ip)
	}
}
