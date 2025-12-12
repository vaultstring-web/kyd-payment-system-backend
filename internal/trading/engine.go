// Package trading implements the trading desk, order matching, and market making engine.
package trading

import (
	"container/heap"
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// TradingEngine is the core trading system with order book and market making.
type TradingEngine struct {
	mu          sync.RWMutex
	orderBooks  map[string]*OrderBook
	marketMaker *MarketMaker
	riskEngine  *TradingRiskEngine
	trades      chan Trade
	config      TradingConfig
}

// TradingConfig holds trading engine configuration.
type TradingConfig struct {
	MaxOrderSize       decimal.Decimal
	MinOrderSize       decimal.Decimal
	MaxDailyVolume     decimal.Decimal
	DefaultSpread      float64
	VolatilityWindow   time.Duration
	MarketMakerEnabled bool
}

// Order represents a trading order.
type Order struct {
	ID            string          `json:"id"`
	UserID        string          `json:"user_id"`
	Pair          string          `json:"pair"`
	Side          string          `json:"side"`
	OrderType     string          `json:"order_type"`
	Price         decimal.Decimal `json:"price"`
	Quantity      decimal.Decimal `json:"quantity"`
	FilledQty     decimal.Decimal `json:"filled_quantity"`
	Status        string          `json:"status"`
	TimeInForce   string          `json:"time_in_force"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	ExpiresAt     *time.Time      `json:"expires_at,omitempty"`
}

// Trade represents an executed trade.
type Trade struct {
	ID          string          `json:"id"`
	BuyOrderID  string          `json:"buy_order_id"`
	SellOrderID string          `json:"sell_order_id"`
	Pair        string          `json:"pair"`
	Price       decimal.Decimal `json:"price"`
	Quantity    decimal.Decimal `json:"quantity"`
	BuyerID     string          `json:"buyer_id"`
	SellerID    string          `json:"seller_id"`
	Timestamp   time.Time       `json:"timestamp"`
	Fee         decimal.Decimal `json:"fee"`
}

// OrderBook maintains bid/ask orders for a trading pair.
type OrderBook struct {
	mu        sync.RWMutex
	Pair      string
	Bids      *OrderHeap
	Asks      *OrderHeap
	LastPrice decimal.Decimal
	Volume24h decimal.Decimal
	High24h   decimal.Decimal
	Low24h    decimal.Decimal
	Trades    []Trade
	UpdatedAt time.Time
}

// MarketQuote represents current market pricing.
type MarketQuote struct {
	Pair      string          `json:"pair"`
	Bid       decimal.Decimal `json:"bid"`
	Ask       decimal.Decimal `json:"ask"`
	Spread    decimal.Decimal `json:"spread"`
	SpreadPct float64         `json:"spread_pct"`
	Mid       decimal.Decimal `json:"mid"`
	Volume    decimal.Decimal `json:"volume"`
	Timestamp time.Time       `json:"timestamp"`
}

// NewTradingEngine creates a new trading engine.
func NewTradingEngine(config TradingConfig) *TradingEngine {
	engine := &TradingEngine{
		orderBooks: make(map[string]*OrderBook),
		trades:     make(chan Trade, 10000),
		config:     config,
	}

	engine.marketMaker = NewMarketMaker(engine)
	engine.riskEngine = NewTradingRiskEngine()

	pairs := []string{"MWK-CNY", "MWK-USD", "CNY-USD", "USD-EUR", "USD-GBP", "EUR-GBP"}
	for _, pair := range pairs {
		engine.orderBooks[pair] = NewOrderBook(pair)
	}

	return engine
}

// NewOrderBook creates a new order book for a pair.
func NewOrderBook(pair string) *OrderBook {
	return &OrderBook{
		Pair:      pair,
		Bids:      NewOrderHeap(true),
		Asks:      NewOrderHeap(false),
		Trades:    make([]Trade, 0),
		UpdatedAt: time.Now(),
	}
}

// SubmitOrder processes a new order.
func (te *TradingEngine) SubmitOrder(ctx context.Context, order Order) (*Order, []Trade, error) {
	if err := te.validateOrder(order); err != nil {
		return nil, nil, err
	}

	if err := te.riskEngine.CheckOrderRisk(order); err != nil {
		return nil, nil, err
	}

	te.mu.Lock()
	ob, exists := te.orderBooks[order.Pair]
	if !exists {
		ob = NewOrderBook(order.Pair)
		te.orderBooks[order.Pair] = ob
	}
	te.mu.Unlock()

	order.ID = uuid.New().String()
	order.Status = "pending"
	order.CreatedAt = time.Now()
	order.UpdatedAt = time.Now()

	trades := ob.ProcessOrder(&order)

	if order.FilledQty.Equal(order.Quantity) {
		order.Status = "filled"
	} else if order.FilledQty.GreaterThan(decimal.Zero) {
		order.Status = "partially_filled"
	} else if order.OrderType == "market" {
		order.Status = "rejected"
	} else {
		order.Status = "open"
	}

	return &order, trades, nil
}

// ProcessOrder matches an order against the book.
func (ob *OrderBook) ProcessOrder(order *Order) []Trade {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	var trades []Trade

	if order.Side == "buy" {
		trades = ob.matchBuyOrder(order)
	} else {
		trades = ob.matchSellOrder(order)
	}

	remaining := order.Quantity.Sub(order.FilledQty)
	if remaining.GreaterThan(decimal.Zero) && order.OrderType == "limit" {
		ob.addOrder(order)
	}

	ob.UpdatedAt = time.Now()
	return trades
}

func (ob *OrderBook) matchBuyOrder(order *Order) []Trade {
	var trades []Trade

	for ob.Asks.Len() > 0 {
		remaining := order.Quantity.Sub(order.FilledQty)
		if remaining.LessThanOrEqual(decimal.Zero) {
			break
		}

		bestAsk := ob.Asks.Peek()

		if order.OrderType == "limit" && order.Price.LessThan(bestAsk.Price) {
			break
		}

		tradeQty := decimal.Min(remaining, bestAsk.Quantity.Sub(bestAsk.FilledQty))
		tradePrice := bestAsk.Price

		trade := Trade{
			ID:          uuid.New().String(),
			BuyOrderID:  order.ID,
			SellOrderID: bestAsk.ID,
			Pair:        order.Pair,
			Price:       tradePrice,
			Quantity:    tradeQty,
			BuyerID:     order.UserID,
			SellerID:    bestAsk.UserID,
			Timestamp:   time.Now(),
			Fee:         tradeQty.Mul(tradePrice).Mul(decimal.NewFromFloat(0.001)),
		}

		trades = append(trades, trade)

		order.FilledQty = order.FilledQty.Add(tradeQty)
		bestAsk.FilledQty = bestAsk.FilledQty.Add(tradeQty)

		if bestAsk.FilledQty.Equal(bestAsk.Quantity) {
			heap.Pop(ob.Asks)
		}

		ob.LastPrice = tradePrice
		ob.Trades = append(ob.Trades, trade)
	}

	return trades
}

func (ob *OrderBook) matchSellOrder(order *Order) []Trade {
	var trades []Trade

	for ob.Bids.Len() > 0 {
		remaining := order.Quantity.Sub(order.FilledQty)
		if remaining.LessThanOrEqual(decimal.Zero) {
			break
		}

		bestBid := ob.Bids.Peek()

		if order.OrderType == "limit" && order.Price.GreaterThan(bestBid.Price) {
			break
		}

		tradeQty := decimal.Min(remaining, bestBid.Quantity.Sub(bestBid.FilledQty))
		tradePrice := bestBid.Price

		trade := Trade{
			ID:          uuid.New().String(),
			BuyOrderID:  bestBid.ID,
			SellOrderID: order.ID,
			Pair:        order.Pair,
			Price:       tradePrice,
			Quantity:    tradeQty,
			BuyerID:     bestBid.UserID,
			SellerID:    order.UserID,
			Timestamp:   time.Now(),
			Fee:         tradeQty.Mul(tradePrice).Mul(decimal.NewFromFloat(0.001)),
		}

		trades = append(trades, trade)

		order.FilledQty = order.FilledQty.Add(tradeQty)
		bestBid.FilledQty = bestBid.FilledQty.Add(tradeQty)

		if bestBid.FilledQty.Equal(bestBid.Quantity) {
			heap.Pop(ob.Bids)
		}

		ob.LastPrice = tradePrice
		ob.Trades = append(ob.Trades, trade)
	}

	return trades
}

func (ob *OrderBook) addOrder(order *Order) {
	if order.Side == "buy" {
		heap.Push(ob.Bids, order)
	} else {
		heap.Push(ob.Asks, order)
	}
}

// GetQuote returns the current market quote for a pair.
func (te *TradingEngine) GetQuote(pair string) (*MarketQuote, error) {
	te.mu.RLock()
	ob, exists := te.orderBooks[pair]
	te.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("pair not found: %s", pair)
	}

	ob.mu.RLock()
	defer ob.mu.RUnlock()

	quote := &MarketQuote{
		Pair:      pair,
		Volume:    ob.Volume24h,
		Timestamp: time.Now(),
	}

	if ob.Bids.Len() > 0 {
		quote.Bid = ob.Bids.Peek().Price
	}
	if ob.Asks.Len() > 0 {
		quote.Ask = ob.Asks.Peek().Price
	}

	if !quote.Bid.IsZero() && !quote.Ask.IsZero() {
		quote.Spread = quote.Ask.Sub(quote.Bid)
		quote.Mid = quote.Bid.Add(quote.Ask).Div(decimal.NewFromInt(2))
		midFloat, _ := quote.Mid.Float64()
		spreadFloat, _ := quote.Spread.Float64()
		if midFloat > 0 {
			quote.SpreadPct = (spreadFloat / midFloat) * 100
		}
	}

	return quote, nil
}

func (te *TradingEngine) validateOrder(order Order) error {
	if order.Quantity.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("quantity must be positive")
	}
	if order.OrderType == "limit" && order.Price.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("limit orders must have a price")
	}
	if order.Side != "buy" && order.Side != "sell" {
		return fmt.Errorf("invalid order side")
	}
	return nil
}

// OrderHeap implements heap.Interface for order priority queue.
type OrderHeap struct {
	orders []*Order
	isMax  bool
}

func NewOrderHeap(isMax bool) *OrderHeap {
	return &OrderHeap{
		orders: make([]*Order, 0),
		isMax:  isMax,
	}
}

func (h *OrderHeap) Len() int { return len(h.orders) }

func (h *OrderHeap) Less(i, j int) bool {
	if h.isMax {
		return h.orders[i].Price.GreaterThan(h.orders[j].Price)
	}
	return h.orders[i].Price.LessThan(h.orders[j].Price)
}

func (h *OrderHeap) Swap(i, j int) {
	h.orders[i], h.orders[j] = h.orders[j], h.orders[i]
}

func (h *OrderHeap) Push(x interface{}) {
	h.orders = append(h.orders, x.(*Order))
}

func (h *OrderHeap) Pop() interface{} {
	old := h.orders
	n := len(old)
	item := old[n-1]
	h.orders = old[0 : n-1]
	return item
}

func (h *OrderHeap) Peek() *Order {
	if len(h.orders) == 0 {
		return nil
	}
	return h.orders[0]
}

// MarketMaker provides automated liquidity.
type MarketMaker struct {
	engine    *TradingEngine
	mu        sync.RWMutex
	positions map[string]*Position
	config    MarketMakerConfig
}

type MarketMakerConfig struct {
	BaseSpread       float64
	MinSpread        float64
	MaxSpread        float64
	InventoryLimit   decimal.Decimal
	QuoteSize        decimal.Decimal
	UpdateInterval   time.Duration
	VolatilityFactor float64
	TimeOfDayFactor  bool
}

type Position struct {
	Pair     string
	Long     decimal.Decimal
	Short    decimal.Decimal
	AvgPrice decimal.Decimal
	PnL      decimal.Decimal
}

func NewMarketMaker(engine *TradingEngine) *MarketMaker {
	return &MarketMaker{
		engine:    engine,
		positions: make(map[string]*Position),
		config: MarketMakerConfig{
			BaseSpread:       0.015,
			MinSpread:        0.005,
			MaxSpread:        0.05,
			InventoryLimit:   decimal.NewFromInt(1000000),
			QuoteSize:        decimal.NewFromInt(10000),
			UpdateInterval:   5 * time.Second,
			VolatilityFactor: 2.0,
			TimeOfDayFactor:  true,
		},
	}
}

// CalculateDynamicSpread computes spread based on market conditions.
func (mm *MarketMaker) CalculateDynamicSpread(pair string, midPrice decimal.Decimal) decimal.Decimal {
	spread := mm.config.BaseSpread

	if mm.config.TimeOfDayFactor {
		hour := time.Now().Hour()
		if hour < 8 || hour >= 18 {
			spread *= 1.5
		}
		weekday := time.Now().Weekday()
		if weekday == time.Saturday || weekday == time.Sunday {
			spread *= 1.3
		}
	}

	if pos, ok := mm.positions[pair]; ok {
		netPosition := pos.Long.Sub(pos.Short).Abs()
		if netPosition.GreaterThan(mm.config.InventoryLimit.Div(decimal.NewFromInt(2))) {
			spread *= 1.2
		}
	}

	spread = math.Max(mm.config.MinSpread, math.Min(mm.config.MaxSpread, spread))

	return midPrice.Mul(decimal.NewFromFloat(spread))
}

// GenerateQuotes creates bid/ask quotes for a pair.
func (mm *MarketMaker) GenerateQuotes(pair string, midPrice decimal.Decimal) (bid, ask decimal.Decimal) {
	spreadAmount := mm.CalculateDynamicSpread(pair, midPrice)
	halfSpread := spreadAmount.Div(decimal.NewFromInt(2))

	bid = midPrice.Sub(halfSpread)
	ask = midPrice.Add(halfSpread)

	return bid, ask
}

// TradingRiskEngine manages trading risk limits.
type TradingRiskEngine struct {
	mu     sync.RWMutex
	limits map[string]*RiskLimits
	usage  map[string]*RiskUsage
}

type RiskLimits struct {
	UserID          string
	MaxOrderSize    decimal.Decimal
	MaxDailyVolume  decimal.Decimal
	MaxOpenOrders   int
	MaxPositionSize decimal.Decimal
	MaxLoss         decimal.Decimal
}

type RiskUsage struct {
	UserID       string
	DailyVolume  decimal.Decimal
	OpenOrders   int
	PositionSize decimal.Decimal
	DailyPnL     decimal.Decimal
	LastReset    time.Time
}

func NewTradingRiskEngine() *TradingRiskEngine {
	return &TradingRiskEngine{
		limits: make(map[string]*RiskLimits),
		usage:  make(map[string]*RiskUsage),
	}
}

func (re *TradingRiskEngine) CheckOrderRisk(order Order) error {
	re.mu.RLock()
	defer re.mu.RUnlock()

	limits, hasLimits := re.limits[order.UserID]
	if !hasLimits {
		limits = &RiskLimits{
			MaxOrderSize:   decimal.NewFromInt(100000),
			MaxDailyVolume: decimal.NewFromInt(1000000),
			MaxOpenOrders:  100,
		}
	}

	orderValue := order.Quantity.Mul(order.Price)
	if orderValue.GreaterThan(limits.MaxOrderSize) {
		return fmt.Errorf("order exceeds maximum size limit")
	}

	if usage, ok := re.usage[order.UserID]; ok {
		if usage.DailyVolume.Add(orderValue).GreaterThan(limits.MaxDailyVolume) {
			return fmt.Errorf("order would exceed daily volume limit")
		}
		if usage.OpenOrders >= limits.MaxOpenOrders {
			return fmt.Errorf("maximum open orders reached")
		}
	}

	return nil
}

func (re *TradingRiskEngine) SetLimits(userID string, limits RiskLimits) {
	re.mu.Lock()
	defer re.mu.Unlock()
	limits.UserID = userID
	re.limits[userID] = &limits
}

func (re *TradingRiskEngine) RecordTrade(userID string, volume decimal.Decimal) {
	re.mu.Lock()
	defer re.mu.Unlock()

	usage, ok := re.usage[userID]
	if !ok {
		usage = &RiskUsage{
			UserID:    userID,
			LastReset: time.Now(),
		}
		re.usage[userID] = usage
	}

	if time.Since(usage.LastReset) > 24*time.Hour {
		usage.DailyVolume = decimal.Zero
		usage.LastReset = time.Now()
	}

	usage.DailyVolume = usage.DailyVolume.Add(volume)
}
