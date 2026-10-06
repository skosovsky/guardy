# T00: независимая приёмка полноты

Вердикт: **PASS — 100% (3/3 критериев T00)**.

Проверен текущий `.cursor/tasks/task27-guardy-review-remediation.md`: исходные требования и добавленный план T00–T12. Реализация не проверялась и не объявляется завершённой. Чужие отчёты не читались.

| Критерий T00 | Доказательство | Выполнен |
|---|---|---|
| 1. Все R01–R09 и D01–D25 имеют явное решение и этап; нет неподтверждённого статуса реализации. | «Контрактные решения» содержит все 34 ID с выбранным поведением и этапами. Шапка отмечает T00 на приёмке; вступление плана явно отличает целевой контракт от текущей реализации; T01–T12 ожидают предшественника. | Да |
| 2. Последовательность зависимостей, acceptance gates, evidence и commit workflow зафиксированы. | «Правила исполнения»: строго T00→…→T12, один открытый этап, контракт/AAA baseline до изменения; два новых независимых проверяющих, completeness только 100%, correctness без подтверждённых нерешённых ошибок, обе повторные приёмки после исправлений. Все этапы имеют короткое commit message; evidence и журнал определены. | Да |
| 3. План покрывает исходные docs checklist, DoD, benchmarks и isolated candidate/consumer verification. | Матрица ниже покрывает каждый пункт исходного checklist и DoD. T04/T05/T12 требуют stream benchmarks относительно baseline; T12.2 требует isolated prepare/verify финального candidate, exact refs/module graph, independent consumer и fixture tests. | Да |

## Покрытие R/D

| Исходные требования | Этапы и конкретные критерии |
|---|---|
| R01, D07 | T01.1–4: named concrete types/interface, classifications, typed migration/deprecation removal. |
| R02, D01–D03 | T02.1–4: canonical delivery, explicit destination/classifier contract, finite cycle watchdog, fallback compatibility/typed nil, migration. |
| R03, D12–D16 | T03.1–4: clean-pass option matrix, fallible constructors/Must, length/detector validation, vault failures, match-based regex, naming/limits. D12 дополнительно T08.1. |
| R04, D18–D19 | T04.1–4: exact-limit/all partitions, overflow zero writes, pending bound, liveness/reentry/partition contracts, race и benchmarks. |
| R05, D17 | T05.1–4: normal/transformed/fallback JSON matrix, input/output budgets, no mandatory fault fallback, separate whole-response contract. |
| R07 | T06.1–3 и T07.2: prior successful observations через direct/RunResult/boundary/PolicyFailure, error/cancel, recursive adapters, output suppression. |
| R06 | T07.1–3: deterministic traversal/strongest completed outcome, nested/repeated/order tests, original payload при mandatory failure, shadow/cancel. |
| D04–D06, D10–D12 | T08.1–5: report configuration, phase rename/labels, runtime panic contract, route validation и два fault channels. D10 также T11.1. |
| D08–D09, D23–D24 | T09.1–4: immutable borrow, sharing/alias contracts, reload/ScopeFactory, optional schema pin/probes, declaration vs enforcement; D24 fixtures T10.3. |
| D20–D22 | T10.1–4: setup sentinel/cause, HTTP cap/status/body lifecycle/matrix, safe wrappers и partial result semantics. D21 также T11.1. |
| R08–R09 | T11.1–4: runnable exhaustive routing, authoritative returned T/struct/Map/HTTP sink bytes, current guide/migration. |
| D25 | T12.2: retained tooling, exact refs/graph, fixtures и isolated candidate/independent consumer verification. |

Все **9/9 R и 25/25 D** сопоставлены этапам. Сохранённые решения D04/D05/D08/D09/D10/D19/D23/D24/D25 выбраны явно; конкретное потребительское обоснование сохранений остаётся обязательным результатом T09/T12, а не считается уже выполненным.

## Покрытие исходного docs checklist

| Пункт в исходном порядке | Этап/доказательство покрытия |
|---|---|
| 1. R08/R09, authoritative output, channels, Projection | T11.1 и T08.4. |
| 2. Concurrency/ownership, mutation, transient facts, no authorization/rollback | T09.1–2. |
| 3. HTTP limits/status/ownership/cancel/extractor/injector | T10.2. |
| 4. Current guide/versioned MIGRATION/v2-style clarification | T11.2. |
| 5. jsonredact inventory, optional import/install, root-only | T11.3. |
| 6. Stream bounds/expansion/fallback/partitions/lock/cancel | T04.1–3 и T05.1–3; T11.4 сверяет весь исходный checklist. |
| 7. Detector/statistical quality/telemetry/benchmark limitations | T03.4, T10.1, T11.3. |
| 8. Single migration for D02/D06/D16, labels/serialized contracts, legacy deletion | T01.3–4, T02.1/4, T03.4, T08.2, T11.2. |

Покрытие checklist: **8/8**.

## Покрытие DoD и исходного порядка приёмки

| Требование | Этап/правило |
|---|---|
| Все девять R исправлены с воспроизводимой AAA-приёмкой | T01–T07/T11; baseline regression правило; T12.3 полный audit. |
| Все D реализованы либо конкретно обоснованно сохранены | Таблица 34 решений, T09.4, T12.3. |
| Нет скрытого fallback/config coercion | T02.3, T03.2–3, T08.1, T12.3. |
| Docs/API/examples согласованы, migration и логи приложены | Поэтапные docs/checks, T11.2–4, evidence workflow, T12.1/3–4. |
| Fault не допускает delivery; отклонённый исходный payload не выпускается | T02.2–3, T05.1/3, T06.2, T07.2, T08.3–4, T10.3, T11.1; T12.3 включает полный DoD. |
| Fallback отдельно проверен и разрешён явным контрактом | T02.3, T05.1/3, T08.4; исходные обязательные требования явно сохранены. |
| BYOT/optional boundaries, без обязательных harness/domain dependencies | План сохраняет исходные «Границы библиотеки», T07.3, T09.3–4, T11.3, T12.3. |
| make test/make lint всех модулей, race/finite watchdog | T12.1; релевантные промежуточные gates T01–T10. |
| Stream partition/work benchmarks против baseline | T04.4/T05.4/T12.1, без недоказанного perf claim. |
| Изолированный candidate prepare/verify и independent consumer при API/packaging изменениях | T12.2, без origin publish. |
| Старые зелёные тесты не заменяют новые regressions | Правило baseline AAA; каждый R назначен конкретному regression критерию. |

Пробелы в полноте плана: **нет**.

Git evidence: файл первоначально игнорировался Git, после force-add проверен `git diff --cached`: новый task-файл, 345 строк. Рабочая версия совпадает с индексом (unstaged diff отсутствует). Исходные требования и план прочитаны целиком. T00 commit ещё не создан и не объявляется выполненным; после двух приёмок workflow требует добавить evidence и сделать отдельный commit.
