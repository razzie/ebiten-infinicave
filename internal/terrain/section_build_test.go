package terrain

import (
	"context"
	"reflect"
	"testing"
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
