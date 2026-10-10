package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/url"
	"strings"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestEnglishCountryRequiresEnglishLocale(t *testing.T) {
	for _, country := range []string{"au", "us", "gb"} {
		server := &artworkServer{}
		value, _ := structpb.NewStruct(map[string]interface{}{"english_country": country})
		if err := server.configure(context.Background(), []*pluginv1.ConfigEntry{{Key: "artwork", Value: value}}); err != nil || server.englishCountry != country {
			t.Fatalf("English country %s rejected: %v", country, err)
		}
	}
	for _, country := range []string{"fr", "jp"} {
		server := &artworkServer{englishCountry: "au"}
		value, _ := structpb.NewStruct(map[string]interface{}{"english_country": country})
		if err := server.configure(context.Background(), []*pluginv1.ConfigEntry{{Key: "artwork", Value: value}}); status.Code(err) != codes.InvalidArgument {
			t.Errorf("non-English country %s accepted: %v", country, err)
		}
		if server.englishCountry != "au" {
			t.Errorf("invalid settings changed the country to %s", server.englishCountry)
		}
	}
}

func Test404OnlyMeansMissingTitleForDetailEndpoints(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	for _, path := range []string{"/configurations", "/search", "/movies/umc.cmc.movie", "/shows/umc.cmc.show", "/seasons/umc.cmc.season/metadata"} {
		err := client.get(context.Background(), client.appleURL+path, url.Values{}, &struct{}{})
		if strings.HasPrefix(path, "/movies/") || strings.HasPrefix(path, "/shows/") || strings.HasPrefix(path, "/seasons/") {
			if !errors.Is(err, errNoTitle) {
				t.Errorf("missing title %s: %v", path, err)
			}
		} else if status.Code(err) != codes.Unavailable {
			t.Errorf("upstream outage %s: %v", path, err)
		}
	}
	if err := client.get(context.Background(), client.wikidataURL, url.Values{}, &struct{}{}); status.Code(err) != codes.Unavailable {
		t.Errorf("Wikidata 404 classified as a title: %v", err)
	}
}

func TestConfiguration404IsNotEmptyArtworkSuccess(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sparql" {
			writeJSON(t, w, map[string]interface{}{"results": map[string]interface{}{"bindings": []interface{}{map[string]interface{}{"item": map[string]string{"value": "Q1"}, "apple": map[string]string{"value": "umc.cmc.show"}}}}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	server := &artworkServer{client: client, englishCountry: "au"}
	ids := &structpb.Struct{Fields: map[string]*structpb.Value{"tmdb": structpb.NewStringValue("42")}}
	if result, err := server.GetImages(context.Background(), &pluginv1.GetImagesRequest{ItemType: "series", ProviderIds: ids}); status.Code(err) != codes.Unavailable {
		t.Fatalf("configuration outage produced %v, error %v", result, err)
	}
}

func TestOriginalArtworkRetainsSourceCropDimensions(t *testing.T) {
	au, _ := countryStorefront("au")
	for _, test := range []struct {
		field, original, card string
		width, height         int32
	}{
		{"posterArt", "4000x6000", "400x600", 4000, 6000},
		{"contentImageTall", "3360x5040", "400x600", 3360, 7272},
		{"contentImage", "7680x4320", "592x333", 7680, 4320},
	} {
		result, err := mapImages(map[string]appleImage{test.field: testImage("large", "nr", test.width, test.height)}, nil, nil, au, au, nil)
		if err != nil || len(result.Images) != 1 {
			t.Fatalf("image mapping: %v %v", result, err)
		}
		for variant, dimensions := range map[string]string{"original": test.original, "full": test.original, "": test.original, "future": test.original, "card": test.card} {
			resolved, err := resolveImage(result.Images[0].Url, variant)
			if err != nil || !strings.HasSuffix(resolved, "/"+dimensions+"nr.jpg") {
				t.Errorf("%s %q: %s %v", test.field, variant, resolved, err)
			}
		}
	}
}

func TestLiveArtworkRejectsIncompleteAndOversizedImages(t *testing.T) {
	for _, format := range []string{"jpeg", "png"} {
		var encoded bytes.Buffer
		var err error
		original := image.NewRGBA(image.Rect(0, 0, 64, 64))
		if format == "jpeg" {
			err = jpeg.Encode(&encoded, original, nil)
		} else {
			err = png.Encode(&encoded, original)
		}
		if err != nil {
			t.Fatal(err)
		}
		body := encoded.Bytes()
		if _, config, got, err := readLiveArtwork(bytes.NewReader(body)); err != nil || got != format || config.Width != 64 || config.Height != 64 {
			t.Fatalf("valid %s: %v %v", format, config, err)
		}
		cut := 33
		if format == "jpeg" {
			cut = len(body) - 50
		}
		truncated := body[:cut]
		if _, _, err := image.DecodeConfig(bytes.NewReader(truncated)); err != nil {
			t.Fatalf("fixture needs an intact %s header: %v", format, err)
		}
		if _, _, _, err := readLiveArtwork(bytes.NewReader(truncated)); err == nil {
			t.Errorf("truncated %s accepted", format)
		}
		oversized := append(append([]byte(nil), body...), make([]byte, 4*1024*1024+1-len(body))...)
		if _, _, _, err := readLiveArtwork(bytes.NewReader(oversized)); err == nil {
			t.Errorf("oversized %s accepted", format)
		}
	}
}
