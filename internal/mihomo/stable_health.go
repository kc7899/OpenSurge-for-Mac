package mihomo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/routing"
)

// Mihomo's /proxies only lists top-level proxies. Provider members have their
// own inventory and healthcheck endpoint; treating them as top-level proxies
// loses their labels and turns HTTP 404 into a false node failure.
func mergeStableProviderHealth(ctx context.Context, cfg config.Config, client *http.Client, body *proxiesResponse) error {
	pool, dynamic, err := stablePolicyPool(cfg)
	if !dynamic {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot read applied node pool: %w", err)
	}
	req, err := newAPIRequest(ctx, cfg, http.MethodGet, "/providers/proxies/"+url.PathEscape(routing.PoolName), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("node pool inventory API returned %s", resp.Status)
	}
	var provider struct {
		Proxies []proxyRecord `json:"proxies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&provider); err != nil {
		return err
	}
	if body.Proxies == nil {
		body.Proxies = map[string]proxyRecord{}
	}
	valid := map[string]bool{}
	for _, c := range pool.Graph.Candidates {
		valid[c.ID] = true
	}
	for _, proxy := range provider.Proxies {
		if valid[proxy.Name] {
			proxy.Provider = routing.PoolName
			body.Proxies[proxy.Name] = proxy
			delete(valid, proxy.Name)
		}
	}
	if len(valid) != 0 {
		return fmt.Errorf("running node pool differs from saved pool; refresh the view")
	}
	return nil
}
