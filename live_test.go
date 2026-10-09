package main

import (
	"context"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestLiveArtwork(t *testing.T) {
	if os.Getenv("APPLE_LIVE_TEST") != "1" {
		t.Skip("Set APPLE_LIVE_TEST=1 to query public Apple and Wikidata services.")
	}
	directory, err := os.MkdirTemp("", "apple-artwork-live-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Public artwork evidence: %s", directory)
	server := &artworkServer{client: newClient(), englishCountry: "au"}
	season := int32(1)
	for _, test := range []struct {
		name, id, kind, language string
		season                   *int32
	}{
		{"Martian", "286217", "movie", "en", nil},
		{"Severance", "95396", "series", "fr", nil},
		{"LastOfUs", "100088", "series", "en", nil},
		{"Andor", "83867", "series", "en", nil},
		{"SeveranceSeason1", "95396", "series", "en", &season},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			ids := &structpb.Struct{Fields: map[string]*structpb.Value{"tmdb": structpb.NewStringValue(test.id)}}
			result, err := server.GetImages(ctx, &pluginv1.GetImagesRequest{ItemType: test.kind, ProviderIds: ids, Language: test.language, SeasonNumber: test.season})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Images) == 0 {
				t.Fatal("No live artwork returned for the acceptance title.")
			}
			t.Logf("%d images", len(result.Images))
			for _, record := range result.Images {
				if test.season != nil && (record.Kind != "poster" || record.SeasonNumber == nil || *record.SeasonNumber != *test.season) {
					t.Fatal("season scope lost")
				}
				rawURL, err := resolveImage(record.Url, "card")
				if err != nil {
					t.Fatal(err)
				}
				request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
				if err != nil {
					t.Fatal(err)
				}
				response, err := server.client.http.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != 200 {
					failure, _ := io.ReadAll(io.LimitReader(response.Body, 500))
					response.Body.Close()
					t.Fatalf("image HTTP %d at %s: %s", response.StatusCode, rawURL, failure)
				}
				body, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
				response.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				name := record.Metadata.Fields["apple_field"].GetStringValue()
				file := filepath.Join(directory, test.name+"-"+name+".image")
				if err := os.WriteFile(file, body, 0600); err != nil {
					t.Fatal(err)
				}
				input, err := os.Open(file)
				if err != nil {
					t.Fatal(err)
				}
				config, format, err := image.DecodeConfig(input)
				input.Close()
				if err != nil {
					t.Fatal(err)
				}
				if record.Kind == "poster" && config.Width*3 != config.Height*2 {
					t.Fatalf("poster crop returned %dx%d", config.Width, config.Height)
				}
				// Thumbnail dimensions round to whole pixels; allow one pixel of ratio error.
				if record.Kind == "backdrop" && abs(config.Width*9-config.Height*16) > 16 {
					t.Fatalf("backdrop crop returned %dx%d", config.Width, config.Height)
				}
				if record.Kind == "logo" && format != "png" {
					t.Fatalf("logo format %s", format)
				}
				t.Logf("%s: %dx%d %s language=%q", name, config.Width, config.Height, format, record.Language)
			}
		})
	}
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
