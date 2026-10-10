package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func fakeClient(t *testing.T, handle http.HandlerFunc) *appleClient {
	t.Helper()
	server := httptest.NewServer(handle)
	t.Cleanup(server.Close)
	client := newClient()
	client.http.Transport = server.Client().Transport
	client.appleURL = server.URL + "/uts/v3"
	client.wikidataURL = server.URL + "/sparql"
	return client
}

func writeJSON(t *testing.T, w http.ResponseWriter, value interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Error(err)
	}
}

func configuration(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `{"data":{"applicationProps":{"requiredParamsMap":{"Default":{"pfm":"web","utsk":"public-test-key"}},"storefront":{"storefrontId":%q}}}}`, r.URL.Query().Get("sf"))
}

func TestRPCThroughMappedWikidataAndSeason(t *testing.T) {
	const id = "umc.cmc.testshow"
	calls := 0
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sparql" {
			if !strings.Contains(r.URL.Query().Get("query"), `wdt:P4983 "42"`) {
				t.Error("wrong Wikidata property or ID")
			}
			fmt.Fprintf(w, `{"results":{"bindings":[{"item":{"value":"Q1"},"apple":{"value":%q}},{"item":{"value":"Q1"},"apple":{"value":%q}}]}}`, id, id)
			return
		}
		if r.URL.Path == "/uts/v3/configurations" {
			configuration(w, r)
			return
		}
		if r.URL.Query().Get("pfm") != "ipad" {
			t.Error("third-party catalogue platform lost")
		}
		switch r.URL.Path {
		case "/uts/v3/shows/" + id:
			calls++
			detail := appleDetail{Content: &appleContent{ID: id, Type: "Show", Images: map[string]appleImage{"posterArt": testImage("show", "", 2000, 3000)}}, Seasons: map[string]appleSeason{"umc.cmc.season": {ID: "umc.cmc.season", ShowID: id, Number: new(int32)}}}
			writeJSON(t, w, struct{ Data appleDetail }{detail})
		case "/uts/v3/seasons/umc.cmc.season/metadata":
			writeJSON(t, w, struct{ Data appleSeason }{appleSeason{ID: "umc.cmc.season", ShowID: id, Number: new(int32), Images: map[string]appleImage{"coverArt2X3": testImage("special", "", 2000, 3000)}}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	server := &artworkServer{client: client, englishCountry: "au"}
	ids := &structpb.Struct{Fields: map[string]*structpb.Value{"tmdb": structpb.NewStringValue("42")}}
	result, err := server.GetImages(context.Background(), &pluginv1.GetImagesRequest{ItemType: "series", ProviderIds: ids, SeasonNumber: new(int32)})
	if err != nil || len(result.Images) != 1 || result.Images[0].SeasonNumber == nil {
		t.Fatalf("RPC: %v %v", result, err)
	}
	if calls != 1 {
		t.Fatalf("unexpected title fetch count: %d", calls)
	}
}

func TestWikidataFallbackRequiresExactTypeTitleAndYear(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sparql":
			fmt.Fprint(w, `{"results":{"bindings":[{"item":{"value":"Q1"},"itemLabel":{"value":"Andor"},"date":{"value":"2022-09-21T00:00:00Z"}}]}}`)
		case "/uts/v3/configurations":
			configuration(w, r)
		case "/uts/v3/search":
			if r.URL.Query().Get("searchTerm") != "Andor" {
				t.Error("wrong fallback title")
			}
			fmt.Fprint(w, `{"data":{"canvas":{"shelves":[{"items":[{"id":"umc.cmc.movie","type":"Movie","title":"Andor"},{"id":"umc.cmc.correct","type":"Show","title":"Andor"},{"id":"umc.cmc.wrongyear","type":"Show","title":"Andor"},{"id":"umc.cmc.partial","type":"Show","title":"Andor bonus"}]}]}}}`)
		case "/uts/v3/shows/umc.cmc.correct":
			fmt.Fprint(w, `{"data":{"content":{"id":"umc.cmc.correct","type":"Show","title":"Andor","releaseDate":1663718400000}}}`)
		case "/uts/v3/shows/umc.cmc.wrongyear":
			fmt.Fprint(w, `{"data":{"content":{"id":"umc.cmc.wrongyear","type":"Show","title":"Andor","releaseDate":164099520000}}}`)
		default:
			t.Errorf("unexpected lookup %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	au, _ := countryStorefront("au")
	id, err := client.identify(context.Background(), "83867", "series", au)
	if err != nil || id != "umc.cmc.correct" {
		t.Fatalf("fallback matched %q: %v", id, err)
	}
}

func TestWikidataMissingAmbiguousAndMalformed(t *testing.T) {
	for _, test := range []struct {
		body string
		code codes.Code
	}{
		{`{"results":{"bindings":[]}}`, codes.OK},
		{`{"results":{}}`, codes.DataLoss},
		{`{"results":{"bindings":[{"item":{"value":"Q1"},"apple":{"value":"umc.cmc.a"}},{"item":{"value":"Q2"},"apple":{"value":"umc.cmc.b"}}]}}`, codes.DataLoss},
		{`{"results":{"bindings":[{"item":{"value":"Q1"},"apple":{"value":"../../bad"}}]}}`, codes.DataLoss},
		{`{"results":{"bindings":[{"item":{"value":"Q1"},"apple":{"value":"umc.cmc.a"}},{"item":{"value":"Q1"},"apple":{"value":"umc.cmc.b"}}]}}`, codes.DataLoss},
	} {
		client := fakeClient(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, test.body) })
		au, _ := countryStorefront("au")
		_, err := client.identify(context.Background(), "1", "movie", au)
		if status.Code(err) != test.code {
			t.Fatalf("%s: %v", test.body, err)
		}
	}
}

func TestUpstreamFailuresAreNotEmptySuccess(t *testing.T) {
	for _, test := range []struct {
		httpCode int
		body     string
		code     codes.Code
	}{
		{200, `not json`, codes.DataLoss}, {200, `{}`, codes.DataLoss}, {429, `{}`, codes.ResourceExhausted}, {500, `{}`, codes.Unavailable}, {400, `{"message":"bad request"}`, codes.Unavailable},
	} {
		client := fakeClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(test.httpCode); fmt.Fprint(w, test.body) })
		au, _ := countryStorefront("au")
		_, err := client.params(context.Background(), au)
		if status.Code(err) != test.code {
			t.Fatalf("HTTP %d: %v", test.httpCode, err)
		}
	}
	client := fakeClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://evil.example")
		w.WriteHeader(302)
	})
	if err := client.get(context.Background(), client.wikidataURL, url.Values{}, &struct{}{}); status.Code(err) != codes.Unavailable {
		t.Fatal("redirect followed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.get(ctx, client.wikidataURL, url.Values{}, &struct{}{}); status.Code(err) != codes.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestDetailAndSeasonIdentityValidation(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/uts/v3/configurations" {
			configuration(w, r)
			return
		}
		fmt.Fprint(w, `{"data":{"content":{"id":"umc.cmc.other","type":"Movie"},"id":"umc.cmc.other","showId":"umc.cmc.other","seasonNumber":1}}`)
	})
	au, _ := countryStorefront("au")
	if _, err := client.detail(context.Background(), "umc.cmc.expected", "movie", au); status.Code(err) != codes.DataLoss {
		t.Fatal("wrong title accepted")
	}
	number := int32(1)
	detail := appleDetail{Content: &appleContent{ID: "umc.cmc.show"}, Seasons: map[string]appleSeason{"umc.cmc.season": {ID: "umc.cmc.season", ShowID: "umc.cmc.show", Number: &number}}}
	if _, err := client.season(context.Background(), detail, 1, au); status.Code(err) != codes.DataLoss {
		t.Fatal("wrong season accepted")
	}
	if images, err := client.season(context.Background(), detail, 0, au); err != nil || len(images) != 0 {
		t.Fatal("missing Specials should be empty")
	}
}

