# Ротация и отзыв ключей

Инструкция для модели независимых ключей из [ARCHITECTURE.md](./ARCHITECTURE.md).
Плановая ротация ниже относится к доверенной, доступной ноде. При подозрении на компрометацию
сразу используйте раздел «Отзыв скомпрометированной ноды»: новые секреты на неё не передаются.

## Что меняется при ротации

| Объект | Где задаётся | Что нужно обновить |
| ------ | ----------- | ------------------ |
| age-ключ ноды | `/persist/var/lib/lattice/age/identity` | Ключ на ноде, получатель во всех соответствующих `secrets.nix`, сами `.age`-файлы |
| Recovery-ключ оператора | Закрытая часть только у оператора; публичная в `secrets.nix` | Получатели всех доступных ему секретов и их шифротексты |
| SSH-ключ доступа оператора | `users.users.<user>.openssh.authorizedKeys.keys` | Списки доступа на всех затронутых нодах; это отдельная операция от смены age-получателя |
| SSH host key | `services.openssh.hostKeys`, у homelab — `/persist/etc/ssh/` | Ключ сервера и проверенные записи `known_hosts` клиентов |
| Reticulum/rnsh identity | Файл сервиса; `lattice.rnsh.identity` и `lattice.rnsh.allowed` | Идентичность сервиса, destinations и списки доверия его клиентов/серверов |
| Radicle key | `age.secrets.radicle-private-key`, `services.radicle.publicKey` | Пара ключей и доверие к соответствующим DID/NID |
| LLM gateway client key | `age.secrets.llm-gateway-client-key` | Авторизация клиентов на gateway; не является provider credential |
| LLM provider API key | Отдельный `age.secrets.llm-provider-<name>-key` для каждого upstream | Доступ gateway к provider; клиентам не выдаётся |
| LLM model catalog key | `providers.<name>.modelsApiKeyFile`, если discovery требует отдельную авторизацию | Доступ только к `modelsUrl`; inference key на другой host не переиспользуется неявно |

Смена age-ключа не меняет значения секретов и не отзывает доступ по SSH или rnsh. У age нет
центрального списка отзыва: исключение получателя действует только на заново зашифрованные файлы.
Старый ключ продолжает расшифровывать доступные ему версии из истории Git, копий и snapshots.
Если ключ утёк, нужно также заменить все действующие пароли, токены и сервисные закрытые ключи,
которые он позволял получить. Удаление шифротекстов или переписывание Git не возвращает секретность.

## LLM gateway credentials

Для каждого credential создаётся отдельный `.age`-файл; цельный runtime config не шифруется и не
редактируется вручную. Сначала добавьте правила в `nodes/<name>/secrets/secrets.nix`, используя
тех же законных recipients, что и для остальных секретов ноды:

```nix
"llm-gateway-client-key.age".publicKeys = [ admin node ];
"llm-provider-primary-key.age".publicKeys = [ admin node ];
"llm-provider-primary-models-key.age".publicKeys = [ admin node ];
```

Затем с доверенной машины создайте значения интерактивно, не передавая их через аргументы shell:

```sh
llm_recovery_key=/trusted/path/to/recovery-key
test -f "$llm_recovery_key"
cd nodes/mytecor-homelab/secrets
agenix -e llm-gateway-client-key.age -i "$llm_recovery_key"
agenix -e llm-provider-primary-key.age -i "$llm_recovery_key"
agenix -e llm-provider-primary-models-key.age -i "$llm_recovery_key"
```

Каждый файл содержит ровно один key с допустимым завершающим переводом строки. В конфигурации
ноды объявите secrets с автоматическим перезапуском gateway:

```nix
age.secrets.llm-gateway-client-key = {
  file = ./secrets/llm-gateway-client-key.age;
  mode = "0400";
  restartUnits = [ "llm-gateway.service" ];
};
age.secrets.llm-provider-primary-key = {
  file = ./secrets/llm-provider-primary-key.age;
  mode = "0400";
  restartUnits = [ "llm-gateway.service" ];
};
age.secrets.llm-provider-primary-models-key = {
  file = ./secrets/llm-provider-primary-models-key.age;
  mode = "0400";
  restartUnits = [ "llm-gateway.service" ];
};

lattice.llm-gateway = {
  runtime = "bifrost";
  package = pkgs.lattice.llm-gateway;
  clientCredentialFile = config.age.secrets.llm-gateway-client-key.path;
  providers.primary = {
    accessGroup = "primary";
    inferenceUrl = "https://inference.example.invalid";
    modelsUrl = "https://catalog.example.invalid/v1/models";
    apiKeyFile = config.age.secrets.llm-provider-primary-key.path;
    modelsApiKeyFile = config.age.secrets.llm-provider-primary-models-key.path;
  };
};
```

