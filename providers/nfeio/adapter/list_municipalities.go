package adapter

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Municipality is one entry returned by GET /v2/municipalities.
type Municipality struct {
	Code         string `json:"code"`
	Name         string `json:"name"`
	State        string `json:"state"`
	SupportsNFSe bool   `json:"supportsNfse"`
}

// ListMunicipalitiesInput supports server-side pagination + state filter.
type ListMunicipalitiesInput struct {
	StateCode string `json:"state_code,omitempty"`
	Page      int    `json:"page,omitempty"`
	PageSize  int    `json:"page_size,omitempty"`
}

// ListMunicipalitiesOutput carries the paged page of municipalities.
type ListMunicipalitiesOutput struct {
	Municipalities []Municipality `json:"municipalities"`
	Total          int            `json:"total"`
	Page           int            `json:"page"`
}

// MunicipalitiesCache memoizes /v2/municipalities responses for the TTL.
// Key is (state_code, page, page_size). Stale entries are kept past expiry
// so the stale-while-error path can still surface a usable answer when the
// upstream call fails.
type MunicipalitiesCache struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]municipalitiesCacheEntry
}

type municipalitiesCacheEntry struct {
	data      *ListMunicipalitiesOutput
	expiresAt time.Time
}

// NewMunicipalitiesCache constructs a cache with the given TTL. Pass a
// real 1h TTL in production; tests pass 1ms to force stale-while-error.
func NewMunicipalitiesCache(ttl time.Duration) *MunicipalitiesCache {
	return &MunicipalitiesCache{ttl: ttl, entries: map[string]municipalitiesCacheEntry{}}
}

func (c *MunicipalitiesCache) key(in ListMunicipalitiesInput) string {
	return fmt.Sprintf("%s|p%d|s%d", in.StateCode, in.Page, in.PageSize)
}

func (c *MunicipalitiesCache) get(k string) (data *ListMunicipalitiesOutput, fresh bool, present bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[k]
	if !ok {
		return nil, false, false
	}
	return e.data, time.Now().Before(e.expiresAt), true
}

func (c *MunicipalitiesCache) set(k string, v *ListMunicipalitiesOutput) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[k] = municipalitiesCacheEntry{data: v, expiresAt: time.Now().Add(c.ttl)}
}

// ListMunicipalities GETs /v2/municipalities. Honors the cache TTL; on
// upstream failure, falls back to the last cached payload (regardless of
// expiry) so a transient NFe.io outage doesn't ripple into the caller.
func ListMunicipalities(ctx context.Context, cli *Client, cache *MunicipalitiesCache, in ListMunicipalitiesInput) (*ListMunicipalitiesOutput, error) {
	if in.Page == 0 {
		in.Page = 1
	}
	if in.PageSize == 0 {
		in.PageSize = 100
	}
	if in.PageSize > 500 {
		in.PageSize = 500
	}
	key := cache.key(in)
	if cached, fresh, present := cache.get(key); present && fresh {
		return cached, nil
	}
	q := fmt.Sprintf("/v2/municipalities?page=%d&pageSize=%d", in.Page, in.PageSize)
	if in.StateCode != "" {
		q += "&stateCode=" + in.StateCode
	}
	out := &ListMunicipalitiesOutput{}
	if err := cli.do(ctx, http.MethodGet, q, nil, out); err != nil {
		// Stale-while-error: return last cached payload regardless of expiry.
		if cached, _, present := cache.get(key); present {
			return cached, nil
		}
		return nil, err
	}
	cache.set(key, out)
	return out, nil
}
