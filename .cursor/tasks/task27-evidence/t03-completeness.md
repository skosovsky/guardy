# T03 — независимая приёмка полноты

Проверяющая: отдельный субагент `t03_completeness`. Основа: текущий diff относительно
`1f53a40`, новые untracked contract tests, исходные R03/D12–D16 и четыре критерия T03.
Заключение другого проверяющего не читалось; код реализации не менялся.

Итоговый verdict после повторной проверки исправлений: **PASS, 100% (4/4)**.
Проверены финальный runtime diff, новые tests, Godoc, CONTRACTS/MIGRATION и
последние логи. Выявленные ниже пробелы устранены до приёмки.

| Критерий | Доказательства | Выполнен |
|---|---|---|
| 1. Семь clean/hit Fatal/Retryable/SafeUserMessage, manual fatal-pass, baseline | `ext/pass_flags_regression_test.go`, `ext/construction_contract_test.go`; `/tmp/guardy-task27/t03-baseline.log` подтверждает семь baseline failures. Clean `passReport` использует только ActionPass. Hit matrix, terminal guarded suppression и manual fatal-pass сохранены. TechnicalJSON hit semantics явно документированы. | Да |
| 2. Fallible/Must, nil options/detectors, finite threshold scale, bounds, unsupported options, no coercion | Семь constructors используют fallible setup; Must делегируют New; `semantic_construction_test.go` покрывает nonfinite и arbitrary finite шкалу; construction matrix покрывает nil options, invalid actions, typed nil detector и shared bounds. Explicit replacement вместе с configured vault теперь отвергается, обе PII/wordlist regressions добавлены. SemanticFixture возвращает настоящий nil при ошибке setup. | Да |
| 3. Error-returning vault, error/panic/empty/identity matrix, no degradation, successful consumers | `ext/vault_fault_contract_test.go`, token-vault tests, `examples/reversible_redaction/main_test.go`: fault matrix на PII/wordlist, original/no report, suppressed boundary, preserved storage causes, later partial transform discarded, zero-value storage, no-token allowlist restoration. Lifecycle/ACL/side effects явно host-owned. | Да |
| 4. Regex match-based cases, new names, limits docs, migrated modules | Identity/empty/zero-width matrix; legacy constructors удалены; TagPattern/Classifier naming и limits в Godoc/CONTRACTS/MIGRATION. `/tmp/guardy-task27/t03-modules.log`: все19модулейPASS, race. `/tmp/guardy-task27/t03-final-root.log`: финальный root/ext/guardytest racePASS. `/tmp/guardy-task27/t03-jsonschema.log`: optional schema racePASS. Финальный `/tmp/guardy-task27/t03-all-lint.log`: все19модулей0issues. | Да |

## Найденные пробелы и повторная приёмка

Выбранный D12 требует отвергать неиспользуемые options. Первоначально `validateRuleOptions`
принимал `WithTokenVault(validVault)` вместе с явно заданным
`WithRedactionReplacement("X")` для redaction PII/wordlist. При configured vault
`storeRedaction` всегда возвращает token либо fault; replacement ни в одной ветке
не использовался. Исправлено: combination отвергается с ConfigurationError,
добавлены PII/wordlist regressions и явный CONTRACTS пункт о взаимном исключении.

Также первоначальные MustLength/MustTagPattern Godoc описывали лишь часть новых
ошибок setup; синхронизированы с полным fallible construction. SemanticFixture
прямо конвертировал nil `*SemanticValidator` в ненулевой interface на ошибке;
теперь явно возвращает nil, err, с проверкой в fixture test. Форматирование,
выявленное полным lint, исправлено; финальный lint проходит. При повторном чтении
финального diff эти изменения подтверждены. Нерешённых пробелов T03 не осталось.

## Границы приёмки

Pipeline construction/phase API относится к T08, stream/aggregation/consumer-doc
дефекты — к последующим этапам. Они не объявлены закрытыми T03. Fallible semantic
construction сохраняет runtime fail-closed guards и caller-owned finite шкалу.
