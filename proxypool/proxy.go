package proxypool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/0x10240/mihomo-proxy-pool/db"
	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/adapter/inbound"
	"github.com/metacubex/mihomo/common/convert"
	"github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/tunnel"
	logger "github.com/sirupsen/logrus"
)

type CProxy = constant.Proxy

var proxyPoolStartPort = 40001
var allowIps = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
var localPortMaps = make(map[int]string, 0)
var cproxies = make(map[string]CProxy, 0)
var listeners = make(map[string]CListener, 0)

var dbClient *db.RedisClient
var mu = sync.Mutex{}

const subscriptionKeyPrefix = "sub:"
const proxyKeyPrefix = "proxy:"

func getSubscriptionKey(subName string) string {
	return subscriptionKeyPrefix + subName
}

func getProxyKey(name string) string {
	return proxyKeyPrefix + name
}

type AddProxyReq struct {
	Link        string         `json:"link"`   // 链接
	Config      map[string]any `json:"config"` // 配置，json信息
	SubName     string         `json:"sub_name,omitempty"`
	ForceUpdate bool           `json:"update"`
}

type DelProxyReq struct {
	Name string `json:"name"`
}

type AddSubscriptionReq struct {
	SubUrl      string `json:"sub"` // 订阅链接
	SubName     string `json:"sub_name"`
	ForceUpdate bool   `json:"update"`
}

type DelSubscriptionReq struct {
	SubName string `json:"sub_name"`
}

type SubscriptionResp struct {
	SubName string `json:"sub_name"`
	SubUrl  string `json:"sub_url"`
}

type Subscription struct {
	SubName string `json:"sub_name"`
	SubUrl  string `json:"sub_url"`
}

type Proxy struct {
	Config        map[string]any `json:"config"`
	Name          string         `json:"name"`
	LocalPort     int            `json:"local_port"`
	OutboundIp    string         `json:"ip"`
	Region        string         `json:"region"`
	IpType        string         `json:"ip_type"`
	IpRiskScore   string         `json:"ip_risk_score"`
	FailCount     int            `json:"fail"`
	SuccessCount  int            `json:"success"`
	LastCheckTime int64          `json:"last_check_time"`
	AddTime       int64          `json:"add_time"`
	Delay         int            `json:"delay"`
	SubName       string         `json:"sub"`
}

type ProxyResp struct {
	Name          string    `json:"name"`
	Server        string    `json:"server"`
	ServerPort    int       `json:"server_port"`
	AddTime       time.Time `json:"add_time"`
	LocalPort     int       `json:"local_port"`
	Success       int       `json:"success"`
	Fail          int       `json:"fail"`
	Delay         int       `json:"delay"`
	Ip            string    `json:"ip"`
	IpType        string    `json:"ip_type"`
	Region        string    `json:"region"`
	IpRiskScore   string    `json:"ip_risk_score"`
	LastCheckTime time.Time `json:"last_check_time"`
	AliveTime     string    `json:"alive_time"`
	SubName       string    `json:"sub"`
}

// CalculateAliveTime calculates the time difference in "XdXhXm" format
func CalculateAliveTime(addTime int64) string {
	duration := time.Since(ConvertTimestampToTime(addTime))

	days := duration / (24 * time.Hour)
	hours := (duration % (24 * time.Hour)) / time.Hour
	minutes := (duration % time.Hour) / time.Minute

	return fmt.Sprintf("%dd%dh%dm", days, hours, minutes)
}

func ConvertTimestampToTime(timestamp int64) time.Time {
	return time.Unix(timestamp, 0)
}

func (p Proxy) ToResp() ProxyResp {
	resp := ProxyResp{
		Name:          p.Name,
		LocalPort:     p.LocalPort,
		Server:        p.Config["server"].(string),
		ServerPort:    int(p.Config["port"].(float64)),
		Ip:            p.OutboundIp,
		IpType:        p.IpType,
		IpRiskScore:   p.IpRiskScore,
		Region:        p.Region,
		LastCheckTime: ConvertTimestampToTime(p.LastCheckTime),
		AddTime:       ConvertTimestampToTime(p.AddTime),
		AliveTime:     CalculateAliveTime(p.AddTime),
		Success:       p.SuccessCount,
		Fail:          p.FailCount,
		Delay:         p.Delay,
		SubName:       p.SubName,
	}

	return resp
}

