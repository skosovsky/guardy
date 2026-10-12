# Приёмка: выполнено

Candidate SHA: `06b20d14a7c7c18a77b178e3662f659128d3cde5`.
Ветка: `codex/performance-memory-2026-10-12`.
Baseline: `0a99ffbd296c281b25da7a34acaeb3a029936f0a`.

Два независимых субагента приняли один final candidate:

- Reviewer A: **26/26 = 100% APPROVE**, все обязательные требования PASS; FAIL/NOT RUN/partial:0.
- Reviewer B: **APPROVE, найденных неисправленных ошибок нет**.

Verdicts: reviewer-a.md и reviewer-b.md. Exact-SHA verification: final-verification/verified.json. Source hashes совпадают с коммитом и замерами; tracked worktree чистый. Verdicts, этот итог и SHA-bound дополнительные логи являются локальными sidecars: они сохраняют SHA принятого коммита.

| Workload | allocs/op до → после | B/op до → после |
|---|---:|---:|
| Run,0 policies |4 →2|128 →32|
| Run,1 policy |6 →3|176 →80|
| Run,8 policies |13 →3|512 →80|
| Run,64 policies |69 →3|3328 →80|
| Bind,256 integers |294 →21|21041 →7195|
| Bind,4096 integers |4155 →32|475925 →171053|

Bytes — медианы10 samples, все allocation gates соблюдены. Убраны контексты пустых фаз и per-policy adapters без middleware; scoped factories по-прежнему создаются на каждом вызове. Typed Bind не строит временное дерево значений на валидном пути. Сохранены BYOT, syntax admission до custom callbacks, duplicate/escaped/Unicode key checks, precision, depth, cancellation, наблюдаемость и request isolation.

На exact SHA повторно PASS: uncached go test -race -count=1 ./... в root, ext/jsonschema, ext/jsonredact, build, ext/guardyotel, integration и integration/downstream; strict diagnostic/admission/callback fuzz30s. Root lint PASS0issues. Детерминированные stream work/capacity guards входят в root race.

Полный matrix count10 и дополнительные balanced ABBA20 серии не подтвердили статистически значимой деградации в проверенных смежных workloads. Начальные неисправности исправлены; исходные результаты, замечания и решения сохранены в round1/ и findings.md. Полные выводы: results.md; воспроизведение: README.md и run_*.py.

Релиз, consumer pins и зависимости не менялись. B/op отражает временные allocations, а не retained heap. Timing относится к локальным измерениям без SLA. Приёмка подтверждает проверенные требования и отсутствие найденных неисправленных ошибок; она не гарантирует отсутствие неизвестных дефектов.
