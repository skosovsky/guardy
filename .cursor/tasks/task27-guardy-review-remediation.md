# Task 27. Guardy: исправить контрактные дефекты и упростить API

Статус: **в работе; T00 принят; T01 — следующий**. Независимое ревью: 2026-10-06. Проверенный commit: `e7e1c8c51e656e94106c5cf7c101ef0bc7d139d1` (`fix: release flow`). Разрешены clear break и реорганизация. Код библиотеки в ходе ревью не менялся.

## Цель и вывод

Закрыть подтверждённые ошибки typed scope, delivery, stream release и встроенных validators; исправить опасные примеры документации; согласовать ownership и публичные контракты. Сохранить guardy независимой BYOT-библиотекой, без agent runtime и сервисных моделей.

Основа спроектирована хорошо: generic Pipeline/Validator, caller-owned ExecutionScope, отдельные optional build/schema/OTel-модули, канонические Decision/PolicyFailure, проверяемые stream capabilities и явный Complete. Core зависит только от x/sync. Большая часть сложности streaming и release tooling оправдана контрактами. При этом девять замечаний ниже требуют исправления; успешные штатные проверки их не исключают.

Ревью выполнено шестью субагентами: core/policy, streaming, ext validators, integrations, architecture, docs/release. Основные публичные воспроизведения повторены ведущим ревьюером. Архитектурные сомнения отделены от доказанных ошибок; не каждое необычное решение объявлено уязвимостью.

## Проверки и доказательства

| Проверка | Результат |
|---|---|
| `GOCACHE=/tmp/guardy-review-gocache PYTHONDONTWRITEBYTECODE=1 make test` | PASS: 19 модулей, `go test -v -race ./...` |
| Release tooling tests внутри make test | PASS: 15 Python-тестов, 74.723s; temporary fixtures/local remotes |
| `GOCACHE=/tmp/guardy-review-gocache GOLANGCI_LINT_CACHE=/tmp/guardy-review-lintcache make lint` | PASS: 19 модулей, 0 issues |
| Targeted stream/release/ring race tests субагента | PASS |
| Независимые public API repro | Подтверждают перечисленные runtime/doc дефекты и отдельно отмеченные contract gaps |

19 модулей включают core, build, integration, три optional extensions и 13 example modules. Тесты не равнозначны выполнению всех example main. Полный verify реального нового release candidate, публикация, fuzz campaign, benchmark campaign и статистическая оценка detector quality в этом ревью не выполнялись. Слова `Released` в test log относятся к изолированным release fixtures, не к origin проекта.

[Отчёты и логи](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/ai-libs/reviews/guardy-2026-10-06/evidence>), [исходники repro](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/ai-libs/reviews/guardy-2026-10-06/repro>), [результаты repro](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/ai-libs/reviews/guardy-2026-10-06/evidence/reproductions.log>). Каждая `.go` — самостоятельная программа. Запускать по одному файлу из корня guardy с рабочим go.work: `GOCACHE=/tmp/guardy-review-gocache go run /absolute/path/to/repro.go`. Cycle-repro ограничен временем жизни отдельного процесса; не переносить бесконечные goroutines прямо в общую test suite.

## Подтверждённые замечания

P2 здесь — обязательное исправление в рамках задачи. Приоритет отражает работу, а не удалённую эксплуатируемость. Нет доказанного универсального обхода всех guardrails; конкретные условия каждого нарушения указаны ниже.

### GRD-R01 · P2 — typed scope precheck и Lookup проверяют разные типовые контракты

**Код:** [scope.go:277](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/scope.go:277>), [scope.go:117](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/scope.go:117>).

Precheck использует `reflect.Type.AssignableTo`, а `ScopeKey[T].Lookup` — type assertion `value.(T)`. Для `ScopeKey[map[string]string]` значение `type Tags map[string]string` проходит precheck, но typed Lookup возвращает false. Policy callback вызывается, хотя объявленная обязательная предпосылка не выполнена. В repro callback возвращает pass: `ran=true decision=none error=<nil>`. Built-in typed-present может вместо этого отказать как policy violation; это тоже неверная классификация относительно ScopeTypeError.

**Исправить:** compiled prerequisite должен точно соответствовать semantics Lookup. Для non-interface T требуется точный dynamic type; для interface T — реализация интерфейса. Можно хранить acceptance predicate из typed key, чтобы не дублировать правила. Не заменять всё безусловным type equality, сломав interface keys.

**AAA:** named map/slice/struct/function, assignable к ожидаемому типу, но не assertable → ErrScopeIncompatible/SystemFault, callback не вызван. Exact type и concrete implementation interface key проходят. Boundary и PolicyFailure сохраняют ту же classification.

### GRD-R02 · P2 — delivery classifier зацикливается на допустимых Go-значениях

**Код:** [guarded_output.go:275](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/guarded_output.go:275>), [guarded_output.go:297](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/guarded_output.go:297>).

`GuardOutput[any]` с пустым pipeline не завершается для `var v any; v = &v`. Независимый случай: `type P *P; var p P` зацикливает обход nil-pointer type. Циклы dereference не имеют visited/bound; context deadline ничего не меняет. Нужен соответствующий host BYOT value — это не утверждение об атаке обычной JSON-строкой.

**Исправить:** ограниченный обход value/type graph и явное завершение с typed fault для cyclic/unclassifiable values. Предпочтительно не превращать неизвестное значение в safe payload. Если classifier станет opt-in согласно D01, его собственная реализация всё равно обязана завершаться.

**AAA:** обе формы цикла и обычные pointer-to-string/nil/struct/bytes; любой разрешённый delivery policy, включая technical, не должен оставлять CPU-loop. Проверять regression в subprocess/watchdog; ошибка подавляет выдачу и сохраняет корректную fault category.

