package server

import (
	"crypto/subtle"
	"net/http"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/0x10240/mihomo-proxy-pool/proxypool"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/metacubex/mihomo/adapter/inbound"
	"github.com/metacubex/mihomo/common/utils"
	"github.com/metacubex/mihomo/log"
	"github.com/sagernet/cors"
)

var (
	httpServer *http.Server
)

type Config struct {
	Addr                       string
	TLSAddr                    string
	UnixAddr                   string
	PipeAddr                   string
	Secret                     string
	Certificate                string
	PrivateKey                 string
	DohServer                  string
	IsDebug                    bool
	SubscriptionUpdateInterval time.Duration
	Cors                       Cors
}

type Cors struct {
	AllowOrigins        []string
	AllowPrivateNetwork bool
}

func (c Cors) Apply(r chi.Router) {
	r.Use(cors.New(cors.Options{
		AllowedOrigins:      c.AllowOrigins,
		AllowedMethods:      []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
		AllowedHeaders:      []string{"Content-Type", "Authorization"},
		AllowPrivateNetwork: c.AllowPrivateNetwork,
		MaxAge:              300,
	}).Handler)
}

func safeEuqal(a, b string) bool {
	aBuf := utils.ImmutableBytesFromString(a)
	bBuf := utils.ImmutableBytesFromString(b)
	return subtle.ConstantTimeCompare(aBuf, bBuf) == 1
}

func authentication(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			// Browser websocket not support custom header
			if r.Header.Get("Upgrade") == "websocket" && r.URL.Query().Get("token") != "" {
				token := r.URL.Query().Get("token")
				if !safeEuqal(token, secret) {
					render.Status(r, http.StatusUnauthorized)
					render.JSON(w, r, ErrUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			header := r.Header.Get("Authorization")
			bearer, token, found := strings.Cut(header, " ")

			hasInvalidHeader := bearer != "Bearer"
			hasInvalidSecret := !found || !safeEuqal(token, secret)
			if hasInvalidHeader || hasInvalidSecret {
				render.Status(r, http.StatusUnauthorized)
				render.JSON(w, r, ErrUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		}
		return http.HandlerFunc(fn)
	}
}

func router(isDebug bool, secret string, cors Cors) *chi.Mux {
	r := chi.NewRouter()
	cors.Apply(r)
	if isDebug {
		r.Mount("/debug", func() http.Handler {
			r := chi.NewRouter()
			r.Put("/gc", func(w http.ResponseWriter, r *http.Request) {
				debug.FreeOSMemory()
			})
			handler := middleware.Profiler
			r.Mount("/", handler())
			return r
		}())
	}

	r.Group(func(r chi.Router) {
		if secret != "" {
			r.Use(authentication(secret))
		}
		r.Get("/", hello)
		r.Get("/get", getRandomProxy)
		r.Get("/all", getAllProxy)
		r.Post("/add", addProxy)
		r.Delete("/del", delProxy)
		r.Route("/sub", func(r chi.Router) {
			r.Post("/add", addSubscription)
			r.Get("/all", getAllSubscription)
			r.Delete("/del", delSubscription)
		})
	})

	return r
}

func addProxy(w http.ResponseWriter, r *http.Request) {
	req := proxypool.AddProxyReq{}
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, ErrBadRequest)
		return
	}

	if err := proxypool.AddProxy(req); err != nil {
		render.Status(r, http.StatusServiceUnavailable)
		render.JSON(w, r, newError(err.Error()))
		return
	}

	render.Status(r, 200)
	render.JSON(w, r, map[string]bool{"success": true})
}

func addSubscription(w http.ResponseWriter, r *http.Request) {
	req := proxypool.AddSubscriptionReq{}
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, ErrBadRequest)
		return
	}

	if err := proxypool.AddSubscription(req); err != nil {
		render.Status(r, http.StatusServiceUnavailable)
		render.JSON(w, r, newError(err.Error()))
		return
	}

	render.Status(r, 200)
	render.JSON(w, r, map[string]bool{"success": true})
}

func getRandomProxy(w http.ResponseWriter, r *http.Request) {
	proxy, err := proxypool.GetRandomProxy()
	if err != nil {
		render.Status(r, http.StatusServiceUnavailable)
		render.JSON(w, r, newError(err.Error()))
		return
	}
	render.JSON(w, r, proxy)
}

