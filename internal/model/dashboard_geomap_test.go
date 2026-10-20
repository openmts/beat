package model

import (
	"errors"
	"math"
	"strings"
	"testing"
)

// geoLocation 构造一条合法地点，便于逐项替换字段。
func geoLocation(match string, latitude, longitude float64) DashboardGeoLocation {
	return DashboardGeoLocation{Match: match, Latitude: floatPointer(latitude), Longitude: floatPointer(longitude)}
}

// geomapDefinition 把默认看板的单面板改造成 geo map 面板：缺省带一条地点，
// 便于逐项验证其它字段，而不用在每个用例里重复地点表。
func geomapDefinition(change func(panel *DashboardPanel)) DashboardDefinition {
	definition := DefaultMetricDashboard("admin").Definition
	panel := definition.Panels[0]
	panel.Type = "geomap"
	panel.GeoLocations = []DashboardGeoLocation{geoLocation("node-a", 39.9042, 116.4074)}
	change(&panel)
	definition.Panels = []DashboardPanel{panel}
	return definition
}

func TestDashboardGeomapAcceptsDocumentedValues(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"defaults": func(panel *DashboardPanel) {},
		"explicit": func(panel *DashboardPanel) {
			panel.GeoLayer, panel.GeoZoom = "heatmap", intPointer(5)
			panel.GeoCenterLatitude, panel.GeoCenterLongitude = floatPointer(31.2304), floatPointer(121.4737)
			panel.GeoMarkerSize = intPointer(20)
			panel.GeoTileURL = "https://tiles.example.com/{z}/{x}/{y}.png"
		},
		"markers layer": func(panel *DashboardPanel) { panel.GeoLayer = "markers" },
		"zoom bounds":   func(panel *DashboardPanel) { panel.GeoZoom = intPointer(MaxDashboardGeoZoom) },
		"marker bounds": func(panel *DashboardPanel) { panel.GeoMarkerSize = intPointer(MinDashboardGeoMarkerSize) },
		"label":         func(panel *DashboardPanel) { panel.GeoLocations[0].Label = "北京" },
		"device match":  func(panel *DashboardPanel) { panel.GeoLocations[0] = geoLocation("gpu-0", 37.7749, -122.4194) },
		"multiple": func(panel *DashboardPanel) {
			panel.GeoLocations = append(panel.GeoLocations, geoLocation("node-b", -33.8688, 151.2093))
		},
		"http tiles":    func(panel *DashboardPanel) { panel.GeoTileURL = "http://tiles.internal/{z}/{x}/{y}.jpg" },
		"max locations": func(panel *DashboardPanel) { panel.GeoLocations = geoLocations(MaxDashboardGeoLocations) },
		"multiple series": func(panel *DashboardPanel) {
			panel.Series = append(panel.Series, DashboardSeries{RefID: "B", NodeID: panel.Series[0].NodeID, Metric: panel.Series[0].Metric, Aggregation: panel.Series[0].Aggregation, Color: "green"})
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboard(geomapDefinition(change)); err != nil {
				t.Fatalf("valid geo map rejected: %v", err)
			}
		})
	}
}

// geoLocations 生成 count 条互不相同的地点。
func geoLocations(count int) []DashboardGeoLocation {
	locations := make([]DashboardGeoLocation, 0, count)
	for index := 0; index < count; index++ {
		locations = append(locations, geoLocation("node-"+string(rune('a'+index%26))+string(rune('a'+index/26)), float64(index%80), float64(index%170)))
	}
	return locations
}

