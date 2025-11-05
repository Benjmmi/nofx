package trader

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/Benjmmi/okx"
	"github.com/Benjmmi/okx/api"
	account2 "github.com/Benjmmi/okx/requests/rest/account"
	"github.com/Benjmmi/okx/requests/rest/public"
	"github.com/Benjmmi/okx/requests/rest/trade"
)

// OkxTrader Okx合约交易器
type OkxTrader struct {
	client *api.Client

	// 余额缓存
	cachedBalance     map[string]interface{}
	balanceCacheTime  time.Time
	balanceCacheMutex sync.RWMutex

	// 持仓缓存
	cachedPositions     []map[string]interface{}
	positionsCacheTime  time.Time
	positionsCacheMutex sync.RWMutex

	// 缓存有效期（15秒）
	cacheDuration time.Duration
}

// NewOkxTrader 创建合约交易器
func NewOkxTrader(apiKey, secretKey, passphrase string) (*OkxTrader, error) {
	client, err := api.NewClient(context.Background(), apiKey, secretKey, passphrase, okx.NormalServer)
	if err != nil {
		log.Fatal("获取 OKX 链接失败")
		return nil, err
	}
	return &OkxTrader{
		client:        client,
		cacheDuration: 15 * time.Second, // 15秒缓存
	}, nil
}

// GetBalance 获取账户余额（带缓存）
func (t *OkxTrader) GetBalance() (map[string]interface{}, error) {
	// 先检查缓存是否有效
	t.balanceCacheMutex.RLock()
	if t.cachedBalance != nil && time.Since(t.balanceCacheTime) < t.cacheDuration {
		cacheAge := time.Since(t.balanceCacheTime)
		t.balanceCacheMutex.RUnlock()
		log.Printf("✓ 使用缓存的账户余额（缓存时间: %.1f秒前）", cacheAge.Seconds())
		return t.cachedBalance, nil
	}
	t.balanceCacheMutex.RUnlock()

	// 缓存过期或不存在，调用API
	log.Printf("🔄 缓存过期，正在调用OkxAPI获取账户余额...")
	balance, err := t.client.Rest.Account.GetBalance(account2.GetBalance{})
	if err != nil || balance.Balances == nil {
		log.Printf("❌ OkxAPI调用失败: %v", err)
		return nil, fmt.Errorf("获取账户信息失败: %w", err)
	}
	a := balance.Balances[0]

	result := make(map[string]interface{})
	result["totalWalletBalance"], _ = strconv.ParseFloat(a.TotalEq, 64)
	result["availableBalance"], _ = strconv.ParseFloat(a.AvailEq, 64)
	result["totalUnrealizedProfit"], _ = strconv.ParseFloat(a.Upl, 64)

	log.Printf("✓ OkxAPI返回: 总余额=%s, 可用=%s, 未实现盈亏=%s",
		a.TotalEq, a.AvailEq, a.Upl)

	// 更新缓存
	t.balanceCacheMutex.Lock()
	t.cachedBalance = result
	t.balanceCacheTime = time.Now()
	t.balanceCacheMutex.Unlock()

	return result, nil
}

// OpenLong 开多仓
func (t *OkxTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	// 先取消该币种的所有委托单（清理旧的止损止盈单）
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("  ⚠ 取消旧委托单失败（可能没有委托单）: %v", err)
	}

	// 设置杠杆
	resp, err := t.client.Rest.Account.SetLeverage(account2.SetLeverage{
		InstID:  symbol,
		MgnMode: "cross",
		Lever:   int64(leverage),
	})

	if err != nil || resp.Code != 200 {
		return nil, err
	}

	// 注意：仓位模式应该由调用方（AutoTrader）在开仓前通过 SetMarginMode 设置

	// 格式化数量到正确精度
	quantityStr, err := t.FormatQuantity(symbol, quantity)
	if err != nil {
		return nil, err
	}

	// 创建市价买入订单
	orderResp, err := t.client.Rest.Trade.PlaceOrder(trade.PlaceOrder{
		InstID:  symbol,
		TdMode:  "cross",
		Side:    "buy",
		PosSide: "long",
		OrdType: "market",
		Sz:      quantity,
	})

	if err != nil || orderResp.Code != 0 {
		return nil, fmt.Errorf("开多仓失败: %w", err)
	}

	order := orderResp.PlaceOrders[0]

	log.Printf("✓ 开多仓成功: %s 数量: %s", symbol, quantityStr)
	log.Printf("  订单ID: %d", order.OrdID)

	result := make(map[string]interface{})
	result["orderId"] = order.OrdID
	result["symbol"] = symbol
	result["status"] = order.SCode
	return result, nil
}

