package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gophertribe/megaphone/input/sip"
	"github.com/gophertribe/megaphone/media"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/spf13/viper"
)

const defaultConfigPath string = "/etc/megaphone"

const (
	configLogLevel = "log.level"
	configAddr     = "addr"
)

type App struct {
	hs       *http.Server
	wg       sync.WaitGroup
	log      *os.File
	queue    *media.Queue
	endpoint *sip.Endpoint
}

func NewApp() *App {
	return &App{}
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	app := NewApp()
	now := time.Now()

	var configDir string
	flag.StringVar(&configDir, "config", defaultConfigPath, "configuration dir path")
	flag.Parse()

	// app sets up components and runtime context
	ctx, err := app.setup(ctx, configDir)
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Printf("error during setup: %s\n", err.Error())
		cancel()
		os.Exit(1)
	}
	slog.Info("setup completed", "uptime", time.Since(now))

	err = app.run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Printf("error during runtime: %s\n", err.Error())
	}
	slog.Info("runtime exit", "uptime", time.Since(now))

	cancel()
	err = app.shutdown(context.WithTimeout(context.Background(), 10*time.Second))
	if err != nil {
		fmt.Printf("error during shutdown: %s\n", err.Error())
		os.Exit(1)
	}
	slog.Info("shutdown completed", "uptime", time.Since(now))
}

func (app *App) setup(ctx context.Context, configDir string) (context.Context, error) {

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(configDir)

	viper.SetDefault(configLogLevel, int(log.InfoLevel))
	viper.SetDefault(configAddr, ":8080")

	err := viper.ReadInConfig()
	if err != nil {
		var notFoundErr viper.ConfigFileNotFoundError
		if !errors.As(err, &notFoundErr) {
			return ctx, fmt.Errorf("error in config file: %w", err)
		}
	}

	logger := log.New(os.Stdout)
	logger.SetLevel(log.Level(viper.GetInt(configLogLevel)))
	slog.SetDefault(slog.New(logger))

	app.queue = media.NewQueue()
	app.endpoint, err = sip.NewEndpoint("", app.queue)
	if err != nil {
		return ctx, fmt.Errorf("could not initialize sip endpoint")
	}

	r := chi.NewRouter()
	//r.Use(middleware.RequestID)
	//r.Use(middleware.RealIP)
	//r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	app.hs = &http.Server{
		Addr: viper.GetString(configAddr),
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
		Handler:        r,
		ReadTimeout:    360 * time.Second,
		WriteTimeout:   360 * time.Second,
		MaxHeaderBytes: 1 << 20,
		ErrorLog:       logger.StandardLog(log.StandardLogOptions{ForceLevel: log.ErrorLevel}),
	}

	return ctx, ctx.Err()

}

func (app *App) run(ctx context.Context) error {
	app.wg.Add(1)
	go func() {
		defer app.wg.Done()
		slog.Info("starting backend api server", "addr", app.hs.Addr)
		err := app.hs.ListenAndServe()
		if err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				slog.Warn("api server error", "err", err)
			}
		}
	}()

	<-ctx.Done()
	return ctx.Err()
}

func (app *App) shutdown(ctx context.Context, cancel context.CancelFunc) error {
	go func() {
		if app.hs != nil {
			err := app.hs.Close()
			if err != nil {
				slog.Warn("could not close api server", "err", err)
			}
		}
		if app.log != nil {
			err := app.log.Close()
			if err != nil {
				slog.Warn("could not close log file", "err", err)
			}
		}
		app.wg.Wait()
		cancel()
	}()

	// this context will cancel either when the shutdown procedure is over or when the timeout expires
	<-ctx.Done()
	// canceled context is fine here
	if errors.Is(ctx.Err(), context.Canceled) {
		return nil
	}
	return ctx.Err()
}
