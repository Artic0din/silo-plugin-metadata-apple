package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func yearOf(milliseconds int64) int { return time.UnixMilli(milliseconds).UTC().Year() }

func (c *appleClient) search(ctx context.Context, title, kind string, years map[int]bool, region storefront) (string, error) {
	params, err := c.params(ctx, region)
	if err != nil {
		return "", err
	}
	params.Set("searchTerm", title)
	var response struct {
		Data struct {
			Canvas *struct {
				Shelves []struct{ Items []appleContent }
			}
		}
	}
	if err := c.get(ctx, c.appleURL+"/search", params, &response); err != nil {
		return "", err
	}
	if response.Data.Canvas == nil || response.Data.Canvas.Shelves == nil {
		return "", invalidData("Apple search returned no shelves.")
	}
	expected := "Movie"
	if kind == "series" {
		expected = "Show"
	}
	candidates := make(map[string]bool)
	for _, shelf := range response.Data.Canvas.Shelves {
		for _, item := range shelf.Items {
			if item.Type != expected || !strings.EqualFold(strings.TrimSpace(item.Title), title) {
				continue
			}
			if !appleIDPattern.MatchString(item.ID) {
				return "", invalidData("Apple search returned an invalid title ID.")
			}
			candidates[item.ID] = true
		}
	}
	match := ""
	for id := range candidates {
		detail, err := c.detail(ctx, id, kind, region)
		if errors.Is(err, errNoTitle) {
			continue
		}
		if err != nil {
			return "", err
		}
		if detail.Content.ReleaseDate == nil || !years[yearOf(*detail.Content.ReleaseDate)] || !strings.EqualFold(strings.TrimSpace(detail.Content.Title), title) {
			continue
		}
		if match != "" {
			return "", invalidData("Apple title matching is ambiguous.")
		}
		match = id
	}
	return match, nil
}

func (c *appleClient) identify(ctx context.Context, tmdbID, kind string, region storefront) (string, error) {
	tmdbProperty, appleProperty := "P4947", "P9586"
	dateProperty := "wdt:P577"
	if kind == "series" {
		tmdbProperty, appleProperty = "P4983", "P9751"
		dateProperty = "(wdt:P577|wdt:P580)"
	}
	query := fmt.Sprintf(`SELECT ?item ?apple ?date ?itemLabel WHERE { ?item wdt:%s "%s". OPTIONAL { ?item wdt:%s ?apple } OPTIONAL { ?item %s ?date } SERVICE wikibase:label { bd:serviceParam wikibase:language "en". } }`, tmdbProperty, tmdbID, appleProperty, dateProperty)
	type bindingValue struct{ Value string }
	var response struct {
		Results *struct {
			Bindings []struct{ Item, Apple, Date, ItemLabel bindingValue }
		}
	}
	if err := c.get(ctx, c.wikidataURL, url.Values{"query": {query}, "format": {"json"}}, &response); err != nil {
		return "", err
	}
	if response.Results == nil || response.Results.Bindings == nil {
		return "", invalidData("Wikidata returned no result bindings.")
	}
	entity, appleID, title := "", "", ""
	years := make(map[int]bool)
	for _, row := range response.Results.Bindings {
		if row.Item.Value == "" || (entity != "" && entity != row.Item.Value) {
			return "", invalidData("Wikidata title matching is ambiguous or has no identity.")
		}
		entity, title = row.Item.Value, strings.TrimSpace(row.ItemLabel.Value)
		if row.Apple.Value != "" {
			if !appleIDPattern.MatchString(row.Apple.Value) || (appleID != "" && appleID != row.Apple.Value) {
				return "", invalidData("Wikidata returned invalid or conflicting Apple IDs.")
			}
			appleID = row.Apple.Value
		}
		if row.Date.Value != "" {
			date, err := time.Parse(time.RFC3339, row.Date.Value)
			if err != nil {
				return "", invalidData("Wikidata returned an invalid release date.")
			}
			years[date.Year()] = true
		}
	}
	if appleID != "" {
		return appleID, nil
	}
	if entity == "" || title == "" || len(years) == 0 {
		return "", nil
	}
	id, err := c.search(ctx, title, kind, years, region)
	if err != nil || id != "" || region.Country == "us" {
		return id, err
	}
	baseline, _ := countryStorefront("us")
	return c.search(ctx, title, kind, years, baseline)
}