### GRD-R03 · P2 — WithFatal блокирует input без нарушения

**Код:** [ext/options.go:91](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/ext/options.go:91>), [ext/options.go:113](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/ext/options.go:113>), [ext/options.go:145](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/ext/options.go:145>).

Опция объявлена как escalation **нарушения**, но `passReport` передаёт весь violation config, включая Fatal. Regex `secret` с `WithFatal(true)` на `hello` возвращает `ActionPass / terminal_deny`. Проверено также для length, wordlist, PII, ML, tag и TechnicalJSON: чистый результат становится недоставляемым.

**Исправить:** отделить clean-pass construction от violation-only control flags. Проверить Retryable/SafeUserMessage на ту же ошибку применения. Канонический контракт явно созданного caller report `ActionPass,Fatal:true` не ослаблять — он и дальше означает deny.

**AAA:** каждый affected built-in: чистое значение проходит; реальное нарушение с Fatal запрещено; ручной fatal-pass report запрещён. Неподдерживаемые сочетания classifier/options либо отклоняются при construction, либо имеют явно задокументированное поведение.

### GRD-R04 · P2 — финальная строка ровно MaxUnitBytes блокируется до Complete

**Код:** [release.go:344](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/release.go:344>), [stream_splitter.go:27](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/stream_splitter.go:27>).

Newline unit framing допускает финальную строку без `\n` при Complete, но Write раньше завершает stream при `pending.size >= MaxUnitBytes`. При max=4: `123` проходит; `1234` получает buffer_limit; `123\n` проходит. Допустимый четырёхбайтовый финальный хвост нельзя завершить.

**Исправить:** при ровно max ждать trusted Complete либо следующего байта, доказывающего overflow. Не flush по достижению лимита и не переименовывать off-by-one в новое ограничение. Учесть `MaxPendingBytes == MaxUnitBytes` без дополнительного unbounded buffering.

**AAA:** `1234` во всех разбиениях + Complete → одна выдача и success; `12345` / `1234\n` → limit и ноль выданных байтов; меньший tail, newline-terminated unit и JSON exact-limit regressions остаются корректными.

### GRD-R05 · P2 — JSON unit fallback меняет разрешённый wire format

**Код:** [release.go:501](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/release.go:501>), [release.go:546](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/release.go:546>), [stream_splitter.go:65](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/stream_splitter.go:65>).

Основной `JSONValues` unit framer принимает только objects/arrays, fallback проверяет лишь `json.Valid`. Repro: `{}` отклоняется default user-text delivery; fallback `123` успешно записывается. Тот же scalar как основной input отвергается. Аналогичная разница возможна после redaction object → scalar. Pipeline проверка fallback не обходится; нарушен именно обещанный формат unit stream.

**Исправить:** единый profile-aware framing contract для обычного, transformed и fallback unit. Сохранить object/array contract либо явно изменить API/документацию и все пути одновременно. Предпочтительный вариант — отклонять scalar в unit JSON profile до writer. Whole-response JSON может иметь отдельные semantics.

**AAA:** number/bool/null/string/object/array через normal, transformed и fallback paths; одинаковые классы допустимого framing, zero writes для неподдерживаемого результата. Mandatory fault никогда не превращается в fallback delivery.

### GRD-R06 · P2 — порядок map меняет JSON decision между retry и deny

**Код:** [ext/jsonredact/jsonredact.go:90](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/ext/jsonredact/jsonredact.go:90>), [ext/jsonredact/jsonredact.go:95](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/ext/jsonredact/jsonredact.go:95>), [ext/jsonredact/jsonredact.go:119](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/ext/jsonredact/jsonredact.go:119>).

Object обходится через Go map и прекращает проверки после первой non-none disposition. Для одного JSON с `retry` leaf и `deny` leaf 1000 повторов дают обе classifications. Первый агент получил 845 retry/155 deny; независимый повтор — 880/120. Точные доли несущественны. Payload запрещён в обоих случаях; нестабильны retry-routing и код решения.

**Исправить Contract-First:** детерминированный порядок и явные stop/aggregation rules. Рекомендация — продолжать после correction для выявления более сильного deny/fault, агрегировать через общий контракт. Если сознательно сохраняется short-circuit, документировать порядок/остановку и не обещать глобально сильнейшее решение непроверенных siblings. Одного sorted keys достаточно для устранения randomness, но недостаточно для обещания strongest outcome.

**AAA:** mixed retry/deny/fault в object и nested arrays/objects, разные source key orders и повторные оценки; стабильная canonical classification, исходный документ при mandatory failure, cancellation и shadow не скрывают fault. Выбранный code воспроизводим при одинаковом документе/policy.

### GRD-R07 · P2 — MapSlice теряет уже полученную payload classification при ошибке

**Код:** [ext/map_slice.go:51](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/ext/map_slice.go:51>), [pipeline.go:351](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/pipeline.go:351>), [CONTRACTS.md:46](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/CONTRACTS.md:46>).

Первый элемент успешно классифицируется как `PayloadTechnicalPayload`; второй возвращает nil report и error. MapSlice возвращает report=nil вместо accumulated combined; классификация становится safe_user_text. Repro использует чистые callbacks и string values, без aliasing. Ошибка продолжает запрещать выдачу — это потеря contract/telemetry evidence, не доказанный выпуск данных.

**Исправить:** сохранять классификацию успешно завершённых внутренних проверок при последующем fault/cancellation. Провести её через error projection родительского Pipeline и boundaries: сейчас pipeline игнорирует report рядом с error, поэтому локальной правки MapSlice недостаточно. Отдельно определить, как доверенно передаются completed observations; не считать failed callback successful и не превращать его output в deliverable. Проверить аналогичные recursive adapters, в частности jsonredact, на тот же контракт.