func GetProxyTransport(proxy CProxy) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, portStr, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			port, err := strconv.ParseUint(portStr, 10, 16)
			if err != nil {
				return nil, err
			}
			return proxy.DialContext(ctx, &constant.Metadata{
				Host:    host,
				DstPort: uint16(port),
			})
		},
	}
}

func GetProxiesFromDb() (map[string]Proxy, error) {
	resp, err := dbClient.GetAllByPrefix(proxyKeyPrefix)
	if err != nil {
		return map[string]Proxy{}, err
	}

	ret := make(map[string]Proxy, 0)

	for _, value := range resp {
		//if !strings.Contains(k, "103.114.163.93") {
		//	continue
		//}
		proxy := Proxy{}
		if err = json.Unmarshal([]byte(value), &proxy); err != nil {
			logger.Infof("unmarshal proxy: %v from db failed", value)
			continue
		}
		ret[proxy.Name] = proxy
	}

	return ret, nil
}

func GetSubscriptionsFromDb() (map[string]Subscription, error) {
	resp, err := dbClient.GetAllByPrefix(subscriptionKeyPrefix)
	if err != nil {
		return map[string]Subscription{}, err
	}

	ret := make(map[string]Subscription)
	for key, value := range resp {
		sub := Subscription{}
		if err := json.Unmarshal([]byte(value), &sub); err != nil {
			logger.Infof("unmarshal subscription: %v from db failed", value)
			continue
		}
		if sub.SubName == "" {
			sub.SubName = strings.TrimPrefix(key, subscriptionKeyPrefix)
		}
		ret[sub.SubName] = sub
	}

	return ret, nil
}

func AddSubscription(req AddSubscriptionReq) error {
	req.SubName = strings.TrimSpace(req.SubName)
	req.SubUrl = strings.TrimSpace(req.SubUrl)

	if req.SubName == "" {
		return fmt.Errorf("sub_name is required")
	}
	if req.SubUrl == "" {
		return fmt.Errorf("sub url is required")
	}

	if err := syncSubscriptionProxies(req); err != nil {
		return err
	}

	key := getSubscriptionKey(req.SubName)
	sub := Subscription{
		SubName: req.SubName,
		SubUrl:  req.SubUrl,
	}
	return dbClient.Put(key, sub)
}

func DeleteProxy(proxy Proxy) error {
	mu.Lock()
	defer mu.Unlock()

	key := getProxyKey(proxy.Name)

	if err := dbClient.Delete(key); err != nil {
		logger.Errorf("delete proxy %s failed, err: %v", key, err)
		return err
	}

	listenerKey := getListenerKey(proxy.LocalPort)

	delete(cproxies, proxy.Name)
	delete(listeners, listenerKey)
	delete(localPortMaps, proxy.LocalPort)

	tunnel.UpdateProxies(cproxies, nil)
	startListen(listeners, true)

	return nil
}

func UpdateProxyDB(proxy *Proxy) error {
	mu.Lock()
	defer mu.Unlock()

	name := proxy.Name
	proxy.Name = name
	proxy.LastCheckTime = time.Now().Unix()
	key := getProxyKey(name)

	if err := dbClient.Put(key, proxy); err != nil {
		logger.Errorf("update proxy failed: %v", err)
		return err
	}

	return nil
}

func getListenerKey(localPort int) string {
	return fmt.Sprintf("in_%d", localPort)
}

func InitProxyPool() error {
	var err error
	dbClient, err = db.NewRedisClientFromURL("mihomo_proxy_pool", "redis://:@127.0.0.1:6379/0")
	if err != nil {
		return err
	}

	values, err := dbClient.GetAllByPrefix(proxyKeyPrefix)
	if err != nil {
		return err
	}

	for _, value := range values {
		proxy := Proxy{}

		if err = json.Unmarshal([]byte(value), &proxy); err != nil {
			continue
		}

		cproxy, err := adapter.ParseProxy(proxy.Config)
		if err != nil {
			continue
		}

		proxyName := cproxy.Name()
		cproxies[proxyName] = cproxy

		listener, err := getListenerByLocalPort(proxy.LocalPort, proxyName)
		if err != nil {
			continue
		}

		logger.Infof("%s listen at %v", proxyName, proxy.LocalPort)

		listenerKey := getListenerKey(proxy.LocalPort)
		listeners[listenerKey] = listener

		localPortMaps[proxy.LocalPort] = proxyName
	}

	inbound.SetAllowedIPs(allowIps)
	tunnel.UpdateProxies(cproxies, nil)
	startListen(listeners, true)
	tunnel.OnRunning()

	return nil
}

