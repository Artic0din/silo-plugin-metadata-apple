package main

import (
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed storefronts.json
var storefrontJSON []byte

type storefront struct{ ID, Name, Country, Locale string }

func loadStorefronts() []storefront {
	var rows [][4]string
	if err := json.Unmarshal(storefrontJSON, &rows); err != nil {
		panic(err)
	}
	result := make([]storefront, 0, len(rows))
	for _, row := range rows {
		result = append(result, storefront{row[0], row[1], row[2], row[3]})
	}
	return result
}

var storefronts = loadStorefronts()

func countryStorefront(country string) (storefront, bool) {
	for _, region := range storefronts {
		if strings.EqualFold(region.Country, country) {
			return region, true
		}
	}
	return storefront{}, false
}

func languageStorefront(language, englishCountry string) (storefront, error) {
	language = strings.ToLower(strings.Split(strings.ReplaceAll(language, "_", "-"), "-")[0])
	if language == "" {
		language = "en"
	}
	country := map[string]string{"en": englishCountry, "es": "mx", "de": "de", "fr": "fr", "pt": "br", "yue": "hk"}[language]
	if country != "" {
		region, _ := countryStorefront(country)
		return region, nil
	}
	for _, region := range storefronts {
		if language == imageLanguage(region) {
			return region, nil
		}
	}
	return storefront{}, invalidArgument("No Apple TV storefront is configured for this language.")
}

func imageLanguage(region storefront) string {
	return strings.ToLower(strings.Split(region.Locale, "-")[0])
}

func languageStorefronts(preferred storefront) []storefront {
	regions := []storefront{preferred}
	if baseline, ok := countryStorefront("us"); ok && baseline.ID != preferred.ID && imageLanguage(baseline) == imageLanguage(preferred) {
		regions = append(regions, baseline)
	}
	for _, region := range storefronts {
		if region.ID != preferred.ID && region.Country != "us" && imageLanguage(region) == imageLanguage(preferred) {
			regions = append(regions, region)
		}
	}
	return regions
}