**AAA:** prior technical/internal observation + поздний error/wrapped cancellation → direct adapter, RunResult, boundary Decision и PolicyFailure согласованы; transformed output подавлен. Первый fault без завершённых наблюдений сохраняет корректный default.

### GRD-R08 · P2 — Quick Start направляет report-only fault в ветку success

**Код:** [README.md:50](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/README.md:50>), [pipeline.go:358](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/pipeline.go:358>), [decision.go:49](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/decision.go:49>).

Пример проверяет Go error, затем IsTerminal/IsRetryable, иначе использует Output как успешный. Low-level Run может вернуть report-only SystemFault с err=nil; IsTerminal и IsRetryable тогда false. Custom validator с `Action(255)` воспроизводит `output="secret" systemFault=true terminal=false retry=false err=<nil>`.

Это дефект копируемого routing pattern после расширения pipeline, а не заявление, что неизменённый демонстрационный набор built-ins сейчас раскрывает данные.

**Исправить:** показать exhaustive routing с IsSystemFault либо начать boundary quick start с GuardOutput/GuardDelivery и DeliverableValue/Projection. Рядом с Run объяснить два способа представления fault, если этот low-level контракт сохраняется; рассмотреть его унификацию в D10.

**AAA:** исполняемый doc example с pass, redact, deny, retry, report-only fault и Go error; только одобренный результат достигает sink.

### GRD-R09 · P2 — README ошибочно назначает MutatedText источником результата

**Код:** [README.md:108](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/README.md:108>), [README.md:163](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/README.md:163>), [http_guard.go:121](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/guardy/http_guard.go:121>).

Текст обещает применение `Report.MutatedText` pipeline/HTTP wrapper. Реализация правильно использует возвращённый T / RunResult.Output. Repro validator возвращает исходный `secret` и report с `MutatedText:"clean"`; реальный Output остаётся `secret`. Следуя документации, автор custom validator может ошибочно считать данные очищенными.

**Исправить:** returned T — единственный authoritative transformed value; MutatedText — optional diagnostic mirror. Не переделывать generic pipeline под string-поле отчёта. Обновить prose, Godoc, custom-validator example и HTTP описание.

**AAA:** исполняемый пример custom redactor проверяет именно bytes, дошедшие до handler/sink; struct/Map примеры не зависят от MutatedText.

## Архитектурные и контрактные решения — отдельно от дефектов

По каждому D-пункту принять указанное решение либо документировать обоснованное сохранение с конкретным consumer contract. Эти пункты не следует механически превращать в большой refactoring ради абстракций.

