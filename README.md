# Locali E2E API Testing Engine (Go)

Высокопроизводительный движок End-to-End API тестирования для микросервисной экосистемы доставки **Locali**.

---

## 1. Архитектура и ключевые компоненты

```
e2e-engine/
├── Dockerfile                  # Контейнеризированный запуск тестов
├── docker-compose.yml          # Запуск в изолированном стеке
├── Makefile                    # Команды сборки и тестирования
├── go.mod
├── config/                     # Управление конфигурацией (BaseURL, токены, NATS)
│   └── config.go
├── pkg/
│   ├── client/                 # Мульти-сессионный HTTP клиент для 4-х ролей
│   │   ├── models.go           # DTO модели запросов/ответов
│   │   ├── http_client.go      # REST клиент с интерцепторами и логированием
│   │   ├── session_manager.go  # Менеджер активных JWT токенов 4 ролей
│   │   ├── client_api.go       # API Клиента
│   │   ├── rest_api.go         # API Ресторана
│   │   ├── courier_api.go      # API Курьера
│   │   └── admin_api.go        # API Директора/Админа
│   ├── events/                 # Перехват асинхронных событий
│   │   └── nats_listener.go    # NATS JetStream / Event Bus listener
│   ├── statemachine/           # Валидатор переходов состояний (State Machine)
│   │   └── order_sm.go         # Матрица допустимых переходов и проверка ролей
│   ├── fixtures/               # Генератор изолированных тестовых данных и Teardown
│   │   └── generator.go
│   └── dsl/                    # Fluent DSL (Given/When/Then)
│       └── flow_builder.go
├── testserver/                 # Встроенный Mock Backend для полной автономности
│   └── mock_backend.go
└── tests/                      # Наборы тестов
    ├── flow_a_restaurant_test.go          # Флоу А: Полный цикл заказа из ресторана
    ├── flow_b_point2point_test.go          # Флоу Б: Независимая доставка посылки
    ├── state_machine_negative_test.go     # Проверка запрещенных прыжков статусов
    └── main_test.go
```

---

## 2. Ключевые возможности

1. **Мульти-сессии для 4 ролей**:
   - Одновременное хранение и автоматическая передача Bearer JWT токенов для **Клиента**, **Ресторана**, **Курьера** и **Директора**.
2. **Валидация State Machine**:
   - Гарантирует корректность цепочки статусов (`NEW` -> `COURIER_ASSIGNED` -> `PREPARING` -> `READY_FOR_PICKUP` -> `PICKED_UP` -> `DELIVERED`).
   - Блокирует любые нелегальные прыжки (например, из `NEW` сразу в `DELIVERED`).
3. **Изолированность (Fixtures & Teardown)**:
   - Каждый тест автоматически генерирует уникальных клиентов, рестораны и курьеров.
   - Очистка ресурсов после завершения через стек `t.Cleanup`.
4. **Конфигурируемость**:
   - Переменная `BASE_URL` позволяет направлять тесты как на живой бэкенд (`http://localhost:3000`), так и на стейджинг или встроенный мок.
   - Поддержка готовых токенов через `CLIENT_TOKEN`, `REST_TOKEN`, `COURIER_TOKEN`, `ADMIN_TOKEN`.

---

## 3. Запуск в Docker

### Запуск тестов в контейнере:
```bash
docker build -t locali-e2e-engine .
docker run --rm locali-e2e-engine
```

### Запуск тестов против живого бэкенда:
```bash
docker run --rm -e BASE_URL=http://host.docker.internal:3000 locali-e2e-engine
```
