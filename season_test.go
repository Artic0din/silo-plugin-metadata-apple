package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestSeasonSummaryArtworkWhenMetadataUnavailable(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		withImages bool
		code       codes.Code
	}{
		{"summary poster survives 404", 404, true, codes.OK},
		{"missing summary images", 404, false, codes.OK},
		{"metadata failure propagates", 500, true, codes.Unavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			number := int32(1)
			client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/sparql":
					fmt.Fprint(w, `{"results":{"bindings":[{"item":{"value":"Q1"},"apple":{"value":"umc.cmc.show"}}]}}`)
				case "/uts/v3/configurations":
					configuration(w, r)
				case "/uts/v3/shows/umc.cmc.show":
					season := appleSeason{ID: "umc.cmc.season", ShowID: "umc.cmc.show", Number: &number}
					if test.withImages {
						season.Images = map[string]appleImage{"coverArt2X3": testImage("summary", "", 2000, 3000)}
					}
					writeJSON(t, w, struct{ Data appleDetail }{appleDetail{Content: &appleContent{ID: "umc.cmc.show", Type: "Show"}, Seasons: map[string]appleSeason{season.ID: season}}})
				case "/uts/v3/seasons/umc.cmc.season/metadata":
					w.WriteHeader(test.status)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			server := &artworkServer{client: client, englishCountry: "au"}
			ids := &structpb.Struct{Fields: map[string]*structpb.Value{"tmdb": structpb.NewStringValue("42")}}
			result, err := server.GetImages(context.Background(), &pluginv1.GetImagesRequest{ItemType: "series", ProviderIds: ids, SeasonNumber: &number})
			if status.Code(err) != test.code {
				t.Fatalf("season lookup: %v, want %s", err, test.code)
			}
			if err != nil {
				return
			}
			if !test.withImages {
				if len(result.Images) != 0 {
					t.Fatal("missing images produced artwork")
				}
				return
			}
			if len(result.Images) != 1 || result.Images[0].GetSeasonNumber() != number || result.Images[0].Kind != "poster" {
				t.Fatalf("exact-season poster discarded: %v", result)
			}
			resolved, err := resolveImage(result.Images[0].Url, "full")
			if err != nil || !strings.Contains(resolved, "/summary/") {
				t.Fatalf("summary artwork not preserved: %s %v", resolved, err)
			}
		})
	}
}
