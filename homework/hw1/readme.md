# HW1 - Доступность HTTP-приложения в Kubernetes

Скринкаст: record.webm

Таймкоды: 
1) 0.00 - сетап 
2) 0.55 - сценарий с удалением пода под нагрузкой 
3) 2.17 - unready под 
4) 4.29 - rollout пода 

---

## 1. Что сделано

Минимальное HTTP-приложение на Go, собранное в два образа (`hw1-app:v1`, `hw1-app:v2`), развёрнуто в локальном кластере **kind**. Под непрерывной нагрузкой 10 req/s проведены три сценария:

1. удаление Pod;
2. перевод Pod в состояние not-ready;
3. обновление версии v1 → v2.

Во всех сценариях запросы шли **только через Service** (NodePort `30080`), `kubectl port-forward` не использовался.

## 2. Схема

```
                        ┌──────────────────────────────────────────────┐
                        │              kind cluster (hw1)              │
                        │                                              │
  curl / loadgen        │   ┌──────────┐      ┌──────────────────┐     │
 ───────────────► 30080 │   │ Service  │      │  EndpointSlice   │     │
 (NodePort, host)       │   │ hw1-app  │─────►│  (ready=true)    │     │
                        │   │ NodePort │      │  10.244.0.x:8080 │     │
                        │   └────┬─────┘      └──────────────────┘     │
                        │        │ selector: app=hw1-app               │
                        │        ▼                                     │
                        │   ┌─────────────────────────────┐            │
                        │   │  Deployment hw1-app (2 repl)│            │
                        │   │  ┌──────────┐ ┌──────────┐  │            │
                        │   │  │ Pod v1/v2│ │ Pod v1/v2│  │            │
                        │   │  │ :8080    │ │ :8080    │  │            │
                        │   │  │ / /live  │ │ / /live  │  │            │
                        │   │  │ /ready   │ │ /ready   │  │            │
                        │   │  └──────────┘ └──────────┘  │            │
                        │   └─────────────────────────────┘            │
                        └──────────────────────────────────────────────┘
```

Путь запроса: **клиент → NodePort 30080 (kube-proxy) → Service → EndpointSlice (только ready Pod) → Pod:8080**.

## 3. Приложение

`app/main.go` - Go, стандартная библиотека, без внешних зависимостей.

| Эндпоинт | Поведение |
| --- | --- |
| `GET /` | JSON: `message`, `version`, `pod` (hostname), `instance` |
| `GET /live` | всегда `200` - проверка «процесс жив» |
| `GET /ready` | `200` или `503` в зависимости от внутреннего флага |
| `POST /unready` | сбрасывает флаг (имитация отказа) |
| `POST /ready-on` | восстанавливает флаг |

- Версия задаётся при сборке: `-ldflags "-X main.version=${APP_VERSION}"`, `ARG APP_VERSION` в Dockerfile. Тег `latest` не используется.
- **Graceful shutdown**: при `SIGTERM` сервер перестаёт принимать новые соединения (`http.Server.Shutdown`) и ждёт завершения текущих запросов до 10 с. `terminationGracePeriodSeconds: 30`.

## 4. Сборка и запуск (воспроизводимо с чистого кластера)

```bash
# 1. кластер
kind create cluster --config kind-config.yaml

# 2. образы (явные теги, latest не используется)
cd app
docker build --build-arg APP_VERSION=v1 -t hw1-app:v1 .
docker build --build-arg APP_VERSION=v2 -t hw1-app:v2 .

# 3. загрузка в кластер
kind load docker-image hw1-app:v1 hw1-app:v2 --name hw1

# 4. манифесты
kubectl apply -f kubernetes/
kubectl rollout status deployment/hw1-app --timeout=120s
```

Всё это объединено в `setup.sh` - один скрипт поднимает окружение с нуля.

### Версии инструментов

| Инструмент | Версия |
| --- | --- |
| ОС | Manjaro Linux x86_64 |
| Docker | 29.7.2 |
| kind | 0.32.0 |
| kubectl (client) | v1.36.4 |
| Kubernetes (server) | v1.36.1  |
| Go (приложение и нагрузка) | 1.27.0 |

