package main

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestManifestCatalogContract(t *testing.T) {
	value, err := manifest.Load(manifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.ValidateCatalogPresentation(value, "https://github.com/Artic0din/silo-plugin-metadata-apple"); err != nil {
		t.Fatal(err)
	}
}

func TestConfigureInvalidCountryAndUnknownSetting(t *testing.T) {
	server := &artworkServer{}
	for _, values := range []map[string]interface{}{{"english_country": "zz"}, {"unexpected": "value"}} {
		value, err := structpb.NewStruct(values)
		if err != nil {
			t.Fatal(err)
		}
		if err := server.configure(context.Background(), []*pluginv1.ConfigEntry{{Key: "artwork", Value: value}}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("accepted %v: %v", values, err)
		}
	}
	value, _ := structpb.NewStruct(map[string]interface{}{"english_country": "US"})
	if err := server.configure(context.Background(), []*pluginv1.ConfigEntry{{Key: "artwork", Value: value}}); err != nil || server.englishCountry != "us" {
		t.Fatalf("country setting: %s %v", server.englishCountry, err)
	}
}

func TestConnectionRequiresAppleAndWikidata(t *testing.T) {
	for _, test := range []struct {
		name           string
		appleStatus    int
		wikidataStatus int
		body           string
		code           codes.Code
	}{
		{"healthy", 200, 200, `{"boolean":true}`, codes.OK},
		{"Apple unavailable", 503, 200, `{"boolean":true}`, codes.Unavailable},
		{"Wikidata blocked", 200, 403, `{}`, codes.Unavailable},
		{"Wikidata rate limited", 200, 429, `{}`, codes.ResourceExhausted},
		{"Wikidata malformed", 200, 200, `not json`, codes.DataLoss},
		{"Wikidata missing result", 200, 200, `{}`, codes.DataLoss},
		{"Wikidata missing mappings", 200, 200, `{"boolean":false}`, codes.DataLoss},
	} {
		t.Run(test.name, func(t *testing.T) {
			wikidataCalls := 0
			client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/uts/v3/configurations":
					w.WriteHeader(test.appleStatus)
					configuration(w, r)
				case "/sparql":
					wikidataCalls++
					if r.URL.Query().Get("query") != "ASK { ?item wdt:P4947 ?id }" || r.URL.Query().Get("format") != "json" {
						t.Error("connection test did not query Wikidata's TMDB mappings")
					}
					w.WriteHeader(test.wikidataStatus)
					fmt.Fprint(w, test.body)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			server := &artworkServer{client: client, englishCountry: "au"}
			result, err := server.Search(context.Background(), &pluginv1.SearchMetadataRequest{})
			if status.Code(err) != test.code {
				t.Fatalf("connection test: %v, want %s", err, test.code)
			}
			if test.appleStatus == 200 && wikidataCalls != 1 {
				t.Fatal("connection test skipped Wikidata")
			}
			if err == nil && len(result.GetResults()) != 0 {
				t.Fatal("connection test returned metadata matches")
			}
		})
	}
}
