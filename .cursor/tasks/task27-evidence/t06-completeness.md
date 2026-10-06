# T06 — независимая приёмка полноты

Reviewer: `/root/t06_completeness`. Проверена окончательная версия diff относительно `98a52c2`, включая новые файлы; реализация reviewer не изменялась. Заключение другого reviewer не использовалось.

| Критерий T06 / R07 | Проверенное доказательство | Выполнен |
| --- | --- | --- |
| 1. Типизированный completed evidence contract; failed report/output не становятся successful evidence; MapSlice сохраняет только prior observations | `CompletedObservationsError` хранит private cause/snapshot, `CompletedReportFromError` обходит все joined branches; snapshots очищают MutatedText. `MapSlice` и mapping adapters игнорируют raw failed report. `TestCompletedObservationsSnapshotAndJoinedCauses`, `TestFailedReportsAreNotCompletedObservations`, `TestMappingFaultProjectsAttestedHistoryWithoutInjection` проверяют copies, safe error string, absent evidence, first-fault default и отсутствие injection. | Да |
| 2. Prior technical/internal + error/wrapped cancellation согласованы direct/RunResult/boundary/PolicyFailure; transformed output подавлен; AAA baseline | `TestMapSliceCompletedClassificationSurvivesFault`: оба kind × error/wrapped cancellation × fast/policy/slow; direct возвращает original input, boundary не выдаёт value и содержит SystemFault, PolicyFailure сохраняет kind/cause. Baseline на `98a52c2` воспроизводит все 12 failures; итоговая regression PASS. JSON matrix аналогично проверяет direct, Run и boundary. First fault без attestation остаётся safe_user_text в отдельном all-phase тесте. | Да |
| 3. Все фазы и recursive adapters; failure/cancellation causes сохранены; contracts/tests актуальны | Pipeline fast/policy/slow потребляет carriers; nested Pipeline передаёт свою completed history; slow sibling cancellation сохраняет evidence без искусственного fault. MapSlice, JSON recursive walk, Map и MapJSONRawMessage проверены. Новые direct AAA MapSlice/JSON cases подтверждают сохранение одновременно независимого detector error и context cancellation. CONTRACTS/MIGRATION фиксируют trusted-code attestation, snapshots, suppression, no fallback и ограничения. | Да |

Полнота: **3/3 = 100%**. Пробелов в пределах T06/R07 не обнаружено.

Проверки окончательной версии:

- Независимый targeted `go test -race` root: completed evidence, all phases, nested pipeline, sibling cancellation, mapping, MapSlice matrix и concurrent error/cancel — PASS.
- Независимый targeted `go test -race` optional JSON module: classification matrix и concurrent error/cancel — PASS.
- `t06-modules.txt`: race tests всех 19 модулей, `ALL 19 MODULES PASS`.
- `t06-all-lint.txt`: `make lint`, все 19 модулей, 0 issues; завершение exit 0 подтверждено parent и прочитан финальный log.
- `git diff --check` — PASS.

Не засчитывались будущие задачи: T07 deterministic traversal/strongest JSON aggregation, T08 central panic recovery. Приёмка полноты не является доказательством абсолютного отсутствия ошибок или принятия ещё не выполненных задач.