| ID | Странность / evidence | Решение для реализации |
|---|---|---|
| D01 | Default user policy автоматически считает JSON/struct/map/slice technical. Custom string type с MarshalJSON→object, напротив, проходит как safe text. `guarded_output.go:245–300`, repro core | Отделить generic delivery от явно названного UserText recipe. Определить supported wire types и caller classifier/canonicalization contract. Shape heuristic не доказывает безопасность serialized bytes; не вызывать произвольный MarshalJSON внутри core для «универсального» решения. Сохранить нужную политику как явный opt-in, без UI зависимости. Удаление heuristic не делает unknown/default classification безопасной: eligibility требует явного destination policy/classifier contract. |
| D02 | GuardedOutput и GuardedDelivery дублируют все поля/accessors; Projection только у второго. `guarded_output.go:53–98` | Один canonical guarded delivery type и удобный user-channel helper. Clear break позволяет удалить дубликат, не держать compatibility façade. |
| D03 | `DeliveryPolicy.Fallback any` при несовпадении T молча игнорируется. `guarded_output.go:146–151` | Typed policy/fallback либо проверка совместимости до processing. Явно определить typed nil. Существующее suppression fail-closed, это configuration UX, а не утечка. |
| D04 | RouteDecision выбирает retry exhaustion/fallback по RetryAttempt/MaxRetries и дублирует flags. `route.go:15–90` | Вынести в optional integration recipe либо явно оставить stateless projection. Validation core возвращает Decision; счётчики, retries, scheduling принадлежат host. Fallback route — предложение, не разрешение отправить непроверенный FallbackMessage. Определить отрицательные attempts, если helper сохраняется. |
| D05 | Report одновременно содержит Action, Disposition, Retryable, Fatal, ShadowMode. `guardy.go:64`, `disposition.go:79` | Упростить публичные комбинации до validated outcome + observation/correction metadata, если это уменьшает surface. Сохранить отделение diagnostics от Decision и fail-closed для invalid combinations. Не переписывать всё только ради другого DTO. |
| D06 | Fast/Slow названы по скорости, но контракт — sequential mutation / parallel read-only; ещё есть policy phase. `pipeline.go:58–81`, `guardy.go:1` | Sequential/Policy/Parallel либо точные пояснения. Header «two phases» привести к реальному порядку. Изменение telemetry phase names объявлять отдельно, не ломать dashboards молча. |
| D07 | Deprecated untyped scope constructors остаются в core. `policy.go:41–54,209–219` | Удалить NewPolicyFunc/NewAttributePresent compatibility APIs в clear break, мигрировать на typed variants. Сохранить low-level ExecutionScope.Lookup для BYOT. |
| D08 | Compiled DeepEqual operand сохраняет mutable caller map; mutation spec меняет terminal_deny→none. StaticScope immutable только структурно. `build/compile.go:202–215`, `scope.go:49–71`, repro ownership | Явно выбрать freeze/borrow contract. Borrowed immutable operands + safe whole-pipeline reload либо snapshot ограниченной declarative data model. Не писать универсальный reflective deep copier. Если обещан snapshot — реализовать и протестировать mutation/race. Не называть нынешнее поведение атакой без ownership promise. |
| D09 | Pipeline объявлен concurrent-safe без требования к callbacks, middleware, scope, aliases. `pipeline.go:20–23` | Документировать условную sharing safety: конфигурация не мутирует, но caller-owned state обязан быть thread-safe. Use не клонирует внутренности validators. Для MapSlice явно связать Godoc с запретом alias mutation; не обещать rollback произвольных setters. |
| D10 | Report-only SystemFault может идти с nil Go error; consumer обязан знать оба канала. `pipeline.go:351–358` | Выбрать единый публичный fault contract либо заметно задокументировать low-level отличие и гарантировать exhaustive high-level helpers. Не терять cause/report classification при унификации. R08 исправить независимо от этого выбора. |
| D11 | Panic fast/policy escapes, slow превращается в validator fault. `pipeline.go:347,378,420` | Явный runtime callback panic contract по всем фазам. Допустимо recovery для изолированных goroutines, но перенос rule между фазами не должен менять поведение без документированного основания. Никогда не превращать panic в pass; public error безопасен, детали только opt-in. |
| D12 | Nil rules/options и invalid ext options проверяются неодинаково; некоторые Action принудительно заменяются block. `pipeline.go:58–80`, `ext/options.go`, `ext/regex.go:32` | Fallible construction/Compile и явные Must wrappers; проверять deterministic config errors заранее. Typed per-validator config либо небольшая validation; не строить новый config framework. Игнорируемые WithTokenVault/WithLowercase/Action убрать или явно ограничить. |
| D13 | NewLength принимает min>max, Must отвергает; nonpositive limits выключают стороны. `ext/length.go` | Один контракт в New и Must, явные zero semantics. Nil detector/nonfinite threshold валидировать при construction, если API реорганизуется; runtime fail-closed оставить для реальных callback faults. |
| D14 | TokenVault.Store panic/empty/identity token молча заменяется irreversible redaction. `ext/token_vault.go:92–105` | Error-returning token contract и явный выбор irreversible degradation, если он нужен. Default не должен скрывать потерю обещанной обратимости. Vault lifecycle/ACL/storage — host; не добавлять их в core. |
| D15 | Regex определяет redaction hit по изменению output: pattern=secret,replacement=secret даёт pass; другие detectors сохраняют hit. `ext/regex.go:49–51` | Решить, report описывает detection или mutation; предпочтительно согласованный match-based outcome. Проверить identity/empty/zero-width cases. Caller replacement не обязан быть доказуемо безопасным, не объявлять это универсальным bypass. |
| D16 | TagSanitizer фактически blocks patterns; NewMLValidator — adapter чужого classifier. `ext/tag_sanitizer.go`, `ext/ml_validator.go` | Имена TagPatternValidator/ClassifierValidator точнее. Сохранить ограничения regex/PII/wordlist; библиотека не содержит trained model и не обещает статистическую защиту от prompt injection. |
| D17 | MaxUnitBytes после redaction expansion не проверяется, только общий MaxOutputBytes. Repro maxUnit4 → output unit10. `release.go:629` | Определить input-only или input+output unit budget. README обещание expansion уточнить; если bound обоих — применить к transformed/fallback. Не добавлять новый budget тип без реального сценария. |
| D18 | Oversized Write отклоняется целиком; мелкие writes могут успеть выдать units до общего input overflow. `release.go:WriteContext`, `CONTRACTS.md:238` | Ограничить обещание partition independence допустимыми inputs либо изменить admission contract. Зафиксировать irreversible prefixes при overbudget input; не обещать одинаковые released bytes в несовместимых сценариях. |
| D19 | Validation/writer/observer под stream mutex; Abort/Outcome ждут blocked callback. `release.go`, CONTRACTS stream section | Сохранить простой ownership и cooperative cancellation, явно распространить liveness/reentry требования на writer/observer. Не добавлять goroutine timeout, оставляющий unkillable workers. |
| D20 | guardyotel молча игнорирует ошибки создания counter/histogram. `ext/guardyotel/middleware.go:119–125` | Error-returning setup или bounded caller diagnostic. Потеря runtime telemetry не должна переводить бизнес-validation в pass/fault по скрытому правилу. Fake meter с sentinel error обязан обнаруживаться при setup. |
| D21 | WrapOutput возвращает unvalidated partial result при handler error, WrapGuardedOutput suppresses. `interceptor.go:67`, `adapters.go` | Сейчас задокументировано, поэтому не defect. Сделать safe suppression предпочтительным boundary API; низкоуровневый partial-result contract назвать явно. Никаких retries/undo handler side effects в guardy. |
| D22 | HTTP hard cap 1MiB назван DefaultMaxBodyBytes; oversized →400; read/replace не закрывает custom Body wrapper явно. `http_guard.go:13,76–85` | Document status/limit/ownership table; выбрать validated configurable cap или честное fixed-limit naming. Проверить tracking ReadCloser и закрытие consumed wrapper; не заявлять универсальную socket leak — net/http сохраняет underlying body. Отдельно решить 413 vs текущий400. |
| D23 | JSON-schema extension отражает engine schema graph и компилирует safe-number variant. `ext/jsonschema/numbers.go` | Сложность оправдана exact-number/overflow contract. Сохранить optional module, pin dependency и probes на upgrade. Не удалять защиту ради краткости и не переносить engine в core. |
| D24 | BoundaryProfile объявляет coverage, не связываясь с реальными adapters. `boundary.go:44–88` | Оставить lightweight declared-coverage contract; реальные handlers/sinks проверять integration fixtures. Не создавать plugin registry и не выдавать caller declaration за доказательство enforcement. |
| D25 | Release tooling крупный, но проверяет непубликованный graph 19 modules | Сохранить isolated prepare→verify→publish, exact refs и fixture tests. Возможный перенос общего dev tooling — отдельная задача, не новая runtime-библиотека. Подтверждённого release defect в этом ревью не найдено. |