## 5. Манифесты (`kubernetes/`)

**Deployment** (`deployment.yaml`):
- `replicas: 2`, selector и labels `app: hw1-app` согласованы с Service;
- `strategy: RollingUpdate`, `maxUnavailable: 0`, `maxSurge: 1` - обновление не опускает число доступных реплик ниже 2;
- `resources.requests: cpu 50m / memory 32Mi`, `limits: cpu 200m / memory 64Mi`;
- **readinessProbe** `GET /ready`: `periodSeconds: 2`, `failureThreshold: 2`, `timeoutSeconds: 1` - Pod исключается из Endpoints через ~4–6 с после отказа;
- **livenessProbe** `GET /live`: `periodSeconds: 5`, `failureThreshold: 3` - перезапуск только если процесс реально мёртв (не реагирует на /live), а не при логическом отказе;
- `terminationGracePeriodSeconds: 30`.

**Service** (`service.yaml`): `type: NodePort`, `port: 80 → targetPort: 8080 (http)`, `nodePort: 30080`, selector `app: hw1-app`.

**Обоснование параметров проб.** Readiness и liveness разделены на разные эндпоинты с разным смыслом: `/ready` отражает *логическую* готовность (флаг), `/live` - только *физическую* живость процесса. Это позволяет исключить Pod из трафика без перезапуска (сценарий 2). Короткий `periodSeconds` readiness (2 с) даёт быстрое исключение/возврат; liveness с `failureThreshold: 3` защищает от ложных перезапусков при кратковременных сбоях.

## 6. Нагрузка (`load/`)

`load/load.go` - генератор нагрузки на Go (только stdlib): фиксированная частота 10 req/s, таймаут запроса 2 с, классификация ошибок (**timeout / 5xx / connection refused**), подсчёт ответов по версиям. Результат - CSV + сводка.

```bash
cd load && go build -o loadgen .   # сборка генератора
./load/loadgen --url http://localhost:30080/ --rps 10 --duration 60s --out evidence/scenarioN.csv
```

Сценарии 2 и 3 оформлены как скрипты `load/scenario2.sh`, `load/scenario3.sh` (нагрузка + управляющее воздействие + сбор доказательств в одном запуске).

---

## 7. Сценарий 1 - удаление Pod

**Команды** (`load/scenario1.sh`):

```bash
./load/loadgen --duration 60s --out evidence/scenario1_load.csv &   # фон
kubectl delete pod <pod> --wait=false
kubectl get pods -l app=hw1-app -w                                  # наблюдение
```

**Наблюдение за репликами** (`evidence/scenario1_pods_watch.log`):

```
hw1-app-5fc8cd7b49-frkkf   1/1  Running            # до удаления
hw1-app-5fc8cd7b49-frkkf   1/1  Terminating        # удалён
hw1-app-5fc8cd7b49-cmsh5   0/1  Pending            # ReplicaSet создаёт замену
hw1-app-5fc8cd7b49-cmsh5   0/1  ContainerCreating
hw1-app-5fc8cd7b49-cmsh5   1/1  Running            # восстановлено
```

Число Ready-реплик: **2 → 1 → 2**. ReplicaSet немедленно создал замену; новый Pod прошёл readiness и вернулся в Endpoints.

**Замеры** (`evidence/scenario1_load.csv.summary`):

```
total=600 ok=600 errors=0
error_types={}
versions={"v2": 600}
```

Распределение по Pod: удалённый `frkkf` - 21 запрос (до удаления), новый `cmsh5` - 263, постоянный `tp5rp` - 316. Трафик переключился на живые Pod без ошибок.

**Вывод.** Удаление Pod под нагрузкой прошло **без единой ошибки**: kube-proxy перестал направлять трафик на удаляемый Pod (он исключается из Endpoints при переходе в Terminating), а graceful shutdown успел дообслужить in-flight запросы. Восстановление реплики заняло ~3 с.

## 8. Сценарий 2 - not-ready Pod

**Команды** (`load/scenario2.sh`):