func parseProxyLink(link string) (map[string]any, error) {
	ret := map[string]any{}

	cfgs, err := convert.ConvertsV2Ray([]byte(link))
	if err != nil {
		return ret, err
	}

	if len(cfgs) != 1 {
		return ret, errors.New("invalid proxy link")
	}

	return ret, nil
}

func getLocalPort() int {
	for p := proxyPoolStartPort; p <= 65535; p++ {
		if _, ok := localPortMaps[p]; !ok {
			return p
		}
	}
	return rand.Intn(65535)
}

func addMihomoProxy(proxyCfg map[string]any, proxyName string, localPort int) error {
	cproxy, err := adapter.ParseProxy(proxyCfg)
	if err != nil {
		return err
	}

	cproxies[proxyName] = cproxy
	tunnel.UpdateProxies(cproxies, nil)

	listener, err := getListenerByLocalPort(localPort, proxyName)
	if err != nil {
		return err
	}

	listenerKey := getListenerKey(localPort)
	listeners[listenerKey] = listener

	startListen(listeners, true)
	return nil
}

func GetRandomProxy() (ProxyResp, error) {
	proxies, err := GetProxiesFromDb()
	if err != nil {
		return ProxyResp{}, err
	}
	if len(proxies) == 0 {
		return ProxyResp{}, fmt.Errorf("no proxy available")
	}

	index := rand.Intn(len(proxies))
	i := 0
	for _, proxy := range proxies {
		if i == index {
			return proxy.ToResp(), nil
		}
		i++
	}

	return ProxyResp{}, fmt.Errorf("no proxy available")
}

func GetAllProxies() ([]ProxyResp, error) {
	proxies, err := GetProxiesFromDb()
	if err != nil {
		return []ProxyResp{}, err
	}

	ret := []ProxyResp{}
	for _, proxy := range proxies {
		ret = append(ret, proxy.ToResp())
	}

	return ret, nil
}

func AddProxy(req AddProxyReq) error {
	var cfg map[string]any
	var err error

	if req.Link != "" {
		if cfg, err = parseProxyLink(req.Link); err != nil {
			return err
		}
	}

	if len(req.Config) > 0 {
		cfg = req.Config
	}

	name := fmt.Sprintf("%v:%v", cfg["server"], cfg["port"])
	key := getProxyKey(name)

	if !req.ForceUpdate && dbClient.Exists(key) {
		logger.Infof("key: %s exists", key)
		return nil
	}

	cfg["name"] = name
	localPort := getLocalPort()
	proxy := Proxy{
		Config:    cfg,
		AddTime:   time.Now().Unix(),
		LocalPort: localPort,
		Name:      name,
		SubName:   req.SubName,
	}

	if err = addMihomoProxy(cfg, name, localPort); err != nil {
		return err
	}

	localPortMaps[localPort] = name
	return dbClient.Put(key, proxy)
}

func DeleteProxyByName(name string) error {
	proxies, err := GetProxiesFromDb()
	if err != nil {
		return err
	}

	proxy, ok := proxies[name]
	if !ok {
		return fmt.Errorf("proxy %s not found", name)
	}

	return DeleteProxy(proxy)
}

func DeleteSubscription(req DelSubscriptionReq) error {
	proxies, err := GetProxiesFromDb()
	if err != nil {
		return err
	}

	for _, proxy := range proxies {
		if proxy.SubName != req.SubName {
			continue
		}
		if err := DeleteProxy(proxy); err != nil {
			logger.Warnf("Delete proxy failed: %v", err)
		}
	}

	if err := dbClient.Delete(getSubscriptionKey(req.SubName)); err != nil {
		return err
	}

	return nil
}
