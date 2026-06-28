package filter

import (
	"sismo/internal/config"
	"sismo/internal/usgs"
	"testing"
	"time"
)

func TestFilterEvaluate(t *testing.T) {
	cfg := &config.Config{
		MinMagnitude: 4.5,
		MinLatitude:  0.5,
		MaxLatitude:  12.5,
		MinLongitude: -73.5,
		MaxLongitude: -59.5,
	}

	f := NewFilter(cfg)

	// Case 1: Matching event (Venezuela, mag >= 4.5)
	matchingFeature := usgs.Feature{
		ID: "event1",
		Properties: usgs.Properties{
			Mag:   5.0,
			Place: "Offshore Venezuela",
			Time:  time.Now().UnixNano() / 1e6,
		},
		Geometry: usgs.Geometry{
			Coordinates: []float64{-65.0, 10.0, 10.0}, // [longitude, latitude, depth]
		},
	}

	if !f.Evaluate(matchingFeature, false) {
		t.Error("Expected matchingFeature to pass filter, but it failed")
	}

	// Case 2: Deduplication - same event should be filtered out
	if f.Evaluate(matchingFeature, false) {
		t.Error("Expected matchingFeature to be deduplicated, but it passed")
	}

	// Case 3: Low magnitude event
	lowMagFeature := usgs.Feature{
		ID: "event2",
		Properties: usgs.Properties{
			Mag:   3.5,
			Place: "Offshore Venezuela",
			Time:  time.Now().UnixNano() / 1e6,
		},
		Geometry: usgs.Geometry{
			Coordinates: []float64{-65.0, 10.0, 10.0},
		},
	}
	if f.Evaluate(lowMagFeature, false) {
		t.Error("Expected lowMagFeature to fail due to low magnitude, but it passed")
	}

	// Case 4: Out of bounds location (e.g., Japan)
	outOfBoundsFeature := usgs.Feature{
		ID: "event3",
		Properties: usgs.Properties{
			Mag:   6.0,
			Place: "Japan",
			Time:  time.Now().UnixNano() / 1e6,
		},
		Geometry: usgs.Geometry{
			Coordinates: []float64{138.0, 36.0, 10.0},
		},
	}
	if f.Evaluate(outOfBoundsFeature, false) {
		t.Error("Expected outOfBoundsFeature to fail due to location, but it passed")
	}

	// Case 5: Startup run (isStartup = true) should populate cache but return false (no notification)
	startupFeature := usgs.Feature{
		ID: "event4",
		Properties: usgs.Properties{
			Mag:   6.0,
			Place: "Offshore Venezuela",
			Time:  time.Now().UnixNano() / 1e6,
		},
		Geometry: usgs.Geometry{
			Coordinates: []float64{-65.0, 10.0, 10.0},
		},
	}
	if f.Evaluate(startupFeature, true) {
		t.Error("Expected startupFeature to return false during startup, but it returned true")
	}

	// Verify that startupFeature is now in the cache (and will be deduplicated on normal run)
	if f.Evaluate(startupFeature, false) {
		t.Error("Expected startupFeature to be deduplicated on subsequent run, but it passed")
	}
}

func TestCleanCache(t *testing.T) {
	cfg := &config.Config{
		MinMagnitude: 4.5,
	}
	f := NewFilter(cfg)

	// Add an item to cache with older timestamp
	f.mu.Lock()
	f.seenCache["old_event"] = time.Now().Add(-25 * time.Hour)
	f.seenCache["new_event"] = time.Now().Add(-1 * time.Hour)
	f.mu.Unlock()

	f.CleanCache()

	f.mu.RLock()
	defer f.mu.RUnlock()

	if _, seen := f.seenCache["old_event"]; seen {
		t.Error("Expected old_event to be cleaned from cache, but it remains")
	}

	if _, seen := f.seenCache["new_event"]; !seen {
		t.Error("Expected new_event to remain in cache, but it was cleaned")
	}
}
