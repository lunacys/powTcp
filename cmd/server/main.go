package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"powTcp/internal/pow"
	"powTcp/internal/quotes"
	"powTcp/internal/server"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
)

const port = 8090
const maxConnections = 16
const quoteURL = "https://zenquotes.io/api/random"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cache := pow.NewReplayCache(time.Now)
	qc := quotes.NewClient(quoteURL, time.Second*3)
	secret, err := pow.GenerateTestSecret()
	if err != nil {
		log.Fatal(err)
	}

	connHandler := server.NewConnectionHandler(secret, cache, qc, server.DefaultConnHandlerConfig())
	powServer := server.New(port, maxConnections, connHandler)

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { cache.Run(ctx, time.Second*10); return nil })
	g.Go(func() error { return powServer.ListenAndServe(ctx) })
	if err := g.Wait(); err != nil {
		log.Fatal(err)
	}
}
