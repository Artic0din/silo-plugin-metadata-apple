package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func testStorefrontRegistry(t *testing.T) {
	t.Helper()
	before := storefronts
	storefronts = nil
	for _, country := range []string{"au", "us", "gb", "fr"} {
		for _, region := range before {
			if region.Country == country {
				storefronts = append(storefronts, region)
			}
		}
	}
	t.Cleanup(func() { storefronts = before })
}

func TestMergeAllSameLanguageStorefronts(t *testing.T) {
	testStorefrontRegistry(t)
	for _, mode := range []string{"additional", "preferred unavailable", "upstream failure", "season"} {
		t.Run(mode, func(t *testing.T) {
			var mu sync.Mutex
			visited := make(map[string]bool)
			seasonCalls := make(map[string]int)
			client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/sparql" {
					fmt.Fprint(w, `{"results":{"bindings":[{"item":{"value":"Q1"},"apple":{"value":"umc.cmc.show"}}]}}`)
					return
				}
				if r.URL.Path == "/uts/v3/configurations" {
					configuration(w, r)
					return
				}
				region := r.URL.Query().Get("sf")
				mu.Lock()
				visited[region] = true
				mu.Unlock()
				au, _ := countryStorefront("au")
				gb, _ := countryStorefront("gb")
				if region == au.ID && mode == "preferred unavailable" {
					http.NotFound(w, r)
					return
				}
				if region == gb.ID && mode == "upstream failure" {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				images := map[string]appleImage{"posterArt": testImage("shared", "", 2000, 3000)}
				if region == gb.ID {
					images["posterArt"] = testImage("gb-only", "", 2000, 3000)
				}
				if strings.Contains(r.URL.Path, "/seasons/") {
					mu.Lock()
					seasonCalls[region]++
					mu.Unlock()
					writeJSON(t, w, struct{ Data appleSeason }{appleSeason{ID: "umc.cmc.season", ShowID: "umc.cmc.show", Number: new(int32(1)), Images: map[string]appleImage{"coverArt2X3": images["posterArt"]}}})
					return
				}
				detail := appleDetail{Content: &appleContent{ID: "umc.cmc.show", Type: "Show", Images: images}, Seasons: map[string]appleSeason{"umc.cmc.season": {ID: "umc.cmc.season", ShowID: "umc.cmc.show", Number: new(int32(1))}}}
				if mode == "season" {
					detail.Content.Countries = append(detail.Content.Countries, struct {
						Code string `json:"countryCode"`
					}{"us"})
				}
				writeJSON(t, w, struct{ Data appleDetail }{detail})
			})
			server := &artworkServer{client: client, englishCountry: "au"}
			req := &pluginv1.GetImagesRequest{ItemType: "series", Language: "en", ProviderIds: &structpb.Struct{Fields: map[string]*structpb.Value{"tmdb": structpb.NewStringValue("42")}}}
			if mode == "season" {
				req.SeasonNumber = new(int32(1))
			}
			result, err := server.GetImages(context.Background(), req)
			if mode == "upstream failure" {
				if status.Code(err) != codes.Unavailable {
					t.Fatalf("upstream error lost: %v", err)
				}
				return
			}
			if err != nil || len(result.Images) != 2 {
				t.Fatalf("combined unique images: %v, %v", result, err)
			}
			for _, country := range []string{"au", "us", "gb"} {
				region, _ := countryStorefront(country)
				if !visited[region.ID] {
					t.Errorf("did not query %s", country)
				}
			}
			fr, _ := countryStorefront("fr")
			if visited[fr.ID] {
				t.Fatal("queried a different language")
			}
			for _, image := range result.Images {
				if image.Language != "en" || (mode == "season" && image.GetSeasonNumber() != 1) {
					t.Fatalf("classification or season lost: %v", image)
				}
			}
			if mode == "season" {
				for region, calls := range seasonCalls {
					if calls != 1 {
						t.Errorf("season %s fetched %d times", region, calls)
					}
				}
			}
		})
	}
}

func TestUnavailableStorefrontConfiguration(t *testing.T) {
	for _, body := range []string{`400:Invalid request`, `{"message":"Feature enabler CountryExpansion2026 is not enabled"}`, `{"message":"China is not a supported storefront"}`} {
		t.Run(body, func(t *testing.T) {
			client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, body)
			})
			region, _ := countryStorefront("au")
			if _, err := client.params(context.Background(), region); err != errStorefront {
				t.Fatalf("unavailable storefront: %v", err)
			}
		})
	}
}

func TestImageDeduplicationPreservesChoices(t *testing.T) {
	images := map[string]appleImage{"posterArt": testImage("shared", "SC", 2000, 3000)}
	region, _ := countryStorefront("au")
	result, err := mapImages(images, images, images, region, region, nil)
	if err != nil {
		t.Fatal(err)
	}
	image := result.Images[0]
	duplicate := proto.Clone(image).(*pluginv1.ImageRecord)
	duplicate.Url = strings.Replace(image.Url, "is1-ssl", "is2-ssl", 1)
	if imageKey(image) != imageKey(duplicate) {
		t.Fatal("CDN node created a duplicate choice")
	}
	for _, change := range []func(*pluginv1.ImageRecord){
		func(i *pluginv1.ImageRecord) { i.Url = strings.Replace(i.Url, "SC", "PO", 1) },
		func(i *pluginv1.ImageRecord) { i.Language = "fr" },
		func(i *pluginv1.ImageRecord) { i.Kind = "backdrop" },
	} {
		variant := proto.Clone(image).(*pluginv1.ImageRecord)
		change(variant)
		if imageKey(image) == imageKey(variant) {
			t.Fatal("distinct choice discarded")
		}
	}
}

func TestSameLanguageGroups(t *testing.T) {
	for _, country := range []string{"au", "mx", "de", "br", "hk", "fr"} {
		preferred, _ := countryStorefront(country)
		regions := languageStorefronts(preferred)
		seen := make(map[string]bool)
		for _, region := range regions {
			if seen[region.ID] || imageLanguage(region) != imageLanguage(preferred) {
				t.Fatalf("invalid group for %s: %v", country, region)
			}
			seen[region.ID] = true
		}
		for _, region := range storefronts {
			if imageLanguage(region) == imageLanguage(preferred) && !seen[region.ID] {
				t.Fatalf("missing %s from %s", region.Country, country)
			}
		}
	}
}

func TestThirdStorefrontTitleDiscovery(t *testing.T) {
	testStorefrontRegistry(t)
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sparql":
			fmt.Fprint(w, `{"results":{"bindings":[{"item":{"value":"Q1"},"itemLabel":{"value":"Classic"},"date":{"value":"2000-01-01T00:00:00Z"}}]}}`)
		case "/uts/v3/configurations":
			configuration(w, r)
		case "/uts/v3/search":
			gb, _ := countryStorefront("gb")
			items := `[]`
			if r.URL.Query().Get("sf") == gb.ID {
				items = `[{"id":"umc.cmc.classic","type":"Movie","title":"Classic"}]`
			}
			fmt.Fprintf(w, `{"data":{"canvas":{"shelves":[{"items":%s}]}}}`, items)
		default:
			writeJSON(t, w, struct{ Data appleDetail }{appleDetail{Content: &appleContent{ID: "umc.cmc.classic", Type: "Movie", Title: "Classic", ReleaseDate: new(int64(946684800000))}}})
		}
	})
	region, _ := countryStorefront("au")
	id, err := client.identify(context.Background(), "42", "movie", region)
	if err != nil || id != "umc.cmc.classic" {
		t.Fatalf("third-storefront identity: %q %v", id, err)
	}
}