func TestDashboardGeomapRejectsInvalidValues(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"no locations": func(panel *DashboardPanel) { panel.GeoLocations = nil },
		"too many locations": func(panel *DashboardPanel) {
			panel.GeoLocations = geoLocations(MaxDashboardGeoLocations + 1)
		},
		"empty match": func(panel *DashboardPanel) { panel.GeoLocations[0].Match = "   " },
		"long match": func(panel *DashboardPanel) {
			panel.GeoLocations[0].Match = strings.Repeat("n", MaxDashboardGeoMatchBytes+1)
		},
		"duplicate match": func(panel *DashboardPanel) {
			panel.GeoLocations = append(panel.GeoLocations, geoLocation("NODE-A", 1, 1))
		},
		"missing latitude":  func(panel *DashboardPanel) { panel.GeoLocations[0].Latitude = nil },
		"missing longitude": func(panel *DashboardPanel) { panel.GeoLocations[0].Longitude = nil },
		"non finite latitude": func(panel *DashboardPanel) {
			panel.GeoLocations[0].Latitude = floatPointer(math.NaN())
		},
		"infinite longitude": func(panel *DashboardPanel) {
			panel.GeoLocations[0].Longitude = floatPointer(math.Inf(1))
		},
		"latitude out of range": func(panel *DashboardPanel) {
			panel.GeoLocations[0].Latitude = floatPointer(DashboardGeoMaxLatitude + 1)
		},
		"longitude out of range": func(panel *DashboardPanel) {
			panel.GeoLocations[0].Longitude = floatPointer(MaxDashboardGeoLongitude + 1)
		},
		"invalid label": func(panel *DashboardPanel) { panel.GeoLocations[0].Label = "\u0000" },
		"long label": func(panel *DashboardPanel) {
			panel.GeoLocations[0].Label = strings.Repeat("x", MaxDashboardGeoLabelBytes+1)
		},
		"unknown layer":      func(panel *DashboardPanel) { panel.GeoLayer = "polygons" },
		"zoom below minimum": func(panel *DashboardPanel) { panel.GeoZoom = intPointer(MinDashboardGeoZoom - 1) },
		"zoom above maximum": func(panel *DashboardPanel) { panel.GeoZoom = intPointer(MaxDashboardGeoZoom + 1) },
		"center without longitude": func(panel *DashboardPanel) {
			panel.GeoCenterLatitude = floatPointer(10)
		},
		"center without latitude": func(panel *DashboardPanel) {
			panel.GeoCenterLongitude = floatPointer(10)
		},
		"center out of range": func(panel *DashboardPanel) {
			panel.GeoCenterLatitude, panel.GeoCenterLongitude = floatPointer(DashboardGeoMaxLatitude+1), floatPointer(0)
		},
		"marker size too small": func(panel *DashboardPanel) { panel.GeoMarkerSize = intPointer(MinDashboardGeoMarkerSize - 1) },
		"marker size too large": func(panel *DashboardPanel) { panel.GeoMarkerSize = intPointer(MaxDashboardGeoMarkerSize + 1) },
		"value mappings": func(panel *DashboardPanel) {
			panel.ValueMappings = []DashboardValueMapping{{Type: "value", Value: floatPointer(1), Text: "one"}}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboard(geomapDefinition(change)); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("invalid geo map accepted: %v", err)
			}
		})
	}
}

func TestDashboardGeomapRejectsOptionsOnOtherPanels(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"locations":  func(panel *DashboardPanel) { panel.GeoLocations = []DashboardGeoLocation{geoLocation("node-a", 1, 1)} },
		"layer":      func(panel *DashboardPanel) { panel.GeoLayer = "heatmap" },
		"tile url":   func(panel *DashboardPanel) { panel.GeoTileURL = "https://tiles.example.com/{z}/{x}/{y}.png" },
		"zoom":       func(panel *DashboardPanel) { panel.GeoZoom = intPointer(3) },
		"center lat": func(panel *DashboardPanel) { panel.GeoCenterLatitude = floatPointer(10) },
		"center lon": func(panel *DashboardPanel) { panel.GeoCenterLongitude = floatPointer(10) },
		"marker size": func(panel *DashboardPanel) {
			panel.GeoMarkerSize = intPointer(12)
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			definition := DefaultMetricDashboard("admin").Definition
			panel := definition.Panels[0]
			change(&panel)
			definition.Panels = []DashboardPanel{panel}
			if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("geo map options accepted on %s panel: %v", panel.Type, err)
			}
		})
	}
}

