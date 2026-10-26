package main

import (
	"dasslm/server/internal/config"
	"dasslm/server/internal/database"
	"dasslm/server/internal/router"
	"fmt"
	"log"
	"os"
)

// version 当前版本号，发布时可通过 -ldflags "-X main.version=x.y.z" 覆盖
var version = "0.0.1"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println("dasslm-server", version)
		return
	}
	log.Printf("DaSSLm v%s starting", version)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	db, err := database.Connect(cfg.Database)
	if err != nil {
		log.Fatalf("connect database failed: %v", err)
	}

	if err := database.AutoMigrate(db); err != nil {
		log.Fatalf("migrate database failed: %v", err)
	}

	if err := database.EnsureInitialInstall(db, cfg.InstallLockPath); err != nil {
		log.Fatalf("ensure initial install failed: %v", err)
	}

	r := router.New(cfg, db)
	if err := r.Run(cfg.Server.Addr()); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
