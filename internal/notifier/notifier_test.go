package notifier

import (
	"sismo/internal/usgs"
	"testing"
	"time"
)

func TestConsoleNotifierNotify(t *testing.T) {
	c := NewConsoleNotifier()
	f := usgs.Feature{
		ID: "test_notify",
		Properties: usgs.Properties{
			Mag:   4.8,
			Place: "Caracas, Venezuela",
			Time:  time.Now().UnixNano() / 1e6,
			URL:   "https://earthquake.usgs.gov/earthquakes/eventpage/test_notify",
		},
		Geometry: usgs.Geometry{
			Coordinates: []float64{-66.9036, 10.4806, 15.0},
		},
	}
	err := c.Notify(f)
	if err != nil {
		t.Errorf("Expected no error from ConsoleNotifier.Notify, got: %v", err)
	}
}
