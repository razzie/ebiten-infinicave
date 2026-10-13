package render

import (
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestSupportIndexIgnoresVisualBoundsAndSharesPairedDeletion(t *testing.T) {
	anchors := []geom.V{{X: .5, Y: .5}, {X: .52, Y: .5}}
	source := []Attachment{
		{Kind: GrassAttachment, Anchors: anchors, Min: geom.V{X: .6, Y: .2}, Max: geom.V{X: .7, Y: .4}},
		{Kind: GrassAttachment, Anchors: anchors, Min: geom.V{X: .6, Y: .2}, Max: geom.V{X: .7, Y: .4}},
		{Kind: SurfaceMoss, Anchors: []geom.V{{X: .5, Y: .5}}},
		{Kind: MushroomAttachment, Anchors: []geom.V{{X: .8, Y: .5}}},
	}
	index := AttachmentIndex(source)
	if len(index.points) != 3 || len(index.points[0].owners) != 2 {
		t.Fatal("moss was indexed or paired supports were duplicated")
	}
	cut, _ := terrain.CutFromHole(terrain.Hole{Shape: terrain.HoleCircle, Center: geom.V{X: .5, Y: -2.5}, Radius: .01})
	filtered, removed := FilterAttachmentsIndexedWithChanges(source, index, []terrain.RockCut{cut}, -3)
	if !reflect.DeepEqual(removed, []int{0, 1}) || !filtered[0].Dead || !filtered[1].Dead || filtered[2].Dead || filtered[3].Dead {
		t.Fatalf("support hit lost paired deletion or damaged unrelated records: %v", removed)
	}
	if source[0].Dead || source[1].Dead {
		t.Fatal("filter mutated the original published recipe")
	}
	if again, removed := FilterAttachmentsIndexedWithChanges(filtered, index, []terrain.RockCut{cut}, -3); len(removed) != 0 || &again[0] != &filtered[0] {
		t.Fatal("dead supports were deleted again")
	}
	miss, _ := terrain.CutFromHole(terrain.Hole{Shape: terrain.HoleCircle, Center: geom.V{X: .65, Y: -2.7}, Radius: .01})
	if got, removed := FilterAttachmentsIndexedWithChanges(source, index, []terrain.RockCut{miss}, -3); len(removed) != 0 || &got[0] != &source[0] {
		t.Fatal("sprite-only overlap removed a decoration")
	}
	if allocations := testing.AllocsPerRun(100, func() { FilterAttachmentsIndexedWithChanges(source, index, []terrain.RockCut{miss}, -3) }); allocations != 0 {
		t.Fatalf("anchor miss allocated %v times", allocations)
	}
}

func TestWebFreeNodesDoNotRemoveSupportedNetwork(t *testing.T) {
	w := terrain.NewSpiderWeb([]geom.V{{X: .2, Y: .3}, {X: .4, Y: .3}, {X: .25, Y: .5}}, .5)
	attachments := prepareWebAttachments([]terrain.SpiderWeb{w}, false)
	if len(attachments) != 1 || len(attachments[0].Footprint) != 0 {
		t.Fatal("web retained silk damage footprints")
	}
	hub := w.Network.Nodes[w.Network.Hub[0]]
	cut, _ := terrain.CutFromHole(terrain.Hole{Shape: terrain.HoleCircle, Center: hub, Radius: .001})
	if attachments[0].Hit(cut, 0) {
		t.Fatal("free silk damage removed an anchored web")
	}
	cut, _ = terrain.CutFromHole(terrain.Hole{Shape: terrain.HoleCircle, Center: w.Network.Nodes[0], Radius: .001})
	if !attachments[0].Hit(cut, 0) {
		t.Fatal("support damage left a web alive")
	}
}
