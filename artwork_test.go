package main

import (
	"context"
	"net/url"
	"strings"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func testImage(source, crop string, width, height int32) appleImage {
	return appleImage{URL: "https://is1-ssl.mzstatic.com/image/thumb/" + source + "/{w}x{h}" + crop + ".{f}", Width: width, Height: height}
}

func TestImageMappingAndResolution(t *testing.T) {
	au, _ := countryStorefront("au")
	us, _ := countryStorefront("us")
	fr, _ := countryStorefront("fr")
	images := map[string]appleImage{
		"posterArt":        testImage("poster", "CA.TVA23C01", 2000, 3000),
		"contentImageTall": testImage("tall", "nr", 1680, 3636),
		"contentImage16X9": testImage("cover", "", 3840, 2160),
		"contentImage":     testImage("wide", "BDW.TVAESM02", 4320, 3240),
		"contentLogo":      testImage("logo", "", 7095, 1176),
		"headshot":         testImage("person", "", 1000, 1000),
	}
	response, err := mapImages(images, images, nil, au, us, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Images) != 5 {
		t.Fatalf("got %d images", len(response.Images))
	}
	for i, record := range response.Images {
		text := record.Metadata.Fields["includes_text"].GetBoolValue()
		if (record.Language != "") == !text {
			t.Fatalf("language and includes_text disagree: %v", record)
		}
		if text && record.Language != "en" {
			t.Fatalf("wrong home language: %s", record.Language)
		}
		if record.SeasonNumber != nil {
			t.Fatal("title image has season")
		}
		if record.Kind == "poster" && record.Width*3 != record.Height*2 {
			t.Fatal("wrong poster aspect")
		}
		if record.Kind == "backdrop" && record.Width*9 != record.Height*16 {
			t.Fatal("wrong backdrop aspect")
		}
		full, err := resolveImage(record.Url, "full")
		if err != nil {
			t.Fatal(err)
		}
		thumb, err := resolveImage(record.Url, "card")
		if err != nil {
			t.Fatal(err)
		}
		if strings.ContainsAny(full, "{}") || full == thumb {
			t.Fatalf("bad rendition: %s %s", full, thumb)
		}
		if i == 1 && !strings.HasSuffix(full, "1680x2520nr.jpg") {
			t.Fatalf("tall crop changed: %s", full)
		}
		if i == 3 && !strings.HasSuffix(full, "3840x2160BDW.TVAESM02.jpg") {
			t.Fatalf("wide crop changed: %s", full)
		}
		if record.Kind == "logo" && !strings.HasSuffix(full, ".png") {
			t.Fatal("logo transparency format lost")
		}
	}
	localized := map[string]appleImage{"posterArt": testImage("fr-poster", "", 2000, 3000)}
	response, err = mapImages(localized, images, nil, fr, us, nil)
	if err != nil || response.Images[0].Language != "fr" {
		t.Fatalf("localized language: %v %v", response, err)
	}
}

func TestSeasonMapping(t *testing.T) {
	au, _ := countryStorefront("au")
	number := int32(0)
	parent := map[string]appleImage{"contentImageTall": testImage("show", "nr", 1680, 3636)}
	images := map[string]appleImage{
		"coverArt2X3":      testImage("season", "", 2000, 3000),
		"contentImageTall": testImage("show", "{c}", 1680, 3636),
		"coverArt16X9":     testImage("wide", "", 3840, 2160),
	}
	response, err := mapImages(images, images, parent, au, au, &number)
	if err != nil || len(response.Images) != 1 || response.Images[0].SeasonNumber == nil || *response.Images[0].SeasonNumber != 0 {
		t.Fatalf("specials or show exclusion failed: %v %v", response, err)
	}
	images["contentImageTall"] = testImage("square", "", 3000, 3000)
	response, err = mapImages(images, images, parent, au, au, &number)
	if err != nil || len(response.Images) != 1 {
		t.Fatalf("square should be skipped: %v %v", response, err)
	}
	images["contentImageTall"] = testImage("season-tall", "nr", 1680, 3636)
	response, err = mapImages(images, images, parent, au, au, &number)
	if err != nil || len(response.Images) != 2 || response.Images[1].Language != "" {
		t.Fatalf("season textless image: %v %v", response, err)
	}
}

func TestMissingAndInvalidImages(t *testing.T) {
	au, _ := countryStorefront("au")
	response, err := mapImages(nil, nil, nil, au, au, nil)
	if err != nil || len(response.Images) != 0 {
		t.Fatal("missing fields should return no images")
	}
	for _, image := range []appleImage{testImage("x", "", 0, 3000), testImage("x", "", 2000, -1), {URL: "https://attacker.example/{w}x{h}.{f}", Width: 2000, Height: 3000}} {
		_, err := mapImages(map[string]appleImage{"posterArt": image}, nil, nil, au, au, nil)
		if status.Code(err) != codes.DataLoss {
			t.Fatalf("invalid image accepted: %v", err)
		}
	}
}

func TestResolverRejectsUntrustedPaths(t *testing.T) {
	good, err := imagePath(testImage("safe", "nr", 2000, 3000).URL, 2000, 3000, "jpg")
	if err != nil {
		t.Fatal(err)
	}
	bad := []string{"https://evil.example/x", "appleart://image?width=1", good + "&width=1", good + "#fragment"}
	for _, template := range []string{"http://is1-ssl.mzstatic.com/image/thumb/x/{w}x{h}.{f}", "https://evil.mzstatic.com.evil.example/image/thumb/x/{w}x{h}.{f}", "https://user@is1-ssl.mzstatic.com/image/thumb/x/{w}x{h}.{f}", "https://is1-ssl.mzstatic.com:443/image/thumb/x/{w}x{h}.{f}", "https://is1-ssl.mzstatic.com/image/thumb/x/{w}x{h}.{f}?url=https://evil.example"} {
		bad = append(bad, "appleart://image?"+url.Values{"template": {template}, "width": {"2000"}, "height": {"3000"}, "format": {"jpg"}}.Encode())
	}
	for _, path := range bad {
		if _, err := resolveImage(path, "full"); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("accepted %s: %v", path, err)
		}
	}
	server := &artworkServer{}
	if _, err := server.ResolveImageURLs(context.Background(), &pluginv1.ResolveImageURLsRequest{Paths: []string{good, bad[0]}}); status.Code(err) != codes.InvalidArgument {
		t.Fatal("batch resolver suppressed invalid path")
	}
}

