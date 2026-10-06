# Integrated Recorder Plugin Registry

**한국어** | [English](README.en.md)

이 저장소는 Integrated Recorder 공식 Plugin Registry의 source of truth이며, 승인된 plugin release의 정확한 artifact identity와 배포 metadata를 관리합니다.

## 현재 배포 catalog

Owncast `0.2.0`이 first-party source plugin으로 공식 catalog에 등록되어 있습니다.

- v3 catalog: [catalog-v3.json](https://integrated-recorder.github.io/plugin-registry/catalog-v3.json)
- v2 호환 catalog: [catalog.json](https://integrated-recorder.github.io/plugin-registry/catalog.json)

v3는 publisher metadata를 포함하며, v2 catalog는 기존 Core 호환성을 위한 projection입니다. `storage.local`은 Core에 bundled된 reference Storage Plugin이므로 이 Registry에서 배포하지 않습니다.

## 역할과 책임 경계

Plugin repository가 source를 관리하고 executable을 build/release합니다. GitHub Release나 HTTPS artifact host는 bytes를 전달합니다. 이 Registry는 검토 절차를 통해 plugin ID/type/version, protocol, source commit, platform별 URL·filename·size·SHA-256을 승인합니다. Core Runtime Host가 다운로드한 bytes와 descriptor를 검증하고 immutable lifecycle로 import합니다.

이 저장소는 plugin source를 clone/build하지 않습니다. Registry approval은 승인된 배포 metadata를 뜻합니다. plugin이 안전하거나 malware-free라는 보장, sandbox, source 재현성 보장은 아닙니다. 설치된 plugin의 실행을 위해 Registry가 계속 온라인일 필요는 없습니다.

## Source of truth와 검증

사람이 관리하는 입력은 plugin별 `plugins/*.json` 파일입니다. Builder는 결정적인 v3 catalog와 publisher 필드를 제외한 v2 호환 catalog를 생성하며, generated output은 배포 artifact이지 수동 편집 대상이 아닙니다. 기존 release identity는 같은 version에서 artifact metadata나 digest를 바꿀 수 없습니다. 수정이 필요하면 새 plugin version을 발행해야 합니다.

```sh
go test ./...
go run ./cmd/registryctl validate
go run ./cmd/registryctl build
go run ./cmd/registryctl verify-artifacts
go run ./cmd/registryctl check-core-schemas
go run ./cmd/registryctl check-immutability
```

빈 catalog도 유효하며 plugin entry가 없으면 artifact 검증은 네트워크 요청 없이 성공합니다. Core schema baseline과 vendored schema 갱신 절차는 [CORE_SCHEMA_SOURCE.md](schemas/CORE_SCHEMA_SOURCE.md)를 참고하세요.

## 변경 제출과 운영

현재 Registry는 solo-maintainer mode입니다. 모든 변경은 PR을 거쳐야 하고 필수 자동 검증과 immutable artifact 검증을 통과해야 합니다. 독립 승인 review는 현재 요구하지 않습니다. 이는 별도 human review가 있었다는 뜻이 아닙니다. 두 번째 trusted maintainer가 활동을 시작하면 required approval을 최소 1건으로 복구할 계획입니다. 자세한 절차는 [CONTRIBUTING](CONTRIBUTING.md), [SECURITY](SECURITY.md), [관리자 설정](docs/ADMIN_SETUP.md)을 참고하세요.

Official first-party plugin은 `integrated-recorder` organization 소유 repository를 사용해야 합니다. Third-party plugin은 외부 repository에서 올 수 있습니다. Source ID `hls`와 Storage ID `local`은 Core bundled identities로 예약되어 있습니다.

## 생태계 위치와 라이선스

- [Integrated Recorder Core](https://github.com/integrated-recorder/core) — artifact를 검증하고 설치하는 Runtime
- [Adapter SDK for Go](https://github.com/integrated-recorder/adapter-sdk-go) — Source Plugin authoring SDK
- [source.owncast](https://github.com/integrated-recorder/source.owncast) — official Registry의 first-party Owncast integration
- [source.soop](https://github.com/integrated-recorder/source.soop) — 개발/실험 중이며 stable Registry 배포 전

이 저장소의 라이선스는 [LICENSE](LICENSE)를 참고하세요.