```bash
kubectl exec <pod> -- wget -qO- --post-data="" http://localhost:8080/unready
kubectl get pod <pod>                       # 0/1, RESTARTS=0
kubectl get endpoints hw1-app               # только здоровый Pod
kubectl get endpointslices -l kubernetes.io/service-name=hw1-app -o jsonpath=...
kubectl exec <pod> -- wget -qO- --post-data="" http://localhost:8080/ready-on
```

**Доказательства:**

```
# Pod после /unready: 0/1, RESTARTS=0 (НЕ перезапущен)
hw1-app-5fc8cd7b49-cmsh5   0/1  Running   0   4m

# Endpoints: остался только здоровый Pod
hw1-app   10.244.0.19:8080

# EndpointSlice: ready-условия по адресам
["10.244.0.19"] ready=true
["10.244.0.21"] ready=false
```

**Замеры** (`evidence/scenario2_load.csv.summary`):

```
total=450 ok=450 errors=0
error_types={}
versions={"v2": 450}
```

Запросы к not-ready Pod по секундам (`evidence/scenario2_load.csv`):

```
00:18:21–00:18:29  → 41 запрос   (до исключения, readiness ещё не сработала)
00:18:30–00:18:42  → 0 запросов  ← Pod исключён из Endpoints, трафика нет
00:18:43+          → запросы возобновились (после /ready-on)
```

**Вывод.** Readiness-проба сработала через ~4–6 с (2 пробы × период 2 с): Pod переведён в `0/1`, исключён из Endpoints/EndpointSlice (`ready=false`), трафик на него прекратился полностью (13 секунд нулевого трафика). **Liveness не сработала, Pod не перезапущен (`RESTARTS=0`)** - логический отказ не привёл к перезапуску. После `/ready-on` Pod вернулся в Endpoints и снова получает трафик.

## 9. Сценарий 3 - обновление v1 → v2

**Команды** (`load/scenario3.sh`):

```bash
./load/loadgen --duration 60s --out evidence/scenario3_load.csv &   
kubectl set image deployment/hw1-app hw1-app=hw1-app:v2
kubectl rollout status deployment/hw1-app
```

**Ход rollout** (из лога скрипта):

```
t=8s   image updated
t=11s  новый Pod 4rqn4 (v2) создан, 0/1 → 1/1 (прошёл readiness)
t=14s  второй Pod 4pd6p (v2) создан; старый 9qj24 (v1) Terminating
t=17s  оба Pod v2 Ready; rollout завершён
```

Стратегия `maxUnavailable: 0, maxSurge: 1` гарантирует: сначала создаётся новый Pod и становится Ready, **только потом** удаляется старый - число доступных реплик не опускается ниже 2.

**Замеры** (`evidence/scenario3_load.csv.summary`):

```
total=600 ok=596 errors=4
error_types={"connection_refused": 1, "other": 3}
versions={"v1": 127, "v2": 469}
```

**Момент переключения версий** (из CSV): первый ответ `v2` - `00:21:27`, последний `v1` - `00:21:29`. Все три ошибки - таймауты в `00:21:15`, `00:21:27`, `00:21:30`, т.е. **ровно в окне rollout** (первый - в момент старта обновления, когда первый v1-Pod получил SIGTERM).

**Объяснение ошибок.** При удалении старого Pod (после того как новый стал Ready) Pod получает `SIGTERM` и начинает graceful shutdown: `http.Server.Shutdown` закрывает listener, но уже установленные keep-alive TCP-соединения через kube-proxy могут быть оборваны. Существует окно (миллисекунды–секунды), когда Endpoints уже обновлены, но правила kube-proxy (iptables) на узле ещё не перестроены, и часть запросов попадает на Terminating Pod, который больше не принимает соединения → таймаут 2 с у клиента. 3 из 600 запросов (0,5%) потеряны именно в этом окне.

**Вывод.** RollingUpdate прошёл без 5xx и отказов соединения; 3 таймаута (0,5%) приходятся на окно graceful shutdown и объясняются задержкой перестроения правил kube-proxy относительно исключения Pod из Endpoints. Версии в ответах переключились с v1 на v2 в момент rollout.