func convertIpRiskScore(percentage string) int {
	// 去掉百分号
	trimmed := strings.TrimSuffix(percentage, "%")

	// 将字符串转换为整数
	intValue, err := strconv.Atoi(trimmed)
	if err != nil {
		return 100
	}

	return intValue
}

func getAllProxy(w http.ResponseWriter, r *http.Request) {
	proxies, err := proxypool.GetAllProxies()
	if err != nil {
		render.Status(r, http.StatusServiceUnavailable)
		render.JSON(w, r, newError("Failed to retrieve proxies: "+err.Error()))
		return
	}

	sortProxies(proxies, r.URL.Query().Get("sort"))

	resp := map[string]any{
		"count":   len(proxies),
		"proxies": proxies,
	}

	render.JSON(w, r, resp)
}

func getAllSubscription(w http.ResponseWriter, r *http.Request) {
	subscriptionMap, err := proxypool.GetSubscriptionsFromDb()
	if err != nil {
		render.Status(r, http.StatusServiceUnavailable)
		render.JSON(w, r, newError("Failed to retrieve subscriptions: "+err.Error()))
		return
	}

	subscriptions := make([]proxypool.SubscriptionResp, 0, len(subscriptionMap))
	for subName, sub := range subscriptionMap {
		subscriptions = append(subscriptions, proxypool.SubscriptionResp{SubName: subName, SubURL: sub.SubURL})
	}

	sort.Slice(subscriptions, func(i, j int) bool {
		return subscriptions[i].SubName < subscriptions[j].SubName
	})

	resp := map[string]any{
		"count":         len(subscriptions),
		"subscriptions": subscriptions,
	}

	render.JSON(w, r, resp)
}

func sortProxies(proxies []proxypool.ProxyResp, sortKey string) {
	switch sortKey {
	case "risk_score":
		sort.Slice(proxies, func(i, j int) bool {
			return convertIpRiskScore(proxies[i].IpRiskScore) < convertIpRiskScore(proxies[j].IpRiskScore)
		})
	case "delay":
		sort.Slice(proxies, func(i, j int) bool {
			return proxies[i].Delay < proxies[j].Delay
		})
	case "time":
		sort.Slice(proxies, func(i, j int) bool {
			return proxies[i].AddTime.Unix() < proxies[j].AddTime.Unix()
		})
	}
}

func delProxy(w http.ResponseWriter, r *http.Request) {
	req := proxypool.DelProxyReq{}
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, ErrBadRequest)
		return
	}

	if req.Name == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, newError("name is required"))
		return
	}

	if err := proxypool.DeleteProxyByName(req.Name); err != nil {
		render.Status(r, http.StatusServiceUnavailable)
		render.JSON(w, r, newError(err.Error()))
		return
	}

	render.Status(r, 200)
	render.JSON(w, r, map[string]bool{"success": true})
}

func delSubscription(w http.ResponseWriter, r *http.Request) {
	req := proxypool.DelSubscriptionReq{}
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, ErrBadRequest)
		return
	}

	if req.SubName == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, newError("sub_name is required"))
		return
	}

	if err := proxypool.DeleteSubscription(req); err != nil {
		render.Status(r, http.StatusServiceUnavailable)
		render.JSON(w, r, newError("Failed to delete subscription: "+err.Error()))
		return
	}

	render.Status(r, 200)
	render.JSON(w, r, map[string]bool{"success": true})
}

func hello(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, render.M{"hello": "mihomo proxy pool"})
}

func Start(cfg *Config) {
	// first stop existing server
	if httpServer != nil {
		_ = httpServer.Close()
		httpServer = nil
	}

	// handle addr
	if len(cfg.Addr) > 0 {
		l, err := inbound.Listen("tcp", cfg.Addr)
		if err != nil {
			log.Errorln("API serve listen error: %s", err)
			return
		}
		log.Infoln("RESTful API listening at: %s", l.Addr().String())

		server := &http.Server{
			Handler: router(cfg.IsDebug, cfg.Secret, cfg.Cors),
		}
		httpServer = server
		if err = server.Serve(l); err != nil {
			log.Errorln("API serve error: %s", err)
		}
	}
}
