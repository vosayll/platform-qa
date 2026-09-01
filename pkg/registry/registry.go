// Package registry contains the declarative catalog of E2E suites and their checks.
// It is the single source of truth for the Admin UI: suite keys, human-readable
// titles, categories, tags and stable snake_case check identifiers.
package registry

// CheckItem is a single verifiable assertion inside a suite.
type CheckItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Suite describes a runnable group of checks.
type Suite struct {
	Key         string      `json:"key"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Category    string      `json:"category"` // flow | negative | security | reliability | edge
	Tags        []string    `json:"tags,omitempty"`
	Checks      []CheckItem `json:"checks"`
}

var suites = []Suite{
	{
		Key:         "flow_a",
		Title:       "Flow A: Полный цикл ресторанного заказа",
		Description: "Жизненный цикл заказа в ресторан: создание клиентом, назначение курьера, готовка, выдача и доставка.",
		Category:    "flow",
		Tags:        []string{"restaurant", "lifecycle"},
		Checks: []CheckItem{
			{ID: "setup", Title: "Генерация уникальных фикстур: клиент, ресторан, курьер"},
			{ID: "create_order", Title: "Клиент создаёт заказ → статус NEW"},
			{ID: "assign_courier", Title: "Админ назначает курьера → COURIER_ASSIGNED"},
			{ID: "cooking", Title: "Ресторан начинает готовку → PREPARING"},
			{ID: "ready_for_pickup", Title: "Заказ собран → READY_FOR_PICKUP"},
			{ID: "pickup", Title: "Курьер забрал заказ → PICKED_UP"},
			{ID: "delivered", Title: "Заказ доставлен → DELIVERED"},
		},
	},
	{
		Key:         "flow_b",
		Title:       "Flow B: Доставка посылки А→Б",
		Description: "Независимый заказ-посылка из точки А в точку Б без участия ресторана.",
		Category:    "flow",
		Tags:        []string{"independent", "p2p"},
		Checks: []CheckItem{
			{ID: "setup", Title: "Генерация уникальных фикстур: клиент, курьер"},
			{ID: "create_parcel", Title: "Клиент создаёт заказ-посылку → NEW"},
			{ID: "assign_courier", Title: "Админ назначает курьера → COURIER_ASSIGNED"},
			{ID: "pickup", Title: "Курьер забрал в точке А → PICKED_UP"},
			{ID: "delivered", Title: "Доставлено в точку Б → DELIVERED"},
		},
	},
	{
		Key:         "cancellation",
		Title:       "Отмена заказа на разных стадиях",
		Description: "Правила отмены на ранних стадиях и запрет модификации терминальных состояний.",
		Category:    "edge",
		Checks: []CheckItem{
			{ID: "cancel_at_new", Title: "Клиент отменяет заказ на статусе NEW → успех"},
			{ID: "reject_during_cooking", Title: "Отмена во время готовки отклоняется (403)"},
			{ID: "terminal_protection", Title: "Модификация после DELIVERED блокируется (терминальное состояние)"},
		},
	},
	{
		Key:         "idempotency",
		Title:       "Идемпотентность и конкурентность",
		Description: "Защита от дублирующих запросов через Idempotency-Key и изоляция параллельных заказов.",
		Category:    "reliability",
		Checks: []CheckItem{
			{ID: "same_key_same_order", Title: "Повтор запроса с тем же Idempotency-Key → тот же OrderID"},
			{ID: "concurrent_isolation", Title: "10 параллельных созданий → уникальные независимые заказы"},
		},
	},
	{
		Key:         "security_rbac",
		Title:       "RBAC и изоляция токенов",
		Description: "Аутентификация без токена и межролевые ограничения доступа к API.",
		Category:    "security",
		Checks: []CheckItem{
			{ID: "no_token_401", Title: "Запрос без Bearer токена → 401"},
			{ID: "client_admin_403", Title: "Токен клиента к Admin API → 403"},
			{ID: "courier_rest_403", Title: "Токен курьера к Restaurant API → 403"},
		},
	},
	{
		Key:         "negative_sm",
		Title:       "State Machine: запрет нелегальных переходов",
		Description: "Валидация конечного автомата заказов: нелегальные переходы статусов и чужие роли.",
		Category:    "negative",
		Checks: []CheckItem{
			{ID: "illegal_jump_new_delivered", Title: "NEW → DELIVERED запрещён"},
			{ID: "illegal_jump_preparing_delivered", Title: "PREPARING → DELIVERED запрещён"},
			{ID: "unauthorized_role_client", Title: "Клиент не может установить DELIVERED"},
			{ID: "unauthorized_role_restaurant", Title: "Ресторан не может назначать курьера"},
		},
	},
	{
		Key:         "courier_reassignment",
		Title:       "Переназначение курьера на заказе",
		Description: "Заказ, уже назначенный на одного курьера, корректно переназначается администратором на другого.",
		Category:    "edge",
		Tags:        []string{"courier", "reassignment"},
		Checks: []CheckItem{
			{ID: "setup", Title: "Генерация уникальных фикстур: клиент, 2 курьера"},
			{ID: "assign_courier_1", Title: "Админ назначает заказ на курьера №1"},
			{ID: "reassign_courier_2", Title: "Админ переназначает заказ на курьера №2"},
		},
	},
	{
		Key:         "input_validation",
		Title:       "Валидация входных данных и граничные условия",
		Description: "Отклонение некорректных данных на входе: формат телефона, код верификации, отрицательная цена, отсутствующий адрес.",
		Category:    "negative",
		Tags:        []string{"validation", "boundary"},
		Checks: []CheckItem{
			{ID: "reject_invalid_phone", Title: "Отклонён нероссийский формат телефона при регистрации"},
			{ID: "reject_empty_verification_code", Title: "Отклонён вход с пустым кодом верификации"},
			{ID: "reject_negative_price", Title: "Отклонён заказ-посылка с отрицательной ценой"},
			{ID: "reject_missing_address", Title: "Отклонён заказ без адреса доставки"},
		},
	},
}

// All returns a copy of the full suite catalog in canonical order:
// flow_a, flow_b, cancellation, idempotency, security_rbac, negative_sm,
// courier_reassignment, input_validation.
func All() []Suite {
	out := make([]Suite, len(suites))
	copy(out, suites)
	return out
}

// Get returns the suite by key.
func Get(key string) (Suite, bool) {
	for _, s := range suites {
		if s.Key == key {
			return s, true
		}
	}
	return Suite{}, false
}

// Keys returns all suite keys (the virtual key "all" is not part of the catalog).
func Keys() []string {
	keys := make([]string, 0, len(suites))
	for _, s := range suites {
		keys = append(keys, s.Key)
	}
	return keys
}