// OpenShort 开空仓
func (t *OkxTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	// 先取消该币种的所有委托单（清理旧的止损止盈单）
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("  ⚠ 取消旧委托单失败（可能没有委托单）: %v", err)
	}

	// 设置杠杆
	resp, err := t.client.Rest.Account.SetLeverage(account2.SetLeverage{
		InstID:  symbol,
		MgnMode: "cross",
		Lever:   int64(leverage),
	})

	if err != nil || resp.Code != 200 {
		return nil, err
	}

	// 注意：仓位模式应该由调用方（AutoTrader）在开仓前通过 SetMarginMode 设置

	// 格式化数量到正确精度
	quantityStr, err := t.FormatQuantity(symbol, quantity)
	if err != nil {
		return nil, err
	}

	// 创建市价卖出订单
	orderResp, err := t.client.Rest.Trade.PlaceOrder(trade.PlaceOrder{
		InstID:  symbol,
		TdMode:  "cross",
		Side:    "sell",
		PosSide: "short",
		OrdType: "market",
		Sz:      quantity,
	})

	if err != nil || orderResp.Code != 0 {
		return nil, fmt.Errorf("开空仓失败: %w", err)
	}

	order := orderResp.PlaceOrders[0]

	log.Printf("✓ 开空仓成功: %s 数量: %s", symbol, quantityStr)
	log.Printf("  订单ID: %d", order.OrdID)

	result := make(map[string]interface{})
	result["orderId"] = order.OrdID
	result["symbol"] = symbol
	result["status"] = order.SCode
	return result, nil
}

// CloseLong 平多仓
func (t *OkxTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	// 如果数量为0，获取当前持仓数量
	if quantity == 0 {
		positions, err := t.GetPositions()
		if err != nil {
			return nil, err
		}

		for _, pos := range positions {
			if pos["symbol"] == symbol && pos["side"] == "long" {
				quantity = pos["positionAmt"].(float64)
				break
			}
		}

		if quantity == 0 {
			return nil, fmt.Errorf("没有找到 %s 的多仓", symbol)
		}
	}

	// 格式化数量
	quantityStr, err := t.FormatQuantity(symbol, quantity)
	if err != nil {
		return nil, err
	}

	// 创建市价卖出订单（平多）
	orderResp, err := t.client.Rest.Trade.ClosePosition(trade.ClosePosition{
		InstID:  symbol,
		MgnMode: "cross",
		PosSide: "long",
	})

	if err != nil || orderResp.Code != 0 {
		return nil, fmt.Errorf("平多仓失败: %w", err)
	}

	//order := orderResp.ClosePositions[0]

	log.Printf("✓ 平多仓成功: %s 数量: %s", symbol, quantityStr)

	// 平仓后取消该币种的所有挂单（止损止盈单）
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("  ⚠ 取消挂单失败: %v", err)
	}

	result := make(map[string]interface{})
	//result["orderId"] = order.InstID
	result["symbol"] = symbol
	result["status"] = orderResp.Code
	return result, nil
}

// CloseShort 平空仓
func (t *OkxTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	// 如果数量为0，获取当前持仓数量
	if quantity == 0 {
		positions, err := t.GetPositions()
		if err != nil {
			return nil, err
		}

		for _, pos := range positions {
			if pos["symbol"] == symbol && pos["side"] == "short" {
				quantity = -pos["positionAmt"].(float64) // 空仓数量是负的，取绝对值
				break
			}
		}

		if quantity == 0 {
			return nil, fmt.Errorf("没有找到 %s 的空仓", symbol)
		}
	}

	// 格式化数量
	quantityStr, err := t.FormatQuantity(symbol, quantity)
	if err != nil {
		return nil, err
	}

	// 创建市价买入订单（平空）
	orderResp, err := t.client.Rest.Trade.ClosePosition(trade.ClosePosition{
		InstID:  symbol,
		MgnMode: "cross",
		PosSide: "short",
	})

	if err != nil || orderResp.Code != 0 {
		return nil, fmt.Errorf("平空仓失败: %w", err)
	}

	log.Printf("✓ 平空仓成功: %s 数量: %s", symbol, quantityStr)

	// 平仓后取消该币种的所有挂单（止损止盈单）
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("  ⚠ 取消挂单失败: %v", err)
	}

	result := make(map[string]interface{})
	//result["orderId"] = order.InstID
	result["symbol"] = symbol
	result["status"] = orderResp.Code
	return result, nil
}