Профиль `profiles/llm-gateway` добавляется в imports ноды только в том же проверенном изменении,
где объявлены client credential, хотя бы один provider, logical mappings/rules и все secret-файлы. До публикации
соберите `nixosConfigurations.<node>`; после применения проверьте unit, loopback endpoint и
отсутствие provider IDs в `/v1/models`. Runtime config создаётся с mode `0600` в
`/run/llm-gateway` и содержит раскрытые credentials, поэтому каталог доступен только
`llm-gateway` и исчезает после остановки/перезагрузки; публичный шаблон в Nix store секретов не
содержит.

Для плановой ротации provider key сначала выпустите новое значение у provider, замените содержимое
соответствующего `.age`, примените конфигурацию и проверьте запрос через gateway, затем отзовите
старое значение. URL, logical models, client key и конфигурация Pi при этом не меняются. Если
provider поддерживает одновременные keys, безопаснее выполнить ротацию через отдельное короткое
окно: добавить второй provider instance с новым secret в тот же logical route, проверить его,
затем удалить старый instance и отозвать старый key.

Client key имеет другую границу: закреплённый runtime принимает одно значение, поэтому его ротация
требует согласованного обновления авторизованных клиентов и краткого окна переключения. Не
маскируйте provider key под client key ради «бесшовности». При компрометации сначала ограничьте
сетевой доступ к loopback/управляемому ingress, замените key и завершите активные клиентские
процессы; provider credentials меняйте отдельно только если они также могли утечь.

Account-backed OAuth upstreams пока не подключены к декларативному Lattice proxy, даже если
Bifrost поддерживает соответствующий provider flow в других режимах. Не помещайте OAuth record или
всю базу в Nix store. До отдельной typed integration используйте API keys либо оставляйте такой
provider выключенным.

## Подготовка плановой ротации

Работайте с доверенной машины оператора. Команды ниже выполняются в Bash; начните в корне Lattice.
Этот shell использует `age` и `agenix` из графа зависимостей текущего `flake.lock`:

```sh
nix shell --impure --expr '
  let f = builtins.getFlake (toString ./.); s = builtins.currentSystem;
  in [ f.inputs.nixpkgs.legacyPackages.${s}.age
       f.inputs.agenix.packages.${s}.default ]
' -c bash

set -euo pipefail
umask 077
lattice_repo=$PWD
lattice_node=mytecor-homelab
lattice_host=root@mytecor-homelab.local
lattice_old_key="$lattice_repo/.secrets/$lattice_node.agekey"
lattice_new_key="$lattice_repo/.secrets/$lattice_node.next.agekey"
lattice_recovery_key="$HOME/.ssh/mytecor-homelab"
lattice_secrets="$lattice_repo/nodes/$lattice_node/secrets"
```

`lattice_recovery_key` должен указывать на закрытую часть `admin` из `secrets.nix`; имя файла
не подтверждает соответствие. Проверьте расшифрование каждого текущего `.age` этим ключом:

```sh
for file in "$lattice_secrets"/*.age; do
  age --decrypt -i "$lattice_recovery_key" "$file" > /dev/null
done
```

Зафиксируйте исходный commit, текущую систему (`readlink /run/current-system` на ноде), публичные
идентификаторы и план отката. Согласуйте окно без параллельных изменений конфигурации; публикуйте
каждую фазу только после её проверки. `comin` сам применяет GitHub `main`, поэтому push — часть
развёртывания. Чужие незакоммиченные изменения не включайте в ротационный commit.

Сохраните рабочий SSH-сеанс и проверьте второй независимый вход с проверкой host key. Для homelab
Wi-Fi — единственный канал: при плановой ротации age его SSID/пароль остаются прежними.
Проверьте доступность старого ключа для отката и recovery-ключа независимо от самой ноды.

`agenix -r` временно расшифровывает файлы в каталоге `mktemp`, затем удаляет их. Используйте
доверенный зашифрованный диск или приватный tmpfs; не включайте `set -x`, `agenix -v` и запись
терминала при работе с секретами. Редакторы секретов также не должны оставлять swap/backup в Git.

## Фаза A: добавить нового получателя

