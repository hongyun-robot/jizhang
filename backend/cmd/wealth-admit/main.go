package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"jizhang/internal/config"
	"jizhang/internal/database"
	"jizhang/internal/logger"
	"jizhang/internal/service"
)

func main() {
	code := flag.String("code", "", "integrated share code")
	status := flag.String("status", "", "pending_validation, enabled or retired")
	evidence := flag.String("evidence", "", "completed real-time qualification state.json, required for enabled")
	flag.Parse()
	var raw []byte
	var err error
	if *evidence != "" {
		raw, err = os.ReadFile(*evidence)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cannot read admission evidence")
			os.Exit(1)
		}
	}
	if _, err = os.Stat(".env"); err == nil {
		_ = godotenv.Load(".env")
	} else {
		_ = godotenv.Load("../.env")
	}
	logger.Init()
	defer logger.Sync()
	svc := service.NewWealthService(database.Init(config.Load()), nil, nil)
	if err = svc.SetAdmission(*code, *status, raw); err != nil {
		fmt.Fprintln(os.Stderr, "admission refused:", err)
		os.Exit(1)
	}
	fmt.Println("wealth product admission status saved")
}
