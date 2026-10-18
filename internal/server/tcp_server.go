package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

type ConnHandler interface {
	Handle(ctx context.Context, conn net.Conn)
}

type Server struct {
	port           int
	maxConnections int
	connHandler    ConnHandler
}

func New(port, maxConnections int, connHandler ConnHandler) *Server {
	return &Server{
		port:           port,
		maxConnections: maxConnections,
		connHandler:    connHandler,
	}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return err
	}

	return s.Serve(ctx, ln)
}

func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	defer func() { _ = ln.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = ln.Close() })
	defer stop()

	var wg sync.WaitGroup
	sem := make(chan struct{}, s.maxConnections)

	log.Printf("Listening on %s", ln.Addr())

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				break
			}
			log.Println("Error accepting connection:", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		select {
		case sem <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		wg.Go(func() {
			log.Printf("Accepted new connection from %s", conn.RemoteAddr())
			defer func() {
				<-sem
			}()
			s.connHandler.Handle(ctx, conn)
		})
	}

	wg.Wait()

	return nil
}