## Что исключено из дефектов

- MapSlice с mutating `[]*Msg` setter меняет aliased caller input до последующего deny. Это воспроизведено, но **нарушает явный запрет** alias mutation в CONTRACTS. Требуется заметность документации D09, не универсальная deep-copy/rollback implementation.
- Custom MarshalJSON и reflected Go shape расходятся — D01, ограничение representational contract, не доказанная PII утечка при заданной policy.
- Подозрение на устаревший outcome из Complete опровергнуто публичным repro.
- Schema overflow через external/nested `$id` корректно отвергается; нового обхода не найдено.
- Make fuzz сейчас имеет по одному fuzz target в затронутом package; прежний дефект другой библиотеки сюда не переносится.
- Нестабильный выбор diagnostic code между равносильными parallel reports отдельно разрешён контрактом. R06 касается **разных canonical dispositions**, не только порядка telemetry.

## Границы библиотеки

Оставить в guardy: validation/transformation caller-owned values, typed policy facts, canonical result/error classification, interception adapters, bounded release и проверку fallback перед фактической выдачей. Generic Map/MapSlice и JSON/schema adapters здесь уместны. ScopeFactory обновляет facts; это не атомарная execution authorization и не хранилище approvals.

Host/другие библиотеки владеют agent loop, Ask/Plan/Action, UI, исполнением и rollback инструментов, retries/scheduler/counters, моделями ролей/ресурсов, identity/provenance evidence, persistence, vault lifecycle и detector training/calibration. Metry — telemetry; guardyotel остаётся optional adapter. Evals измеряет detector quality; guardy принимает score и обеспечивает его validation/error semantics. Ни одной новой runtime-библиотеки для этой задачи не требуется.

Оправданные fallback-поведения сохранить: отдельную checked fallback delivery, explicit best-effort profile без ложной whole-value гарантии, shadow только для разрешённых nonfatal policy observations. Mandatory faults не маскировать. Неявное отключение reversible redaction или telemetry configuration — отдельные D14/D20 с явным решением.

## Документация и naming

- [ ] Закрыть R08/R09 исполняемыми consumer examples; объяснить authoritative output, Decision/error channels и безопасную выдачу через Projection.
- [ ] Уточнить общую concurrency/ownership contract, mutation preconditions, transient/current ScopeFactory facts, no authorization/rollback guarantee.
- [ ] Описать HTTP limits/statuses, replacement/body ownership, cancellation и ошибки extractor/injector.
- [ ] Свести ~870 строк README и повторяющиеся migration layers в текущий guide + versioned MIGRATION. Убрать внутреннее «v2-style» либо объяснить, что это design label, не Go `/v2` module path. Не форсировать v2 release ради документации.
- [ ] Добавить ext/jsonredact в inventory и явные install/import примеры optional modules. Сохранить dependency boundaries и root-only install сценарий.
- [ ] Согласовать StreamConfig/README/CONTRACTS/STREAM_MEASUREMENTS по exact byte bounds, transformed output, fallback framing, partition guarantees, lock/reentry и cooperative cancellation.
- [ ] Сохранить честные matcher limitations, отличие deterministic fixtures от statistical quality, telemetry privacy defaults и измеренные benchmark limitations.
- [ ] Переименования D02/D06/D16 и удаления deprecated APIs оформить одной migration, включая изменения telemetry labels и сериализуемых контрактов. Не оставлять legacy wrappers без consumer причины.

## Порядок реализации и приёмка

1. Сначала зафиксировать contracts R01–R09 и D01/D08–D12/D17–D18, где поведение неоднозначно. Остальные D тоже получают явное решение; не откладывать их молча как «когда-нибудь».
2. Добавить независимые AAA regressions, которые воспроизводят текущее нарушение; исправить core/type/stream/ext paths. Не менять ожидаемые значения тестов так, чтобы узаконить случайные defaults.
3. Реализовать согласованный clear break: один delivery type, typed/fallible config где принято, удалить redundant/deprecated paths. Сохранить BYOT и small optional modules.
4. Синхронизировать docs/examples и реальную выдачу через adapters; проверить error/cancel/fallback scenarios и удержание payload classification.
5. Выполнить make test/make lint всех 19 modules, targeted race и finite cycle watchdog. При изменениях stream buffer/framing повторить релевантные partition/work benchmarks против указанной baseline; не заявлять performance improvement без измерения.
6. Если меняются module/API packaging, проверить изолированный release candidate через существующий prepare/verify, включая independent consumer. Реальный publish не входит в эту задачу.

**Definition of Done:** девять R исправлены и покрыты воспроизводимой AAA-приёмкой; каждый D реализован либо имеет конкретно обоснованное сохранение; нет скрытых fallback/config coercion без контракта; docs/API/examples согласованы; fault не допускает delivery; отклонённый исходный payload не выпускается; отдельно проверенный fallback разрешён только явным контрактом; не появились обязательные harness/domain dependencies. Логи и migration приложены к результату. Старые зелёные тесты не заменяют новые regressions.


## План исполнения и журнал приёмки

