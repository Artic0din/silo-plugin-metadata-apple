# Changelog

All notable changes are documented here, following Keep a Changelog.

## [Unreleased]

### Added

- Added an Apple TV artwork provider for TMDB-matched movies, series and exact-season posters, with textless choices and language tags inferred from Apple fields and storefronts.
- Added repository contribution guidance, agent instructions and a pull request template for the planned Apple TV artwork plugin.

### Fixed

- Combined artwork from all available storefronts in the requested language and removed repeated choices.
- Fixed Apple artwork previews rejecting the paths Silo passes to image resolvers.
- Fixed title matching stopping at unavailable Apple search results.
- Fixed the connection test reporting success when Wikidata is unavailable.
- Fixed exact-season summary artwork disappearing when season metadata returns 404.
- Fixed non-English storefronts being accepted as the English artwork country.
- Fixed upstream configuration outages being treated as missing artwork.
- Fixed original artwork requests losing full source crop dimensions.
- Fixed live artwork validation accepting truncated or oversized downloads.
