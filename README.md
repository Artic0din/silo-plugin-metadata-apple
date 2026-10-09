# Apple TV Artwork for Silo

Choose Apple TV posters, backdrops, logos and textless artwork in Silo's existing image picker.
The catalogue includes Apple TV+ and titles from other services, such as HBO Max, Disney+ and Hulu.

## Status

The initial artwork provider is under development and has not been released.
Live Silo API testing verified movie, series and season choices, preview downloads and saved artwork persistence.
Visible picker controls and the Textless filter still need browser validation.
[#1](https://github.com/Artic0din/silo-plugin-metadata-apple/issues/1) tracks the artwork provider.
Titles, descriptions, cast and other Apple metadata remain a future enhancement under [#2](https://github.com/Artic0din/silo-plugin-metadata-apple/issues/2).

## Server requirement

The artwork provider requires Silo's `image_picker_lookup_provider_ids` extension, as the [Aura plugin](https://github.com/Artic0din/silo-plugin-metadata-aura) does.
Without that extension, a title with no Apple ID cannot reach the artwork-only provider through the picker.

## Build and use

```sh
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off go build -trimpath -o dist/silo-plugin-metadata-apple-linux-amd64 .
```

Use `GOARCH=arm64` for Linux ARM or `GOOS=darwin GOARCH=arm64` for an Apple Silicon Mac.
The binary embeds its manifest and uses the published Silo SDK without a local dependency replacement.

1. Install the binary through **Admin > Plugins > Catalog > Install from a file**.
2. Save your **English artwork country** (default `au`), test the connection and enable **Apple TV Artwork** in the library's movie, series and season provider priorities.
3. Open a TMDB-matched title's **Edit Metadata > Images** and select an Apple image.

No Apple account or API key is required.
The plugin does not identify titles or select images during metadata refresh.
Wikidata supplies the TMDB-to-Apple ID bridge; when its Apple ID is missing, the plugin searches Apple using Wikidata's English title and release or premiere year.
Only an exact type, title and year match is accepted, and ambiguous matches fail visibly.
Titles missing from Wikidata or Apple's storefront catalogue produce no choices.
Network, rate-limit and malformed-response failures are reported separately.

## Artwork and language limits

Apple's public catalogue API is undocumented and can change.
Requests use the iPad catalogue, which includes titles distributed by other services.
Missing image fields are skipped rather than replaced with unrelated images.
Episodes, headshots and watch-provider metadata are excluded.

Posters are requested at 2:3 and backdrops at 16:9, preserving the crop suffix in each Apple URL template.
Logos retain their source proportions and use PNG.
Season choices contain only exact-season posters; square or show-level reused tall images are excluded.
Specials are returned only when Apple identifies season zero.

Apple does not guarantee an image is textless.
The plugin infers textless artwork from `contentImage` and `contentImageTall`, and sends an empty language plus `includes_text: false` so the picker filter and automatic image classification agree.
Season tall images must also pass the portrait and distinct-source checks.
Review the preview before applying artwork; inferred classifications and Apple crops can be wrong.

Text images are tagged by storefront language.
If the requested and home storefronts return the identical URL, the home storefront's language is used.
This is an inference, especially for multilingual countries; `originalSpokenLanguages` is not used because it can describe a dubbed version.
Shared-language defaults are English to the configured country, Spanish to Mexico, German to Germany, Portuguese to Brazil and Cantonese to Hong Kong.
Other languages use the matching storefront in the checked-in registry.
An explicitly unsupported storefront or unavailable title falls back to the US storefront to discover the title's home country; an unavailable home storefront retains that baseline.
Other upstream errors are not suppressed.

The storefront registry and API field knowledge come from the existing [media-asset-tool](https://github.com/Artic0din/media-asset-tool) project.
Registry membership does not guarantee Apple TV availability.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) before making changes.
They apply Silo's shared contribution rules, AI disclosure policy and checked-in writing skill.

## Attribution

This community plugin is not affiliated with or endorsed by Apple, Silo or the services whose titles appear in the Apple TV catalogue.
Apple TV is a trademark of Apple Inc.
See [LICENSE](LICENSE) for the AGPL-3.0-or-later license.
