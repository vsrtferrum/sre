# HW2 - runtime feature flag `new-greeting`

## Что реализовано

Приложение из HW1 расширено runtime-флагом `new-greeting`. Флаг хранится в `atomic.Bool`, поэтому изменение применяется следующим запросом без перезапуска процесса или Pod.

| Endpoint | Доступ | Результат |
| --- | --- | --- |
| `GET /message` | Service `:8080` | `{"message":"hello"}` при выключенном флаге; новый текст при включенном |
| `GET /feature-status` | Service `:8080` | текущее состояние флага |
| `GET /admin/feature?enabled=true\|false` | только admin-порт `:8081` | включает или выключает флаг |
| `GET /admin/feature/status` | только admin-порт `:8081` | текущее состояние флага |

Админский порт не включен в Service и не имеет NodePort. В локальном кластере он проверяется через `kubectl port-forward pod/... 18081:8081`; в интернет endpoint не выставляется.

По умолчанию `new-greeting=false`. Некорректное значение `enabled` возвращает HTTP 400.

## Локальный запуск и тесты

```bash
cd homework/hw2/app
go test ./...
docker build --build-arg APP_VERSION=$(git rev-parse --short HEAD) -t hw2-app:local .
```

Тесты проверяют начальное выключенное состояние, включение, выключение и некорректное значение параметра.

## CI

`.github/workflows/ci.yml` запускается на каждом `push` и `pull_request`. Он выполняет `go test ./...`, затем собирает Docker-образ с тегом, содержащим `GITHUB_SHA`.
```text
https://github.com/vsrtferrum/sre/actions/runs/42620782053/job/129172054788
```

## Выкладка в локальный Kubernetes

```bash
cd homework/hw2
docker build -t hw2-app:local app
kind load docker-image hw2-app:local --name hw1
kubectl apply -f kubernetes/deployment.yaml
kubectl rollout status deployment/hw2-app --timeout=120s
kubectl get pods -l app=hw2-app -o wide
```

Для проверки выбирается один Pod и фиксируется его UID:

```bash
POD=$(kubectl get pod -l app=hw2-app -o jsonpath='{.items[0].metadata.name}')
kubectl get pod "$POD" -o jsonpath='before pod={.metadata.name} uid={.metadata.uid} restarts={.status.containerStatuses[0].restartCount}{"\n"}'
kubectl port-forward "pod/$POD" 18080:8080 18081:8081
```

В другом терминале:

```bash
curl -s http://127.0.0.1:18080/message
# ожидается: {"message":"hello","new_greeting":false}

curl -s 'http://127.0.0.1:18081/admin/feature?enabled=true'
curl -s http://127.0.0.1:18080/message
# ожидается: {"message":"hello from the new greeting","new_greeting":true}
curl -s http://127.0.0.1:18080/feature-status

curl -s 'http://127.0.0.1:18081/admin/feature?enabled=false'
curl -s http://127.0.0.1:18080/message
# ожидается: {"message":"hello","new_greeting":false}
```

После переключений:

```bash
kubectl get pod "$POD" -o jsonpath='after pod={.metadata.name} uid={.metadata.uid} restarts={.status.containerStatuses[0].restartCount}{"\n"}'
```

Фактический запуск в kind `hw1` дал:

```text
before pod=hw2-app-bd9bf6cff-g9fmn uid=018794ac-8fa6-401f-82ca-cebbba0df089 restarts=0
GET /ready: 200
before: {"message":"hello","new_greeting":false}
enable: {"new_greeting":true,"updated":true}
after-enable: {"message":"hello from the new greeting","new_greeting":true}
status: {"new_greeting":true}
disable: {"new_greeting":false,"updated":true}
after-disable: {"message":"hello","new_greeting":false}
invalid-status: 400
after pod=hw2-app-bd9bf6cff-g9fmn uid=018794ac-8fa6-401f-82ca-cebbba0df089 restarts=0
```

## Вывод

Выкладка новой версии меняет образ и обычно приводит к созданию новых Pod через rollout. Feature flag меняет поведение уже работающего процесса: образ, Deployment и Pod остаются прежними, а переключение применяется к следующему запросу. Ограничение этого решения - состояние хранится отдельно в каждом Pod; для нескольких реплик управляющий запрос должен попасть в каждый Pod либо состояние нужно вынести в общее хранилище. Поэтому admin API оставлен только для локального учебного кластера и не должен использоваться как production control plane.