func TestRequestValidation(t *testing.T) {
	server := &artworkServer{}
	for _, req := range []*pluginv1.GetImagesRequest{{ItemType: "episode"}, {ItemType: "movie"}} {
		result, err := server.GetImages(context.Background(), req)
		if err != nil || len(result.Images) != 0 {
			t.Fatalf("unsupported request: %v %v", result, err)
		}
	}
	for _, id := range []string{"0", "-1", "1 OR 1=1", "001", "1.2"} {
		ids := &structpb.Struct{Fields: map[string]*structpb.Value{"tmdb": structpb.NewStringValue(id)}}
		_, err := server.GetImages(context.Background(), &pluginv1.GetImagesRequest{ItemType: "movie", ProviderIds: ids})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("accepted %s: %v", id, err)
		}
	}
}

func TestStorefrontDefaults(t *testing.T) {
	for lang, country := range map[string]string{"en": "au", "es": "mx", "de": "de", "pt": "br", "yue": "hk", "fr": "fr", "ja": "jp"} {
		region, err := languageStorefront(lang, "au")
		if err != nil || region.Country != country {
			t.Fatalf("%s: %v %v", lang, region, err)
		}
	}
	if _, err := languageStorefront("xx", "au"); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unknown language accepted")
	}
	server := &artworkServer{}
	if err := server.configure(context.Background(), nil); err != nil || server.englishCountry != "au" {
		t.Fatal("default configuration failed")
	}
}
