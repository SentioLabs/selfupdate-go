# Changelog

## [0.2.0](https://github.com/SentioLabs/selfupdate-go/compare/v0.1.2...v0.2.0) (2026-09-09)


### ⚠ BREAKING CHANGES

* **replace:** RollbackError is removed. No caller used it.
* rename module to selfupdate-go
* two-phase Installer and release assets

### Features

* **archive:** native installer for goreleaser tarballs ([86fb2ca](https://github.com/SentioLabs/selfupdate-go/commit/86fb2cab80fadfe430ded0182a09914d2eebc5e4))
* **checksum:** parse and verify goreleaser checksum files ([64165b5](https://github.com/SentioLabs/selfupdate-go/commit/64165b54df994b6f6d61f09bffe0d6d3a334306b))
* **examples:** add mytool demo CLI ([0c3620e](https://github.com/SentioLabs/selfupdate-go/commit/0c3620ef50f82ea61c79935217f889a108a34c81))
* **extract:** pull one named entry from a tar.gz ([be4e24d](https://github.com/SentioLabs/selfupdate-go/commit/be4e24d33c91dad94cab6d60e4153cf65dc8fbf4))
* **managed:** refuse to replace package-managed binaries ([6686658](https://github.com/SentioLabs/selfupdate-go/commit/66866580554f1135355a949a300b55791e06c062))
* **progress:** terminal-only download progress bar ([5c09f7d](https://github.com/SentioLabs/selfupdate-go/commit/5c09f7da9a9b7c8a1044d82fbadefe2a82408cc9))
* rename module to selfupdate-go ([51c3056](https://github.com/SentioLabs/selfupdate-go/commit/51c3056db4c2b4d51933b8d42e930160a7f36f97))
* **replace:** atomic binary replace with rollback ([45e1870](https://github.com/SentioLabs/selfupdate-go/commit/45e187014a0bdf5cf6c4cd303a339f933a724343))
* two-phase Installer and release assets ([d7c2aac](https://github.com/SentioLabs/selfupdate-go/commit/d7c2aacfb2887f91c1923b6282e816b5e5afb366))


### Bug Fixes

* **archive:** share the default client and cap the download ([3a6a2f5](https://github.com/SentioLabs/selfupdate-go/commit/3a6a2f5acdffc0611a5659bfcba71c5166f52058))
* **examples:** satisfy lint in mytool ([3c87797](https://github.com/SentioLabs/selfupdate-go/commit/3c87797b9c19f7ea75fae3f4313e681a14f33c16))
* **managed:** anchor Cellar and add sbin, lib64, libexec ([6d8d122](https://github.com/SentioLabs/selfupdate-go/commit/6d8d122572e9fe19ae2721c7c30ef682092afa3a))
* **managed:** name test constant and document target ([cf31403](https://github.com/SentioLabs/selfupdate-go/commit/cf31403fb5b181d99f7a80be0a281e25d39d2357))
* remove a stale .new before every update ([e6538cc](https://github.com/SentioLabs/selfupdate-go/commit/e6538ccc14e44c4cf47e6281d6637eac55d77948))
* remove a stale .new before every update ([0844315](https://github.com/SentioLabs/selfupdate-go/commit/084431513ad89578c3911f710411cf9a7c3aadda))
* **replace:** apply requested mode after create ([7791d97](https://github.com/SentioLabs/selfupdate-go/commit/7791d97f8c68737512171d04d3d80f2e7251be66))
* **replace:** rename .new over the target in one step ([ac53966](https://github.com/SentioLabs/selfupdate-go/commit/ac53966e4ee27e8c64a69a08a15258dab073a395))
* **test:** compare installed path after symlink resolution ([801d43a](https://github.com/SentioLabs/selfupdate-go/commit/801d43aacfaa789de3e3893dac52079c9fd2a305))


### Refactoring

* **examples:** print hook lines via cobra writer ([3fdcb34](https://github.com/SentioLabs/selfupdate-go/commit/3fdcb34a09786824b19ed7f2117de155b2f665e2))

## [0.1.2](https://github.com/SentioLabs/go-selfupdate/compare/v0.1.1...v0.1.2) (2026-09-06)


### Bug Fixes

* compare legacy RC versions numerically ([798e6a3](https://github.com/SentioLabs/go-selfupdate/commit/798e6a37e29469910b3540ad50d68b487f277bbc))
* compare legacy RC versions numerically ([98c8695](https://github.com/SentioLabs/go-selfupdate/commit/98c86952869c3f524af1807cb2aaf685fc994638))

## [0.1.1](https://github.com/SentioLabs/go-selfupdate/compare/v0.1.0...v0.1.1) (2026-09-04)


### Bug Fixes

* **selfupdate:** fall back to Latest when no stable listed ([3cc3577](https://github.com/SentioLabs/go-selfupdate/commit/3cc3577b43886bb38cd7c8a233b45f60fac3d9fa))
* **selfupdate:** fall back to Latest when no stable listed ([6d6202e](https://github.com/SentioLabs/go-selfupdate/commit/6d6202e02f57545392acad248a55faa2fa2c3a58))
* **selfupdate:** stop the whole pipeline on cancel ([ffe2376](https://github.com/SentioLabs/go-selfupdate/commit/ffe237640a8fc9269c59a64889737fca2f0b6957))
* **selfupdate:** stop the whole pipeline on cancel ([8afce16](https://github.com/SentioLabs/go-selfupdate/commit/8afce163baa22a553df79cd4e1a897fde319b09e))
