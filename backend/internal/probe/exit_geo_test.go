package probe

import (
	"context"
	"testing"

	"proxycheck/backend/internal/storage"
)

func TestExitGeoProberStoresNodeMeta(t *testing.T) {
	store := &fakeMetaStore{}
	prober := ExitGeoProber{
		Lookup: func(context.Context, storage.Node) (GeoResult, error) {
			return GeoResult{
				ExitIP:  "203.0.113.10",
				ASN:     "AS64500",
				Country: "US",
				Region:  "California",
				ISP:     "Example ISP",
			}, nil
		},
		Store: store,
	}
	result := prober.Probe(context.Background(), testNode("node-a", 20001))[0]
	if !result.Success || result.Metric != "exit_geo" {
		t.Fatalf("unexpected exit_geo result: %#v", result)
	}
	if store.nodeID != 1 || store.meta.Country == nil || *store.meta.Country != "US" {
		t.Fatalf("metadata was not stored: %#v", store)
	}
}

type fakeMetaStore struct {
	nodeID int
	meta   NodeMetaUpdate
}

func (s *fakeMetaStore) UpsertNodeMeta(nodeID int, meta NodeMetaUpdate) error {
	s.nodeID = nodeID
	s.meta = meta
	return nil
}
