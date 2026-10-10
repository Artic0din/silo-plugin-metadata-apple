package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const appleAPI = "https://uts-api.itunes.apple.com/uts/v3"
const wikidataAPI = "https://query.wikidata.org/sparql"
const userAgent = "AppleArtworkForSilo/0.1 (https://github.com/Artic0din/silo-plugin-metadata-apple)"
const maxResponseBytes = 8 * 1024 * 1024

var errNoTitle = errors.New("title not available")
var errStorefront = errors.New("unsupported storefront")

type appleClient struct {
	http                  *http.Client
	appleURL, wikidataURL string
	mu                    sync.Mutex
	configurations        map[string]cachedConfiguration
}

type cachedConfiguration struct {
	values  url.Values
	expires time.Time
}

func newClient() *appleClient {
	return &appleClient{http: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, appleURL: appleAPI, wikidataURL: wikidataAPI, configurations: make(map[string]cachedConfiguration)}
}

func invalidArgument(message string) error { return status.Error(codes.InvalidArgument, message) }
func invalidData(message string) error     { return status.Error(codes.DataLoss, message) }

func (c *appleClient) get(ctx context.Context, endpoint string, params url.Values, target interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return invalidArgument("Invalid catalogue request.")
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return status.FromContextError(ctx.Err()).Err()
		}
		return status.Error(codes.Unavailable, "Catalogue request failed.")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return invalidData("Catalogue response is unreadable or exceeds the size limit.")
	}
	switch response.StatusCode {
	case http.StatusNotFound:
		if strings.HasPrefix(endpoint, c.appleURL+"/movies/") || strings.HasPrefix(endpoint, c.appleURL+"/shows/") || strings.HasPrefix(endpoint, c.appleURL+"/seasons/") {
			return errNoTitle
		}
	case http.StatusTooManyRequests:
		return status.Error(codes.ResourceExhausted, "Catalogue request was rate limited.")
	case http.StatusBadRequest:
		var failure struct {
			Message string `json:"message"`
		}
		if strings.HasSuffix(endpoint, "/configurations") && json.Unmarshal(body, &failure) == nil && strings.HasSuffix(failure.Message, " is not a supported storefront") {
			return errStorefront
		}
	}
	if response.StatusCode != http.StatusOK {
		return status.Errorf(codes.Unavailable, "Catalogue returned HTTP %d.", response.StatusCode)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return invalidData("Catalogue returned invalid JSON.")
	}
	return nil
}

func (c *appleClient) params(ctx context.Context, region storefront) (url.Values, error) {
	c.mu.Lock()
	cached := c.configurations[region.ID]
	c.mu.Unlock()
	if cached.values != nil && time.Now().Before(cached.expires) {
		return cloneValues(cached.values), nil
	}
	var response struct {
		Data struct {
			ApplicationProps struct {
				RequiredParamsMap map[string]map[string]string `json:"requiredParamsMap"`
				Storefront        struct {
					ID            json.Number `json:"storefrontId"`
					DefaultLocale string      `json:"defaultLocale"`
				} `json:"storefront"`
			} `json:"applicationProps"`
		} `json:"data"`
	}
	query := url.Values{"utsk": {"0"}, "caller": {"web"}, "v": {"96"}, "pfm": {"ipad"}, "sf": {region.ID}, "locale": {region.Locale}}
	if err := c.get(ctx, c.appleURL+"/configurations", query, &response); err != nil {
		return nil, err
	}
	props := response.Data.ApplicationProps
	defaults := props.RequiredParamsMap["Default"]
	if len(defaults) == 0 || (props.Storefront.ID != "" && props.Storefront.ID.String() != region.ID) {
		return nil, invalidData("Apple configuration has missing parameters or a different storefront.")
	}
	values := make(url.Values)
	for key, value := range defaults {
		values.Set(key, value)
	}
	values.Set("sf", region.ID)
	values.Set("pfm", "ipad")
	locale := props.Storefront.DefaultLocale
	if locale == "" {
		locale = region.Locale
	}
	values.Set("locale", strings.ReplaceAll(locale, "_", "-"))
	c.mu.Lock()
	c.configurations[region.ID] = cachedConfiguration{cloneValues(values), time.Now().Add(time.Hour)}
	c.mu.Unlock()
	return values, nil
}

func cloneValues(values url.Values) url.Values {
	result := make(url.Values, len(values))
	for key, value := range values {
		result[key] = append([]string(nil), value...)
	}
	return result
}

func (c *appleClient) appleGet(ctx context.Context, path string, region storefront, target interface{}) error {
	params, err := c.params(ctx, region)
	if err != nil {
		return err
	}
	return c.get(ctx, c.appleURL+path, params, target)
}

type appleImage struct {
	URL           string `json:"url"`
	Width, Height int32
}
type appleContent struct {
	ID, Type, Title string
	ReleaseDate     *int64 `json:"releaseDate"`
	Countries       []struct {
		Code string `json:"countryCode"`
	} `json:"countriesOfOrigin"`
	Images map[string]appleImage
}
type appleSeason struct {
	ID     string
	ShowID string `json:"showId"`
	Number *int32 `json:"seasonNumber"`
	Images map[string]appleImage
}
type appleDetail struct {
	Content *appleContent
	Seasons map[string]appleSeason
}

func (c *appleClient) detail(ctx context.Context, id, kind string, region storefront) (appleDetail, error) {
	var response struct{ Data appleDetail }
	path := "/movies/"
	expectedType := "Movie"
	if kind == "series" {
		path = "/shows/"
		expectedType = "Show"
	}
	if err := c.appleGet(ctx, path+id, region, &response); err != nil {
		return appleDetail{}, err
	}
	if response.Data.Content == nil || response.Data.Content.ID != id || response.Data.Content.Type != expectedType {
		return appleDetail{}, invalidData("Apple returned a missing or different title identity.")
	}
	return response.Data, nil
}

func (c *appleClient) season(ctx context.Context, detail appleDetail, number int32, region storefront) (map[string]appleImage, error) {
	var selected *appleSeason
	for key, season := range detail.Seasons {
		if season.Number == nil || *season.Number != number {
			continue
		}
		if selected != nil {
			return nil, invalidData("Apple returned more than one season with this number.")
		}
		if season.ID != key || !appleIDPattern.MatchString(key) || season.ShowID != detail.Content.ID {
			return nil, invalidData("Apple returned a different season identity.")
		}
		selected = &season
	}
	if selected == nil {
		return nil, nil
	}
	var response struct{ Data appleSeason }
	if err := c.appleGet(ctx, "/seasons/"+selected.ID+"/metadata", region, &response); err != nil {
		if errors.Is(err, errNoTitle) {
			return selected.Images, nil
		}
		return nil, err
	}
	season := response.Data
	if season.ID != selected.ID || season.ShowID != detail.Content.ID || season.Number == nil || *season.Number != number {
		return nil, invalidData("Apple returned a different season identity.")
	}
	images := make(map[string]appleImage)
	for field, image := range selected.Images {
		images[field] = image
	}
	for field, image := range season.Images {
		images[field] = image
	}
	return images, nil
}
