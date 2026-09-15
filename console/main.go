package main

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"
)

// Task 4 adds persisted deployment credentials here; console never reads account files.
func configFromEnv() (Config, string, error) {
	raw := os.Getenv("WB2A_CORE_URL")
	if raw == "" {
		raw = "http://core:7863"
	}
	target, err := url.Parse(raw)
	if err != nil {
		return Config{}, "", errors.New("WB2A_CORE_URL 无效")
	}
	listen := os.Getenv("WB2A_LISTEN")
	if listen == "" {
		listen = ":7863"
	}
	return Config{CoreURL: target, AdminKey: os.Getenv("WB2A_ADMIN_KEY"), APIKey: os.Getenv("WB2A_API_KEY"), BridgeKey: os.Getenv("WB2A_BRIDGE_KEY"), PublicOrigin: os.Getenv("WB2A_PUBLIC_ORIGIN")}, listen, nil
}
func main() {
	cfg, listen, err := configFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	h, err := NewServer(cfg)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: listen, Handler: h, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("console listening on %s", listen)
	log.Fatal(server.ListenAndServe())
}