func TestNumericAppleStorefrontID(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":{"applicationProps":{"requiredParamsMap":{"Default":{"pfm":"ipad"}},"storefront":{"storefrontId":143460,"defaultLocale":"en_AU"}}}}`)
	})
	au, _ := countryStorefront("au")
	params, err := client.params(context.Background(), au)
	if err != nil || params.Get("locale") != "en-AU" {
		t.Fatalf("numeric storefront contract: %v %v", params, err)
	}
}

func TestSeriesFallbackQueriesWikidataPremiere(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("query"), "P580") {
			t.Error("series lookup omitted Wikidata start time P580")
		}
		fmt.Fprint(w, `{"results":{"bindings":[]}}`)
	})
	au, _ := countryStorefront("au")
	if _, err := client.identify(context.Background(), "83867", "series", au); err != nil {
		t.Fatal(err)
	}
}

func TestFallbackReleaseDatePresenceAndPreEpoch(t *testing.T) {
	for _, test := range []struct {
		name, date string
		year       int
		want       string
	}{
		{"pre-1970", `,"releaseDate":-315619200000`, 1960, "umc.cmc.classic"},
		{"epoch", `,"releaseDate":0`, 1970, "umc.cmc.classic"},
		{"missing", "", 1970, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/uts/v3/configurations":
					configuration(w, r)
				case "/uts/v3/search":
					fmt.Fprint(w, `{"data":{"canvas":{"shelves":[{"items":[{"id":"umc.cmc.classic","type":"Movie","title":"Classic"}]}]}}}`)
				default:
					fmt.Fprintf(w, `{"data":{"content":{"id":"umc.cmc.classic","type":"Movie","title":"Classic"%s}}}`, test.date)
				}
			})
			au, _ := countryStorefront("au")
			id, err := client.search(context.Background(), "Classic", "movie", map[int]bool{test.year: true}, au)
			if err != nil || id != test.want {
				t.Fatalf("matched %q, want %q: %v", id, test.want, err)
			}
		})
	}
}