Baseline: `e7e1c8c51e656e94106c5cf7c101ef0bc7d139d1`; рабочее дерево перед T00 чистое.
Этот раздел задаёт целевой контракт remediation. До принятия соответствующего этапа
он не является утверждением о текущем поведении API. Исходные R/D и DoD выше
остаются обязательными; критерии этапов дополняют, а не сокращают их.

### Правила исполнения

- Строго T00 → T01 → … → T12. Каждый этап зависит от принятого и закоммиченного предыдущего.
- Только один этап реализации открыт. Исправления замечаний приёмки относятся к нему же.
- Перед изменением поведения: контракт и AAA regression с доказательством нарушения baseline.
  Для выбора нового API, которого в baseline нет, проверяется новый контракт; зелёный baseline
  не заменяет regression подтверждённых R. Для документационного T00 runtime tests не нужны.
- Приёмка каждого этапа: два новых независимых субагента, не участвующих в реализации.
  Полнота: все критерии + связанные исходные R/D → доказательства, выполнен/нет, процент;
  принимается только 100%. Корректность: diff, контракты и релевантные failure/edge paths,
  принимается только при отсутствии нерешённых подтверждённых ошибок. Непроверенное отмечается.
  Проверяющим не передаются выводы друг друга. После правок повторяются обе приёмки финального diff.
- После двух PASS: журнал доказательств, статус, отдельный короткий английский commit.
  Ссылки на отчёты/логи и commit каждого этапа сохраняются здесь. Commit отмечается журналом
  следующего этапа, чтобы избежать самоссылки на hash. Только собственные изменения, без push/publish.
- Выбор внутри предусмотренных R/D вариантов автономный. Выход за эти варианты согласовывается.
- В репозитории evidence хранится в `.cursor/tasks/task27-evidence/`; большие сырые логи могут
  находиться в `/tmp/guardy-task27/` с конкретными путями и итогами в отчёте.

### Контрактные решения

| ID | Выбранный целевой контракт | Реализация/доказательство |
|---|---|---|
| R01 | Precheck равен type assertion Lookup: exact dynamic type для concrete T, Implements для interface T; nil interface не удовлетворяет prerequisite. | T01 |
| R02 | UserText classifier имеет ограниченный обход value/type, cyclic/unclassifiable даёт typed SystemFault, zero delivery даже при разрешённом technical kind. | T02 |
| R03 | Clean pass не получает violation-only Fatal/Retryable/SafeUserMessage. Явный caller fatal-pass сохраняет deny. | T03 |
| R04 | Ровно MaxUnitBytes без delimiter ждёт Complete или доказанного overflow; лимит включает newline; pending не растёт сверх bound. | T04 |
| R05 | Unit JSON normal/transformed/fallback допускает только object/array; whole-response JSON отдельно допускает любой valid JSON. Invalid unit никогда не достигает writer. | T05 |
| R06 | JSON object keys sorted, arrays по индексу; correction/deny не прекращают проверку siblings, fault/cancel прекращает; агрегируются только завершённые проверки, fault > deny > correction > none. Equal-rank code выбирается по детерминированному обходу. | T07 |
| R07 | Completed observations передаются отдельным типизированным error evidence; произвольный report рядом с callback error не признаётся успешным. Adapter и pipeline сохраняют prior kind; boundary подавляет transformed output. | T06/T07 |
| R08 | Quick Start использует guarded delivery Projection; low-level пример явно обрабатывает SystemFault и Go error. Только deliverable projection достигает sink. | T11 |
| R09 | Returned T / RunResult.Output — authoritative value; MutatedText только diagnostic mirror. | T11 |
| D01 | Generic delivery требует явные allowed kinds и caller classifier/canonicalization contract; UserText — явный opt-in recipe с ограниченным supported wire representation. Shape не доказывает безопасность custom serialization; core не вызывает MarshalJSON. Unknown не становится safe по default. | T02 |
| D02 | Единственный GuardedDelivery[T], GuardOutput helper возвращает его; дублирующий GuardedOutput удаляется, consumers мигрируют. | T02 |
| D03 | Сохраняется nongeneric policy, но fallback compatibility и typed nil проверяются до Run; несовместимость — ConfigurationError, не молчаливое suppression. nil без типа означает отсутствие fallback. | T02 |
| D04 | RouteDecision остаётся stateless projection host counters, без выполнения retries; negative attempts/max rejected явно. Fallback route только предложение для отдельно проверяемой delivery. | T08 |
| D05 | Report fields сохраняются: Action описывает transformation, Disposition canonical control; Fatal/Retryable/Shadow — metadata/control inputs через FinishReport. Invalid combinations fail-closed; второй DTO не вводится. | T08 |
| D06 | Публичные WithSequential/WithParallel и phase labels sequential/policy/parallel; старые Fast/Slow удаляются. Изменение labels отражается в migration и OTel примерах. | T08 |
| D07 | Удаляются NewPolicyFunc/NewAttributePresent и их private legacy helpers без wrappers; callers используют typed APIs, ExecutionScope.Lookup остаётся BYOT. | T01 |
| D08 | Build operands и значения StaticScope borrowed immutable, без reflective copier. Host не меняет aliased data после compile/bind и при sharing; обновляет facts/config заменой scope/pipeline целиком. Контракт не обещает snapshot. | T09 |
| D09 | Sharing safety условна: validators, middleware, scopes, observer и алиасы caller-owned/thread-safe; Use копирует конфигурацию, не внутреннее состояние. MapSlice getters/setters не мутируют input aliases; rollback не обещается. | T09 |
| D10 | Low-level Run сохраняет report-only SystemFault с nil error, явно описывает оба канала; high-level boundaries exhaustive и fail-closed, сохраняют cause/kind. | T08/T11 |
| D11 | Validate callbacks всех фаз panic → безопасный validator SystemFault; original panic detail только explicit cause inspection, не public Error. Middleware construction panic остаётся явным construction panic; runtime observer/host sink panic contract описывается отдельно, не выдаётся за Validate recovery. | T08 |
| D12 | Fallible pipeline/built-in construction с явными Must wrappers; nil rules/options и deterministic invalid combinations отвергаются до processing. Неиспользуемые options отвергаются для соответствующего validator, без silent coercion. Custom option и middleware construction callbacks должны быть корректны; framework не строится. | T03/T08 |
| D13 | NewLength и Must используют один validation: min/max >= 0, 0 отключает сторону, положительные min>max invalid. Nil classifier/nonfinite или out-of-range threshold — construction error. | T03 |
| D14 | TokenVault.Store возвращает (token,error); configured vault error/panic/empty/identity — fault и zero delivery. Без vault redaction явно irreversible; скрытого degradation при настроенном vault нет. Lifecycle/ACL host-owned. | T03 |
| D15 | Regex outcome определяется match, включая identity/empty/zero-width; изменение bytes не является detection predicate. Replacement safety остаётся caller contract. | T03 |
| D16 | TagPatternValidator/ClassifierValidator и соответствующие constructors; старые misleading names удаляются. Detector limits и отсутствие trained model описываются. | T03 |
| D17 | MaxUnitBytes ограничивает input и итоговый transformed/fallback unit; expansion сверх bound отвергается до writer. Whole-response имеет свой общий budget. | T05 |
| D18 | Partition independence гарантирована для допустимого input; oversized Write admission атомарно отклоняется, ранее released prefixes irreversible. Overbudget partitions не обязаны выпускать одинаковый prefix. | T04 |
| D19 | Stream mutex и cooperative cancellation сохраняются; validators/writer/observer обязаны завершаться и не reenter stream. Abort/Outcome могут ждать callback; detached timeout workers не вводятся. | T04 |
| D20 | OTel setup возвращает error и сохраняет cause instrument creation; setup не меняет бизнес validation. Runtime telemetry privacy/default behavior сохраняются. | T10 |
| D21 | Safe guarded wrapper основной consumer recipe; WrapOutput явно low-level partial-result-on-handler-error, без retry/undo side effects. | T10/T11 |
| D22 | HTTP validated configurable max body, default 1MiB, overflow 413; consumed Body wrapper закрывается и заменяется; ownership/status/error table и tracking ReadCloser regression. | T10 |
| D23 | Optional pinned jsonschema engine и safe-number graph сохраняются: exact-number/overflow нужны consumer contract. Upgrade probes и dependency boundary проверяются. | T09/T12 |
| D24 | BoundaryProfile сохраняется как declared coverage; enforcement доказывают реальные handler/sink integration fixtures, не декларация. | T09/T10 |
| D25 | Сохраняется release prepare→verify→publish tooling: exact refs/19-module graph и fixtures нужны independent consumer. В задаче только isolated prepare/verify, без публикации. | T12 |

