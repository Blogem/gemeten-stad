package gebieden

import "encoding/json"

// rdCRSName is the RD (Amersfoort / RD New, EPSG:28992) CRS URN both Datapunt gebieden and the
// CBS WFS express their geometry in.
const rdCRSName = "urn:ogc:def:crs:EPSG::28992"

// featureCollection mirrors the GeoJSON FeatureCollection shape spike-d's harvest.py produced
// (geojson_polygons), with an explicit RD crs member.
type featureCollection struct {
	Type     string         `json:"type"`
	CRS      crsField       `json:"crs"`
	Features []featureEntry `json:"features"`
}

type crsField struct {
	Type       string        `json:"type"`
	Properties crsProperties `json:"properties"`
}

type crsProperties struct {
	Name string `json:"name"`
}

type featureEntry struct {
	Type       string         `json:"type"`
	Geometry   any            `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

// BuildFeatureCollection assembles an RD GeoJSON FeatureCollection from paged Datapunt records,
// keeping only current (eindGeldigheid null) records with geometry and carrying
// identificatie (= gbdBuurtId) / naam / code / cbsCode / ligtInWijkId. Pure — takes
// already-fetched records; does no filtering by area, since the whole-city harvest carries
// every buurt/wijk the API returns (no stadsdeel scoping).
func BuildFeatureCollection(records []map[string]any) ([]byte, error) {
	features := make([]featureEntry, 0, len(records))
	for _, rec := range records {
		if rec["eindGeldigheid"] != nil {
			continue
		}
		geom := rec["geometrie"]
		if geom == nil {
			continue
		}
		features = append(features, featureEntry{
			Type:     "Feature",
			Geometry: geom,
			Properties: map[string]any{
				"identificatie": rec["identificatie"],
				"naam":          rec["naam"],
				"code":          rec["code"],
				"cbsCode":       rec["cbsCode"],
				"ligtInWijkId":  rec["ligtInWijkId"],
			},
		})
	}

	fc := featureCollection{
		Type:     "FeatureCollection",
		CRS:      crsField{Type: "name", Properties: crsProperties{Name: rdCRSName}},
		Features: features,
	}
	return json.Marshal(fc)
}
