package terrain

import (
	"reflect"
	"testing"
)

func TestGuideCacheIsBoundedIndependentAndOwnsItsOutput(t *testing.T) {
	cache := newGuideCache(42)
	for _, id := range []int64{0, 1, 3, 2, 100, 0} {
		got, want := cache.window(id), worldGuides(42, id)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("guide shapes depend on cache history at section %d", id)
		}
		if len(cache.proposals) > 7 || len(cache.resolved) > 5 {
			t.Fatal("guide cache grew beyond its window")
		}
		if len(got) > 0 {
			got[0].Pts[0].X += 100
			got[0].S[1] += 100
			if !reflect.DeepEqual(cache.window(id), want) {
				t.Fatal("caller changed cached guide data")
			}
		}
	}
}
