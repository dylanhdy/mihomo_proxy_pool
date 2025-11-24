package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/0x10240/mihomo-proxy-pool/healthcheck"
	"github.com/0x10240/mihomo-proxy-pool/proxypool"
	"github.com/0x10240/mihomo-proxy-pool/server"
	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetFormatter(&logrus.TextFormatter{
		DisableColors: true,
		CallerPrettyfier: func(f *runtime.Frame) (string, string) {
			return "", fmt.Sprintf("%s:%d", f.File, f.Line)
		},
	})
	logrus.SetReportCaller(true)

	if err := proxypool.InitProxyPool(); err != nil {
		logrus.Errorf("init proxy pool failed: %v", err)
		os.Exit(1)
	}
	cfg := server.Config{
		Addr:    "0.0.0.0:9999",
		IsDebug: true,
		Cors: server.Cors{
			AllowOrigins:        []string{},
			AllowPrivateNetwork: true,
		},
	}

	go healthcheck.StartHealthCheckScheduler()

	server.Start(&cfg)
}
