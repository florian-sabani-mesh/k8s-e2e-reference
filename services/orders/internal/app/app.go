package app

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/terraform-k8s-e2e-reference/pkg/events"
	"example.com/terraform-k8s-e2e-reference/pkg/runtime"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
)

type service struct {
	db  *pgxpool.Pool
	bus *runtime.Bus
}
type orderInput struct {
	Asset    string `json:"asset"`
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
	Exchange string `json:"exchange"`
}

var symbol = regexp.MustCompile(`^[A-Z0-9]{2,10}$`)
var amountPattern = regexp.MustCompile(`^[0-9]{1,20}(\.[0-9]{1,8})?$`)

func validInput(in orderInput) bool {
	amount, err := decimal.NewFromString(in.Amount)
	return err == nil && amount.IsPositive() && amountPattern.MatchString(in.Amount) && symbol.MatchString(in.Asset) && symbol.MatchString(in.Currency) && (in.Exchange == "coinbase" || in.Exchange == "kraken" || in.Exchange == "kucoin")
}
func Run(ctx context.Context) error {
	db, err := pgxpool.New(ctx, runtime.Required("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close()
	bus, err := runtime.Connect()
	if err != nil {
		return err
	}
	defer bus.Close()
	s := service{db, bus}
	workerCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); s.dispatch(workerCtx) }()
	defer func() { cancel(); wg.Wait() }()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", s.create)
	mux.HandleFunc("GET /orders/{id}", s.get)
	return runtime.Serve(ctx, mux, func(ctx context.Context) error {
		if err := bus.Ready(ctx); err != nil {
			return err
		}
		var exists bool
		return db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM orders LIMIT 1)").Scan(&exists)
	})
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("encode response", "error", err)
	}
}
func (s *service) create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var in orderInput
	if err := decoder.Decode(&in); err != nil {
		http.Error(w, "invalid order JSON", 400)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF || !validInput(in) {
		http.Error(w, "invalid order fields", 400)
		return
	}
	correlation := r.Header.Get("X-Test-ID")
	if correlation == "" {
		correlation = uuid.NewString()
	}
	if len(correlation) > 128 {
		http.Error(w, "X-Test-ID too long", 400)
		return
	}
	id := uuid.NewString()
	event := events.Envelope{Version: 1, ID: uuid.NewString(), OrderID: id, CorrelationID: correlation, Asset: in.Asset, Currency: in.Currency, Amount: in.Amount, Exchange: in.Exchange}
	payload, err := json.Marshal(event)
	if err != nil {
		s.fail(w, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), "INSERT INTO orders(id,correlation_id,asset,currency,amount,exchange,status) VALUES($1,$2,$3,$4,$5,$6,'PENDING')", id, correlation, in.Asset, in.Currency, in.Amount, in.Exchange)
	if err == nil {
		_, err = tx.Exec(r.Context(), "INSERT INTO outbox(event_id,subject,payload) VALUES($1,$2,$3)", event.ID, events.Created, payload)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), "INSERT INTO order_activity(event_id,order_id,kind) VALUES($1,$2,'PENDING')", event.ID, id)
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.fail(w, err)
		return
	}
	slog.Info("order created", "order_id", id, "correlation_id", correlation, "exchange", in.Exchange)
	w.Header().Set("Location", "/orders/"+id)
	w.Header().Set("X-Test-ID", correlation)
	respond(w, 202, map[string]string{"id": id, "status": "PENDING", "correlation_id": correlation})
}
func (s *service) fail(w http.ResponseWriter, err error) {
	slog.Error("order request failed", "error", err)
	http.Error(w, "database unavailable", 503)
}
func (s *service) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "invalid order ID", 400)
		return
	}
	var value []byte
	err := s.db.QueryRow(r.Context(), `SELECT jsonb_build_object(
 'id',o.id,'correlation_id',o.correlation_id,'asset',o.asset,'currency',o.currency,'amount',o.amount::text,'exchange',o.exchange,'status',o.status,'error_code',o.error_code,
 'settlement',CASE WHEN s.id IS NULL THEN NULL ELSE jsonb_build_object('id',s.id,'price',s.price::text,'total',s.total::text) END,
 'settlement_count',(SELECT count(*) FROM settlements WHERE order_id=o.id),
 'activity',COALESCE((SELECT jsonb_agg(jsonb_build_object('event_id',a.event_id,'kind',a.kind) ORDER BY a.created_at,a.event_id) FROM order_activity a WHERE a.order_id=o.id),'[]'::jsonb))
 FROM orders o LEFT JOIN settlements s ON s.order_id=o.id WHERE o.id=$1`, id).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "order not found", 404)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err = w.Write(value); err != nil {
		slog.Error("write response", "error", err)
	}
}