1. Создайте отдельный новый age-ключ. Старый файл не перезаписывается:

   ```sh
   test -f "$lattice_old_key"
   test ! -e "$lattice_new_key"
   age-keygen -o "$lattice_new_key"
   chmod 600 "$lattice_new_key"
   age-keygen -y "$lattice_new_key"
   ```

   Последняя команда показывает только публичного получателя. В `secrets.nix` сохраните `admin`
   и старого `node`, добавьте `nodeNext` с этим получателем. Для каждого секрета ноды временно
   задайте `publicKeys = [ admin node nodeNext ];`. Если старый получатель встречается в других
   каталогах secrets, обработайте их тоже, сохранив остальных законных получателей.

2. Сверьте список `.age` с правилами: каждому действующему секрету нужна запись, а каждое правило
   должно ссылаться на существующий файл. Опциональный отсутствующий секрет не включайте в rekey.
   Изменение только `secrets.nix` ничего не перешифровывает. Выполните из каталога правил:

   ```sh
   (cd "$lattice_secrets" && agenix -r -i "$lattice_recovery_key")
   for file in "$lattice_secrets"/*.age; do
     age --decrypt -i "$lattice_old_key" "$file" > /dev/null
     age --decrypt -i "$lattice_new_key" "$file" > /dev/null
     age --decrypt -i "$lattice_recovery_key" "$file" > /dev/null
   done
   ```

   `agenix -r` меняет файлы последовательно, а не транзакционно. При ошибке не публикуйте частичный
   результат: устраните причину и повторите rekey с recovery-ключом, затем проверьте все файлы.

3. Проверьте diff правил и список изменённых шифротекстов; расшифрованные значения в diff не нужны.
   Выполните `nix flake check --all-systems --no-build` и сборку
   `nix build .#checks.x86_64-linux.mytecor-homelab` на Linux builder/в CI. Зафиксируйте правила и
   все соответствующие `.age` одним commit A и опубликуйте проверенный commit в `main`.
   Дождитесь применения именно A через `comin`, проверьте журнал и `/run/current-system`.

## Переключение ключа на доверенной ноде

1. Передайте новый закрытый ключ через уже проверенный SSH-канал в соседний файл на `/persist`:

   ```sh
   ssh "$lattice_host" 'set -eu
     dir=/persist/var/lib/lattice/age
     test -f "$dir/identity"
     test ! -e "$dir/identity.next"
     umask 077
     cat > "$dir/identity.next"
     chmod 600 "$dir/identity.next"
     chown root:root "$dir/identity.next"
   ' < "$lattice_new_key"
   ```

   Если передача оборвалась, не используйте частичный файл: удалите только `identity.next` и
   повторите её. Ключ оператора/recovery на ноду не передаётся.

2. На ноде под root используйте чистый checkout проверенного commit A. Подставьте его абсолютный
   путь в `lattice_checkout`; сверяйте `git rev-parse HEAD` с A. Следующие команды не выводят секреты:

   ```sh
   set -euo pipefail
   lattice_checkout=/path/to/verified/lattice
   lattice_age_bin=$(nix eval --raw \
     "$lattice_checkout#nixosConfigurations.mytecor-homelab.config.age.ageBin")
   "${lattice_age_bin}-keygen" -y /persist/var/lib/lattice/age/identity.next
   for file in "$lattice_checkout"/nodes/mytecor-homelab/secrets/*.age; do
     "$lattice_age_bin" --decrypt \
       -i /persist/var/lib/lattice/age/identity.next "$file" > /dev/null
   done
   ```

   Сравните публичный получатель с созданным на машине оператора. После успешной проверки замените
   ключ атомарным rename в том же каталоге, сохранив прежний только на время планового отката:

   ```sh
   set -euo pipefail
   cd /persist/var/lib/lattice/age
   test ! -e identity.previous
   install -m 0600 -o root -g root identity identity.previous
   mv -T identity.next identity
   ```

3. На ноде повторно активируйте текущую систему:

   ```sh
   /run/current-system/bin/switch-to-configuration switch
   test -s /run/agenix/wifi-ssid
   test -s /run/agenix/wifi-password
   systemctl is-active NetworkManager comin sshd
   ```

   В текущей конфигурации homelab `agenix` работает в activation scripts, отдельного
   `agenix.service` нет. Проверка старых файлов в `/run/agenix` сама по себе недостаточна:
   проверьте успешный exit активации и отсутствие ошибок расшифрования, затем новый SSH-вход,
   Wi-Fi, DNS и получение `main` агентом. Не меняйте SSH host key при этой операции.

## Фаза B: убрать старого получателя

