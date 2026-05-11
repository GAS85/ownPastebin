# Changelog

All notable changes to this project are documented in this file.

## v2.4.0

### Added

- i18n support.
- Automatic AI translations to CN, CH, DE, FR, IT, RU, UA.
- `PASTEBIN_EXPIRY_TIMES` now you can define custom expiration list.
- Add check of downloaded 3rd party artifacts prior to build

### Changed

- Minify all json files in static folder.
- Add html ID tags.
- Python example update.

## v2.3.0

### Added

- K8s example to deploy.
- JSON Logs support via `PASTEBIN_LOG_FORMAT`.
- `PASTEBIN_PROTECTED_PASTE_ENABLED` to create protected pastes.
- Ensure DB new schema migration.

### Changed

- `.dockerignore` update.
- Commit hash saved inside of the container as variable.
- `PASTEBIN_DATE_FORMAT` refactoring in `entrypoint.sh`.
- `README.md` update with k8s and examples, protected pastes.
- Move some Info messages to Debug.
- `Dockerfile` minor formatting.
- UI Update: Disable delete button when paste was protected + react on a 403 error with explanation.
- Test update to cover protected pastes and API documentation.

## v2.2.1

### Added

- Minify css and js during the build.

### Changed

- Swagger_ui icon path update
- License now in a docker Labels and folder after build

## v2.1.0

### Added

- Storage statistics collection and logging for SQLite and PostgreSQL backends.

### Changed

- Refactor Redis storage Stats() method for improved performance and background execution.
- Update healthcheck command in Dockerfile and enhance UI elements in templates and CSS.

## v2.0.0

### Added

- Add caching middleware Headers for static assets and no-cache for dynamic content.
- SVG favicon support: replaced `static/favicon.ico` with `static/favicon.svg` and updated HTML template references.
- Zombie reaper for Alpine containers to handle `SIGCHLD` and prevent orphaned processes.
- IP rate limiting support in test application setup and per-IP rate limiting in storage/plugin flows.
- `PASTEBIN_FILE_LOG` environment variable for file logging support.
- `PASTEBIN_TRUSTED_PROXY` environment variable.
- PostgreSQL cleanup loop to delete expired rows hourly.
- Delete page support in the UI with w3css.
- Decryption dialog in the UI with w3css.
- Line number support in the UI with w3css.
- Sticky navigation and footer behavior improvements.
- Dark mode support.
- `ErrSlugConflict` error handling to avoid paste overwrite on slug conflict.
- `PeekMeta` storage interface method for metadata retrieval without loading content.
- Support for strong CSP in Swagger UI and templates.

### Changed

- Major change to DB structure.
- Refactored Redis storage methods for improved metadata retrieval and connection handling.
- Enhanced plugin management so assets are loaded conditionally.
- Updated UI with Font Awesome icons, sidebar layout improvements, and general frontend polish.
- Removed unused JavaScript assets and optimized Prism language handling.
- Updated Swagger UI template handling and moved Swagger UI version updates into template resources.
- Switched frontend behavior to native browser APIs instead of jQuery and CryptoJS.
- Improved error handling and database management in storage modules.
- Updated Docker `HEALTHCHECK` interval and commands for better reliability.
- Improved `entrypoint.sh` and README guidance.

### Fixed

- Prevented low-entropy encryption key generation by not falling back to raw string values.
- Fixed process accumulation and reliability issues in Alpine containers.
- Applied CSP fixes for all injected entries, not only `onclick` handlers.
- Fixed Docker and compose reliability issues with updated health checks and environment handling.
- Updated tests and test utilities to support the latest application changes.

### Documentation

- Updated `README.md` with new feature and configuration details.
- Improved `entrypoint.sh` clarity and script reliability.

## v1.2.0

### Added

- Added upload concurrency limiting and improved error handling for paste creation.
- Enhanced `SQLiteStorage.Close` with VACUUM and cleanup improvements.

### Changed

- Continued UI and infrastructure stabilization following `v1.1.0`.

## v1.1.0

### Added

- Updated JavaScript and CSS for better small-screen visibility.
- Added SQLite DB size information on startup.

### Changed

- Workflow rework and documentation improvements.

### Fixed

- Updated display behavior for full-screen views on smaller displays.

## v1.0.0

Initial rebuild of the [Pastebin](https://github.com/mkaczanowski/pastebin). Thanks you for it.