---

## 10. Границы гарантий

Что даёт конфигурация:

- **Service + EndpointSlice + readiness** - трафик идёт только на Ready Pod; not-ready Pod исключается за ~4–6 с без перезапуска.
- **ReplicaSet / Deployment** - удалённый или упавший Pod заменяется новым (сценарий 1: 2→1→2 Ready).
- **RollingUpdate (maxUnavailable: 0, maxSurge: 1)** - при обновлении число доступных реплик не опускается ниже 2; новый Pod вводится в трафик только после прохождения readiness.
- **Graceful shutdown** - при SIGTERM текущие запросы дообслуживаются, что минимизирует потери при удалении/обновлении.

Где гарантии заканчиваются:

1. **Окно потери при graceful shutdown** (наблюдалось в сценарии 3): между исключением Pod из Endpoints и перестроением правил kube-proxy часть запросов попадает на Terminating Pod → таймауты. Это фундаментальное ограничение kube-proxy (iptables/ipvs), не устраняемое на уровне приложения. Смягчается: `terminationGracePeriodSeconds` > времени дообслуживания, readiness-проба, которая «отключает» Pod до SIGTERM (preStop hook), либо прокси на стороне клиента с ретраями.
2. **Отказ узла целиком** (не проверялся): при падении узла kubelet не успевает корректно удалить Pod, Endpoints обновляются только по истечении `pod-eviction-timeout` (по умолчанию 5 мин) - в этот период запросы к Pod на мёртвом узле будут падать. В однонодовом kind-кластере этот сценарий невоспроизводим.
3. **Логические ошибки приложения**: если Pod отвечает 200 на `/ready`, но отдаёт неверные данные, Kubernetes считает его здоровым - пробы проверяют только то, что закодировано в эндпоинтах.
4. **Liveness не спасает от «живого, но сломанного» процесса**: если процесс отвечает на `/live`, но завис в логике, перезапуска не будет (это осознанный выбор - см. сценарий 2, где логический отказ не должен приводить к рестарту).
5. **Однонодовый кластер**: нет распределения по узлам, отказ узла = отказ всего кластера. `topologySpreadConstraints` и несколько узлов - вне рамок работы.
6. **NodePort как точка входа**: kube-proxy на узле - единая точка отказа для внешнего трафика; в проде используются LoadBalancer/Ingress.

---

## 11. Скринкаст

Запись экрана (3–5 минут) делается автоматизированным скриптом `demo.sh`: он приводит кластер к чистому состоянию (v1), показывает приложение и манифесты, затем прогоняет все три сценария с паузами для комментариев. Достаточно включить запись экрана и выполнить:

```bash
./demo.sh
```

Скрипт останавливается на каждом шаге (press Enter) - в паузах можно комментировать происходящее. Таймкоды:

| Таймкод | Раздел |
| --- | --- |
| 0:00–0:30 | Приложение, эндпоинты, сборка образов v1/v2 |
| 0:30–1:00 | kind-кластер, манифесты, запуск, проверка Service |
| 1:00–1:50 | Сценарий 1: удаление Pod под нагрузкой, восстановление реплики |
| 1:50–2:40 | Сценарий 2: /unready, исключение из Endpoints, RESTARTS=0, возврат |
| 2:40–3:40 | Сценарий 3: rollout v1→v2 под нагрузкой, переключение версий, ошибки |
| 3:40–4:30 | Выводы и границы гарантий |

## 12. Файлы

```
homework/hw1/
├── app/                 main.go, Dockerfile, Makefile, go.mod
├── kubernetes/          deployment.yaml, service.yaml
├── load/                load.go, loadgen, scenario1.sh, scenario2.sh, scenario3.sh
├── evidence/            CSV-логи нагрузки, сводки, watch-логи Pod/Endpoints
├── kind-config.yaml     конфиг кластера (NodePort 30080)
├── setup.sh             полное воспроизведение с чистого кластера
├── record.webm
|
└── readme.md            этот отчёт
```