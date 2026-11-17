package routing

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// ParseQuota accepts the subscription-userinfo header. Malformed/missing data
// remains unknown; expiry can be known independently of a traffic allowance.
func ParseQuota(header string, now time.Time) Quota {
	q := Quota{CheckedAt: now}
	values := map[string]uint64{}
	for _, part := range strings.Split(header, ";") {
		pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(pair) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(pair[0]))
		if key != "upload" && key != "download" && key != "total" && key != "expire" {
			continue
		}
		if _, ok := values[key]; ok {
			q.Error = "duplicate quota field"
			return q
		}
		n, err := strconv.ParseUint(strings.TrimSpace(pair[1]), 10, 64)
		if err != nil {
			q.Error = "invalid quota field"
			return q
		}
		values[key] = n
	}
	up, u := values["upload"]
	down, d := values["download"]
	total, t := values["total"]
	if up > math.MaxUint64-down {
		q.Error = "quota overflow"
		return q
	}
	if u && d && t && total > 0 {
		q.Known = true
		q.Used = up + down
		q.Total = total
	}
	if expire, ok := values["expire"]; ok && expire > 0 {
		if expire > math.MaxInt64 {
			q.Error = "invalid expiry"
			q.Known = false
			return q
		}
		q.ExpiresAt = time.Unix(int64(expire), 0)
	}
	if !q.Known && q.ExpiresAt.IsZero() {
		q.Error = fmt.Sprint("quota unavailable")
	}
	return q
}