func TestDashboardRowRejectsGeomapOptions(t *testing.T) {
	cases := map[string]func(panel *DashboardPanel){
		"locations": func(panel *DashboardPanel) {
			panel.GeoLocations = []DashboardGeoLocation{geoLocation("node-a", 1, 1)}
		},
		"layer":    func(panel *DashboardPanel) { panel.GeoLayer = "markers" },
		"tile url": func(panel *DashboardPanel) { panel.GeoTileURL = "https://tiles.example.com/{z}/{x}/{y}.png" },
		"zoom":     func(panel *DashboardPanel) { panel.GeoZoom = intPointer(2) },
		"center": func(panel *DashboardPanel) {
			panel.GeoCenterLatitude, panel.GeoCenterLongitude = floatPointer(1), floatPointer(2)
		},
		"marker": func(panel *DashboardPanel) { panel.GeoMarkerSize = intPointer(9) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			definition := DefaultMetricDashboard("admin").Definition
			panel := definition.Panels[0]
			panel.Type, panel.Size, panel.Legend = "row", "large", false
			panel.Unit, panel.Series = "", nil
			change(&panel)
			definition.Panels = []DashboardPanel{panel}
			if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("row panel accepted geo map option %s: %v", name, err)
			}
		})
	}
}

func TestDashboardGeoTileURLValidation(t *testing.T) {
	valid := []string{
		"",
		"https://tiles.example.com/{z}/{x}/{y}.png",
		"http://10.0.0.9/osm/{z}/{x}/{y}.jpg",
		"https://example.com/a/{x}/b/{y}/{z}",
	}
	for _, raw := range valid {
		if err := validateDashboardGeoTileURL(raw); err != nil {
			t.Fatalf("valid tile template rejected: %q: %v", raw, err)
		}
	}
	invalid := map[string]string{
		"missing z":     "https://tiles.example.com/{x}/{y}.png",
		"missing x":     "https://tiles.example.com/{z}/{y}.png",
		"missing y":     "https://tiles.example.com/{z}/{x}.png",
		"unknown token": "https://tiles.example.com/{z}/{x}/{y}-{s}.png",
		"non http":      "ftp://tiles.example.com/{z}/{x}/{y}.png",
		"relative":      "/tiles/{z}/{x}/{y}.png",
		"whitespace":    "https://tiles.example.com/{z}/ {x}/{y}.png",
		"control char":  "https://tiles.example.com/{z}/{x}/{y}\n.png",
		"too long":      "https://tiles.example.com/" + strings.Repeat("a", MaxDashboardGeoTileURLBytes) + "/{z}/{x}/{y}.png",
	}
	for name, raw := range invalid {
		t.Run(name, func(t *testing.T) {
			if err := validateDashboardGeoTileURL(raw); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("invalid tile template accepted: %q: %v", raw, err)
			}
		})
	}
}

func TestDashboardGeomapBounds(t *testing.T) {
	if MinDashboardGeoZoom != 0 || MaxDashboardGeoZoom != 8 {
		t.Fatalf("geo map zoom bounds must stay 0..8, got %d..%d", MinDashboardGeoZoom, MaxDashboardGeoZoom)
	}
	if MinDashboardGeoMarkerSize != 2 || MaxDashboardGeoMarkerSize != 64 {
		t.Fatalf("geo map marker bounds must stay 2..64, got %d..%d", MinDashboardGeoMarkerSize, MaxDashboardGeoMarkerSize)
	}
	if MaxDashboardGeoLocations != 64 {
		t.Fatalf("geo map location cap must stay 64, got %d", MaxDashboardGeoLocations)
	}
	if math.Abs(DashboardGeoMaxLatitude-85.05112878) > 1e-6 {
		t.Fatalf("geo map latitude limit must stay at the Web Mercator limit, got %v", DashboardGeoMaxLatitude)
	}
}
