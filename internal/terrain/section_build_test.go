package terrain

import (
	"context"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestForegroundMaterialIndependentOfBackground(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		ctx := context.Background()
		builder := NewSectionBuilder(42, nil, orientation)
		build := builder.Begin(ctx, 0)
		early := build.Data().ForegroundTopology
		snapshot := append(RockGrid(nil), early.Grid...)
		if !build.MaterializeForeground(ctx) {
			t.Fatal("foreground preparation failed")
		}
		foreground := build.Data()
		if build.materialized || len(foreground.Background) != 0 || len(foreground.Vines) != 0 || len(foreground.BackgroundNeighbors) != 0 {
			t.Fatal("foreground waited for background material or vines")
		}
		appearance := foreground.ForegroundBake.Appearance
		if len(appearance) == 0 {
			t.Fatal("foreground appearance is empty")
		}
		if !build.Materialize(ctx) || !builder.Decorate(ctx, build) {
			t.Fatal("full preparation failed")
		}
		if !reflect.DeepEqual(appearance, build.Data().ForegroundBake.Appearance) || !reflect.DeepEqual(early.Grid, snapshot) {
			t.Fatal("full material changed foreground appearance or mutated published geometry")
		}
		if &appearance[0] != &build.Data().ForegroundBake.Appearance[0] {
			t.Fatal("background recomputed foreground appearance")
		}
	}
}

func TestDecorationProductsPublishBeforeJoin(t *testing.T) {
	old := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(old)
	builder := NewSectionBuilder(42, nil)
	build := builder.Begin(context.Background(), 0)
	if !build.Materialize(context.Background()) {
		t.Fatal("material preparation failed")
	}
	release := make(chan struct{})
	defer close(release)
	mushrooms := make(chan SectionData, 1)
	finished := make(chan bool, 1)
	go func() {
		finished <- builder.DecorateProducts(context.Background(), build, func(part VegetationParts, data SectionData) {
			switch part {
			case BackgroundVinesPart:
				<-release
			case MushroomsPart:
				mushrooms <- data
			}
		})
	}()
	select {
	case data := <-mushrooms:
		if data.VegetationBake == nil || len(data.Vines) != 0 || len(data.ForegroundVines) != 0 {
			t.Fatal("partial snapshot contains another generator's output")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("mushrooms waited for another product callback")
	}
	select {
	case <-finished:
		t.Fatal("build completed before the blocked product")
	default:
	}
	// Unblock and join before changing the global worker budget.
	release <- struct{}{}
	if !<-finished {
		t.Fatal("build failed")
	}
}

func TestDecorationProductMergesMatchCompleteGeometry(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		builder := NewSectionBuilder(42, nil, orientation)
		build := builder.Begin(context.Background(), 0)
		early := PrepareTerrainGeometry(build.Data(), 0)
		if !build.Materialize(context.Background()) {
			t.Fatal("material preparation failed")
		}
		material := BindTerrainMaterial(early, build.Data())
		var mu sync.Mutex
		products := make(map[VegetationParts]*Geometry)
		if !builder.DecorateProducts(context.Background(), build, func(part VegetationParts, data SectionData) {
			snapshot := BindTerrainVegetation(material, data)
			mu.Lock()
			products[part] = snapshot
			mu.Unlock()
		}) {
			t.Fatal("decoration failed")
		}
		want := BindTerrainVegetation(material, build.Data())
		// Reverse arrivals, and insert terrain-only snapshots between products.
		for _, order := range [][]VegetationParts{
			{BackgroundVinesPart, WebsPart, MushroomsPart, ForegroundVinesPart},
			{ForegroundVinesPart, MushroomsPart, WebsPart, BackgroundVinesPart},
		} {
			got := material
			for _, part := range order {
				got = MergeTerrainVegetation(got, products[part], part)
				got = MergeTerrainVegetation(got, material, 0)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("partial merge changed geometry for orientation %v", orientation)
			}
		}
		if len(material.Vegetation.Mushrooms) != 0 || len(material.Topology.SpiderWebs) != 0 {
			t.Fatal("publication mutated material snapshot")
		}
	}
}

func TestCanceledDecorationProductsRetry(t *testing.T) {
	builder := NewSectionBuilder(42, nil)
	build := builder.Begin(context.Background(), 0)
	if !build.Materialize(context.Background()) {
		t.Fatal("material preparation failed")
	}
	before := build.Data()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if builder.DecorateProducts(ctx, build, func(VegetationParts, SectionData) { cancel() }) {
		t.Fatal("canceled decoration succeeded")
	}
	if !reflect.DeepEqual(build.Data(), before) {
		t.Fatal("canceled decoration mutated build snapshot")
	}
	if !builder.Decorate(context.Background(), build) {
		t.Fatal("retry failed")
	}
	fresh := NewSectionBuilder(42, nil).Begin(context.Background(), 0)
	if !fresh.Materialize(context.Background()) || !builder.Decorate(context.Background(), fresh) {
		t.Fatal("reference build failed")
	}
	if !reflect.DeepEqual(build.Data(), fresh.Data()) {
		t.Fatal("retry changed deterministic geometry")
	}
}