// SetLeverage 设置杠杆（智能判断+冷却期）
func (t *OkxTrader) SetLeverage(symbol string, leverage int) error {
	// 先尝试获取当前杠杆（从持仓信息）
	currentLeverage := 0
	positions, err := t.GetPositions()
	if err == nil {
		for _, pos := range positions {
			if pos["symbol"] == symbol {
				if lev, ok := pos["leverage"].(float64); ok {
					currentLeverage = int(lev)
					break
				}
			}
		}
	}

	// 如果当前杠杆已经是目标杠杆，跳过
	if currentLeverage == leverage && currentLeverage > 0 {
		log.Printf("  ✓ %s 杠杆已是 %dx，无需切换", symbol, leverage)
		return nil
	}

	// 切换杠杆
	leverageResp, err := t.client.Rest.Account.SetLeverage(account2.SetLeverage{
		InstID:  symbol,
		MgnMode: "cross",
		Lever:   int64(leverage),
	})

	if err != nil || leverageResp.Code != 0 {
		return fmt.Errorf("设置杠杆失败: %w", err)
	}

	log.Printf("  ✓ %s 杠杆已切换为 %dx", symbol, leverage)

	// 切换杠杆后等待5秒（避免冷却期错误）
	log.Printf("  ⏱ 等待5秒冷却期...")
	time.Sleep(5 * time.Second)

	return nil
}

// SetMarginMode 设置仓位模式
func (t *OkxTrader) SetMarginMode(symbol string, isCrossMargin bool) error {
	log.Printf("  ✓ OKX 仓位模式默认设置为 %s", "cross")
	return nil
}

// GetMarketPrice 获取市场价格
func (t *OkxTrader) GetMarketPrice(symbol string) (float64, error) {
	prices, err := t.client.Rest.PublicData.GetInstruments(public.GetInstruments{
		InstType: "FUTURES",
		InstID:   symbol,
	})
	if err != nil || prices.Code != 0 {
		return 0, fmt.Errorf("获取价格失败: %w", err)
	}

	if len(prices.Instruments) == 0 {
		return 0, fmt.Errorf("未找到价格")
	}

	price := float64(prices.Instruments[0].CtVal)
	return price, nil
}

// SetStopLoss 设置止损单
func (t *OkxTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	var side okx.OrderSide
	var posSide okx.PositionSide

	if positionSide == "LONG" {
		side = "SELL"
		posSide = "long"
	} else {
		side = "buy"
		posSide = "short"
	}

	// 格式化数量
	quantityStr, err := t.FormatQuantity(symbol, quantity)
	if err != nil {
		return err
	}

	placeOrderResp, err := t.client.Rest.Trade.PlaceOrder(trade.PlaceOrder{
		InstID:  symbol,
		TdMode:  "cross",
		Side:    side,
		PosSide: posSide,
		OrdType: "market",
		Sz:      quantity,
		AttachAlgoOrds: []trade.AttachAlgoOrd{
			{
				SlTriggerPx:     fmt.Sprintf("%.8f", stopPrice),
				SlOrdPx:         fmt.Sprintf("%.8f", stopPrice),
				SlTriggerPxType: "last",
				Sz:              quantityStr,
			},
		},
	})

	if err != nil || placeOrderResp.Code != 0 {
		return fmt.Errorf("设置止损失败: %w", err)
	}

	log.Printf("  止损价设置: %.4f", stopPrice)
	return nil
}

// SetTakeProfit 设置止盈单
func (t *OkxTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	var side okx.OrderSide
	var posSide okx.PositionSide

	if positionSide == "LONG" {
		side = "sell"
		posSide = "long"
	} else {
		side = "buy"
		posSide = "short"
	}

	// 格式化数量
	quantityStr, err := t.FormatQuantity(symbol, quantity)
	if err != nil {
		return err
	}

	placeOrderResp, err := t.client.Rest.Trade.PlaceOrder(trade.PlaceOrder{
		InstID:  symbol,
		TdMode:  "cross",
		Side:    side,
		PosSide: posSide,
		OrdType: "market",
		Sz:      quantity,
		AttachAlgoOrds: []trade.AttachAlgoOrd{
			{
				TpTriggerPx:     fmt.Sprintf("%.8f", takeProfitPrice),
				TpOrdPx:         fmt.Sprintf("%.8f", takeProfitPrice),
				SlTriggerPxType: "last",
				Sz:              quantityStr,
			},
		},
	})

	if err != nil || placeOrderResp.Code != 0 {
		return fmt.Errorf("设置止盈失败: %w", err)
	}

	log.Printf("  止盈价设置: %.4f", takeProfitPrice)
	return nil
}

