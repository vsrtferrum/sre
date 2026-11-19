# HW3 - экспортированные запросы и evidence

Этот файл содержит воспроизводимые запросы для проверки метрики checkout,
трейсов, логов и алертов. Запросы выполняются после запуска:

```bash
cd homework/hw3
docker compose up --build -d
```

Дата эксперимента: `2026-11-19 23:47–2026-11-20 00:25 MSK`  
Ветка: `lab3`  
PR: не создан в локальной рабочей среде; добавить ссылку после push.

## 1. Проверка метрики backend

### Все результаты checkout

VictoriaMetrics → Explore:

```promql
sum by (result, outcome_type) (
  backend_checkout_attempts_total
)
```

Ожидаемые bounded values:

```text
paid                         / success
card_declined                / business_decline
issuer_timeout               / technical_failure
issuer_unavailable           / technical_failure
db_unavailable               / technical_failure
payment_service_unavailable  / technical_failure
out_of_stock                 / business_rejection
```

Фактический результат после эксперимента:

```text
result=paid, outcome_type=success              2
result=card_declined, outcome_type=business_decline 2
result=payment_service_unavailable, outcome_type=technical_failure 3

# VictoriaMetrics, финальный запрос 2026-11-19 21:12 UTC:
{
  "card_declined/business_decline": 15,
  "paid/success": 8,
  "payment_service_unavailable/technical_failure": 3
}
```

### Проверка отсутствия высококардинальных labels

```promql
count by (result, outcome_type) (
  backend_checkout_attempts_total
)
```

У метрики должны присутствовать только `result` и `outcome_type` из
описанного ограниченного множества. `user_id`, `order_id`, `request_id` и
`product_id` в labels отсутствуют.

CLI-вариант:

```bash
curl -s http://localhost:8081/metrics \
  | grep -E 'backend_checkout_(attempts_total|duration_seconds)'
```

## 2. Нормальная покупка

Действие: войти как `alice`, добавить товар в корзину, нажать «Купить».

Запрос:

```promql
increase(backend_checkout_attempts_total{result="paid"}[10m])
```

Ожидание: значение увеличилось минимум на `1`.

Проверка длительности:

```promql
histogram_quantile(
  0.95,
  sum by (le) (
    rate(backend_checkout_duration_seconds_bucket{
      result="paid"
    }[10m])
  )
)
```

Фактический результат: контрольный запрос после пересборки вернул HTTP 200,
заказ `id=11`, время `2026-11-19T21:06:55Z`. После восстановления и шести
дополнительных успешных запросов финальный счётчик содержит 8 наблюдений
`paid`; всего наблюдений: 15 `card_declined` и 3 технических отказа.

## 3. Технический отказ оплаты

В админ-панели задать:

```json
{
  "stage": "bank",
  "fail_rate": 1,
  "latency_ms": 0,
  "fail_kind": "issuer_timeout"
}
```

Сделать минимум 3 checkout-запроса.

Метрика:

```promql
sum by (result, outcome_type) (
  increase(backend_checkout_attempts_total[10m])
)
```

Проверка технической доли:

```promql
sum(rate(backend_checkout_attempts_total{
  outcome_type="technical_failure"
}[2m]))
/
clamp_min(sum(rate(backend_checkout_attempts_total[2m])), 0.001)
```

Фактический результат: в backend-метрике появились 3
`payment_service_unavailable/technical_failure`. В payment-логе исходная
причина каждого вызова - `issuer_timeout`; backend намеренно нормализует
техническую недоступность зависимости в `payment_service_unavailable`.
`card_declined` в техническую долю не входит.

## 4. Алерт технического сбоя

Проверка правила:

```promql
(
  sum(rate(backend_checkout_attempts_total{
    outcome_type="technical_failure"
  }[2m]))
  /
  clamp_min(sum(rate(backend_checkout_attempts_total[2m])), 0.001)
) > 0.20
and
sum(increase(backend_checkout_attempts_total{
  outcome_type="technical_failure"
}[2m])) >= 3
```

Проверка состояния vmalert:

```bash
curl -s http://localhost:8880/api/v1/rules \
  | jq '.data.groups[] | .rules[]
    | select(.name == "CheckoutPaymentsFailing")
    | {name, state, activeAt, alerts}'
```

Фактически зафиксировано (время UTC, по alertreceiver):

```text
CheckoutPaymentsFailing:
  firing:  2026-11-19 21:09:24Z, value=75%
  resolved: 2026-11-19 21:11:24Z

Технический запрос: 3 x HTTP 402, result=payment_service_unavailable.
```

## 5. Две минуты без покупки

Проверка правила:

```promql
sum(increase(backend_checkout_attempts_total{result="paid"}[2m])) == 0
and sum(increase(backend_checkout_attempts_total[2m])) > 0
```

Это условие не срабатывает в тихое время: второе выражение требует хотя бы
одной попытки checkout.

Проверка состояния:

```bash
curl -s http://localhost:8880/api/v1/rules \
  | jq '.data.groups[] | .rules[]
    | select(.name == "CheckoutZeroPurchases")
    | {name, state, activeAt, alerts}'
```

