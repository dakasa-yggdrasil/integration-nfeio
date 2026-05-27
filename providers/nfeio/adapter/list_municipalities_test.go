package adapter

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestListMunicipalities_CachedAfterFirstCall(t *testing.T) {
	calls := 0
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v2/municipalities" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"municipalities":[{"code":"3550308","name":"São Paulo","state":"SP","supportsNfse":true}],"total":1,"page":1}`))
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)
	cache := NewMunicipalitiesCache(1 * time.Hour)

	for i := 0; i < 3; i++ {
		out, err := ListMunicipalities(context.Background(), cli, cache, ListMunicipalitiesInput{Page: 1, PageSize: 100})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Municipalities) != 1 {
			t.Fatalf("len = %d", len(out.Municipalities))
		}
	}
	if calls != 1 {
		t.Errorf("upstream calls = %d; want 1 (cache must hit on calls 2+3)", calls)
	}
}

func TestListMunicipalities_CacheStaleWhileError(t *testing.T) {
	calls := 0
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"municipalities":[{"code":"3550308","name":"SP","state":"SP","supportsNfse":true}],"total":1,"page":1}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer srv.Close()
	cli := mustNewClient(t, srv.URL)
	cache := NewMunicipalitiesCache(1 * time.Millisecond)
	_, _ = ListMunicipalities(context.Background(), cli, cache, ListMunicipalitiesInput{Page: 1})
	time.Sleep(5 * time.Millisecond)
	out, err := ListMunicipalities(context.Background(), cli, cache, ListMunicipalitiesInput{Page: 1})
	if err != nil {
		t.Fatalf("stale-while-error must suppress network error: %v", err)
	}
	if len(out.Municipalities) == 0 {
		t.Fatal("expected stale cache to surface, got empty")
	}
}
