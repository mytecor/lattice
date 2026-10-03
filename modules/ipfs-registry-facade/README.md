# IPFS Registry Facade (`lattice.ipfs-registry-facade`)

Модуль поднимает локальный Kubo IPFS daemon и OCI Registry фасад
через `nerdctl ipfs registry serve` для распространения OCI-образов
агентского рантайма без использования центральных реестров
([f10-04](../../roadmap/f10-disposable-worker/f10-04-agent-runtime-acp.md)).

## Архитектура

```text
nix build .#agent-image
       │
       ▼
nerdctl load (локальный containerd)
       │
       ▼
nerdctl push ipfs://... (Kubo API: `127.0.0.1:5001`)
       │
       ▼
IPFS CID
       │
       ▼
nerdctl ipfs registry serve (`127.0.0.1:5050`)
       │
       ▼
r1s run / containerd pull (`127.0.0.1:5050/ipfs/<CID>@sha256:<digest>`)
```

- **Kubo daemon** (`pkgs.kubo`) хранит и раздаёт IPFS-блоки; состояние и пины сохраняются в `/var/lib/ipfs-daemon`.
- **Registry facade** (`nerdctl ipfs registry serve`) слушает на loopback `127.0.0.1:5050` и транслирует запросы стандартного OCI Distribution API в IPFS.
- HTTP Gateway Kubo отключён: facade использует RPC API напрямую и не занимает дополнительный порт.
- Сервис доступен только локально на loopback, не открывается в публичную сеть.
- Multi-node репликация образов осуществляется через нативный IPFS Bitswap между пирами, а не через HTTP registry.

## Опции

| Опция | Тип | Дефолт | Описание |
| --- | --- | --- | --- |
| `enable` | bool | `false` | Включение модуля |
| `listenAddress` | str | `"127.0.0.1"` | Адрес прослушивания OCI-реестра |
| `port` | port | `5050` | Порт прослушивания OCI-реестра |
| `ipfsApiAddress` | str | `"/ip4/127.0.0.1/tcp/5001"` | Multiaddr локального Kubo API |
| `dataDir` | path | `"/var/lib/ipfs-daemon"` | Каталог постоянного хранилища Kubo |
| `kuboPackage` | package | `pkgs.kubo` | Пакет Kubo (IPFS) |
| `nerdctlPackage` | package | `pkgs.nerdctl` | Пакет nerdctl |

## Проверка

Контракт-тест [`tests/ipfs-registry-facade.nix`](../../tests/ipfs-registry-facade.nix) проверяет
конфигурацию юнитов `ipfs.service` и `ipfs-registry-facade.service`, правильность флагов,
параметров адресов и наличие необходимых утилит в окружении.