### Последовательные этапы и критерии приёмки

Каждый нумерованный критерий учитывается в знаменателе completeness. Связанные
исходные AAA-кейсы/оговорки обязательны внутри соответствующего критерия.

**T00 — contracts/план** (принят; commit `docs: remediation plan`).
1. Все R01–R09 и D01–D25 имеют явное решение и этап; нет неподтверждённого статуса реализации.
2. Последовательность зависимостей, acceptance gates, evidence и commit workflow зафиксированы.
3. План покрывает исходные docs checklist, DoD, benchmarks и isolated candidate/consumer verification.

**T01 — typed scope** (ожидает T00; commit `fix: scope types`; R01, D07).
1. Concrete named map/slice/struct/function reject до callback с ErrScopeIncompatible/SystemFault; exact types/interface implementation/typed nil корректны.
2. Run, guarded boundary и PolicyFailure сохраняют fault category; AAA regressions доказывают baseline defect.
3. Deprecated untyped constructors/helpers удалены; tests/examples/build/docs используют typed APIs; low-level Lookup сохранён.
4. Godoc/CONTRACTS и migration актуальны; релевантные core/build/example tests проходят.

**T02 — delivery** (ожидает T01; commit `refactor: delivery`; R02, D01–D03).
1. Один GuardedDelivery и migrated consumers, explicit generic destination contract + UserText recipe, supported representation docs.
2. Value cycle и nil pointer type cycle завершаются typed fault, no delivery; finite subprocess watchdog и ordinary pointer/nil/struct/bytes matrix, включая technical policy.
3. Configuration validation до callbacks, mismatch fallback/typed nil documented и tested; separately checked fallback, no fault masking, kind/cause/Projection согласованы.
4. Core/integration/examples tests и API migration актуальны.

**T03 — built-ins** (ожидает T02; commit `fix: validators`; R03, D12–D16).
1. Все семь affected built-ins clean-pass/violation Fatal, Retryable, SafeUserMessage matrix; manual fatal-pass deny не ослаблен; baseline regression.
2. Fallible constructors + Must, nil options/detectors/threshold/length/unsupported option combinations deterministic validation; no silent Action coercion.
3. Error-returning vault и failure matrix (error/panic/empty/identity), no implicit irreversible degradation; успешные token/restore consumers migrated.
4. Regex match-based identity/empty/zero-width, names TagPattern/Classifier и detector limitation docs; ext/examples checks PASS.

