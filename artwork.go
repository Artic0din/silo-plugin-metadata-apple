package main

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

var appleIDPattern = regexp.MustCompile(`^umc\.cmc\.[a-zA-Z0-9]{1,100}$`)
var tmdbIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,17}$`)

func unavailableTitle(err error) bool {
	return errors.Is(err, errNoTitle) || errors.Is(err, errStorefront)
}

func (s *artworkServer) GetImages(ctx context.Context, req *pluginv1.GetImagesRequest) (*pluginv1.GetImagesResponse, error) {
	empty := &pluginv1.GetImagesResponse{}
	kind := req.GetItemType()
	if kind != "movie" && kind != "series" {
		return empty, nil
	}
	tmdbID := req.GetProviderIds().GetFields()["tmdb"].GetStringValue()
	if tmdbID == "" {
		return empty, nil
	}
	if !tmdbIDPattern.MatchString(tmdbID) {
		return nil, invalidArgument("Artwork lookup needs a positive numeric TMDB ID.")
	}
	if req.SeasonNumber != nil && (kind != "series" || req.GetSeasonNumber() < 0) {
		return nil, invalidArgument("Season artwork needs a series and a nonnegative season number.")
	}
	requested, err := languageStorefront(req.GetLanguage(), s.englishCountry)
	if err != nil {
		return nil, err
	}
	id, err := s.client.identify(ctx, tmdbID, kind, requested)
	if errors.Is(err, errStorefront) {
		requested, _ = countryStorefront("us")
		id, err = s.client.identify(ctx, tmdbID, kind, requested)
	}
	if err != nil {
		return nil, err
	}
	if id == "" {
		return empty, nil
	}
	detail, err := s.client.detail(ctx, id, kind, requested)
	if unavailableTitle(err) {
		requested, _ = countryStorefront("us")
		detail, err = s.client.detail(ctx, id, kind, requested)
	}
	if errors.Is(err, errNoTitle) {
		return empty, nil
	}
	if err != nil {
		return nil, err
	}
	home := requested
	if len(detail.Content.Countries) > 0 {
		if region, ok := countryStorefront(detail.Content.Countries[0].Code); ok {
			home = region
		}
	}
	homeDetail := detail
	if home.ID != requested.ID {
		homeDetail, err = s.client.detail(ctx, id, kind, home)
		if unavailableTitle(err) {
			home, homeDetail, err = requested, detail, nil
		}
		if err != nil {
			return nil, err
		}
	}
	images, homeImages := detail.Content.Images, homeDetail.Content.Images
	if req.SeasonNumber != nil {
		images, err = s.client.season(ctx, detail, req.GetSeasonNumber(), requested)
		if err != nil {
			return nil, err
		}
		homeImages = images
		if requested.ID != home.ID {
			homeImages, err = s.client.season(ctx, homeDetail, req.GetSeasonNumber(), home)
			if err != nil {
				return nil, err
			}
		}
	}
	return mapImages(images, homeImages, detail.Content.Images, requested, home, req.SeasonNumber)
}

type imageField struct {
	name, kind string
	text       bool
}

var titleFields = []imageField{{"posterArt", "poster", true}, {"contentImageTall", "poster", false}, {"contentImage16X9", "backdrop", true}, {"contentImage", "backdrop", false}, {"contentLogo", "logo", true}}
var seasonFields = []imageField{{"coverArt2X3", "poster", true}, {"contentImageTall", "poster", false}}

func mapImages(images, homeImages, showImages map[string]appleImage, requested, home storefront, season *int32) (*pluginv1.GetImagesResponse, error) {
	fields := titleFields
	if season != nil {
		fields = seasonFields
	}
	response := &pluginv1.GetImagesResponse{}
	seen := make(map[string]bool)
	for _, field := range fields {
		image, ok := images[field.name]
		if !ok {
			continue
		}
		if season != nil && field.name == "contentImageTall" && (image.Height <= image.Width || sameSource(image.URL, showImages[field.name].URL)) {
			continue
		}
		width, height, err := imageDimensions(image, field.kind)
		if err != nil {
			return nil, err
		}
		format := "jpg"
		if field.kind == "logo" {
			format = "png"
		}
		path, err := imagePath(image.URL, width, height, format)
		if err != nil {
			return nil, err
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		language := ""
		if field.text {
			language = imageLanguage(requested)
			if image.URL == homeImages[field.name].URL {
				language = imageLanguage(home)
			}
		}
		response.Images = append(response.Images, &pluginv1.ImageRecord{
			Kind: field.kind, Url: path, Language: language, Width: width, Height: height, SeasonNumber: season,
			Metadata: &structpb.Struct{Fields: map[string]*structpb.Value{
				"includes_text":       structpb.NewBoolValue(field.text),
				"apple_field":         structpb.NewStringValue(field.name),
				"text_classification": structpb.NewStringValue("inferred from Apple image field"),
			}},
		})
	}
	return response, nil
}

func sameSource(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	// A season may reuse the show source with a different crop suffix.
	return strings.TrimSuffix(left, "/"+lastSegment(left)) == strings.TrimSuffix(right, "/"+lastSegment(right))
}
func lastSegment(value string) string { parts := strings.Split(value, "/"); return parts[len(parts)-1] }

func imageDimensions(image appleImage, kind string) (int32, int32, error) {
	if image.Width <= 0 || image.Height <= 0 || image.Width > 30000 || image.Height > 30000 {
		return 0, 0, invalidData("Apple returned invalid image dimensions.")
	}
	if kind == "logo" {
		return image.Width, image.Height, nil
	}
	unitW, unitH, maxUnits := int32(2), int32(3), int32(1000)
	if kind == "backdrop" {
		unitW, unitH, maxUnits = 16, 9, 240
	}
	units := min(image.Width/unitW, image.Height/unitH, maxUnits)
	if units == 0 {
		return 0, 0, invalidData("Apple image is too small for the picker aspect ratio.")
	}
	return units * unitW, units * unitH, nil
}

func validTemplate(template string) bool {
	raw := strings.NewReplacer("{w}", "1", "{h}", "1", "{f}", "jpg", "{c}", "").Replace(template)
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && strings.HasSuffix(u.Hostname(), ".mzstatic.com") && u.RawQuery == "" && u.Fragment == "" && strings.HasPrefix(u.Path, "/image/thumb/") && !strings.ContainsAny(raw, "{}") && strings.Contains(template, "{w}") && strings.Contains(template, "{h}") && strings.Contains(template, "{f}")
}

func imagePath(template string, width, height int32, format string) (string, error) {
	if !validTemplate(template) {
		return "", invalidData("Apple returned an unsupported image URL template.")
	}
	return "appleart://image?" + url.Values{"template": {template}, "width": {strconv.Itoa(int(width))}, "height": {strconv.Itoa(int(height))}, "format": {format}}.Encode(), nil
}

func resolveImage(path, variant string) (string, error) {
	u, err := url.Parse(path)
	if err != nil || u.Scheme != "appleart" || u.Host != "image" || u.User != nil || u.Path != "" || u.Fragment != "" {
		return "", invalidArgument("Invalid Apple artwork path.")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query) != 4 {
		return "", invalidArgument("Invalid Apple artwork parameters.")
	}
	for _, key := range []string{"template", "width", "height", "format"} {
		if len(query[key]) != 1 {
			return "", invalidArgument("Invalid Apple artwork parameters.")
		}
	}
	width, e1 := strconv.ParseInt(query.Get("width"), 10, 32)
	height, e2 := strconv.ParseInt(query.Get("height"), 10, 32)
	format, template := query.Get("format"), query.Get("template")
	if e1 != nil || e2 != nil || width <= 0 || height <= 0 || width > 30000 || height > 30000 || (format != "jpg" && format != "png") || !validTemplate(template) {
		return "", invalidArgument("Invalid Apple artwork dimensions or template.")
	}
	limit := int64(0)
	switch variant {
	case "card":
		limit = 600
	case "featured", "large":
		limit = 1600
	}
	if limit > 0 && max(width, height) > limit {
		if width*3 == height*2 {
			units := min(limit/3, width/2, height/3)
			width, height = units*2, units*3
		} else if width*9 == height*16 {
			units := min(limit/16, width/16, height/9)
			width, height = units*16, units*9
		} else {
			scale := float64(limit) / float64(max(width, height))
			width, height = max(1, int64(float64(width)*scale)), max(1, int64(float64(height)*scale))
		}
	}
	return strings.NewReplacer("{w}", strconv.FormatInt(width, 10), "{h}", strconv.FormatInt(height, 10), "{f}", format, "{c}", "").Replace(template), nil
}

func (s *artworkServer) ResolveImageURL(_ context.Context, req *pluginv1.ResolveImageURLRequest) (*pluginv1.ResolveImageURLResponse, error) {
	value, err := resolveImage(req.GetPath(), req.GetVariant())
	if err != nil {
		return nil, err
	}
	return &pluginv1.ResolveImageURLResponse{Url: value}, nil
}

func (s *artworkServer) ResolveImageURLs(_ context.Context, req *pluginv1.ResolveImageURLsRequest) (*pluginv1.ResolveImageURLsResponse, error) {
	result := make(map[string]string, len(req.GetPaths()))
	for _, path := range req.GetPaths() {
		value, err := resolveImage(path, req.GetVariant())
		if err != nil {
			return nil, err
		}
		result[path] = value
	}
	return &pluginv1.ResolveImageURLsResponse{Urls: result}, nil
}
