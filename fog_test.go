package infinicave

import "testing"

func TestFogConfigurationAndLifecycle(t *testing.T) {
	configs := []Config{{}, DefaultConfig(), {Fog: true}}
	for view := ViewClay; view <= ViewShadows; view++ {
		configs = append(configs, Config{Fog: true, View: view})
	}
	for _, config := range configs {
		scene, err := NewScene(config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(scene.Close)
		if enabled := config.Fog && config.View == ViewShaded; (scene.fog != nil) != enabled {
			t.Fatalf("fog enabled incorrectly for config %+v", config)
		}
		if scene.fog == nil {
			continue
		}
		fog := scene.fog
		scene.Update(Viewport{})
		if fog.time != 0 {
			t.Fatal("invalid viewport advanced fog animation")
		}
		scene.Update(Viewport{Y: -.8, Height: .8})
		if fog.time <= 0 {
			t.Fatal("valid update did not advance fog animation while terrain loads")
		}
		scene.Reset(42)
		if scene.fog != fog {
			t.Fatal("world reset lost the fog renderer")
		}
		scene.Close()
		before := fog.time
		scene.Update(Viewport{Y: -.8, Height: .8})
		if fog.time != before {
			t.Fatal("closed scene advanced fog animation")
		}
		scene.Close()
	}
}