**T04 — stream input/liveness** (ожидает T03; commit `fix: stream limits`; R04, D18–D19).
1. Exact-limit tail во всех разбиениях Complete → одна выдача; следующий byte/newline overflow → zero writes, pending==unit bound; baseline regression.
2. Меньший tail/newline unit/JSON exact-limit и bounded buffering regressions проходят.
3. Partition guarantee только допустимым inputs, irreversible overbudget prefixes и mutex/cooperative cancellation/reentry contract согласованы в StreamConfig/README/CONTRACTS/STREAM_MEASUREMENTS.
4. Targeted race/partition/work tests, baseline и changed partition/work benchmarks сохранены без недоказанного perf claim.

**T05 — stream output/framing** (ожидает T04; commit `fix: stream framing`; R05, D17).
1. Number/bool/null/string/object/array normal/transformed/fallback matrix, одинаковый unit object/array framing, invalid zero writes; baseline repro.
2. Input+output MaxUnitBytes проверен для expansion/fallback до writer; budget docs согласованы.
3. Mandatory faults не становятся fallback, whole-response JSON отдельный contract не повреждён.
4. Stream race/partition/work tests и релевантные changed benchmarks сохранены.

**T06 — completed observations** (ожидает T05; commit `fix: fault evidence`; R07).
1. Типизированный completed evidence contract не доверяет failed callback report/output; MapSlice сохраняет только prior successful observations.
2. Technical/internal + error/wrapped cancellation согласованы direct/RunResult/boundary/PolicyFailure; first fault default; transformed output suppress; AAA baseline repro.
3. Все фазы pipeline и recursive adapters проверены, failure/cancellation cause сохранён; contracts/tests актуальны.

**T07 — JSON aggregation** (ожидает T06; commit `fix: json decisions`; R06, R07).
1. Sorted keys/index traversal и strongest completed outcome; mixed retry/deny/fault nested/source order/repeated evaluations deterministic, including diagnostic code.
2. Mandatory fault/cancel сохраняют исходный документ и prior kind через T06 evidence; shadow не скрывает fault; AAA baseline repro.
3. Optional adapter tests, contracts и examples PASS; no domain dependencies.

**T08 — core API/config/faults** (ожидает T07; commit `refactor: pipeline`; D04–D06, D10–D12).
1. Fallible construction/Must и nil config validation; consumers migrated; invalid report combinations fail-closed, сохранение Report обосновано контрактом.
2. Sequential/policy/parallel API/labels едины, Fast/Slow legacy удалён; migration включает serialized/telemetry labels.
3. Validate panic all-phase → safe SystemFault, cause explicit, no pass; runtime/construction callback boundaries documented и tested.
4. Route negative inputs validated/stateless, fallback suggestion требует checked delivery; low-level two fault channels/high-level exhaustive behavior regression matrix.
5. Core/integration/build/OTel consumers compile и tests PASS.

**T09 — ownership и сохранённые boundaries** (ожидает T08; commit `docs: ownership`; D08–D09, D23–D24).
1. Borrowed immutable build operands/StaticScope, sharing conditions/Use/no rollback/ScopeFactory transient facts явно в Godoc и CONTRACTS; reload recipe и regression не обещают snapshot.
2. MapSlice alias precondition виден; authorization/rollback/host lifecycle явно вне core.
3. JSON-schema pinned optional exact-number/overflow contract и upgrade probes сохранены; BoundaryProfile declared-only и fixture enforcement различены.
4. Build/schema/core relevant tests PASS; конкретные consumer причины сохранения D23/D24 задокументированы.

**T10 — integrations/setup** (ожидает T09; commit `fix: adapters`; D20–D22, D24).
1. Fake meter sentinel creation error обнаруживается setup, cause сохранён, бизнес-validation/telemetry privacy не меняется скрыто.
2. HTTP configurable validated cap/default/status 413/ownership table, tracking ReadCloser и extractor/injector/cancel/error/replace matrix.
3. Safe guarded wrappers/sinks integration fixtures suppress fault/partial result; WrapOutput low-level contract explicit, no side-effect undo.
4. Integration/OTel/HTTP tests PASS, migration актуальна.

**T11 — consumer docs** (ожидает T10; commit `docs: contracts`; R08–R09, D10, D21 + весь исходный docs checklist).
1. Исполняемые pass/redact/deny/retry/report-only fault/Go error consumer examples: только approved value reaches sink; redactor authoritative T/struct/Map/HTTP bytes проверены.
2. README текущий guide и единая versioned MIGRATION, legacy/v2-style removed или объяснён; все изменённые APIs/labels/serialized contracts отражены.
3. Inventory включает jsonredact, optional install/import и root-only dependency сценарий; honest detector/telemetry/benchmark limitations сохранены.
4. Все исходные docs checklist пункты сверены с API и отмечены только по доказательствам; runnable examples/test checks PASS.

**T12 — итоговая верификация** (ожидает T11; commit `test: remediation`; все R/D/DoD).
1. make test/make lint всего текущего module inventory (baseline 19), необходимые race/finite watchdog и stream benchmarks against baseline, logs сохранены.
2. Изолированный prepare/verify реального candidate из финального API, exact refs/module graph и independent consumer; release tooling fixture tests PASS, origin не публикуется.
3. Requirement-by-requirement audit всех R/D/DoD/docs checklist: результат→доказательство→commit, каждое сохранение обосновано; no hidden fallback/coercion/domain dependencies.
4. Два независимых итоговых PASS, clean committed worktree и итоговый отчёт; goal complete только после этого.

### Журнал

| Этап | Полнота | Корректность | Evidence | Commit |
|---|---|---|---|---|
| T00 | PASS 100% (3/3) | PASS, подтверждённых ошибок нет | [полнота](task27-evidence/t00-completeness.md), [корректность](task27-evidence/t00-correctness.md) | `docs: remediation plan` (hash в следующем этапе) |
