package main

import (
	"context"
	"net/url"
	"sync"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
)

func eachStorefront(regions []storefront, visit func(int, storefront) error) []error {
	errors := make([]error, len(regions))
	var workers sync.WaitGroup
	jobs := make(chan int)
	for range min(6, len(regions)) {
		workers.Go(func() {
			for i := range jobs {
				errors[i] = visit(i, regions[i])
			}
		})
	}
	for i := range regions {
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	return errors
}

type storefrontArtwork struct {
	detail appleDetail
	images map[string]appleImage
}

func (c *appleClient) combinedImages(ctx context.Context, id, kind string, preferred storefront, season *int32) (*pluginv1.GetImagesResponse, error) {
	regions := languageStorefronts(preferred)
	results := make([]*pluginv1.GetImagesResponse, len(regions))
	var mu sync.Mutex
	loads := make(map[string]func() (storefrontArtwork, error))
	load := func(region storefront) (storefrontArtwork, error) {
		mu.Lock()
		fetch := loads[region.ID]
		if fetch == nil {
			fetch = sync.OnceValues(func() (storefrontArtwork, error) {
				detail, err := c.detail(ctx, id, kind, region)
				if err != nil {
					return storefrontArtwork{}, err
				}
				images := detail.Content.Images
				if season != nil {
					images, err = c.season(ctx, detail, *season, region)
				}
				return storefrontArtwork{detail, images}, err
			})
			loads[region.ID] = fetch
		}
		mu.Unlock()
		return fetch()
	}
	errors := eachStorefront(regions, func(i int, region storefront) error {
		params, err := c.params(ctx, region)
		if unavailableTitle(err) {
			return nil
		}
		if err != nil {
			return err
		}
		region.Locale = params.Get("locale")
		if imageLanguage(region) != imageLanguage(preferred) {
			return nil
		}
		artwork, err := load(region)
		if unavailableTitle(err) {
			return nil
		}
		if err != nil {
			return err
		}
		results[i], err = storefrontImages(artwork, region, season, load)
		return err
	})
	response := &pluginv1.GetImagesResponse{}
	seen := make(map[string]bool)
	for i, err := range errors {
		if err != nil {
			return nil, err
		}
		for _, image := range results[i].GetImages() {
			key := imageKey(image)
			if !seen[key] {
				seen[key] = true
				response.Images = append(response.Images, image)
			}
		}
	}
	return response, nil
}

func imageKey(image *pluginv1.ImageRecord) string {
	path, _ := url.Parse(image.Url)
	query := path.Query()
	template, _ := url.Parse(query.Get("template"))
	// CDN node names vary by storefront while the asset and crop stay the same.
	template.Host = "mzstatic.com"
	query.Set("template", template.String())
	return query.Encode() + "|" + image.Kind + "|" + image.Language + "|" + image.Metadata.Fields["includes_text"].String()
}
