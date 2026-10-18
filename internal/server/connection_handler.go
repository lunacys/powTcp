package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"powTcp/internal/pow"
	"strconv"
	"strings"
	"time"
)

type QuoteProvider interface {
	GetRandomQuote(ctx context.Context) (string, error)
}

type ReplayGuard interface {
	TryAdd(id string, expires time.Time) bool
}

type ConnectionHandlerConfig struct {
	Difficulty int
	TTL        time.Duration
}

func DefaultConnHandlerConfig() ConnectionHandlerConfig {
	return ConnectionHandlerConfig{
		Difficulty: 20,
		TTL:        time.Second * 30,
	}
}

type ConnectionHandler struct {
	secret []byte
	guard  ReplayGuard
	qc     QuoteProvider
	config ConnectionHandlerConfig
}

func NewConnectionHandler(secret []byte, cache ReplayGuard, qc QuoteProvider, config ConnectionHandlerConfig) *ConnectionHandler {
	return &ConnectionHandler{
		secret: secret,
		guard:  cache,
		qc:     qc,
		config: config,
	}
}

func (h *ConnectionHandler) Handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	defer func() {
		log.Printf("Closing connection from %s", conn.RemoteAddr())
		if r := recover(); r != nil {
			log.Printf("panic is handled: %v", r)
		}
	}()

	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	reader := bufio.NewReaderSize(conn, 1024)

	clientIP, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return
	}

	token, err := pow.GenerateToken(h.config.Difficulty, clientIP, h.config.TTL, time.Now, h.secret)
	if err != nil {
		return
	}

	reply(conn, "CHALLENGE "+token)

	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	line, err := reader.ReadSlice('\n')
	if err != nil {
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			_, _ = fmt.Fprint(conn, "ERR line_too_long\n")
		case errors.Is(err, os.ErrDeadlineExceeded):
			log.Printf("Read timeout")
		case errors.Is(err, io.EOF), errors.Is(err, net.ErrClosed):
		default:
			log.Println("Error reading from connection:", err)
		}
		return
	}

	fields := strings.Fields(string(line))
	if len(fields) != 3 || fields[0] != "SOLUTION" {
		reply(conn, "ERR bad_request")
		return
	}

	counter, err := strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		reply(conn, "ERR bad_request")
		return
	}

	chal, err := pow.VerifyToken(fields[1], clientIP, h.secret, time.Now)
	switch {
	case errors.Is(err, pow.ErrExpired):
		reply(conn, "ERR expired")
		return
	case err != nil:
		reply(conn, "ERR bad_request")
		return
	}

	if !pow.Check(fields[1], counter, chal.Difficulty) {
		reply(conn, "ERR invalid_pow")
		return
	}

	if !h.guard.TryAdd(chal.ID, chal.Expires) {
		reply(conn, "ERR replay")
		return
	}

	quote, err := h.qc.GetRandomQuote(ctx)
	if err != nil {
		log.Printf("quote fetch failed: %v", err)
		reply(conn, "ERR unavailable")
		return
	}

	reply(conn, "QUOTE "+quote)
}

func reply(conn net.Conn, msg string) {
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintf(conn, "%s\n", msg); err != nil {
		return
	}
}