// GetPositions 获取所有持仓（带缓存）
func (t *OkxTrader) GetPositions() ([]map[string]interface{}, error) {
	// 先检查缓存是否有效
	t.positionsCacheMutex.RLock()
	if t.cachedPositions != nil && time.Since(t.positionsCacheTime) < t.cacheDuration {
		cacheAge := time.Since(t.positionsCacheTime)
		t.positionsCacheMutex.RUnlock()
		log.Printf("✓ 使用缓存的持仓信息（缓存时间: %.1f秒前）", cacheAge.Seconds())
		return t.cachedPositions, nil
	}
	t.positionsCacheMutex.RUnlock()

	// 缓存过期或不存在，调用API
	log.Printf("🔄 缓存过期，正在调用币安API获取持仓信息...")
	positionsResp, err := t.client.Rest.Account.GetPositions(account2.GetPositions{})
	if err != nil || positionsResp.Code != 0 {
		return nil, fmt.Errorf("获取持仓失败: %w", err)
	}

	positions := positionsResp.Positions

	var result []map[string]interface{}
	for _, pos := range positions {

		if pos.Pos == 0 {
			continue // 跳过无持仓的
		}

		posMap := make(map[string]interface{})
		posMap["symbol"] = pos.InstType
		posMap["positionAmt"] = pos.Pos
		posMap["entryPrice"] = pos.AvgPx
		posMap["markPrice"] = pos.MarkPx
		posMap["unRealizedProfit"] = pos.Upl
		posMap["leverage"] = pos.Lever
		posMap["liquidationPrice"] = pos.LiqPx

		// 判断方向
		posMap["side"] = pos.PosSide

		result = append(result, posMap)
	}

	// 更新缓存
	t.positionsCacheMutex.Lock()
	t.cachedPositions = result
	t.positionsCacheTime = time.Now()
	t.positionsCacheMutex.Unlock()

	return result, nil
}

// CancelAllOrders 取消该币种的所有挂单
func (t *OkxTrader) CancelAllOrders(symbol string) error {
	resp, err := t.client.Rest.Trade.GetOrderList(trade.OrderList{})
	if err != nil {
		return err
	}
	if len(resp.Orders) == 0 {
		return nil
	}
	cancelReq := []trade.CancelAlgoOrder{}
	for _, order := range resp.Orders {
		cancelReq = append(cancelReq, trade.CancelAlgoOrder{
			order.InstID,
			order.AlgoID,
		})
	}
	cancelResp, err := t.client.Rest.Trade.CancelAlgoOrder(cancelReq)

	if err != nil || cancelResp.Code != 0 {
		return fmt.Errorf("取消挂单失败: %w", err)
	}

	log.Printf("  ✓ 已取消 %s 的所有挂单", symbol)
	return nil
}

// FormatQuantity 格式化数量到正确的精度
func (t *OkxTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	precision, err := t.GetSymbolPrecision(symbol)
	if err != nil {
		// 如果获取失败，使用默认格式
		return fmt.Sprintf("%.3f", quantity), nil
	}

	format := fmt.Sprintf("%%.%df", precision)
	return fmt.Sprintf(format, quantity), nil
}

// GetSymbolPrecision 获取交易对的数量精度
func (t *OkxTrader) GetSymbolPrecision(symbol string) (int, error) {
	exchangeInfo, err := t.client.Rest.PublicData.GetInstruments(public.GetInstruments{
		InstID:   symbol,
		InstType: "FUTURES",
	})
	if err != nil {
		return 0, fmt.Errorf("获取交易规则失败: %w", err)
	}

	for _, s := range exchangeInfo.Instruments {
		if s.InstID == symbol {
			// 从LOT_SIZE filter获取精度
			stepSize := fmt.Sprintf("%f", s.LotSz)
			precision := calculatePrecision(stepSize)
			log.Printf("  %s 数量精度: %d (stepSize: %s)", symbol, precision, stepSize)
			return precision, nil
		}
	}

	log.Printf("  ⚠ %s 未找到精度信息，使用默认精度3", symbol)
	return 3, nil // 默认精度为3
}
