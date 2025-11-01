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
func NewOkxTrader(apiKey, secretKey, passphrase string) *OkxTrader {
	client, err := api.NewClient(context.Background(), apiKey, secretKey, passphrase, okx.NormalServer)
	if err != nil {
		log.Fatal("获取 OKX 链接失败")
	}
	return &OkxTrader{
		client:        client,
		cacheDuration: 15 * time.Second, // 15秒缓存
	}
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
