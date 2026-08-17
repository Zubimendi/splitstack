package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

var (
	ExpensesAdded = promauto.NewCounter(prometheus.CounterOpts{
		Name: "splitstack_expenses_added_total",
		Help: "Expenses successfully recorded.",
	})

	SettlementsRecorded = promauto.NewCounter(prometheus.CounterOpts{
		Name: "splitstack_settlements_recorded_total",
		Help: "Settlements successfully recorded.",
	})

	ConcurrentUpdateConflicts = promauto.NewCounter(prometheus.CounterOpts{
		Name: "splitstack_concurrent_update_conflicts_total",
		Help: "Balance writes rejected due to a concurrent modification (optimistic lock failure).",
	})
)

func NewLogger() *zap.Logger {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	return logger
}

func MetricsHandler() http.Handler {
	return promhttp.Handler()
}
