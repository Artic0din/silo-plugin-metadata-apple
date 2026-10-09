# Apple TV Artwork for Silo

Planned: choose Apple TV posters, backdrops, logos and textless artwork in Silo's existing image picker.
The catalogue includes Apple TV+ and titles from other services, such as HBO Max, Disney+ and Hulu.

## Status

The plugin is not built yet.
[#1](https://github.com/Artic0din/silo-plugin-metadata-apple/issues/1) tracks the artwork provider, and [#2](https://github.com/Artic0din/silo-plugin-metadata-apple/issues/2) tracks Apple metadata.

## Server requirement

The planned artwork provider requires Silo's `image_picker_lookup_provider_ids` extension, as the [Aura plugin](https://github.com/Artic0din/silo-plugin-metadata-aura) does.
Without that extension, a title with no Apple ID cannot reach the artwork-only provider through the picker.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) before making changes.
They apply Silo's shared contribution rules, AI disclosure policy and checked-in writing skill.

## Attribution

This community plugin is not affiliated with or endorsed by Apple, Silo or the services whose titles appear in the Apple TV catalogue.
Apple TV is a trademark of Apple Inc.
See [LICENSE](LICENSE) for the AGPL-3.0-or-later license.