1. На машине оператора замените `node` в правилах новым получателем, удалите `nodeNext` и старый
   recipient. Оставьте `[ admin node ]` и других законных получателей там, где они были.
   Повторите `agenix -r -i "$lattice_recovery_key"` в каждом затронутом каталоге.
   Для каждого нового шифротекста новый ключ и recovery должны работать, старый — отказать:

   ```sh
   for file in "$lattice_secrets"/*.age; do
     age --decrypt -i "$lattice_new_key" "$file" > /dev/null
     age --decrypt -i "$lattice_recovery_key" "$file" > /dev/null
     if age --decrypt -i "$lattice_old_key" "$file" > /dev/null 2>&1; then
       echo "Old key still decrypts: $file" >&2
       exit 1
     fi
   done
   ```при

2. Повторите проверки flake и сборку, опубликуйте правила и шифротексты одним commit B. Дождитесь
   его применения. На ноде проверьте текущим `identity` каждый шифротекст из проверенного checkout B,
   успешную активацию и административный доступ. В согласованное окно перезагрузите ноду и проверьте
   возвращение Wi-Fi, SSH и `comin`: только так проверяется ключ на `/persist` при загрузке.

3. После успешной проверки B и загрузки удалите `identity.previous` с ноды. На машине оператора
   перенесите прежнюю `.agekey` в защищённый архив отката, а новую — на штатное место
   `.secrets/mytecor-homelab.agekey`; не потеряйте recovery-ключ. Обновите публичные recipients в
   инструкциях ноды, если они приведены буквально. Архив старого ключа и snapshots имеют прежний
   уровень секретности; обычное удаление на Btrfs/SSD не гарантирует физического стирания.

### Откат плановой ротации

До B оба ключа читают фазу A: при необходимости восстановите `identity.previous` атомарно через
соседний файл и повторите активацию A. После B нельзя сначала вернуть старый ключ: он не прочитает
новые секреты. Сначала верните правила и шифротексты A отдельным проверенным commit, дождитесь
применения A новым ключом и только затем возвращайте старый ключ. Согласуйте `main` и `comin`,
иначе автоматическое обновление вернёт B. Не используйте слепой `nixos-rebuild --rollback`:
старые поколения могут содержать шифротексты, несовместимые с текущим ключом.

Если доступа уже нет, используйте аварийный канал и recovery-ключ на доверенной машине, чтобы
подготовить согласованные конфигурацию и секреты. Это восстановление, а не штатный удалённый путь.
При компрометации откат к отозванным ключам и значениям запрещён.

## Отзыв скомпрометированной ноды

1. Изолируйте ноду на доверенной стороне: порт коммутатора, Wi-Fi/AP, VPN, TCP gateway или firewall
   её соседей — в зависимости от реально включённых каналов. Локальной команде на захваченной ноде
   нельзя доверять. Недоступность устройства не мешает отзыву на остальных участниках.

2. С доверенной машины перечислите всё доступное ноде: age-получателя, все исторически доступные
   `.age`, локальные сервисные ключи и токены, выданные права SSH/rnsh/Radicle и общие сетевые секреты.
   Секреты, читаемые через утёкший age-ключ из Git, считайте раскрытыми. Компрометация root означает
   компрометацию локальных закрытых ключей, но не автоматически закрытого ключа оператора,
   который на ноду не передавался; отдельно проверьте риск agent forwarding и операторских токенов.

3. Удалите recipient этой ноды из всех правил. Для её эксклюзивных секретов, пока замены нет,
   оставьте только доверенный recovery recipient; для общих — остальных доверенных участников.
   Не добавляйте новый recipient на захваченную установку и не используйте фазу перекрытия A.
   Recovery-ключом перешифруйте все затронутые файлы и проверьте отказ старого ключа.

4. Замените сами раскрытые значения и отзовите прежние в системах, которые их принимают:
   токены — у провайдера, SSH-ключи — в authorized keys доверенных серверов, пароль root — новым
   паролем и новым хешем, сервисные identities — новыми парами и списками доверия. Одного `agenix -r`
   для этого недостаточно. Новые значения редактируйте через `agenix -e FILE -i RECOVERY_KEY`
   интерактивно; шифруйте только для оставшихся доверенных получателей. Внешний секрет и его `.age`
   должны представлять одно действующее значение.

   Для общего Wi-Fi PSK нужно сменить пароль на AP и у всех оставшихся клиентов. MAC-блокировка
   сама по себе не отзывает знание PSK. На Wi-Fi-only homelab нельзя обещать бесшовный переход без
   поддержки второго SSID/PSK на AP: заранее подготовьте новый канал на доверенных клиентах либо
   согласуйте перерыв и локальный recovery. Утёкший PSK окончательно выключите на AP.