Фактически зафиксировано (время UTC, по alertreceiver):

```text
CheckoutZeroPurchases:
  firing:  2026-11-19 21:09:34Z
  resolved: 2026-11-19 21:11:34Z

Условие attempts>0 выполнялось, поэтому тихое время без трафика не вызвало
alert; при техническом эксперименте paid не было в двухминутном окне.
```

## 6. Бизнес-отказ карты

Переключить chaos:

```json
{
  "stage": "bank",
  "fail_rate": 1,
  "latency_ms": 0,
  "fail_kind": "card_declined"
}
```

Сделать одну покупку.

Метрика:

```promql
increase(backend_checkout_attempts_total{
  result="card_declined",
  outcome_type="business_decline"
}[10m])
```

Проверка, что технический алерт не зацепил бизнес-отказ:

```promql
sum(rate(backend_checkout_attempts_total{
  outcome_type="technical_failure"
}[2m]))
```

Проверка circuit breaker:

```promql
backend_payment_circuit_breaker_state
```

Фактический результат контрольной серии: две попытки вернули HTTP 402 на backend, но payment
ответил HTTP 200 со статусом `declined`; метрика стала
`card_declined/business_decline=2`. После двух бизнес-отказов
`backend_payment_circuit_breaker_state=0` (`CLOSED`). Технический алерт на
этом сценарии не сработал.

Для проверки отдельного warning-алерта была выполнена расширенная серия: ещё
13 запросов с `card_declined`, всего 15 бизнес-отказов. После выдержки
`for: 2m` сработал `CheckoutCardDeclinesSpiking` со значением доли около
94–100%.
После возврата `fail_rate=0` и шести успешных покупок warning перешёл в
`resolved`. Ни один из этих отказов не попал в
`outcome_type="technical_failure"`; breaker оставался `CLOSED`.

Фактические trace IDs: `f49a3cb41ed53231cc8ea888ed3c999b` и
`58d983ca02d107b0b17dbf876226998b`.

## 7. Trace и log одного запроса

Найти request ID в логах:

```bash
docker compose logs --no-log-prefix backend payment \
  | grep -E 'checkout failed|checkout succeeded|request_id'
```

Фактический запрос для проверки связности:

```text
request_id: hw3-propagated-1795122415
trace_id:   000000000000000018e98e537316f339
```

Лог backend:

```bash
docker compose logs --no-log-prefix backend \
  | grep 'hw3-propagated-1795122415'
```

Лог payment:

```bash
docker compose logs --no-log-prefix payment \
  | grep 'hw3-propagated-1795122415'
```

В Grafana → Explore → Tempo открыть
`000000000000000018e98e537316f339` и показать цепочку:

```text
checkout.process
└── payment.request
    └── payment.charge
        └── bank.authorize
```

Для технического сбоя ожидается лог `checkout failed: payment service
degraded`; для карты - `checkout failed: card declined`.

## 8. Alertmanager и маршрутизация

Состояние активных алертов:

```bash
curl -s http://localhost:9093/api/v2/alerts \
  | jq '.[] | {labels, status, startsAt, endsAt}'
```

Ожидаемая маршрутизация:

```text
severity=critical → oncall-webhook
severity=warning  → ticket-webhook
group_by          → alertname, service
```

Проверить inhibition для одновременного технического и бизнес-алерта:

```text
source: CheckoutPaymentsFailing
target: CheckoutCardDeclinesSpiking
equal:  service=backend
```

Фактические timestamps firing/resolved и уведомления receiver:

```text
alertreceiver получил:
  CheckoutPaymentsFailing  firing  21:09:24Z
  CheckoutZeroPurchases    firing  21:09:34Z
  CheckoutPaymentsFailing  resolved 21:11:24Z
  CheckoutZeroPurchases    resolved 21:11:34Z
  CheckoutCardDeclinesSpiking firing  21:16:24Z (value≈93.75%)
  CheckoutCardDeclinesSpiking resolved 21:19:24Z

Оба критичных алерта маршрутизированы как severity=critical. Для единичного
card_declined уведомления CheckoutCardDeclinesSpiking не было: не достигнуты
минимум 10 попыток и устойчивый порог 70%.
```

## 9. Восстановление

Вернуть chaos в норму:

```json
{
  "stage": "bank",
  "fail_rate": 0,
  "latency_ms": 0,
  "fail_kind": "card_declined"
}
```

Сделать успешную покупку и дождаться `resolved` для технических алертов.
Финальная проверка:

```promql
increase(backend_checkout_attempts_total{result="paid"}[10m]) > 0
```

Результат восстановления: chaos возвращён в `fail_rate=0`; шесть контрольных
checkout-запросов вернули HTTP 200, `payment_approved` и `checkout succeeded`.
Один из них имел общий
`request_id=hw3-final-recovered-1795122685` и
`trace_id=c1a8a5fcd0d62cad2c5b38a2515e3ce0`. Финальная метрика содержит
`paid=8`, `card_declined=15`, `payment_service_unavailable=3`; breaker
state=0. После восстановления все три checkout-правила были в состоянии
`inactive`.
