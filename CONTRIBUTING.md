# Contributing to the Apple TV Artwork Plugin

The [Silo contribution guide](https://github.com/Silo-Server/.github/blob/main/CONTRIBUTING.md)
covers project-wide coordination, focused changes, evidence, AI disclosure, and pull request expectations.
Those requirements apply here; this guide adds the plugin-specific workflow.

## Before you start

Open an [issue](https://github.com/Artic0din/silo-plugin-metadata-apple/issues) before changing title matching, artwork lookup, image filtering, image resolution, configuration, or the advertised capabilities.
This repository owns Apple TV title matching, artwork lookup and image resolution; plugin contracts belong in [`silo-plugin-sdk`](https://github.com/Silo-Server/silo-plugin-sdk), while host metadata orchestration belongs in [`silo-server`](https://github.com/Silo-Server/silo-server).

## Development setup

Use the Go version declared in `go.mod`.
A local `go.work` may point at a sibling SDK checkout while developing both repositories, but committed code and CI must resolve released dependencies with `GOWORK=off`.
Never commit credentials, captured private data or a local filesystem `replace` directive.

## Validate your change

```sh
GOWORK=off go test ./...
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go build ./...
GOWORK=off go run . manifest >/dev/null
gofmt -l .
```

The manifest command must exit successfully.
`gofmt -l .` should print nothing; if it reports unrelated pre-existing drift, none of the Go files touched by your change may appear in the output.
Do not add to the output, and report what remains.
Add focused coverage for title matching, field mapping, crop codes, season filtering, language tagging, and upstream error handling when those behaviors change.

Run `APPLE_LIVE_TEST=1 GOWORK=off go test -run TestLiveArtwork -v` to check the public Apple and Wikidata APIs and download sample renditions.
This check needs network access and saves public artwork evidence in a temporary directory printed by the test.
Offline CI skips it; report API availability separately from deterministic test results.

For documentation-only changes, check the complete diff for whitespace errors with `git diff --check` (or `git diff --cached --check` after staging), and verify local Markdown links resolve.

## Open the pull request

Use a Conventional Commit title, explain any artwork filtering, title matching or upstream API risk, and paste the actual validation results.
Read the [AI-assisted contribution policy](https://github.com/Silo-Server/silo-server/blob/main/docs/ai-contributions.md) and include its disclosure block in every issue and pull request.