func TestIdentificationFallsBackWhenRegionalSearchHasNoMatch(t *testing.T) {
	searched := []string{}
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sparql":
			fmt.Fprint(w, `{"results":{"bindings":[{"item":{"value":"Q1"},"itemLabel":{"value":"Classic"},"date":{"value":"1960-01-01T00:00:00Z"}}]}}`)
		case "/uts/v3/configurations":
			configuration(w, r)
		case "/uts/v3/search":
			region := r.URL.Query().Get("sf")
			searched = append(searched, region)
			if region == "143441" {
				fmt.Fprint(w, `{"data":{"canvas":{"shelves":[{"items":[{"id":"umc.cmc.classic","type":"Movie","title":"Classic"}]}]}}}`)
			} else {
				fmt.Fprint(w, `{"data":{"canvas":{"shelves":[]}}}`)
			}
		default:
			fmt.Fprint(w, `{"data":{"content":{"id":"umc.cmc.classic","type":"Movie","title":"Classic","releaseDate":-315619200000}}}`)
		}
	})
	au, _ := countryStorefront("au")
	id, err := client.identify(context.Background(), "1", "movie", au)
	if err != nil || id != "umc.cmc.classic" || len(searched) != 2 || searched[0] != "143460" || searched[1] != "143441" {
		t.Fatalf("id=%q storefronts=%v err=%v", id, searched, err)
	}
}

func TestSearchSkipsUnavailableCandidates(t *testing.T) {
	for _, test := range []struct {
		name   string
		valid  bool
		status int
		want   string
		code   codes.Code
	}{
		{"all unavailable", false, http.StatusNotFound, "", codes.OK},
		{"valid alongside unavailable", true, http.StatusNotFound, "umc.cmc.valid", codes.OK},
		{"upstream failure", false, http.StatusInternalServerError, "", codes.Unavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/uts/v3/configurations":
					configuration(w, r)
				case "/uts/v3/search":
					items := []appleContent{{ID: "umc.cmc.stale", Type: "Movie", Title: "Classic"}}
					if test.valid {
						items = append(items, appleContent{ID: "umc.cmc.valid", Type: "Movie", Title: "Classic"})
					}
					writeJSON(t, w, map[string]interface{}{"data": map[string]interface{}{"canvas": map[string]interface{}{"shelves": []interface{}{map[string]interface{}{"items": items}}}}})
				case "/uts/v3/movies/umc.cmc.valid":
					fmt.Fprint(w, `{"data":{"content":{"id":"umc.cmc.valid","type":"Movie","title":"Classic","releaseDate":0}}}`)
				default:
					w.WriteHeader(test.status)
				}
			})
			au, _ := countryStorefront("au")
			id, err := client.search(context.Background(), "Classic", "movie", map[int]bool{1970: true}, au)
			if id != test.want || status.Code(err) != test.code {
				t.Fatalf("id=%q err=%v", id, err)
			}
		})
	}
}
