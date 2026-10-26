# Secunda Test

REST API для управления задачами внутри команд: JWT, роли `owner / admin / member`, история изменений, комментарии, Redis-кеш и SQL-аналитика.

## Быстрый запуск

Нужны Docker Engine и Docker Compose v2. Для запуска контейнеров Go на компьютере не требуется.

```sh
cp .env.example .env
# Для любого окружения кроме локальной разработки замените секреты в .env.
# Случайный JWT_SECRET: openssl rand -hex 32
docker compose up --build -d --wait
```

Compose запускает MySQL и Redis, дожидается MySQL, применяет миграции отдельным контейнером и запускает приложение от непривилегированного пользователя. MySQL хранит данные в именованном volume. Порты БД и Redis наружу не публикуются; API доступен только на localhost.

На системах с отдельным бинарником Compose используйте `docker-compose` вместо `docker compose`, либо `make up COMPOSE=docker-compose`.

- API: [http://localhost:8080/api/v1](http://localhost:8080/api/v1)
- Swagger UI: [http://localhost:8080/swagger](http://localhost:8080/swagger)
- OpenAPI: [http://localhost:8080/openapi.yaml](http://localhost:8080/openapi.yaml)
- Liveness: [http://localhost:8080/healthz](http://localhost:8080/healthz)
- Readiness: [http://localhost:8080/readyz](http://localhost:8080/readyz)

Swagger UI использует закреплённую версию ресурсов с CDN; браузеру нужен доступ в интернет. Сам OpenAPI встроен в бинарник и доступен офлайн.

```sh
docker compose logs -f app
docker compose down            # остановить, сохранив данные
# docker compose down -v       # удалить также данные MySQL
```

## Архитектура

```text
cmd/app/                         точка входа, сигналы завершения
config/                          чтение и валидация env
internal/app/                    сборка зависимостей, жизненный цикл
internal/entity/                 сущности, статусы, ошибки, изменения полей
internal/usecase/                бизнес-правила, права доступа, порты
internal/controller/http/v1/     HTTP, DTO, JWT middleware, ошибки
internal/repo/persistent/        MySQL, транзакции, SQL-отчёт
internal/repo/cache/             Redis-адаптер списка задач
pkg/auth/                        bcrypt и JWT
pkg/logger/                      настройка slog, логгер запроса
pkg/mysql/                       соединение и пул MySQL
migrations/                      встроенные SQL-миграции
docs/                            OpenAPI, Swagger UI, решения
integration-test/                тесты с настоящими MySQL и Redis
live-test/                       HTTP-тесты отдельно запущенного сервера
```

Бизнес-логика не импортирует HTTP, SQL, Redis или JWT-библиотеку. Интерфейсы определены со стороны usecase; инфраструктура их реализует. Обычные репозитории не предоставляют транзакционные блокировки и отдельную запись истории. Внутри транзакционного порта `TaskWriter` сохраняет задачу вместе с историей и ревизией, а `TeamWriter` — команду вместе с владельцем; бизнес-проверки остаются в usecase. HTTP принимает локальные потребительские интерфейсы и тестируется через `ServeHTTP` без БД. SQL остаётся в репозиториях, HTTP DTO — в контроллерах. Словарь предметной области: [CONTEXT.md](CONTEXT.md).

## API

Для защищённых маршрутов: `Authorization: Bearer <access_token>`.

| Метод | Маршрут | Назначение |
|---|---|---|
| POST | `/api/v1/register` | Регистрация |
| POST | `/api/v1/login` | Выдача JWT |
| POST | `/api/v1/teams` | Команда и участие владельца одной транзакцией |
| GET | `/api/v1/teams` | Только свои команды |
| POST | `/api/v1/teams/{id}/invite` | Добавление зарегистрированного пользователя |
| PATCH | `/api/v1/teams/{id}/members/{user_id}` | Смена роли владельцем |
| POST | `/api/v1/tasks` | Создание задачи и начальной истории |
| GET | `/api/v1/tasks?team_id=...` | Задачи команды с фильтрами |
| PUT | `/api/v1/tasks/{id}` | Частичное обновление с обязательной версией |
| GET | `/api/v1/tasks/{id}/history` | История изменений |
| POST | `/api/v1/tasks/{id}/comments` | Добавление комментария |
| GET | `/api/v1/tasks/{id}/comments` | Комментарии |
| GET | `/api/v1/teams/{id}/stats` | SQL-отчёт для владельца/администратора |

Списки возвращают `{ "items": [], "limit": 20, "offset": 0 }`; `limit` — 1–100, по умолчанию 20. Команды и задачи упорядочены по `id DESC`, история и комментарии — по `id ASC`. Для задач `team_id` обязателен; дополнительные фильтры: `status`, `assignee_id`.

### Курсорная пагинация

Все четыре списка — команды, задачи, история и комментарии — поддерживают курсоры. Первый запрос: `GET /api/v1/tasks?team_id=1&limit=20&cursor=`. Если есть следующая страница, ответ содержит `next_cursor`; передайте его в параметре `cursor` следующего запроса с теми же ресурсом и фильтрами. Отсутствие `next_cursor` означает конец списка. Размер `limit` можно менять.

Курсор подписан HMAC, привязан к пользователю, пути и фильтрам. Подмена, перенос между ресурсами/пользователями и сочетание с `offset` дают `400`. Для каждой страницы повторно проверяется доступ. Подпись использует отдельный производный ключ из `JWT_SECRET`; при ротации секрета старые курсоры становятся недействительными.

В MySQL используется условие `id < last_id` для команд/задач и `id > last_id` для истории/комментариев, с чтением одной дополнительной строки. Вставка перед текущей позицией не сдвигает следующие страницы и не повторяет уже прочитанные записи. Это не снимок состояния: изменение статуса, исполнителя или членства может изменить состав следующих страниц. Режим `limit/offset` сохранён; `offset` в ответе курсорного режима всегда равен нулю. Redis различает режим и позицию курсора.

### Роли

| Действие | Owner | Admin | Member |
|---|---|---|---|
| Просмотр задач, истории, комментариев своей команды | Да | Да | Да |
| Создание задачи, добавление комментария | Да | Да | Да |
| Изменение любой задачи | Да | Да | Нет |
| Изменение собственной задачи | Все поля | Все поля | Название, описание, статус, исполнитель |
| Изменение назначенной задачи | Все поля | Все поля | Только статус, если не создатель |
| Приглашение member | Да | Да | Нет |
| Назначение admin, смена роли | Да | Нет | Нет |
| Аналитика команды | Да | Да | Нет |

Владелец всегда состоит в своей команде. Роль owner нельзя выдать через приглашение или изменить через API. Повторное приглашение возвращает `409` и не повышает роль существующего участника. Исполнитель должен состоять в той же команде — это проверяется и бизнес-логикой, и составным внешним ключом MySQL. Чужая/несуществующая команда или задача возвращает одинаковый `404`; запрещённое действие внутри своей команды — `403`.

### Семантика задач

- Новая задача имеет `status=todo`, `version=1`; `created_by` берётся из JWT.
- Допустимы `todo`, `in_progress`, `done`. ТЗ не задаёт граф переходов: разрешён переход между любыми из этих статусов.
- `PUT` обновляет только переданные поля. Обязательны `version` и минимум одно изменяемое поле. `assignee_id: null` снимает исполнителя; пропущенный `assignee_id` сохраняет его.
- При переходе в `done` устанавливается `closed_at`; при повторном `done` время сохраняется; при открытии очищается.
- Обновление с устаревшей версией возвращает `409`: нужно перечитать список и явно повторить изменение с новой версией. Сервер не перезаписывает изменения автоматически.
- Запрос, не меняющий значения, сохраняет версию и не создаёт лишнюю историю.
- История содержит старое и новое значение каждого изменённого поля, автора и UTC-время. При создании старые значения равны `null`. Комментарии хранятся отдельно, версия задачи от них не меняется.

### SQL-отчёт

[internal/repo/persistent/stats.sql](internal/repo/persistent/stats.sql) — один CTE-запрос с `JOIN`, `GROUP BY`, агрегатами и датами; проверка доступа выполняется перед ним.

- `tasks_by_status`: количество задач каждого статуса, включая нули.
- `top_assignees`: до трёх текущих исполнителей по числу задач в `done`, закрытых за последние 30 дней. При равенстве сортировка по `user_id ASC`. Задачи без исполнителя не входят в рейтинг.
- `average_close_seconds`: среднее время от создания до закрытия всех задач, которые сейчас в `done`, без ограничения 30 днями. Если закрытых задач нет — `null`.
- `comment_count`: общее количество комментариев к задачам команды.

Отдельные CTE для комментариев и задач исключают размножение строк при `JOIN` и искажение статистики.

## Пример работы

Примеры ниже используют `curl` и `jq`.

```sh
API=http://localhost:8080/api/v1

curl -fsS "$API/register" -H 'Content-Type: application/json' \
  -d '{"email":"owner@example.com","password":"correct-horse-battery","name":"Owner"}'

TOKEN=$(curl -fsS "$API/login" -H 'Content-Type: application/json' \
  -d '{"email":"owner@example.com","password":"correct-horse-battery"}' | jq -r .access_token)

TEAM_ID=$(curl -fsS "$API/teams" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"Backend"}' | jq -r .id)

MEMBER_ID=$(curl -fsS "$API/register" -H 'Content-Type: application/json' \
  -d '{"email":"member@example.com","password":"another-safe-password","name":"Member"}' | jq -r .id)

curl -fsS "$API/teams/$TEAM_ID/invite" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d "{\"user_id\":$MEMBER_ID,\"role\":\"member\"}"

TASK_ID=$(curl -fsS "$API/tasks" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"team_id\":$TEAM_ID,\"title\":\"Ship API\",\"assignee_id\":$MEMBER_ID}" | jq -r .id)

curl -fsS "$API/tasks?team_id=$TEAM_ID&status=todo&assignee_id=$MEMBER_ID&limit=20&offset=0" \
  -H "Authorization: Bearer $TOKEN"

curl -fsS -X PUT "$API/tasks/$TASK_ID" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"version":1,"status":"done"}'

curl -fsS "$API/tasks/$TASK_ID/comments" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"content":"Ready for review"}'

curl -fsS "$API/tasks/$TASK_ID/history" -H "Authorization: Bearer $TOKEN"
curl -fsS "$API/teams/$TEAM_ID/stats" -H "Authorization: Bearer $TOKEN"
```

Пример ошибки:

```json
{"error":{"code":"conflict","message":"resource already exists or task version is stale; reload and retry","request_id":"..."}}
```

## Конфигурация

Конфигурация читается из окружения. Compose читает `.env`; сам Go-бинарник автоматически `.env` не загружает.

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `MYSQL_DSN` | Обязательна | DSN go-sql-driver/mysql; в Compose собирается из MYSQL_* |
| `MYSQL_DATABASE`, `MYSQL_USER`, `MYSQL_PASSWORD`, `MYSQL_ROOT_PASSWORD` | `.env.example` | Инициализация MySQL в Compose |
| `REDIS_URL` | `redis://localhost:6379/0` | Redis, также поддерживается `rediss://` |
| `JWT_SECRET` | Обязательна, минимум 32 байта | Ключ HS256 |
| `JWT_ISSUER` | `secunda` | Проверяемый издатель JWT |
| `JWT_AUDIENCE` | `secunda-api` | Проверяемая аудитория JWT |
| `JWT_TTL` | `24h` | Срок действия JWT |
| `BCRYPT_COST` | `12` | Стоимость хеширования, диапазон 10–16 |
| `HTTP_ADDRESS` | `:8080` | Адрес Go-сервера |
| `HTTP_PORT` | `8080` | Только порт публикации Compose |
| `HTTP_TIMEOUT` | `10s` | Лимит времени запроса и чтения |
| `SHUTDOWN_TIMEOUT` | `15s` | Ожидание активных запросов при завершении |
| `DB_MAX_OPEN`, `DB_MAX_IDLE` | `20`, `10` | Пул MySQL |
| `CACHE_TIMEOUT` | `150ms` | Тайм-аут операции Redis |
| `CACHE_BREAKER_FAILURES` | `5` | Последовательные ошибки Redis до открытия circuit breaker |
| `CACHE_BREAKER_TIMEOUT` | `10s` | Пауза до пробного обращения к Redis |
| `RATE_LIMIT_ENABLED` | `true` | Включить ограничения HTTP-запросов |
| `RATE_LIMIT_RPS`, `RATE_LIMIT_BURST` | `20`, `100` | Скорость пополнения и максимальный запас запросов на IP и пользователя |
| `AUTH_RATE_LIMIT_RPS`, `AUTH_RATE_LIMIT_BURST` | `1`, `10` | Общий бюджет входа и регистрации на IP |
| `RATE_LIMIT_MAX_KEYS` | `10000` | Максимум идентификаторов в каждом из трёх ограничителей |
| `RATE_LIMIT_IDLE_TTL` | `10m` | Удаление неактивных записей; не короче времени полного пополнения |
| `LOG_LEVEL` | `info` | debug, info, warn, error |
| `LOG_FORMAT` | `json` | json или text |

Параметры Go-приложения, не перечисленные в `environment` Compose, можно добавить туда или передать локальному бинарнику. Время хранится в UTC с точностью MySQL `DATETIME(6)`. Все SQL-значения параметризованы. API ограничивает тело запроса 1 MiB и отклоняет неизвестные поля JSON. Пароли — 12–72 байта; email приводится к нижнему регистру.

## Миграции и локальный запуск

Go 1.26.10 или новее. Миграции встроены в бинарник; рабочая директория при запуске значения не имеет.

```sh
export MYSQL_DSN='user:password@tcp(127.0.0.1:3306)/secunda?charset=utf8mb4'
export REDIS_URL='redis://127.0.0.1:6379/0'
export JWT_SECRET='replace-with-a-random-secret-at-least-32-bytes'
export LOG_FORMAT=text

go run ./cmd/app migrate
go run ./cmd/app
```

В Compose миграции запускаются автоматически. Для повторного применения: `docker compose run --rm migrate`. Повторный `up` безопасен. Используется `golang-migrate`: блокировка миграций в БД, таблица `schema_migrations`, фиксация dirty-версии при сбое. MySQL DDL не транзакционен; при dirty-состоянии сначала выясните причину и фактическое состояние схемы. Для ручного отката и восстановления можно использовать CLI `migrate` с файлами из `migrations/`; приложение предоставляет только команду `migrate` для применения `up`.

## Проверки

```sh
make check          # vet, unit-тесты с -race, сборка
make lint           # golangci-lint v2.12.2 или новее
make vuln           # govulncheck
make integration    # настоящие MySQL 8.4 и Redis, тесты с -race
# При отдельном Compose: make integration COMPOSE=docker-compose
```

Интеграционные тесты используют отдельный Compose-проект `secunda-test`, MySQL на tmpfs и не затрагивают рабочую БД. После `make integration` контейнеры удаляются. Можно запускать вручную: `TEST_MYSQL_DSN=... TEST_REDIS_URL=... go test -race -tags=integration -count=1 -v ./integration-test/...` — только на тестовой БД; тесты создают данные. Без этих переменных тесты завершаются ошибкой, а не молча пропускаются.

Проверяются SQL-отчёт с чужой командой, пустой командой и границей 30 дней; матрица прав; валидация JWT; API от регистрации до истории; одновременные обновления; откат задач и истории; TTL, фильтры, инвалидация и недоступность Redis. Дополнительно проверяются точность чисел в истории, тайм-аут неотвечающего Redis, маршруты HTTP без инфраструктуры и graceful shutdown с настоящим TCP listener. GitHub Actions запускает проверки и интеграционные тесты.

Проверка отдельно запущенного API через настоящий HTTP-порт:

```sh
LIVE_API_URL=http://127.0.0.1:8080 make live

# Полная проверка локального compose.yaml: также временно приостанавливает Redis,
# проверяет отчёт с датированным SQL-fixture и удаляет данные только этого прогона.
LIVE_API_URL=http://127.0.0.1:8080 LIVE_COMPOSE='docker compose' make live
# Для отдельного бинарника: LIVE_COMPOSE=docker-compose
```

Укажите фактический `HTTP_PORT`. Тесты получают JWT через регистрацию/вход и создают пользователей и команды с уникальным префиксом. Без `LIVE_COMPOSE` данные остаются для ручного просмотра, а проверки отказа Redis и исторической даты пропускаются. С `LIVE_COMPOSE` используется локальный проект `secunda` из корневого `compose.yaml`; тесты временно делают Redis неотвечающим через `pause`, затем восстанавливают его. Очистка ограничена ID и случайным префиксом текущего прогона; существующие данные не удаляются. Пароли и JWT в журнал не выводятся.

Live-тесты ожидают стандартные настройки ограничителя и circuit breaker. Обычные сценарии соблюдают `Retry-After`, а отдельные проверки исчерпывают бюджеты и проверяют `429`. Unit-тесты покрывают матрицу прав, конкурентное расходование токенов, пополнение и очистку ограничителя, структуру логов, проверку курсоров, отмену запросов и состояния circuit breaker. Интеграционные тесты проверяют все четыре курсорных списка, вставку между страницами и сохранение позиции в Redis.

## Ограничение запросов

Используется token bucket: запас допускает короткий всплеск, затем пополняется с заданной скоростью. Все `/api/*` ограничиваются по IP до аутентификации. После проверки JWT защищённые маршруты дополнительно ограничиваются по ID пользователя. Вход и регистрация делят более строгий бюджет IP до bcrypt и обращения к БД. При превышении API возвращает JSON с `code=rate_limited`, HTTP `429` и `Retry-After` в секундах. `/healthz`, `/readyz`, Swagger и OpenAPI не ограничиваются.

IP берётся из TCP-соединения (`RemoteAddr`); заголовки `X-Forwarded-For` не принимаются на доверии. При работе за прокси общий IP-бюджет относится к этому прокси. Состояние хранится в памяти одного экземпляра, поэтому лимиты действуют отдельно на каждую реплику и сбрасываются при перезапуске. Для общего лимита кластера следует настроить входной gateway. Память ограничена, неактивные записи удаляются, заполнение таблицы не вытесняет активные бюджеты.

## Логи и circuit breaker

`log/slog` пишет структурированный JSON в stdout; `LOG_FORMAT=text` доступен для локальной разработки. Событие `http.request` содержит `request_id`, `user_id` после успешной аутентификации, метод, шаблон маршрута, статус и `duration_ms`. Успехи записываются на INFO, ошибки клиента — WARN, ошибки сервера — ERROR. Пароли, JWT, тело и строка запроса не логируются. Превышение лимита имеет событие `http.rate_limited` с областью бюджета и задержкой повтора.

Redis защищён [sony/gobreaker v2](https://pkg.go.dev/github.com/sony/gobreaker/v2). После пяти последовательных ошибок операций breaker переходит из `closed` в `open`: чтения сразу используют MySQL, записи кеша пропускаются. Через 10 секунд допускается один пробный запрос (`half-open`), остальные запросы продолжают использовать MySQL. Успех, включая обычное отсутствие ключа, закрывает breaker; ошибка запускает новую паузу. Отмена клиентского запроса не считается отказом Redis. События `circuit_breaker.state_change` содержат зависимость и состояния `from/to`; пропущенные обращения записываются только на DEBUG.

MySQL остаётся обязательным источником данных и определяет readiness. Кеш имеет TTL 5 минут. Ревизия списка увеличивается вместе с изменением задачи в SQL-транзакции, поэтому после восстановления Redis старые ключи не возвращают устаревшие задачи. Circuit breaker ограничивает ожидание неисправного кеша и не изменяет проверки доступа.
