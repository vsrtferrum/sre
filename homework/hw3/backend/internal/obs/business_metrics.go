// Бизнес-метрики пользователей, склада и отзывов в namespace "backend".
// Регистрируются так же, как в resilience.NewMetrics: молча игнорируя
// AlreadyRegisteredError (безопасно при повторном создании, например в
// тестах). RED-метрики по новым маршрутам получаются автоматически через
// InstrumentServer - здесь только дополняющие бизнес-счётчики.

package obs

import (
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// BusinessMetrics хранит бизнес-счётчики пользователей/склада/отзывов.
type BusinessMetrics struct {
	// Checkout outcome is deliberately bounded: result is one of the values
	// produced by the checkout/payment state machine, never an ID from a
	// request or database row.
	CheckoutAttempts *prometheus.CounterVec
	CheckoutDuration *prometheus.HistogramVec

	// Пользователи и сессии.
	UserLogins     prometheus.Counter
	UserLogouts    prometheus.Counter
	ActiveSessions prometheus.Gauge
	UserActions    *prometheus.CounterVec // by action (не по user_id!)
	AdminDenied    prometheus.Counter

	// Склад.
	OrdersBlockedByStock prometheus.Counter
	InventoryRestock     prometheus.Counter
	OutOfStockEvents     prometheus.Counter
	LowStockProducts     prometheus.Gauge
	StockMovement        *prometheus.CounterVec // by reason

	// Отзывы.
	ReviewPosted   prometheus.Counter
	ReviewRejected prometheus.Counter
	ReviewRating   *prometheus.CounterVec // by rating 1..5
}

func newCounter(name, help string) prometheus.Counter {
	c := prometheus.NewCounter(prometheus.CounterOpts{Namespace: "backend", Name: name, Help: help})
	registerBM(c)
	return c
}

func newGauge(name, help string) prometheus.Gauge {
	g := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "backend", Name: name, Help: help})
	registerBM(g)
	return g
}

func newCounterVec(name, help string, labels ...string) *prometheus.CounterVec {
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "backend", Name: name, Help: help}, labels)
	registerBM(c)
	return c
}

func newHistogramVec(name, help string, buckets []float64, labels ...string) *prometheus.HistogramVec {
	h := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "backend",
		Name:      name,
		Help:      help,
		Buckets:   buckets,
	}, labels)
	registerBM(h)
	return h
}

func registerBM(c prometheus.Collector) {
	if err := prometheus.Register(c); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			return
		}
		panic(err)
	}
}

// NewBusinessMetrics создаёт и регистрирует бизнес-метрики.
func NewBusinessMetrics() *BusinessMetrics {
	return &BusinessMetrics{
		CheckoutAttempts: newCounterVec(
			"checkout_attempts_total",
			"Checkout outcomes by bounded result and outcome type.",
			"result", "outcome_type",
		),
		CheckoutDuration: newHistogramVec(
			"checkout_duration_seconds",
			"End-to-end checkout duration by bounded result and outcome type.",
			[]float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			"result", "outcome_type",
		),
		UserLogins:           newCounter("user_logins_total", "Сколько раз пользователи вошли (вход по имени)."),
		UserLogouts:          newCounter("user_logouts_total", "Сколько раз пользователи вышли."),
		ActiveSessions:       newGauge("active_sessions", "Число активных in-memory сессий."),
		UserActions:          newCounterVec("user_actions_total", "Действия пользователей по типу (ТОЛЬКО action, не user_id).", "action"),
		AdminDenied:          newCounter("admin_denied_total", "Сколько раз не-админу отказано в доступе к админ-ручке."),
		OrdersBlockedByStock: newCounter("orders_blocked_by_stock_total", "Заказы, заблокированные нехваткой склада (409)."),
		InventoryRestock:     newCounter("inventory_restock_total", "Восполнения остатков через админ-панель."),
		OutOfStockEvents:     newCounter("out_of_stock_events_total", "События перехода товара в состояние 'нет в наличии'."),
		LowStockProducts:     newGauge("low_stock_products", "Число товаров с нулевым остатком."),
		StockMovement:        newCounterVec("stock_movement_total", "Движения остатков по причине.", "reason"),
		ReviewPosted:         newCounter("reviews_posted_total", "Отзывы, успешно созданные."),
		ReviewRejected:       newCounter("reviews_rejected_total", "Отзывы, отклонённые валидацией (рейтинг/пустой комментарий)."),
		ReviewRating:         newCounterVec("reviews_by_rating_total", "Опубликованные отзывы по оценке.", "rating"),
	}
}

// ObserveCheckout records exactly one terminal checkout outcome.
func (b *BusinessMetrics) ObserveCheckout(result string, duration time.Duration) {
	if b == nil || b.CheckoutAttempts == nil || b.CheckoutDuration == nil {
		return
	}
	outcomeType := "technical_failure"
	switch result {
	case "paid":
		outcomeType = "success"
	case "card_declined":
		outcomeType = "business_decline"
	case "out_of_stock":
		outcomeType = "business_rejection"
	}
	b.CheckoutAttempts.WithLabelValues(result, outcomeType).Inc()
	b.CheckoutDuration.WithLabelValues(result, outcomeType).Observe(duration.Seconds())
}

// UpdateGaugeCount обновляет датчик активных сессий текущим значением.
func (b *BusinessMetrics) SetActiveSessions(n int) {
	if b == nil || b.ActiveSessions == nil {
		return
	}
	b.ActiveSessions.Set(float64(n))
}
