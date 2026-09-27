package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kardianos/service"

	"skilltree/internal/app"
	"skilltree/internal/config"
)

type program struct {
	cfg config.Config
	log *slog.Logger
	a   *app.App
}

func (p *program) Start(s service.Service) error {
	p.a = app.New(p.cfg, p.log)
	go func() {
		if err := p.a.Run(); err != nil {
			p.log.Error("application stopped", "err", err)
			os.Exit(1)
		}
	}()
	return nil
}

func (p *program) Stop(s service.Service) error {
	if p.a != nil {
		p.a.Stop()
	}
	return nil
}

func main() {
	ctrl := flag.String("service", "", "Windows/Linux service control: install|uninstall|start|stop")
	flag.Parse()

	if err := config.LoadDotEnv(".env"); err != nil {
		fmt.Fprintf(os.Stderr, "load .env: %v\n", err)
		os.Exit(1)
	}

	cfg := config.Load()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	svcConfig := &service.Config{
		Name:        cfg.ServiceName,
		DisplayName: cfg.DisplayName,
		Description: "Skilltree – Wissens- und Skillverwaltung",
		Option: service.KeyValue{
			// On Windows, run recovery / delayed start can be set later.
		},
	}

	prg := &program{cfg: cfg, log: log}
	svc, err := service.New(prg, svcConfig)
	if err != nil {
		log.Error("create service", "err", err)
		os.Exit(1)
	}

	if *ctrl != "" {
		if err := service.Control(svc, *ctrl); err != nil {
			fmt.Fprintf(os.Stderr, "service %s: %v\n", *ctrl, err)
			os.Exit(1)
		}
		fmt.Printf("service %s: ok\n", *ctrl)
		return
	}

	// Interactive / console and service modes.
	if service.Interactive() {
		a := app.New(cfg, log)
		go func() {
			ch := make(chan os.Signal, 1)
			signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
			<-ch
			a.Stop()
		}()
		if err := a.Run(); err != nil {
			log.Error("run failed", "err", err)
			os.Exit(1)
		}
		return
	}

	if err := svc.Run(); err != nil {
		log.Error("service run", "err", err)
		os.Exit(1)
	}
}
