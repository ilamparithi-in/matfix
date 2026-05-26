package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ilamparithi-in/matfix/internal/account"
	"github.com/ilamparithi-in/matfix/internal/admin"
	"github.com/ilamparithi-in/matfix/internal/api"
	"github.com/ilamparithi-in/matfix/internal/bus"
	"github.com/ilamparithi-in/matfix/internal/config"
	"github.com/ilamparithi-in/matfix/internal/correlation"
	"github.com/ilamparithi-in/matfix/internal/engine"
	"github.com/ilamparithi-in/matfix/internal/observability"
	"github.com/ilamparithi-in/matfix/internal/persistence"
	"github.com/ilamparithi-in/matfix/internal/queue"
	"github.com/ilamparithi-in/matfix/internal/submission"
	"github.com/ilamparithi-in/matfix/internal/version"
	"github.com/ilamparithi-in/matfix/internal/worker"
)

func main() {
	// # Flags

	configFile := flag.String("config", "matfix.yaml", "path to YAML config file")
	logLevel := flag.String("log-level", "", "override log level (debug|info|warn|error)")
	showVersion := flag.Bool("version", false, "print build version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("matfix %s\n", version.Full())
		return
	}

	// # Config

	cfg, err := config.Load(*configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "matfix: load config: %v\n", err)
		os.Exit(1)
	}
	if *logLevel != "" {
		cfg.Logging.Level = *logLevel
	}
	if err := config.Validate(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "matfix: invalid config: %v\n", err)
		os.Exit(1)
	}

	// # Observability - set up logging first so all subsequent output is structured

	observability.Setup(cfg.Logging)

	// # Persistence

	db, err := persistence.Open(cfg.Database)
	if err != nil {
		slog.Error("matfix: open database", "error", err)
		os.Exit(1)
	}
	if err := persistence.Migrate(db.SQL()); err != nil {
		slog.Error("matfix: run migrations", "error", err)
		os.Exit(1)
	}
	stores := db.Stores()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// # Queue

	queueMgr := queue.New(queue.Config{
		Store:  stores.Queue,
		Policy: cfg.RetryPolicy,
	})
	if err := queueMgr.RecoverOnStartup(ctx); err != nil {
		slog.Error("matfix: queue crash recovery failed", "error", err)
		os.Exit(1)
	}

	// # Event Bus

	eventBus := bus.NewLocalBus()

	// # Accounts

	accounts := account.New(*cfg, db, eventBus)
	if err := accounts.StartAll(ctx); err != nil {
		slog.Error("matfix: all accounts failed to start", "error", err)
		os.Exit(1)
	}

	// clientLookup satisfies both worker.ClientLookup and submission.ClientLookup.
	clientLookup := func(accountID string) (*engine.Client, bool) {
		actx := accounts.Get(accountID)
		if actx == nil || !actx.IsAvailable() {
			return nil, false
		}
		return actx.Client(), true
	}

	// # Worker Pool

	accountIDs := make([]string, 0, len(cfg.Accounts))
	for _, a := range cfg.Accounts {
		accountIDs = append(accountIDs, a.ID)
	}
	workers := worker.New(worker.Config{
		Accounts:    accountIDs,
		Manager:     queueMgr,
		Clients:     worker.ClientLookup(clientLookup),
		Bus:         eventBus,
		Concurrency: cfg.Queue.Concurrency,
	})
	workers.Start(ctx)

	// # Submission

	submissionMgr := submission.New(submission.Config{
		Accounts: cfg.Accounts,
		Queue:    queueMgr,
		Clients:  submission.ClientLookup(clientLookup),
		Store:    stores.Queue,
	})

	// # Correlation

	correlationMgr := correlation.New(correlation.Config{
		Bus:   eventBus,
		Store: stores.Correlation,
	})
	go correlationMgr.Start(ctx)

	// # Admin Socket Server

	adminSrv := admin.New(admin.Config{
		SocketPath:  cfg.Admin.SocketPath,
		Accounts:    accounts,
		APIKeyStore: stores.APIKey,
		QueueStore:  stores.Queue,
		CorrStore:   stores.Correlation,
	})
	go func() {
		if err := adminSrv.Start(); err != nil {
			slog.Error("matfix: admin server error", "error", err)
		}
	}()

	// # HTTP API Server

	apiSrv := api.New(api.Config{
		BindAddr:    cfg.Server.BindAddr,
		Submission:  submissionMgr,
		Correlation: correlationMgr,
		Accounts:    accounts,
		APIKeyStore: stores.APIKey,
		QueueStore:  stores.Queue,
		CorrStore:   stores.Correlation,
	})
	go func() {
		if err := apiSrv.Start(); err != nil {
			slog.Error("matfix: api server error", "error", err)
		}
	}()

	slog.Info("matfix: startup complete")

	// # Signal handling

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	slog.Info("matfix: shutting down", "signal", sig.String())

	shutdown(apiSrv, adminSrv, workers, accounts, db)
}