5. На доверенных узлах примените обновлённые списки доступа и секреты. `LoadCredential` читается
   при старте сервиса: для Radicle после замены ключа нужен перезапуск `radicle-node`, одного
   перешифрования файла недостаточно. Удаление authorized key не закрывает уже открытые SSH-сеансы;
   отдельно завершите сеансы отозванной учётной записи и её привилегированные процессы. Аналогично
   завершите активные сессии других затронутых сервисов. Не прерывайте единственный доверенный
   recovery-сеанс до подтверждения нового доступа.

6. Проверьте на каждой доверенной стороне: старые credentials больше не авторизуются, новые
   работают; старый age-ключ не читает новые `.age`, recovery читает. Возвращающиеся offline-ноды
   остаются изолированными до применения актуальных правил. Зафиксируйте commit, получателей,
   охват отзыва и результаты проверки без закрытых ключей, паролей и токенов в журнале инцидента.

7. Скомпрометированную ноду возвращайте только после чистой установки из доверенного источника
   с новым age-ключом и новыми затронутыми identities. Обычная перезагрузка ephemeral-root не
   очищает `/persist`, `/nix`, загрузчик и прошивку. Не восстанавливайте скомпрометированные ключи
   из старого persistence. Bootstrap выполните по [DEPLOYMENT.md](./DEPLOYMENT.md), а SSH fingerprint
   проверьте независимо, прежде чем обновить `known_hosts`. Стабильное имя ноды можно сохранить.

### Границы отзыва Reticulum и Radicle

У homelab настроены Reticulum/rnsh; Radicle пока выключен. Централизованного механизма исключения
участника из открытого Reticulum transport в Lattice нет. Удаление `nodes/<name>` из flake или
age recipient не блокирует транспорт и не мешает читать публичный GitHub-репозиторий; GitOps
не является механизмом авторизации участников.

Отозванные initiator hashes удаляются из `lattice.rnsh.allowed`
на каждом слушателе; `noAuth` должен оставаться `false`, а `extraArgs` не должны обходить авторизацию.
Upstream дополнительно читает `allowed_identities` из каталога приложения: проверьте, что в
`/var/lib/rnsh/allowed_identities` нет отозванного hash. После изменения перезапустите `rnsh`,
чтобы закрыть уже установленные сессии; затем проверьте успешный вход новым ключом и явный
отказ отозванному. Ключ оператора не передаётся на ноду. Runtime-файлы identity должны иметь
права `0600`; `UMask=0077` защищает вновь созданные ключи.
Смена identity слушателя требует обновления его доверенных destinations у клиентов. Точные
топология и отсутствие общей Network Identity подтверждены в F3: узел и операторский Mac
подключаются к открытым публичным transport peers. Закрытие активных сессий нужно отдельно
проверить при первой фактической ротации rnsh initiator. Изоляция транспорта выполняется на
реально управляемых точках входа, а не вымышленной командой «ban node».

В F4 новый Radicle DID/NID нужно отдельно включить в необходимые политики доверия, а старый
исключить; если identity имела права делегата репозитория, отозвать и их. Доставка нового закрытого
ключа описана в [профиле Radicle](./profiles/radicle/README.md). Проверка этого отзыва на работающей
сети входит в F4, а не в проверку локального шифрования F2.

## Ротация recovery-ключа и остальных identities

При плановой смене recovery-ключа временно добавьте нового recovery-получателя во все затронутые
правила, перешифруйте и проверьте его отдельно. Затем исключите прежнего и повторите rekey.
age-ключи нод при этом менять не нужно. Если тот же SSH-ключ используется для входа, сначала
добавьте новый authorized key и проверьте отдельный вход без SSH multiplexing, затем удалите старый.
Это две независимые операции. При утечке recovery-ключа меняйте также все доступные через него
значения секретов; новый закрытый ключ храните только на доверенной машине оператора.

Плановая смена SSH host key требует генерации отдельной пары в persistent storage, настройки
`services.openssh.hostKeys` и проверки нового fingerprint у клиентов через доверенный канал.
Не обходите эту проверку через `StrictHostKeyChecking=no`. Ротация identities Reticulum и Radicle
меняет доверенные идентификаторы соответствующих сервисов; новое доверие подтверждается до удаления
старого при плановой операции, а при компрометации старое отзывается сразу.
