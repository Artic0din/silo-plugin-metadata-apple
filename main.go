package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/url"
	"strings"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtime"
)

//go:embed manifest.json
var manifestJSON []byte

var version = "0.1.0"

type artworkServer struct {
	pluginv1.UnimplementedMetadataProviderServer
	pluginv1.UnimplementedImageResolverServer
	client         *appleClient
	englishCountry string
}

func (s *artworkServer) configure(_ context.Context, entries []*pluginv1.ConfigEntry) error {
	country := "au"
	for _, entry := range entries {
		if entry.GetKey() != "artwork" {
			continue
		}
		data, err := json.Marshal(entry.GetValue().AsMap())
		if err != nil {
			return invalidArgument("Invalid Apple artwork settings.")
		}
		var config struct {
			Country string `json:"english_country"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&config); err != nil {
			return invalidArgument("Invalid Apple artwork settings.")
		}
		if config.Country != "" {
			country = strings.ToLower(strings.TrimSpace(config.Country))
		}
	}
	if _, ok := countryStorefront(country); !ok {
		return invalidArgument("English artwork country must be a country code from the storefront registry.")
	}
	// Silo configures before capability calls and restarts on settings changes.
	s.englishCountry = country
	return nil
}

func (s *artworkServer) Search(ctx context.Context, _ *pluginv1.SearchMetadataRequest) (*pluginv1.SearchMetadataResponse, error) {
	region, err := languageStorefront("en", s.englishCountry)
	if err != nil {
		return nil, err
	}
	// Silo calls Search to test the connection; artwork must not identify or refresh titles.
	if _, err := s.client.params(ctx, region); err != nil {
		return nil, err
	}
	var response struct {
		Boolean *bool `json:"boolean"`
	}
	if err := s.client.get(ctx, s.client.wikidataURL, url.Values{"query": {"ASK { ?item wdt:P4947 ?id }"}, "format": {"json"}}, &response); err != nil {
		return nil, err
	}
	if response.Boolean == nil || !*response.Boolean {
		return nil, invalidData("Wikidata returned no TMDB mappings.")
	}
	return &pluginv1.SearchMetadataResponse{}, nil
}

func main() {
	server := &artworkServer{client: newClient(), englishCountry: "au"}
	runtime.ServeManifestWithOptions(manifestJSON, version, runtime.CapabilityServers{
		MetadataProvider: server, ImageResolver: server,
	}, runtime.WithConfigure(server.configure))
}
